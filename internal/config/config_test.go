package config

import (
	"strings"
	"testing"
	"time"
)

func TestExpandedConfigurationLimits(t *testing.T) {
	for key, value := range map[string]string{"ADMIN_TOKEN": "short", "HISTORY_RETENTION_DAYS": "366", "OUTBOX_RETENTION_DAYS": "31", "MEDIA_RETENTION_DAYS": "0", "HISTORY_MAX_MESSAGES": "1000001", "OUTBOX_MAX_BYTES": "1", "S3_ENDPOINT": "minio:9000", "ENABLE_MANAGER": "maybe", "ENABLE_EXPERIMENTAL_INTERACTIVES": "maybe"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid expanded configuration accepted")
			}
		})
	}
	t.Setenv("ADMIN_TOKEN", strings.Repeat("a", 32))
	cfg, err := Load()
	if err != nil || cfg.AdminToken == "" || !cfg.ManagerEnabled || cfg.ExperimentalInteractives || !cfg.S3Secure || cfg.HistoryRetention != 30*24*time.Hour {
		t.Fatalf("defaults %#v %v", cfg, err)
	}
}

func TestOperationalDefaultsAndInvalidSettings(t *testing.T) {
	for _, key := range []string{"PORT", "AUTO_RESTORE_SESSIONS", "SHUTDOWN_TIMEOUT", "WEBHOOK_TIMEOUT", "WEBHOOK_WORKERS", "WEBHOOK_QUEUE_SIZE", "WEBHOOK_ATTEMPTS"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoRestore || cfg.ShutdownTimeout != 25*time.Second || cfg.WebhookAttempts != 4 || cfg.WebhookWorkers != 2 || cfg.WebhookQueueSize != 256 {
		t.Fatal(cfg)
	}
	for key, value := range map[string]string{"PORT": "0", "AUTO_RESTORE_SESSIONS": "maybe", "SHUTDOWN_TIMEOUT": "-1s", "WEBHOOK_TIMEOUT": "1h", "WEBHOOK_WORKERS": "0", "WEBHOOK_QUEUE_SIZE": "10001", "WEBHOOK_ATTEMPTS": "11"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid environment accepted")
			}
		})
	}
}
