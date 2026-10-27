// Command server runs the Errgotrack error-tracking collector and dashboard.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/philippdormann/splat/internal/apikeys"
	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/config"
	"github.com/philippdormann/splat/internal/dashboard"
	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/events"
	"github.com/philippdormann/splat/internal/httpserver"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/logs"
	"github.com/philippdormann/splat/internal/metrics"
	"github.com/philippdormann/splat/internal/notifications"
	"github.com/philippdormann/splat/internal/projects"
	"github.com/philippdormann/splat/internal/quotas"
	"github.com/philippdormann/splat/internal/repolink"
	"github.com/philippdormann/splat/internal/retention"
	"github.com/philippdormann/splat/internal/sessions"
	"github.com/philippdormann/splat/internal/sourcemaps"
	"github.com/philippdormann/splat/internal/sso"
	"github.com/philippdormann/splat/internal/storage"
	"github.com/philippdormann/splat/internal/teams"
	"github.com/philippdormann/splat/internal/traces"
	"github.com/philippdormann/splat/internal/tracing"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// "dev" means a local un-stamped build.
var version = "dev"

// parseLogLevel maps SPLAT_LOG_LEVEL names to slog levels,
// defaulting to info on anything unrecognized.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// runHealthcheck lets `splat -healthcheck` serve as a Docker HEALTHCHECK
// command: the scratch runtime image has no shell or curl, so the binary
// itself does the local GET /healthz and reports success via exit code.
func runHealthcheck() {
	addr := os.Getenv("SPLAT_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host := strings.TrimPrefix(addr, ":")
	if host == addr {
		// The addr var was a bare host:port, use it as-is.
		host = addr
	} else {
		host = "localhost" + addr
	}
	resp, err := http.Get("http://" + host + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	resp.Body.Close()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		runHealthcheck()
		return
	}

	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))

	if cfg.DBDriver == "postgres" {
		logger.Info("database backend", "driver", "postgres")
	} else {
		if dir := filepath.Dir(cfg.DatabasePath); dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				logger.Error("create db directory", "error", err)
				os.Exit(1)
			}
		}
	}

	var db *database.DB
	if cfg.DBDriver == "postgres" {
		db, err = database.OpenPostgres(cfg.DatabaseURL)
	} else {
		db, err = database.OpenSQLite(cfg.DatabasePath)
	}
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	authStore := auth.NewStore(db, cfg.SessionSecret)
	if err := authStore.EnsureAdmin(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("bootstrap admin", "error", err)
		os.Exit(1)
	}

	notif := notifications.NewStore(db, logger)

	quotaStore := quotas.NewStore(db)
	// Breach alerts reuse the normal alert channels (best-effort).
	quotaStore.Notify = func(ctx context.Context, projectID int64, projectName, projectURL, window string, used, limit int) {
		notif.NotifyQuotaBreach(ctx, projectName, projectURL, window, used, limit)
	}

	var ssoProvider *sso.Provider
	if cfg.OIDCIssuer != "" {
		if cfg.OIDCClientID == "" {
			logger.Error("sso misconfigured", "error", "SPLAT_OIDC_ISSUER is set but SPLAT_OIDC_CLIENT_ID is empty")
			os.Exit(1)
		}
		ssoProvider = sso.NewProvider(sso.Config{
			Issuer:       cfg.OIDCIssuer,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Name:         cfg.OIDCName,
			Scopes:       cfg.OIDCScopes,
			RedirectURL:  strings.TrimSuffix(cfg.PublicURL, "/") + "/api/v1/sso/callback",
		}, cfg.SessionSecret)
		logger.Info("sso enabled", "issuer", cfg.OIDCIssuer)
	}

	sweeper := retention.NewSweeper(db, logger)

	var reg *metrics.Registry
	if cfg.MetricsEnabled {
		reg = metrics.NewRegistry()
		logger.Info("prometheus metrics enabled", "path", "/metrics")
	}

	dbPath := ""
	if cfg.DBDriver != "postgres" {
		dbPath = cfg.DatabasePath
	}

	srv, err := httpserver.New(&httpserver.Server{
		Auth:          authStore,
		Projects:      projects.NewStore(db),
		Issues:        issues.NewStore(db),
		Events:        events.NewStore(db),
		Notifications: notif,
		Dashboard:     dashboard.NewStore(db),
		Tracing:       tracing.NewStore(db),
		Retention:     sweeper,
		Storage:       storage.NewStore(db, dbPath),
		SourceMaps:    sourcemaps.NewStore(db),
		Sessions:      sessions.NewStore(db),
		APIKeys:       apikeys.NewStore(db),
		Teams:         teams.NewStore(db),
		Audit:         audit.NewStore(db),
		Quotas:        quotaStore,
		SSO:           ssoProvider,
		Logs:          logs.NewStore(db),
		Traces:        traces.NewStore(db),
		RepoLinks:     repolink.NewStore(db),
		Metrics:       reg,
		DB:            db,
		Logger:        logger,
		PublicURL:     cfg.PublicURL,
		CookieSecure:  strings.HasPrefix(cfg.PublicURL, "https://"),
		Version:       version,
	})
	if err != nil {
		logger.Error("init http server", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go notif.RunWorker(ctx, 5*time.Second)

	go sweeper.Run(ctx, cfg.RetentionSweep)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
