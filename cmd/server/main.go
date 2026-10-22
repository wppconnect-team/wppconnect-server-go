// Command server starts the Go port of wppconnect-server, backed by whatsmeow.
// It keeps the HTTP contract (routes + payloads) compatible with the Node
// server so existing clients can migrate with minimal changes.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/httpapi"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"github.com/wppconnect-team/wppconnect-server-go/internal/webhook"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "21465"
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = run(ctx, cfg); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	options := webhook.DefaultOptions()
	options.Workers, options.QueueSize, options.Attempts, options.Timeout = cfg.WebhookWorkers, cfg.WebhookQueueSize, cfg.WebhookAttempts, cfg.WebhookTimeout
	sink, err := webhook.NewWithOptions(cfg.WebhookURL, options)
	if err != nil {
		return err
	}
	mgr, err := session.NewManager(context.Background(), cfg.DataDir, sink)
	if err != nil {
		sink.Close(context.Background())
		return err
	}
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		mgr.Shutdown(context.Background())
		sink.Close(context.Background())
		return err
	}
	restoreCtx, cancelRestore := context.WithCancel(ctx)
	defer cancelRestore()
	if cfg.AutoRestore {
		done := mgr.RestoreAsync(restoreCtx)
		go func() {
			result := <-done
			log.Printf("session restoration completed: started=%d error=%v", result.Count, result.Err)
		}()
	}
	server := &http.Server{Handler: httpapi.NewRouter(cfg, mgr), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Printf("wppconnect-server-go listening on :%s", cfg.Port)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-served:
	}
	cancelRestore()
	deadline, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	// Finish accepted HTTP operations before disconnecting their WhatsApp clients.
	httpErr := server.Shutdown(deadline)
	if httpErr != nil {
		server.Close()
	}
	sessionErr := mgr.Shutdown(deadline)
	webhookErr := sink.Close(deadline)
	log.Printf("shutdown complete: webhook=%+v", sink.Stats())
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, httpErr, sessionErr, webhookErr)
}
