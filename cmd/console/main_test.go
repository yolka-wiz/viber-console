package main

import (
	"strings"
	"testing"
	"time"

	"github.com/yolka-wiz/viber-console/internal/config"
)

func TestLoadConfigUsesSafeDefaults(t *testing.T) {
	t.Setenv("CONSOLE_LISTEN", "")
	t.Setenv("CONSOLE_MODE", "")

	cfg := loadConfig()
	if cfg.listen != "127.0.0.1:8090" {
		t.Errorf("listen = %q, want loopback default", cfg.listen)
	}
	if cfg.mode != "supervise" {
		t.Errorf("mode = %q, want supervise default", cfg.mode)
	}
	if cfg.startupGrace != 30*time.Second {
		t.Errorf("startupGrace = %s, want 30s", cfg.startupGrace)
	}
}

func TestSettingsValidateRejectsUnknownMode(t *testing.T) {
	cfg := settings{listen: "127.0.0.1:8090", mode: "automatic"}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "CONSOLE_MODE") {
		t.Fatalf("validate error = %v, want useful CONSOLE_MODE error", err)
	}
}

func TestSettingsValidateRequiresTokenForNonLoopbackListen(t *testing.T) {
	for _, listen := range []string{":8090", "0.0.0.0:8090", "[::]:8090", "192.168.1.20:8090"} {
		t.Run(listen, func(t *testing.T) {
			cfg := settings{listen: listen, mode: "monitor"}
			if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "CONSOLE_TOKEN") {
				t.Fatalf("validate error = %v, want useful CONSOLE_TOKEN error", err)
			}
		})
	}
}

func TestSettingsValidateAllowsLocalhostWithoutToken(t *testing.T) {
	for _, listen := range []string{"localhost:8090", "127.0.0.1:8090", "[::1]:8090"} {
		t.Run(listen, func(t *testing.T) {
			cfg := settings{listen: listen, mode: "monitor"}
			if err := cfg.validate(); err != nil {
				t.Fatalf("validate error = %v, want loopback allowed without token", err)
			}
		})
	}
}

func TestLoadValidatedConfigRejectsUnsafeListen(t *testing.T) {
	t.Setenv("CONSOLE_MODE", "monitor")
	t.Setenv("CONSOLE_LISTEN", ":8090")
	t.Setenv("CONSOLE_TOKEN", "")

	if _, err := loadValidatedConfig(); err == nil {
		t.Fatal("loadValidatedConfig error = nil, want unsafe listen rejected")
	}
}

func TestServicesForMonitorDoesNotCreateSupervisedChildren(t *testing.T) {
	cfg := settings{mode: "monitor", viberaydBin: "/missing/viberayd", viberoxyBin: "/missing/viberoxy"}
	services := servicesFor(cfg, config.NewStore(t.TempDir()))
	if len(services) != 0 {
		t.Fatalf("monitor services = %d, want no supervised children", len(services))
	}
}

func TestLoadConfigUsesSeparateViberoxyURLs(t *testing.T) {
	t.Setenv("VIBEROXY_METRICS_URL", "http://127.0.0.1:2111")
	t.Setenv("VIBEROXY_API_URL", "http://127.0.0.1:1980")

	cfg := loadConfig()
	if cfg.vxMetrics != "http://127.0.0.1:2111" {
		t.Fatalf("vxMetrics = %q", cfg.vxMetrics)
	}
	if cfg.vxAPI != "http://127.0.0.1:1980" {
		t.Fatalf("vxAPI = %q", cfg.vxAPI)
	}
}
