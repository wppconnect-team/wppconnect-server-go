package config

import (
	"testing"
	"time"
)

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
