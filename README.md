# viber-console

Control plane for the [viber stack](https://github.com/amirrezaalavi): bundles
[Viberayd](https://github.com/amirrezaalavi/Viberayd) (subscription aggregator)
and [Viberoxy](https://github.com/amirrezaalavi/Viberoxy) (proxy server) and
provides a dashboard backend that aggregates both into one API.

> **Status:** backend only. The WebUI frontend is deliberately deferred until
> the data contract (docs/data-contract.md) is reviewed. This repo will also
> gain the installation bundle (systemd units, compose) in a later phase.

## What it does

- Polls **Viberayd** (`/api/stats`, `/api/configs`, `/api/urls`) — the JSON API
- Polls **Viberoxy** (`/metrics`, `/healthz`, `/readyz`) — Prometheus text,
  parsed stdlib-only (no dependencies)
- Serves an aggregated JSON API for the future frontend
- Degrades gracefully: a dead daemon shows `"reachable": false` instead of
  erroring

## Quick start

```bash
go build -o build/console ./cmd/console
CONSOLE_LISTEN=:8090 \
VIBERAYD_API_URL=http://127.0.0.1:8081 \
VIBEROXY_METRICS_URL=http://127.0.0.1:9090 \
./build/console
```

Requires Viberayd (`HTTP_ENABLED=true`) and Viberoxy (`METRICS_PORT` set) to
be running.

## Configuration (env vars)

| Variable | Default | Description |
|---|---|---|
| `CONSOLE_LISTEN` | `:8090` | Console HTTP listen address |
| `CONSOLE_POLL_INTERVAL` | `10s` | How often daemons are polled |
| `VIBERAYD_API_URL` | `http://127.0.0.1:8081` | Viberayd management API |
| `VIBERAYD_SUB_URL` | `http://127.0.0.1:8080` | Viberayd subscription endpoint (informational) |
| `VIBEROXY_METRICS_URL` | `http://127.0.0.1:9090` | Viberoxy metrics/health |
| `CONSOLE_LOG_LEVEL` | `info` | `info` or `debug` |

## API

| Endpoint | Description |
|---|---|
| `GET /api/health` | Console liveness |
| `GET /api/overview` | One-call dashboard snapshot: viberayd stats + viberoxy WANs/proxy/latency percentiles |
| `GET /api/viberayd/configs?page=&per_page=&state=` | Paginated config table (state filter optional) |
| `GET /api/viberayd/urls` | Subscription URL list |
| `GET /api/viberoxy/metrics` | Raw parsed viberoxy metrics view |

See [docs/data-contract.md](docs/data-contract.md) for the full schema and
design decisions.

## Structure

```
cmd/console/            # entry point: env parsing, wiring, server
internal/collector/     # HTTP client, Prometheus parser, viberayd/viberoxy clients
internal/dashboard/     # poll store + /api handlers
docs/data-contract.md   # dashboard data contract (frontend spec)
scripts/smoke.sh        # end-to-end smoke test against mock daemons
```

## Development

```bash
go build ./...
go test ./...
go test -race ./...
bash scripts/smoke.sh   # boots mock daemons + real binary, curls endpoints
```

## License

GPL-3.0 (matches the viber family).
