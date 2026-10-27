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
}

func Load() (Config, error) {
	c := Config{
		Addr:            getenv("ERRGOTRACK_ADDR", ":8080"),
		DatabasePath:    getenv("ERRGOTRACK_DB_PATH", "data/errgotrack.db"),
		DatabaseURL:     getenv("ERRGOTRACK_DATABASE_URL", ""),
		DBDriver:        getenv("ERRGOTRACK_DB_DRIVER", "sqlite"),
		PublicURL:       getenv("ERRGOTRACK_PUBLIC_URL", "http://localhost:8080"),
		AdminUsername:   getenv("ERRGOTRACK_ADMIN_USERNAME", ""),
		AdminPassword:   getenv("ERRGOTRACK_ADMIN_PASSWORD", ""),
		SessionSecret:   getenv("ERRGOTRACK_SESSION_SECRET", ""),
		RetentionSweep:  getenvDuration("ERRGOTRACK_RETENTION_SWEEP", time.Hour),
		ShutdownTimeout: getenvDuration("ERRGOTRACK_SHUTDOWN_TIMEOUT", 10*time.Second),
		MetricsEnabled:  getenvBool("ERRGOTRACK_METRICS", false),
	}
	if c.SessionSecret == "" {
		return c, fmt.Errorf("ERRGOTRACK_SESSION_SECRET must be set (random 32+ byte string)")
	}
	switch c.DBDriver {
	case "", "sqlite":
		c.DBDriver = "sqlite"
	case "postgres", "postgresql", "pg":
		c.DBDriver = "postgres"
		if c.DatabaseURL == "" {
			return c, fmt.Errorf("ERRGOTRACK_DATABASE_URL must be set when ERRGOTRACK_DB_DRIVER=postgres")
		}
	default:
		return c, fmt.Errorf("unknown ERRGOTRACK_DB_DRIVER %q (want sqlite or postgres)", c.DBDriver)
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
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
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
