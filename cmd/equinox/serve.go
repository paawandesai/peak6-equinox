package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

const shutdownTimeout = 10 * time.Second

func init() { register("serve", runServe) }

// runServe starts the web server on $PORT (default 8080). It does not open a
// database connection, so /healthz answers without one.
func runServe(ctx context.Context, _ []string, _ io.Writer) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcTime}))
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", cfg.Port, err)
	}
	logger.Info("starting server", "version", buildVersion(), "addr", ln.Addr().String(), "config", cfg)
	return serve(ctx, ln, newServer(cfg, logger), logger)
}

// utcTime renders log timestamps in UTC.
func utcTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		a.Value = slog.TimeValue(a.Value.Time().UTC())
	}
	return a
}

func newServer(cfg config.Config, logger *slog.Logger) *web.Server {
	return web.New(web.Config{
		AppEnv:                cfg.AppEnv,
		AdminUser:             cfg.AdminUser,
		AdminPassword:         cfg.AdminPassword,
		PublicRateLimitPerMin: cfg.PublicRateLimitPerMin,
	}, web.Deps{Logger: logger})
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
