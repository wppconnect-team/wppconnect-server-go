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
	ExperimentalInteractives                                 bool
	Port                                                     string
	SecretKey                                                string
	WebhookURL                                               string
	DataDir                                                  string
	AutoRestore                                              bool
	ShutdownTimeout                                          time.Duration
	WebhookWorkers                                           int
	WebhookQueueSize                                         int
	WebhookAttempts                                          int
	WebhookTimeout                                           time.Duration
	AdminToken                                               string
	ManagerEnabled                                           bool
	NATSURL                                                  string
	RabbitMQURL                                              string
	HistoryRetention                                         time.Duration
	HistoryMaxMessages                                       int
	HistoryMaxBytes                                          int64
	OutboxRetention                                          time.Duration
	OutboxMaxEvents                                          int
	OutboxMaxBytes                                           int64
	S3Endpoint, S3AccessKey, S3SecretKey, S3Bucket, S3Region string
	S3Secure                                                 bool
	MediaRetention                                           time.Duration
}

// Load reads configuration from the environment, applying the same defaults the
// Node server uses where applicable.
func Load() (Config, error) {
	cfg := Config{
		Port:       getEnv("PORT", "21465"),
		SecretKey:  getEnv("SECRET_KEY", "THISISMYSECURETOKEN"),
		WebhookURL: getEnv("WEBHOOK_URL", ""),
		DataDir:    getEnv("DATA_DIR", "./userDataDir"),
		AdminToken: os.Getenv("ADMIN_TOKEN"), NATSURL: os.Getenv("NATS_URL"), RabbitMQURL: os.Getenv("AMQP_URL"),
		S3Endpoint: os.Getenv("S3_ENDPOINT"), S3AccessKey: os.Getenv("S3_ACCESS_KEY_ID"), S3SecretKey: os.Getenv("S3_SECRET_ACCESS_KEY"), S3Bucket: os.Getenv("S3_BUCKET"), S3Region: os.Getenv("S3_REGION"),
	}
	var err error
	cfg.ExperimentalInteractives, err = strconv.ParseBool(getEnv("ENABLE_EXPERIMENTAL_INTERACTIVES", "false"))
	if err != nil {
		return cfg, fmt.Errorf("invalid ENABLE_EXPERIMENTAL_INTERACTIVES")
	}
	cfg.ManagerEnabled, err = strconv.ParseBool(getEnv("ENABLE_MANAGER", "true"))
	if err != nil {
		return cfg, fmt.Errorf("invalid ENABLE_MANAGER")
	}
	cfg.S3Secure, err = strconv.ParseBool(getEnv("S3_USE_SSL", "true"))
	if err != nil {
		return cfg, fmt.Errorf("invalid S3_USE_SSL")
	}
	if cfg.AdminToken != "" && len(cfg.AdminToken) < 32 {
		return cfg, fmt.Errorf("ADMIN_TOKEN must contain at least 32 bytes")
	}
	for _, item := range []struct {
		key, fallback string
		dest          *time.Duration
		maximum       int
	}{
		{"HISTORY_RETENTION_DAYS", "30", &cfg.HistoryRetention, 365}, {"OUTBOX_RETENTION_DAYS", "7", &cfg.OutboxRetention, 30}, {"MEDIA_RETENTION_DAYS", "7", &cfg.MediaRetention, 30},
	} {
		days, e := strconv.Atoi(getEnv(item.key, item.fallback))
		if e != nil || days < 1 || days > item.maximum {
			return cfg, fmt.Errorf("invalid %s", item.key)
		}
		*item.dest = time.Duration(days) * 24 * time.Hour
	}
	for _, item := range []struct {
		key, fallback string
		dest          *int
		maximum       int
	}{
		{"HISTORY_MAX_MESSAGES", "100000", &cfg.HistoryMaxMessages, 1000000}, {"OUTBOX_MAX_EVENTS", "100000", &cfg.OutboxMaxEvents, 1000000},
	} {
		*item.dest, err = strconv.Atoi(getEnv(item.key, item.fallback))
		if err != nil || *item.dest < 1 || *item.dest > item.maximum {
			return cfg, fmt.Errorf("invalid %s", item.key)
		}
	}
	for _, item := range []struct {
		key, fallback string
		dest          *int64
	}{
		{"HISTORY_MAX_BYTES", "268435456", &cfg.HistoryMaxBytes}, {"OUTBOX_MAX_BYTES", "134217728", &cfg.OutboxMaxBytes},
	} {
		*item.dest, err = strconv.ParseInt(getEnv(item.key, item.fallback), 10, 64)
		if err != nil || *item.dest < 1<<20 || *item.dest > 10<<30 {
			return cfg, fmt.Errorf("invalid %s", item.key)
		}
	}
	if cfg.S3Endpoint != "" && (cfg.S3AccessKey == "" || cfg.S3SecretKey == "" || cfg.S3Bucket == "") {
		return cfg, fmt.Errorf("S3 credentials and bucket are required when S3_ENDPOINT is set")
	}
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
