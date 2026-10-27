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

	"errgotrack/internal/auth"
	"errgotrack/internal/config"
	"errgotrack/internal/dashboard"
	"errgotrack/internal/database"
	"errgotrack/internal/events"
	"errgotrack/internal/httpserver"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
	"errgotrack/internal/retention"
	"errgotrack/internal/tracing"
)

// runHealthcheck lets `errgotrack -healthcheck` serve as a Docker HEALTHCHECK
// command: the distroless runtime image has no shell or curl, so the binary
// itself does the local GET /healthz and reports success via exit code.
func runHealthcheck() {
	addr := os.Getenv("ERRGOTRACK_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host := strings.TrimPrefix(addr, ":")
	if host == addr {
		// ERRGOTRACK_ADDR was a bare host:port, use it as-is.
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "error", err)
		os.Exit(1)
	}

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

	authStore := auth.NewStore(db)
	if err := authStore.EnsureAdmin(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("bootstrap admin", "error", err)
		os.Exit(1)
	}

	notif := notifications.NewStore(db, logger)

	srv, err := httpserver.New(&httpserver.Server{
		Auth:          authStore,
		Projects:      projects.NewStore(db),
		Issues:        issues.NewStore(db),
		Events:        events.NewStore(db),
		Notifications: notif,
		Dashboard:     dashboard.NewStore(db),
		Tracing:       tracing.NewStore(db),
		Logger:        logger,
		PublicURL:     cfg.PublicURL,
		CookieSecure:  strings.HasPrefix(cfg.PublicURL, "https://"),
	})
	if err != nil {
		logger.Error("init http server", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go notif.RunWorker(ctx, 5*time.Second)

	sweeper := retention.NewSweeper(db, logger)
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
