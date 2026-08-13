# viber-console — Dashboard Data Contract

> **Status:** Backend design. Frontend deliberately deferred — needs more planning.
> This document defines **what data the backend collects from both daemons** and
> **what the dashboard API returns**. It is the contract the future frontend builds against.

---

## 1. Goal

One control plane for the viber stack:

- **Viberayd** = subscription aggregator (what configs are known/working/failed).
- **Viberoxy** = proxy server (the WAN pool actually serving traffic).
- **viber-console** = a Go backend that polls both, aggregates into one JSON API, and
  (later) edits configs + restarts services. Frontend comes later.

The backend is **read-only for now** (dashboard). Config editing/restart is a v2 phase.

---

## 2. Sources — what each app already exposes

### Viberayd (HTTP API port, default `8081`, plus sub port `8080`)

| Endpoint | Shape | What the dashboard uses it for |
|---|---|---|
| `GET /api/stats` | `{total, working, failed, unreachable, updated_at}` | headline counts, staleness |
| `GET /api/configs?page=&per_page=` | `{page, per_page, total, configs:[ConfigEntry]}` | per-config table (host, protocol, state, latency, counts, first/last seen) |
| `GET /api/urls` | list of subscription URLs | config source list |
| `GET /api/health` | `{status:"ok"}` | daemon liveness |
| `GET /metrics` | Prometheus text | `viberayd_configs_total{state=...}`, `viberayd_build_info` |
| `GET /sub` (8080) | base64 working configs | (not needed by dashboard; exists for clients) |

`ConfigEntry` fields (from `internal/daemon/state.go`): `raw, host, port, protocol, source_url, first_seen, last_tested, last_success, success_count, fail_count, state, latency_ms`.

### Viberoxy (metrics port, default `0` = off)

| Endpoint | Shape | What the dashboard uses it for |
|---|---|---|
| `GET /metrics` | Prometheus text | WAN slots, connections, bytes, latency histogram, test duration |
| `GET /healthz` | liveness | proxy process alive |
| `GET /readyz` | readiness | serving traffic (WANs available) |

Viberoxy metric names (from `metrics.go`):

```
viberoxy_wans_active                    gauge
viberoxy_wan_speed_mbps{index}          gauge
viberoxy_wan_stability{index}           gauge
viberoxy_proxy_connections_total{wan,proto}  counter
viberoxy_proxy_bytes_total{wan,direction}    counter
viberoxy_proxy_latency_seconds          histogram (buckets: .05 .1 .25 .5 1 2 4 8 16)
viberoxy_test_duration_seconds          histogram
viberoxy_build_info{version}            gauge
```

> **Note:** viberoxy has **no JSON API** — only Prometheus text. The backend must parse
> Prometheus text format (stdlib-only parser, no dependency). This is deliberate: the
> daemon stays zero-dep, and the console owns the aggregation.

---

## 3. Backend → dashboard API (the contract for the frontend)

Base path: `/api`. All responses JSON. The backend polls daemons on an interval
(default 10s) and serves **snapshots from its own cache** — the frontend never calls the
daemons directly, and a slow/absent daemon yields `"reachable": false` rather than a
backend error.

### `GET /api/overview` — the one call the dashboard homepage needs

```json
{
  "generated_at": "2026-08-13T09:30:00Z",
  "viberayd": {
    "reachable": true,
    "api_url": "http://127.0.0.1:8081",
    "stats": { "total": 120, "working": 8, "failed": 100, "unreachable": 12, "updated_at": "..." },
    "build_info": { "version": "dev" },
    "sub_url": "http://127.0.0.1:8080/sub"
  },
  "viberoxy": {
    "reachable": true,
    "metrics_url": "http://127.0.0.1:9090",
    "wans": {
      "active": 4,
      "slots": [
        { "index": 0, "speed_mbps": 42.1, "stability": 0, "conns": 3, "proto_conns": {"connect": 2, "socks5": 1} },
        ...
      ]
    },
    "proxy": {
      "connections_total": 1234,
      "bytes_up": 1000000,
      "bytes_down": 5000000,
      "latency_p50_s": 0.15,
      "latency_p95_s": 1.2,
      "latency_p99_s": 3.4
    },
    "health": { "healthz": "ok", "readyz": "ok" },
    "build_info": { "version": "dev" }
  }
}
```

Latency percentiles are computed **backend-side** from the histogram buckets
(linear interpolation within bucket, standard approach). The raw histogram is also
available for a detailed view.

### `GET /api/viberayd/configs?page=&per_page=&state=` — paginated config table

```json
{
  "page": 1, "per_page": 50, "total": 120,
  "configs": [ { "host": "1.2.3.4", "port": 443, "protocol": "vless",
                 "state": "working", "latency_ms": 120, "success_count": 5,
                 "fail_count": 0, "first_seen": "...", "last_tested": "...", "last_success": "..." } ]
}
```

(Proxy of viberayd's own `/api/configs`, with `state` filter added backend-side.)

### `GET /api/viberayd/urls` — subscription sources

### `GET /api/viberoxy/metrics` — raw-ish view for a detail page

Per-slot gauges + counters + histogram buckets (useful for the WAN detail panel).

### `GET /api/health` — console liveness: `{status:"ok", generated_at}`

---

## 4. Backend structure (Go, stdlib only)

```
viber-console/
├── go.mod                     # module github.com/yolka-wiz/viber-console
├── cmd/console/main.go        # flag/env parsing, wiring, server start
├── internal/collector/
│   ├── client.go              # tiny HTTP GET helper w/ timeout
│   ├── prometheus.go          # minimal Prometheus text parser (samples + histogram buckets)
│   ├── viberayd.go            # poll /api/stats, /api/configs, /api/urls, /api/health
│   └── viberoxy.go            # poll /metrics, /healthz, /readyz
├── internal/dashboard/
│   ├── store.go               # snapshot cache (mutex), last-good per source
│   ├── overview.go            # aggregate builder
│   └── handlers.go            # /api/* handlers
└── README.md                  # run instructions
```

Env config (mirrors the family style):

| Env | Default | Meaning |
|---|---|---|
| `CONSOLE_LISTEN` | `:8090` | console HTTP listen addr |
| `CONSOLE_POLL_INTERVAL` | `10s` | daemon poll interval |
| `VIBERAYD_API_URL` | `http://127.0.0.1:8081` | viberayd management API |
| `VIBERAYD_SUB_URL` | `http://127.0.0.1:8080` | viberayd subscription endpoint |
| `VIBEROXY_METRICS_URL` | `http://127.0.0.1:9090` | viberoxy metrics/health |
| `CONSOLE_LOG_LEVEL` | `info` | slog level |

## 5. Key decisions / trade-offs

| Decision | Why |
|---|---|
| Backend polls + caches; frontend reads snapshots | decouples dashboard from daemon latency; a dead daemon doesn't hang the UI |
| Prometheus parser in backend (not daemon) | keeps daemons zero-dep; console owns aggregation |
| `reachable:false` instead of errors | dashboard degrades gracefully |
| Percentiles computed backend-side | frontend stays dumb; single source of truth for math |
| Read-only v1 | config editing needs auth + restart flow — separate phase |
| No frontend yet | user asked to plan it separately |

## 6. Open questions (frontend planning phase)

- Auth for config editing (token? basic? localhost-only default?)
- Should console also **edit daemon env files / restart systemd units**, or delegate to the bundle's install tooling?
- Persist historical metrics (timeseries) in console, or keep it a pure snapshot dashboard? (v1 = snapshots only)
