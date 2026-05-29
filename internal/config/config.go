// Package config loads server configuration from environment variables,
// mirroring the relevant knobs of the Node wppconnect-server config.ts.
package config

import "os"

// Config holds the runtime configuration for the Go server. The field set is
// intentionally aligned with the Node server so clients can migrate with the
// same mental model (secretKey auth, webhook URL, port).
type Config struct {
	Port       string
	SecretKey  string
	WebhookURL string
	DataDir    string
}

// Load reads configuration from the environment, applying the same defaults the
// Node server uses where applicable.
func Load() Config {
	return Config{
		Port:       getEnv("PORT", "21465"),
		SecretKey:  getEnv("SECRET_KEY", "THISISMYSECURETOKEN"),
		WebhookURL: getEnv("WEBHOOK_URL", ""),
		DataDir:    getEnv("DATA_DIR", "./userDataDir"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
