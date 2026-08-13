package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestViberoxyFetchMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(testMetrics))
	}))
	defer srv.Close()

	client := NewViberoxyClient(srv.URL, 3*time.Second)
	snap, err := client.FetchMetrics(context.Background())
	if err != nil {
		t.Fatalf("FetchMetrics: %v", err)
	}

	if snap.WansActive != 2 {
		t.Errorf("WansActive = %d, want 2", snap.WansActive)
	}
	if len(snap.Slots) != 2 {
		t.Fatalf("Slots = %d, want 2", len(snap.Slots))
	}

	slot0 := snap.Slots[0]
	if slot0.Index != 0 || slot0.SpeedMbps != 42.5 || slot0.Stability != 0 {
		t.Errorf("slot0 = %+v, want index=0 speed=42.5 stability=0", slot0)
	}
	if slot0.Conns != 15 { // connect 10 + socks5 5
		t.Errorf("slot0.Conns = %d, want 15", slot0.Conns)
	}
	if slot0.ProtoConns["connect"] != 10 || slot0.ProtoConns["socks5"] != 5 {
		t.Errorf("slot0.ProtoConns = %+v", slot0.ProtoConns)
	}

	slot1 := snap.Slots[1]
	if slot1.Index != 1 || slot1.SpeedMbps != 18.2 || slot1.Stability != 2 {
		t.Errorf("slot1 = %+v, want index=1 speed=18.2 stability=2", slot1)
	}
	if slot1.Conns != 7 {
		t.Errorf("slot1.Conns = %d, want 7", slot1.Conns)
	}

	if snap.Proxy.ConnectionsTotal != 22 { // 10+5+7
		t.Errorf("ConnectionsTotal = %d, want 22", snap.Proxy.ConnectionsTotal)
	}
	if snap.Proxy.BytesUp != 3000 || snap.Proxy.BytesDown != 14000 {
		t.Errorf("bytes up/down = %d/%d, want 3000/14000", snap.Proxy.BytesUp, snap.Proxy.BytesDown)
	}

	// p50 with 20 obs: target 10, buckets 8@0.05,12@0.1 -> between 0.05-0.1
	if snap.Proxy.LatencyP50S < 0.05 || snap.Proxy.LatencyP50S > 0.1 {
		t.Errorf("LatencyP50S = %v, want in (0.05, 0.1]", snap.Proxy.LatencyP50S)
	}
	if snap.BuildVersion != "v0.1.0" {
		t.Errorf("BuildVersion = %q, want v0.1.0", snap.BuildVersion)
	}
}

func TestViberoxyFetchMetrics_Unreachable(t *testing.T) {
	client := NewViberoxyClient("http://127.0.0.1:1", 500*time.Millisecond)
	if _, err := client.FetchMetrics(context.Background()); err == nil {
		t.Error("expected error for unreachable server")
	}
}
