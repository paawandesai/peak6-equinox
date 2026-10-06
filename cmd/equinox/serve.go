package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

const shutdownTimeout = 10 * time.Second

func init() { register("serve", runServe) }

// runServe starts the web server on $PORT (default 8080). The database pool
// connects lazily, so /healthz answers without a reachable database and the
// admin routes recover as soon as it is reachable.
func runServe(ctx context.Context, _ []string, _ io.Writer) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	deps, closeDeps, err := buildDeps(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeDeps()
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", cfg.Port, err)
	}
	logger.Info("starting server", "version", buildVersion(), "addr", ln.Addr().String(), "config", cfg)
	return serve(ctx, ln, newServerWith(cfg, logger, deps), logger)
}

// buildDeps wires the store (a lazily connecting pool) and the collector.
func buildDeps(ctx context.Context, cfg config.Config, logger *slog.Logger) (web.Deps, func(), error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return web.Deps{}, nil, fmt.Errorf("configuring database pool: %w", err)
	}
	st := store.New(pool)
	col, err := buildCollector(cfg, logger, st, 0)
	if err != nil {
		pool.Close()
		return web.Deps{}, nil, err
	}
	return web.Deps{Logger: logger, Store: st, Ingester: col}, pool.Close, nil
}

// utcTime renders log timestamps in UTC.
func utcTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		a.Value = slog.TimeValue(a.Value.Time().UTC())
	}
	return a
}

// newServer is a server with no database or collector (health check only).
func newServer(cfg config.Config, logger *slog.Logger) *web.Server {
	return newServerWith(cfg, logger, web.Deps{Logger: logger})
}

func newServerWith(cfg config.Config, logger *slog.Logger, deps web.Deps) *web.Server {
	deps.Logger = logger
	return web.New(web.Config{
		AppEnv:                cfg.AppEnv,
		AdminUser:             cfg.AdminUser,
		AdminPassword:         cfg.AdminPassword,
		PublicRateLimitPerMin: cfg.PublicRateLimitPerMin,
	}, deps)
}

// serve runs the HTTP server on ln until ctx is cancelled, then shuts down gracefully.
func serve(ctx context.Context, ln net.Listener, s *web.Server, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("serving http: %w", err)
	case <-ctx.Done():
	}
	logger.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving http: %w", err)
	}
	return nil
}
