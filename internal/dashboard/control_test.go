package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yolka-wiz/viber-console/internal/collector"
	"github.com/yolka-wiz/viber-console/internal/config"
	"github.com/yolka-wiz/viber-console/internal/supervisor"
)

func newControlHandler(t *testing.T, cfgDir string) (*ControlHandler, *Store) {
	t.Helper()
	vdSrv := fakeViberaydServer(t)
	vxSrv := fakeViberoxyServer(t)
	t.Cleanup(vdSrv.Close)
	t.Cleanup(vxSrv.Close)

	vd := collectorClient(vdSrv.URL)
	vx := collectorClientVX(vxSrv.URL)
	store := NewStore(50*time.Millisecond, vd, vx)
	ctx, cancel := contextWithCancel()
	t.Cleanup(cancel)
	go store.Run(ctx)

	cfgStore := config.NewStore(cfgDir)
	return NewControlHandler(store, cfgStore, nil, vdSrv.URL, ""), store
}

// collectorClient builds a viberayd client against a fake server URL.
func collectorClient(apiURL string) *collector.ViberaydClient {
	return collector.NewViberaydClient(apiURL, apiURL, 2*time.Second)
}

// collectorClientVX builds a viberoxy client against a fake server URL.
func collectorClientVX(url string) *collector.ViberoxyClient {
	return collector.NewViberoxyClient(url, 2*time.Second)
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// fakeSleeper writes a tiny shell script that sleeps, for process tests.
func fakeSleeper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "sleeper")
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func doJSON(t *testing.T, h *ControlHandler, method, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Routes(mux)
	mux.ServeHTTP(rec, req)

	out := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestControlSchema(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "GET", "/api/config/schema", nil)
	if code != http.StatusOK {
		t.Fatalf("schema = %d, want 200", code)
	}
	groups := out["groups"].(map[string]interface{})
	if _, ok := groups["viberayd"]; !ok {
		t.Error("missing viberayd group")
	}
	if _, ok := groups["viberoxy"]; !ok {
		t.Error("missing viberoxy group")
	}
}

func TestControlValuesGet(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "GET", "/api/config/values", nil)
	if code != http.StatusOK {
		t.Fatalf("values = %d, want 200", code)
	}
	if _, ok := out["viberayd"]; !ok {
		t.Error("missing viberayd values")
	}
	if _, ok := out["viberoxy"]; !ok {
		t.Error("missing viberoxy values")
	}
}

func TestControlValuesPost(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "POST", "/api/config/values", map[string]interface{}{
		"group":  "viberoxy",
		"values": map[string]string{"WAN_COUNT": "3", "ROUTE_MODE": "proxy-default"},
	})
	if code != http.StatusOK {
		t.Fatalf("post = %d, want 200 (body %v)", code, out)
	}
	if out["saved"] != true {
		t.Error("saved != true")
	}
}

func TestControlValuesPostInvalid(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "POST", "/api/config/values", map[string]interface{}{
		"group":  "viberoxy",
		"values": map[string]string{"WAN_COUNT": "99"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("invalid post = %d, want 400", code)
	}
	if out["error"] == "" {
		t.Error("expected error message")
	}
}

func TestControlValuesPostBadGroup(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, _ := doJSON(t, h, "POST", "/api/config/values", map[string]interface{}{
		"group": "nope",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bad group = %d, want 400", code)
	}
}

func TestControlAuth(t *testing.T) {
	vdSrv := fakeViberaydServer(t)
	t.Cleanup(vdSrv.Close)
	cfgStore := config.NewStore(t.TempDir())
	h := NewControlHandler(nil, cfgStore, nil, vdSrv.URL, "sekret")

	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest("GET", "/api/config/schema", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/config/schema", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with token = %d, want 200", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/config/schema", nil)
	req.Header.Set("X-Console-Token", "sekret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("x-token = %d, want 200", rec.Code)
	}
}

func TestControlMonitorRejectsMutations(t *testing.T) {
	cfgDir := t.TempDir()
	h := NewControlHandler(nil, config.NewStore(cfgDir), nil, "", "", "monitor")

	code, out := doJSON(t, h, http.MethodPost, "/api/config/values", map[string]interface{}{
		"group": "viberoxy", "values": map[string]string{"WAN_COUNT": "3"},
	})
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "monitor mode") {
		t.Fatalf("config POST = %d, body %v; want clear 409", code, out)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "viberoxy.env")); !os.IsNotExist(err) {
		t.Fatalf("monitor config POST wrote env file: %v", err)
	}

	code, out = doJSON(t, h, http.MethodPost, "/api/control/restart", map[string]string{"service": "viberoxy"})
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "monitor mode") {
		t.Fatalf("restart POST = %d, body %v; want clear 409", code, out)
	}
}

