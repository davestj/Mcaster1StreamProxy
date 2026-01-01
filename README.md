# Mcaster1StreamProxy

**High-performance HTTPS streaming proxy for internet radio.**

Mcaster1StreamProxy is a Go-based streaming proxy that routes HTTP audio/video streams through HTTPS, eliminating browser mixed-content blocks. It implements the ICY 1.x metadata strip protocol — connecting to upstream Icecast/Shoutcast/Steamcast servers as a standard media player, stripping inline metadata, and forwarding clean audio to listeners over TLS.

Built for scale: handles 500–5,000+ concurrent listeners using Go goroutines (~8KB each) instead of traditional PHP-FPM workers (~50MB each). Part of the [CasterClub](https://casterclub.com) streaming platform.

---

## Features

- **HTTPS stream proxy** — Transparently wraps HTTP radio streams in TLS for secure browser playback
- **ICY metadata strip** — 3-state finite state machine strips ICY 1.x in-stream metadata blocks from audio data
- **SSE metadata side-channel** — Real-time Server-Sent Events endpoint for live track title updates (`?meta=1`)
- **4-tier adaptive buffering** — Configurable pre-buffer sizes (small 16KB, medium 32KB, large 56KB, high 64KB) for instant-to-rock-solid playback
- **Concurrency semaphore** — Configurable max concurrent streams with graceful rejection
- **MariaDB integration** — Logs all proxy sessions to `ypman_proxy_stats` for analytics and monitoring
- **TLS with wildcard certs** — Uses existing CasterClub wildcard certificates
- **Health & stats endpoints** — `/health` and `/stats` for monitoring and dashboards
- **SSRF protection** — Blocks requests to private/loopback IP ranges
- **Systemd service** — Production-ready with auto-restart, file descriptor limits, and graceful shutdown

---

## Quick Start

### Build

```bash
cd /var/www/mcaster1.com/Mcaster1StreamProxy
make          # builds to build/mcaster1-stream-proxy
```

### Configure

Edit `etc/config.yaml` with your database credentials, TLS certificate paths, and proxy settings.

### Run

```bash
make run                            # development: build + run
sudo systemctl start mcaster1-stream-proxy   # production: systemd
```

### Test

```bash
# Health check
curl -sk https://127.0.0.1:9877/health

# Stream a station (5 seconds)
timeout 5 curl -sk 'https://yp.casterclub.com/stream?id=7003&buf=small' -o test.bin
file test.bin   # should show audio format (MPEG ADTS, AAC, etc.)

# SSE metadata
curl -sk 'https://yp.casterclub.com/stream?id=7003&meta=1'
```

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/stream?id=N[&buf=small\|medium\|large\|high]` | HTTPS audio stream proxy |
| `GET` | `/stream?id=N&meta=1` | SSE real-time metadata side-channel |
| `GET` | `/health` | JSON health check + active stream count |
| `GET` | `/stats` | JSON proxy statistics |

### Buffer Tiers

| Tier | Size | Best For |
|------|------|----------|
| `small` | 16 KB | Instant playback, strong connections |
| `medium` | 32 KB | Balanced startup/stability (default) |
| `large` | 56 KB | Moderate buffering for weaker connections |
| `high` | 64 KB | Maximum stability, never drops |

---

## Architecture

```
Browser ──HTTPS──▶ nginx ──proxy_pass──▶ Mcaster1StreamProxy (port 9877)
                                              │
                                              ├── Upstream HTTP fetch (libcurl-style)
                                              ├── ICY metadata strip FSM
                                              ├── Pre-buffer N bytes
                                              ├── Forward clean audio over TLS
                                              └── Log to ypman_proxy_stats (MariaDB)
```

### ICY Metadata Strip FSM

Three states process arbitrary-length chunks from upstream:

1. **AUDIO** — Pass through audio bytes, count down to `metaint`
2. **META_LEN** — Read 1-byte length indicator (`N × 16` = metadata block size)
3. **META_BODY** — Consume metadata bytes, extract `StreamTitle`, invoke callback, return to AUDIO

---

## Project Structure

```
Mcaster1StreamProxy/
├── cmd/mcaster1-stream-proxy/
│   └── main.go                 — Entry point, signal handling, config loading
├── internal/
│   ├── config/config.go        — YAML config loader + validation + defaults
│   ├── db/db.go                — MariaDB connection pool + station lookup
│   ├── icy/
│   │   ├── strip.go            — 3-state ICY metadata strip FSM
│   │   └── headers.go          — ICY response header parser
│   ├── proxy/handler.go        — Stream proxy + SSE metadata + health/stats handlers
│   ├── stats/stats.go          — ypman_proxy_stats DB logger (mirrors PHP schema)
│   └── server/server.go        — HTTP/TLS server, route wiring, graceful shutdown
├── etc/
│   └── config.yaml             — Runtime configuration
├── scripts/
│   ├── systemd-setup.sh        — Systemd service installer
│   └── mcaster1-stream-proxy.service — Systemd unit template
├── build/                      — Compiled binary output
├── logs/                       — Runtime log files
├── Makefile                    — Build system (build, test, install, run, fmt, vet)
├── go.mod                      — Go module definition
├── CLAUDE.md                   — AI assistant context
├── README.md                   — This file
├── CHANGELOG.md                — Version history
└── LICENSE.md                  — Proprietary license
```

---

## Configuration

`etc/config.yaml` — key settings:

```yaml
server:
  listen_port: 9877
  tls_cert: /etc/ssl/casterclub/fullchain_casterclub_com.pem
  tls_key: /etc/ssl/casterclub/casterclub-wildcard.key
  max_concurrent_streams: 5000

database:
  host: 127.0.0.1
  port: 3306
  user: DUMMY_MARIADB_USER_SET_VIA_VAULT
  database: casterclub_xiph_yp
  max_open_conns: 20

proxy:
  buffer_small: 16384
  buffer_medium: 32768
  buffer_large: 57344
  buffer_high: 65536
  request_icy_metadata: true
```

---

## Integration with Mcaster1YPMan

Mcaster1StreamProxy is tightly integrated with the [Mcaster1YPMan](https://github.com/mcaster1/Mcaster1YPMan) C++ YP daemon and PHP web frontend:

- **Shared database** — Reads station data from `stations` table, writes proxy stats to `ypman_proxy_stats`
- **Connection ID prefix** — Go sessions use `go_` prefix (vs `php_` for PHP fallback proxy)
- **Dashboard integration** — `proxy-stats.php` shows Go proxy health, active streams, Go/PHP breakdown, start/stop/restart controls
- **nginx routing** — `/stream` → Go proxy, `/proxy-stream.php` → PHP (automatic fallback)
- **Systemd management** — Start/stop/restart via YPMan admin dashboard API

---

## Requirements

- Go 1.23+
- MariaDB 10.6+ / MySQL 8.0+
- TLS certificate and key
- Linux (systemd for production)
- nginx (reverse proxy, optional)

---

## License

Proprietary — MCaster1 LLC. See [LICENSE.md](LICENSE.md).

---

## Author

**David St John** — MCaster1 LLC
- Email: davestj@mcaster1.com
- Web: https://casterclub.com
