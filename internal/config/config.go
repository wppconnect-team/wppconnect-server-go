// Package config loads server configuration from environment variables,
// mirroring the relevant knobs of the Node wppconnect-server config.ts.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the runtime configuration for the Go server. The field set is
// intentionally aligned with the Node server so clients can migrate with the
// same mental model (secretKey auth, webhook URL, port).
type Config struct {
	Port             string
	SecretKey        string
	WebhookURL       string
	DataDir          string
	AutoRestore      bool
	ShutdownTimeout  time.Duration
	WebhookWorkers   int
	WebhookQueueSize int
	WebhookAttempts  int
	WebhookTimeout   time.Duration
}

// Load reads configuration from the environment, applying the same defaults the
// Node server uses where applicable.
func Load() (Config, error) {
	cfg := Config{
		Port:       getEnv("PORT", "21465"),
		SecretKey:  getEnv("SECRET_KEY", "THISISMYSECURETOKEN"),
		WebhookURL: getEnv("WEBHOOK_URL", ""),
		DataDir:    getEnv("DATA_DIR", "./userDataDir"),
	}
	var err error
	cfg.AutoRestore, err = strconv.ParseBool(getEnv("AUTO_RESTORE_SESSIONS", "true"))
	if err != nil {
		return cfg, fmt.Errorf("AUTO_RESTORE_SESSIONS: %w", err)
	}
	for _, item := range []struct {
		key, fallback string
		dest          *time.Duration
		maximum       time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", "25s", &cfg.ShutdownTimeout, 5 * time.Minute},
		{"WEBHOOK_TIMEOUT", "10s", &cfg.WebhookTimeout, time.Minute},
	} {
		*item.dest, err = time.ParseDuration(getEnv(item.key, item.fallback))
		if err != nil || *item.dest < time.Millisecond || *item.dest > item.maximum {
			return cfg, fmt.Errorf("invalid %s duration", item.key)
		}
	}
	for _, item := range []struct {
		key, fallback string
		dest          *int
		maximum       int
	}{
		{"WEBHOOK_WORKERS", "2", &cfg.WebhookWorkers, 32},
		{"WEBHOOK_QUEUE_SIZE", "256", &cfg.WebhookQueueSize, 10000},
		{"WEBHOOK_ATTEMPTS", "4", &cfg.WebhookAttempts, 10},
	} {
		*item.dest, err = strconv.Atoi(getEnv(item.key, item.fallback))
		if err != nil || *item.dest < 1 || *item.dest > item.maximum {
			return cfg, fmt.Errorf("invalid %s limit", item.key)
		}
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return cfg, fmt.Errorf("invalid PORT")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
