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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/eventbus"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/httpapi"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"github.com/wppconnect-team/wppconnect-server-go/internal/vault"
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
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return err
	}
	eventLog, err := eventbus.Open(filepath.Join(cfg.DataDir, ".events.db"))
	if err != nil {
		return err
	}
	eventLog.Limit, eventLog.MaxBytes, eventLog.Retention = cfg.OutboxMaxEvents, cfg.OutboxMaxBytes, cfg.OutboxRetention
	targets := []eventbus.Target{}
	if cfg.WebhookURL != "" {
		target, e := eventbus.NewHTTP(cfg.WebhookURL)
		if e != nil {
			eventLog.Close()
			return e
		}
		targets = append(targets, target)
	}
	if cfg.NATSURL != "" {
		if e := eventbus.ValidateBrokerURL(cfg.NATSURL, "nats", "tls"); e != nil {
			eventLog.Close()
			return e
		}
		targets = append(targets, &eventbus.NATS{URL: cfg.NATSURL})
	}
	if cfg.RabbitMQURL != "" {
		if e := eventbus.ValidateBrokerURL(cfg.RabbitMQURL, "amqp", "amqps"); e != nil {
			eventLog.Close()
			return e
		}
		targets = append(targets, &eventbus.RabbitMQ{URL: cfg.RabbitMQURL})
	}
	options := eventbus.DefaultOptions()
	options.Workers, options.Attempts, options.Timeout = cfg.WebhookWorkers, cfg.WebhookAttempts, cfg.WebhookTimeout
	sink, err := eventbus.New(eventLog, targets, options)
	if err != nil {
		eventLog.Close()
		return err
	}
	mgr, err := session.NewManager(context.Background(), cfg.DataDir, sink)
	if err != nil {
		closeEvents(sink)
		return err
	}
	if err = mgr.ConfigureHistory(history.Options{Retention: cfg.HistoryRetention, MaxMessages: cfg.HistoryMaxMessages, MaxBytes: cfg.HistoryMaxBytes}); err != nil {
		mgr.Shutdown(context.Background())
		closeEvents(sink)
		return err
	}
	listener, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		mgr.Shutdown(context.Background())
		closeEvents(sink)
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
	var mediaVault *vault.Vault
	if cfg.S3Endpoint != "" {
		objects, e := vault.NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3Secure)
		if e != nil {
			listener.Close()
			mgr.Shutdown(context.Background())
			closeEvents(sink)
			return e
		}
		mediaVault = &vault.Vault{Objects: objects, Retention: cfg.MediaRetention}
	}
	jobsCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	cleanupDone := make(chan struct{})
	go func() { defer close(cleanupDone); cleanMedia(jobsCtx, cfg, mgr, mediaVault) }()
	server := &http.Server{Handler: httpapi.NewRouterWithServices(cfg, mgr, httpapi.Services{Events: sink, Media: mediaVault}), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Printf("wppconnect-server-go listening on :%s", cfg.Port)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-served:
	}
	cancelRestore()
	cancelJobs()
	deadline, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	// Finish accepted HTTP operations before disconnecting their WhatsApp clients.
	httpErr := server.Shutdown(deadline)
	if httpErr != nil {
		server.Close()
	}
	sessionErr := mgr.Shutdown(deadline)
	webhookErr := sink.Close(deadline)
	select {
	case <-cleanupDone:
	case <-deadline.Done():
	}
	log.Printf("shutdown complete: events_rejected=%d", sink.Rejected())
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, httpErr, sessionErr, webhookErr)
}

func closeEvents(bus *eventbus.Bus) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	bus.Close(ctx)
}
func cleanMedia(ctx context.Context, cfg config.Config, mgr *session.Manager, mediaVault *vault.Vault) {
	if mediaVault == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		entries, err := os.ReadDir(cfg.DataDir)
		if err == nil {
			for _, entry := range entries {
				if ctx.Err() != nil {
					return
				}
				if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".history.db") {
					continue
				}
				name := strings.TrimSuffix(entry.Name(), ".history.db")
				store, err := mgr.OpenHistory(name)
				if err != nil {
					continue
				}
				operation, cancel := context.WithTimeout(ctx, 10*time.Second)
				err = mediaVault.Cleanup(operation, store, name)
				cancel()
				store.Close()
				if err != nil && ctx.Err() == nil {
					log.Print("stored media retention cleanup failed")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