func TestControlRestartUnknownService(t *testing.T) {
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "POST", "/api/control/restart", map[string]interface{}{
		"service": "nonexistent",
	})
	if code != http.StatusOK {
		t.Fatalf("restart = %d, want 200 (no-op)", code)
	}
	if len(out["restarted"].([]interface{})) != 0 {
		t.Error("expected no restarted services")
	}
}

func TestControlProcesses(t *testing.T) {
	// With no services, processes returns an empty list.
	h, _ := newControlHandler(t, t.TempDir())
	code, out := doJSON(t, h, "GET", "/api/processes", nil)
	if code != http.StatusOK {
		t.Fatalf("processes = %d, want 200", code)
	}
	svcs := out["services"].([]interface{})
	if len(svcs) != 0 {
		t.Errorf("services = %d, want 0", len(svcs))
	}
}

func TestControlProcessesMonitorStartsUnknown(t *testing.T) {
	store := NewStore(time.Second, nil, nil)
	h := NewControlHandler(store, config.NewStore(t.TempDir()), nil, "", "", "monitor")

	code, out := doJSON(t, h, http.MethodGet, "/api/processes", nil)
	if code != http.StatusOK {
		t.Fatalf("processes = %d, want 200", code)
	}
	for _, raw := range out["services"].([]interface{}) {
		service := raw.(map[string]interface{})
		if service["state"] != "starting" || service["running"] != false {
			t.Errorf("initial service = %v, want starting and not yet running", service)
		}
	}
}

func TestControlProcessesMonitorKeepsStartingDuringGrace(t *testing.T) {
	vd := collector.NewViberaydClient("http://127.0.0.1:1", "http://127.0.0.1:1", 20*time.Millisecond)
	vx := collector.NewViberoxyClient("http://127.0.0.1:1", 20*time.Millisecond)
	store := NewStore(time.Second, vd, vx, 50*time.Millisecond)
	store.poll(context.Background())
	h := NewControlHandler(store, config.NewStore(t.TempDir()), nil, "", "", "monitor")

	_, out := doJSON(t, h, http.MethodGet, "/api/processes", nil)
	for _, raw := range out["services"].([]interface{}) {
		if service := raw.(map[string]interface{}); service["state"] != "starting" {
			t.Errorf("during grace service = %v, want starting", service)
		}
	}

	time.Sleep(60 * time.Millisecond)
	_, out = doJSON(t, h, http.MethodGet, "/api/processes", nil)
	for _, raw := range out["services"].([]interface{}) {
		service := raw.(map[string]interface{})
		if service["state"] != "unreachable" {
			t.Errorf("after grace service = %v, want unreachable", service)
		}
		if service["running"] != false || service["externally_managed"] != true || service["restart_available"] != false {
			t.Errorf("after grace service = %v, want unreachable external service without restart", service)
		}
	}
}

func TestControlProcessesMonitorReportsReachableExternalServices(t *testing.T) {
	_, store := newControlHandler(t, t.TempDir())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snap := store.Get(); snap.ViberaydUp && snap.ViberoxyUp {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	h := NewControlHandler(store, config.NewStore(t.TempDir()), nil, "", "", "monitor")
	code, out := doJSON(t, h, http.MethodGet, "/api/processes", nil)
	if code != http.StatusOK {
		t.Fatalf("processes = %d, want 200", code)
	}
	services := out["services"].([]interface{})
	if len(services) != 2 {
		t.Fatalf("services = %d, want 2", len(services))
	}
	for _, raw := range services {
		service := raw.(map[string]interface{})
		if service["running"] != true || service["state"] != "running" {
			t.Errorf("service = %v, want running", service)
		}
		if service["externally_managed"] != true || service["restart_available"] != false {
			t.Errorf("service = %v, want externally managed without restart", service)
		}
		if _, ok := service["pid"]; ok {
			t.Errorf("service = %v, monitor status must not expose a PID", service)
		}
	}
}

func TestControlProcessesWithService(t *testing.T) {
	// Use a fake service whose binary is a sleeper to exercise Status().
	bin := fakeSleeper(t)
	svc := supervisor.NewService("viberoxy", bin, "")
	ctx, cancel := contextWithCancel()
	t.Cleanup(cancel)
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}

	vdSrv := fakeViberaydServer(t)
	t.Cleanup(vdSrv.Close)
	cfgStore := config.NewStore(t.TempDir())
	h := NewControlHandler(nil, cfgStore, []*supervisor.Service{svc}, vdSrv.URL, "")

	code, out := doJSON(t, h, "GET", "/api/processes", nil)
	if code != http.StatusOK {
		t.Fatalf("processes = %d, want 200", code)
	}
	svcs := out["services"].([]interface{})
	if len(svcs) != 1 {
		t.Fatalf("services = %d, want 1", len(svcs))
	}
	first := svcs[0].(map[string]interface{})
	if first["running"] != true || first["pid"] == nil {
		t.Errorf("service status = %v, want running with pid", first)
	}
	svc.Stop()
}
