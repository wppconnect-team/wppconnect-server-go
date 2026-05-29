// Command server starts the Go port of wppconnect-server, backed by whatsmeow.
// It keeps the HTTP contract (routes + payloads) compatible with the Node
// server so existing clients can migrate with minimal changes.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/httpapi"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"github.com/wppconnect-team/wppconnect-server-go/internal/webhook"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "store.db")

	sink := webhook.New(cfg.WebhookURL)
	mgr, err := session.NewManager(context.Background(), dbPath, sink)
	if err != nil {
		log.Fatalf("session manager: %v", err)
	}

	router := httpapi.NewRouter(cfg, mgr)
	log.Printf("wppconnect-server-go listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}
}
