// Command console runs the viber-console backend: a read-only aggregation
// API over Viberayd (aggregator) and Viberoxy (proxy) for the future WebUI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yolka-wiz/viber-console/internal/collector"
	"github.com/yolka-wiz/viber-console/internal/config"
	"github.com/yolka-wiz/viber-console/internal/dashboard"
	"github.com/yolka-wiz/viber-console/internal/supervisor"
)

type settings struct {
	listen       string
	mode         string
	pollInterval time.Duration
	startupGrace time.Duration
	vdAPI        string
	vdSub        string
	vxMetrics    string
	vxAPI        string
	logLevel     string
	configDir    string
	viberaydBin  string
	viberoxyBin  string
	token        string
}

func loadConfig() settings {
	cfg := settings{
		listen:       env("CONSOLE_LISTEN", "127.0.0.1:8090"),
		mode:         env("CONSOLE_MODE", "supervise"),
		pollInterval: envDuration("CONSOLE_POLL_INTERVAL", 10*time.Second),
		startupGrace: envDuration("CONSOLE_STARTUP_GRACE", 30*time.Second),
		vdAPI:        env("VIBERAYD_API_URL", "http://127.0.0.1:8081"),
		vdSub:        env("VIBERAYD_SUB_URL", "http://127.0.0.1:8080"),
		vxMetrics:    env("VIBEROXY_METRICS_URL", "http://127.0.0.1:9090"),
		vxAPI:        env("VIBEROXY_API_URL", "http://127.0.0.1:1980"),
		logLevel:     env("CONSOLE_LOG_LEVEL", "info"),
		configDir:    env("VIBER_CONFIG_DIR", "/etc/viber"),
		viberaydBin:  env("VIBERAYD_BIN", "viberayd"),
		viberoxyBin:  env("VIBEROXY_BIN", "viberoxy"),
		token:        os.Getenv("CONSOLE_TOKEN"),
	}
	return cfg
}

func loadValidatedConfig() (settings, error) {
	cfg := loadConfig()
	if err := cfg.validate(); err != nil {
		return settings{}, err
	}
	return cfg, nil
}

func (cfg settings) validate() error {
	if cfg.mode != "supervise" && cfg.mode != "monitor" {
		return fmt.Errorf("CONSOLE_MODE must be supervise or monitor, got %q", cfg.mode)
	}
	if cfg.token == "" {
		host, _, err := net.SplitHostPort(cfg.listen)
		if err != nil {
			return fmt.Errorf("invalid CONSOLE_LISTEN %q: %w", cfg.listen, err)
		}
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("CONSOLE_TOKEN is required when CONSOLE_LISTEN %q is not loopback", cfg.listen)
		}
	}
	return nil
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

func servicesFor(cfg settings, cfgStore *config.Store) []*supervisor.Service {
	if cfg.mode == "monitor" {
		return nil
	}
	return []*supervisor.Service{
		supervisor.NewService("viberayd", cfg.viberaydBin, cfgStore.FileName("viberayd")),
		supervisor.NewService("viberoxy", cfg.viberoxyBin, cfgStore.FileName("viberoxy")),
	}
}

func main() {
	cfg, err := loadValidatedConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	level := slog.LevelInfo
	if cfg.logLevel == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	vd := collector.NewViberaydClient(cfg.vdAPI, cfg.vdSub, 5*time.Second)
	vx := collector.NewViberoxyClientWithAPI(cfg.vxMetrics, cfg.vxAPI, 5*time.Second)

	store := dashboard.NewStore(cfg.pollInterval, vd, vx, cfg.startupGrace)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go store.Run(ctx)

	// Supervise mode owns daemon lifecycles; monitor mode leaves systemd-owned
	// daemons untouched and observes them over HTTP only.
	cfgStore := config.NewStore(cfg.configDir)
	services := servicesFor(cfg, cfgStore)
	for _, s := range services {
		if err := s.Start(ctx); err != nil {
			slog.Warn("service failed to start (continuing; may be launched separately)", "service", s.Name, "error", err)
		}
	}
	defer func() {
		for _, s := range services {
			s.Stop()
		}
	}()

	handler := dashboard.NewHandler(store, cfg.mode)
	control := dashboard.NewControlHandler(store, cfgStore, services, cfg.vdAPI, cfg.token, cfg.mode)
	mux := handler.Routes()
	control.Routes(mux)
	mux.Handle("/static/", dashboard.StaticHandler())
	mux.Handle("/", dashboard.StaticHandler())
	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           dashboard.RequireAPIToken(mux, cfg.token),
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
