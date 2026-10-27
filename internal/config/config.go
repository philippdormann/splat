// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr            string
	DatabasePath    string
	DatabaseURL     string
	DBDriver        string // "sqlite" (default) or "postgres"
	PublicURL       string // used to build DSNs and notification links, e.g. https://errors.example.com
	AdminUsername   string
	AdminPassword   string // only used to bootstrap the first admin user if none exists
	SessionSecret   string
	RetentionSweep  time.Duration
	ShutdownTimeout time.Duration
	// MetricsEnabled exposes GET /metrics (Prometheus text format).
	// Off by default: the endpoint leaks operational counts.
	MetricsEnabled bool
	// LogLevel is the slog level name (debug, info, warn, error).
	LogLevel string
}

func Load() (Config, error) {
	c := Config{
		Addr:            getenv("ERRGOTRACK_ADDR", ":8080"),
		DatabasePath:    getenv("ERRGOTRACK_DB_PATH", "data/splat.db"),
		DatabaseURL:     getenv("ERRGOTRACK_DATABASE_URL", ""),
		DBDriver:        getenv("ERRGOTRACK_DB_DRIVER", "sqlite"),
		PublicURL:       getenv("ERRGOTRACK_PUBLIC_URL", "http://localhost:8080"),
		AdminUsername:   getenv("ERRGOTRACK_ADMIN_USERNAME", ""),
		AdminPassword:   getenv("ERRGOTRACK_ADMIN_PASSWORD", ""),
		SessionSecret:   getenv("ERRGOTRACK_SESSION_SECRET", ""),
		RetentionSweep:  getenvDuration("ERRGOTRACK_RETENTION_SWEEP", time.Hour),
		ShutdownTimeout: getenvDuration("ERRGOTRACK_SHUTDOWN_TIMEOUT", 10*time.Second),
		MetricsEnabled:  getenvBool("ERRGOTRACK_METRICS", false),
		LogLevel:        getenv("ERRGOTRACK_LOG_LEVEL", "info"),
	}
	if c.SessionSecret == "" {
		return c, fmt.Errorf("SPLAT_SESSION_SECRET must be set (random 32+ byte string)")
	}
	switch c.DBDriver {
	case "", "sqlite":
		c.DBDriver = "sqlite"
	case "postgres", "postgresql", "pg":
		c.DBDriver = "postgres"
		if c.DatabaseURL == "" {
			return c, fmt.Errorf("SPLAT_DATABASE_URL must be set when SPLAT_DB_DRIVER=postgres")
		}
	default:
		return c, fmt.Errorf("unknown SPLAT_DB_DRIVER %q (want sqlite or postgres)", c.DBDriver)
	}
	return c, nil
}

// getenv reads SPLAT_* first, then the pre-rename ERRGOTRACK_* equivalent,
// so existing deployments keep working after the rename. New setups should
// use the SPLAT_* names.
func getenv(key, fallback string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return fallback
}

// lookup checks the SPLAT_* name, then the legacy ERRGOTRACK_* name.
func lookup(key string) (string, bool) {
	if v := os.Getenv(renamed(key)); v != "" {
		return v, true
	}
	if v := os.Getenv(key); v != "" {
		return v, true
	}
	return "", false
}

// renamed maps a legacy ERRGOTRACK_* key to its SPLAT_* successor.
func renamed(key string) string {
	if rest, ok := strings.CutPrefix(key, "ERRGOTRACK_"); ok {
		return "SPLAT_" + rest
	}
	return key
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := lookup(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	if v, ok := lookup(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v, ok := lookup(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
