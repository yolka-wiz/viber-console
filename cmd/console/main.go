// Command console runs the viber-console backend: a read-only aggregation
// API over Viberayd (aggregator) and Viberoxy (proxy) for the future WebUI.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yolka-wiz/viber-console/internal/collector"
	"github.com/yolka-wiz/viber-console/internal/dashboard"
)

type config struct {
	listen       string
	pollInterval time.Duration
	vdAPI        string
	vdSub        string
	vxMetrics    string
	logLevel     string
}

func loadConfig() config {
	cfg := config{
		listen:       env("CONSOLE_LISTEN", ":8090"),
		pollInterval: envDuration("CONSOLE_POLL_INTERVAL", 10*time.Second),
		vdAPI:        env("VIBERAYD_API_URL", "http://127.0.0.1:8081"),
		vdSub:        env("VIBERAYD_SUB_URL", "http://127.0.0.1:8080"),
		vxMetrics:    env("VIBEROXY_METRICS_URL", "http://127.0.0.1:9090"),
		logLevel:     env("CONSOLE_LOG_LEVEL", "info"),
	}
	return cfg
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid duration, using default", "key", key, "value", v, "default", def)
	}
	return def
}

func main() {
	cfg := loadConfig()

	level := slog.LevelInfo
	if cfg.logLevel == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	vd := collector.NewViberaydClient(cfg.vdAPI, cfg.vdSub, 5*time.Second)
	vx := collector.NewViberoxyClient(cfg.vxMetrics, 5*time.Second)

	store := dashboard.NewStore(cfg.pollInterval, vd, vx)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go store.Run(ctx)

	handler := dashboard.NewHandler(store)
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("viber-console listening", "addr", cfg.listen, "poll_interval", cfg.pollInterval.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown", "error", err)
		}
	}

	slog.Info("bye")
}
