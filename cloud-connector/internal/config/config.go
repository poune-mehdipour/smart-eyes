// Package config loads all runtime configuration from the environment.
// There is deliberately no config file: containers and Kubernetes inject
// environment variables, .env.example documents every knob, and nothing
// secret is ever committed.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the full runtime configuration of the connector.
type Config struct {
	// HTTPAddr is the listen address of the public HTTP API (edge ingest,
	// webhooks, health, metrics), e.g. ":8080".
	HTTPAddr string
	// GRPCAddr is the listen address of the internal gRPC API, e.g. ":9090".
	GRPCAddr string

	// DatabaseURL is a standard Postgres URL. The referenced role should be
	// scoped to this service's database only.
	DatabaseURL string

	// EdgeAPIToken is the bearer token Guardian edge devices present on the
	// ingest API. Empty disables edge auth (local development only) and is
	// logged loudly at startup.
	EdgeAPIToken string

	// TokenCipherKey is a 32-byte AES-256 key (hex-encoded in the env) used
	// to encrypt provider OAuth tokens at rest.
	TokenCipherKey []byte

	// BetaGridWebhookSecret is the shared HMAC-SHA256 secret the simulated
	// BetaGrid provider signs webhook deliveries with.
	BetaGridWebhookSecret string
	// WebhookMaxSkew bounds how old (or future-dated) a signed webhook
	// timestamp may be before the delivery is rejected.
	WebhookMaxSkew time.Duration

	// AlphaSense holds the OAuth2 + REST polling configuration for the
	// simulated AlphaSense provider. Polling is disabled when BaseURL is "".
	AlphaSense AlphaSenseConfig

	// CommandTTL is how long an undelivered command stays PENDING before the
	// reaper marks it FAILED.
	CommandTTL time.Duration

	// RequestTimeout bounds each inbound HTTP request end-to-end.
	RequestTimeout time.Duration
	// MaxBodyBytes bounds inbound request bodies (ingest + webhooks).
	MaxBodyBytes int64

	// LogLevel is debug|info|warn|error.
	LogLevel string
}

// AlphaSenseConfig configures the polling provider adapter.
type AlphaSenseConfig struct {
	BaseURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	PollInterval time.Duration
}

// Load reads configuration from the environment, applying defaults suitable
// for local development for everything non-secret.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:              getenv("CONNECTOR_HTTP_ADDR", ":8080"),
		GRPCAddr:              getenv("CONNECTOR_GRPC_ADDR", ":9090"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		EdgeAPIToken:          os.Getenv("EDGE_API_TOKEN"),
		BetaGridWebhookSecret: os.Getenv("BETAGRID_WEBHOOK_SECRET"),
		WebhookMaxSkew:        getdur("WEBHOOK_MAX_SKEW", 5*time.Minute),
		CommandTTL:            getdur("COMMAND_TTL", 24*time.Hour),
		RequestTimeout:        getdur("REQUEST_TIMEOUT", 15*time.Second),
		MaxBodyBytes:          getint64("MAX_BODY_BYTES", 1<<20), // 1 MiB
		LogLevel:              getenv("LOG_LEVEL", "info"),
		AlphaSense: AlphaSenseConfig{
			BaseURL:      os.Getenv("ALPHASENSE_BASE_URL"),
			TokenURL:     os.Getenv("ALPHASENSE_TOKEN_URL"),
			ClientID:     os.Getenv("ALPHASENSE_CLIENT_ID"),
			ClientSecret: os.Getenv("ALPHASENSE_CLIENT_SECRET"),
			PollInterval: getdur("ALPHASENSE_POLL_INTERVAL", 30*time.Second),
		},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if keyHex := os.Getenv("TOKEN_CIPHER_KEY"); keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return Config{}, fmt.Errorf("TOKEN_CIPHER_KEY is not valid hex: %w", err)
		}
		if len(key) != 32 {
			return Config{}, fmt.Errorf("TOKEN_CIPHER_KEY must be 32 bytes (64 hex chars), got %d bytes", len(key))
		}
		cfg.TokenCipherKey = key
	}

	if cfg.AlphaSense.BaseURL != "" {
		if cfg.AlphaSense.TokenURL == "" || cfg.AlphaSense.ClientID == "" || cfg.AlphaSense.ClientSecret == "" {
			return Config{}, fmt.Errorf("ALPHASENSE_BASE_URL is set but token URL / client id / client secret are incomplete")
		}
	}

	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getdur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getint64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
