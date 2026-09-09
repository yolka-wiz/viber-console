# viber-console

Control plane for the [viber stack](https://github.com/amirrezaalavi): bundles
[Viberayd](https://github.com/amirrezaalavi/Viberayd) (subscription aggregator)
and [Viberoxy](https://github.com/amirrezaalavi/Viberoxy) (proxy server). It can
either supervise both daemons or monitor externally managed services, and it
provides a single-page WebUI for live status and configuration.

> **Status:** backend + WebUI built. The install/bundle layer (systemd unit,
> install script, compose) is the remaining phase.

## What it does

- **Explicit lifecycle mode** — `supervise` owns, starts, and restarts child
  processes; `monitor` observes systemd/container-owned daemons over HTTP and
  never spawns, restarts, or mutates them
- **Aggregates status** — polls Viberayd JSON API + Viberoxy Prometheus metrics
  (stdlib-only parser), serves one `/api/overview` snapshot
- **Edits config** — schema-driven settings form writes the daemons' env files
  atomically (with `.bak` rollback) and restarts them
- **Single-page WebUI** — dark/light theme, mobile-friendly, zero frontend deps
  (vanilla JS + CSS embedded via `go:embed`)

## Quick start

```bash
# 1. Build the console
go build -o build/console ./cmd/console

# 2. Supervise child daemons (the default).
VIBER_CONFIG_DIR=/etc/viber \
VIBERAYD_API_URL=http://127.0.0.1:8081 \
VIBEROXY_METRICS_URL=http://127.0.0.1:9090 \
VIBEROXY_API_URL=http://127.0.0.1:1980 \
./build/console

# 3. Open http://127.0.0.1:8090
```

## Configuration (env vars)

| Variable | Default | Description |
|---|---|---|
| `CONSOLE_LISTEN` | `127.0.0.1:8090` | Console HTTP listen address. Non-loopback addresses require `CONSOLE_TOKEN` |
| `CONSOLE_MODE` | `supervise` | `supervise` owns child lifecycles; `monitor` is read-only and observes external daemons over HTTP |
| `CONSOLE_POLL_INTERVAL` | `10s` | How often daemons are polled |
| `CONSOLE_STARTUP_GRACE` | `30s` | Report unreachable external services as `starting` during initial polling |
| `VIBERAYD_API_URL` | `http://127.0.0.1:8081` | Viberayd management API |
| `VIBERAYD_SUB_URL` | `http://127.0.0.1:8080` | Viberayd subscription endpoint |
| `VIBEROXY_METRICS_URL` | `http://127.0.0.1:9090` | Viberoxy metrics/health |
| `VIBEROXY_API_URL` | `http://127.0.0.1:1980` | Viberoxy WAN control API |
| `VIBER_CONFIG_DIR` | `/etc/viber` | Where the daemons' env files live |
| `VIBERAYD_BIN` / `VIBEROXY_BIN` | `viberayd` / `viberoxy` | Daemon binaries to supervise |
| `CONSOLE_TOKEN` | (empty) | Bearer/API token. Startup fails if empty on a non-loopback listener; all `/api/*` routes except `/api/health` require it when configured |
| `CONSOLE_LOG_LEVEL` | `info` | `info` or `debug` |

## API

| Endpoint | Description |
|---|---|
| `GET /api/health` | Console liveness |
| `GET /api/overview` | One-call dashboard snapshot |
| `GET /api/viberayd/configs?page=&per_page=&state=` | Paginated config table |
| `GET /api/viberayd/urls` | Subscription URL list |
| `GET /api/viberoxy/metrics` | Raw parsed viberoxy metrics |
| `GET /api/viberoxy/wans` | Per-WAN state proxied from viberoxy's control API |
| `GET /api/viberoxy/candidates` | Working replacement candidates |
| `POST /api/viberoxy/wans/{index}/drop` | Drop and replace one WAN |
| `POST /api/viberoxy/cycle/trigger` | Trigger an immediate candidate cycle |
| `GET /api/config/schema` | Editable env-var schema for both daemons |
| `GET/POST /api/config/values` | Read / update config (atomic write + optional restart) |
| `GET /api/processes` | Lifecycle-aware service status; monitor mode reports `starting`, `running`, or `unreachable` without PID/restart controls |
| `POST /api/control/restart` | Restart a service (`viberayd`/`viberoxy`/`all`) |

In `monitor` mode all daemon/config mutations return HTTP `409`; read-only
status and configuration endpoints remain available. API clients can send the
token as `Authorization: Bearer <token>` or `X-Console-Token: <token>`.
Browsers can use HTTP Basic authentication with any username and the token as
the password. Put TLS in front of every non-loopback deployment; all supported
header-based methods expose credentials on plaintext HTTP.

## Structure

```
cmd/console/            # entry point: env parsing, wiring, supervisor boot
internal/collector/     # HTTP client, Prometheus parser, viberayd/viberoxy clients
internal/config/        # env-var schema, atomic env-file store, validation
internal/dashboard/     # poll store, /api handlers, control API, embedded SPA
internal/supervisor/    # child-process manager (start/restart/stop, stderr tail)
docs/                   # data-contract.md + frontend-design.md
scripts/                # smoke.sh (backend), ui-smoke.sh / ui-stack.sh (WebUI)
```

## Development

```bash
go build ./...
go test ./...
go test -race ./...
node --check internal/dashboard/static/app.js
bash scripts/ui-smoke.sh   # boots mock daemons + console, curls SPA + API
```

## License

GPL-3.0 (matches the viber family).
