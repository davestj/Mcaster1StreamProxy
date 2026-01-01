# CLAUDE.md — Mcaster1StreamProxy

**Project:** Mcaster1StreamProxy — High-performance Go ICY Stream Proxy
**Owner:** MCaster1 LLC / David St John <davestj@mcaster1.com>
**Source root:** `/var/www/mcaster1.com/Mcaster1StreamProxy/`
**Language:** Go 1.23+
**Companion project:** Mcaster1YPMan (C++ YP daemon + PHP web frontend)

---

## What This Service Does

Proxies HTTP audio/video streams through HTTPS to eliminate browser mixed-content blocks.
Implements ICY 1.x metadata strip protocol — connects upstream as a Winamp-style
media player, strips inline metadata, forwards clean audio to the client.

Designed to replace the PHP `proxy-stream.php` for high-concurrency scenarios
(500–5,000+ concurrent listeners). Uses goroutines (~8KB each) instead of PHP-FPM
workers (~50MB each).

---

## MySQL CLI Rule

```bash
mysql --defaults-extra-file=~/.my.cnf -e "SHOW DATABASES LIKE '%yp%';"
mysql --defaults-extra-file=~/.my.cnf casterclub_xiph_yp -e "SELECT COUNT(*) FROM ypman_proxy_stats;"
```

---

## Build

```bash
cd /var/www/mcaster1.com/Mcaster1StreamProxy
make          # → build/mcaster1-stream-proxy
make test     # run unit tests
make run      # build + run with etc/config.yaml
make fmt      # format Go source
make vet      # static analysis
make install  # install to /usr/local/mcaster1/
```

---

## Running

```bash
# Systemd (production)
sudo systemctl start mcaster1-stream-proxy
sudo systemctl status mcaster1-stream-proxy
sudo systemctl restart mcaster1-stream-proxy

# Manual (development)
make run

# Health check
curl -sk https://127.0.0.1:9877/health
```

---

## Config

**Path:** `etc/config.yaml`

Key settings:
- `server.listen_port: 9877` — HTTPS streaming proxy port
- `server.tls_cert/tls_key` — wildcard CasterClub certs (`/etc/ssl/casterclub/`)
- `server.max_concurrent_streams: 5000` — concurrency semaphore limit
- `database.*` — same MariaDB credentials as Mcaster1YPMan
- `proxy.buffer_small: 16384` / `medium: 32768` / `large: 57344` / `high: 65536`
- `proxy.request_icy_metadata: true` — always request + strip ICY metadata

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/stream?id=N[&buf=small\|medium\|large\|high]` | Stream proxy (audio) |
| GET | `/stream?id=N&meta=1` | SSE metadata side-channel |
| GET | `/health` | JSON health + active stream count + uptime |
| GET | `/stats` | JSON proxy statistics |

### Buffer Tiers

| Tier | Size | Use Case |
|------|------|----------|
| `small` | 16 KB | Instant start, strong connections |
| `medium` | 32 KB | Balanced (default) |
| `large` | 56 KB | Moderate buffering |
| `high` | 64 KB | Maximum stability |

---

## Integration with Mcaster1YPMan

- Logs to the **same `ypman_proxy_stats` table** as PHP proxy-stream.php
- `conn_id` prefix: `go_` (vs `php_` for PHP proxy)
- Dashboard `proxy-stats.php` shows both Go and PHP proxy connections
- nginx routes `/stream` to Go proxy, `/proxy-stream.php` to PHP (fallback)
- YPMan admin panel: start/stop/restart via `web/app/api/go_proxy.php`
- Passwordless sudo: `/etc/sudoers.d/mcaster1-stream-proxy`

---

## Database

**Database:** `casterclub_xiph_yp`
**Credentials:** user=`DUMMY_MARIADB_USER_SET_VIA_VAULT` pass=`DUMMY_MARIADB_PWD_SET_VIA_VAULT` host=`127.0.0.1`

- Reads from `stations` table (station lookup by ID for upstream URL)
- Writes to `ypman_proxy_stats` (same schema as PHP proxy)
- `buf_tier` ENUM: `'small','medium','large','high'`

---

## ICY Metadata Strip FSM

Three states process arbitrary-length byte chunks from upstream:

1. **AUDIO** — Pass through audio bytes, count down to `metaint` boundary
2. **META_LEN** — Read 1-byte length indicator (`N × 16` = metadata block size)
3. **META_BODY** — Consume metadata bytes, extract `StreamTitle`, invoke `TitleCallback`, return to AUDIO

If upstream sends `icy-metaint: 0` or no metaint header, FSM is bypassed (passthrough mode).

---

## SSRF Protection

`proxy/handler.go` blocks upstream connections to:
- `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
- `::1`, `fe80::/10`, `fc00::/7`
- `169.254.0.0/16` (link-local / AWS metadata)

---

## nginx Configuration

```nginx
# /etc/nginx/sites-enabled/yp.casterclub.com
location /stream {
    proxy_pass https://127.0.0.1:9877;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 7200s;
    proxy_send_timeout 7200s;
    send_timeout 7200s;
    proxy_set_header Accept-Encoding "";
}
```

---

## Systemd Service

**Unit:** `mcaster1-stream-proxy.service`
**User:** `mediacast1`
**Type:** `simple`
**LimitNOFILE:** `65535`
**Restart:** `on-failure`

```bash
# Setup (first time)
sudo bash scripts/systemd-setup.sh

# Management
sudo systemctl start mcaster1-stream-proxy
sudo systemctl stop mcaster1-stream-proxy
sudo systemctl restart mcaster1-stream-proxy
sudo journalctl -u mcaster1-stream-proxy -f
```

---

## Project Structure

```
Mcaster1StreamProxy/
├── cmd/mcaster1-stream-proxy/
│   └── main.go                 — Entry point, signal handling, config loading
├── internal/
│   ├── config/config.go        — YAML config loader + validation + defaults
│   ├── db/db.go                — MariaDB connection pool + LookupStation()
│   ├── icy/
│   │   ├── strip.go            — 3-state ICY metadata strip FSM
│   │   └── headers.go          — ICY response header parser + ForwardableHeaders()
│   ├── proxy/handler.go        — ServeStream, serveSSEMeta, ServeHealth, ServeStats
│   ├── stats/stats.go          — ypman_proxy_stats DB logger (LogStart/Progress/End)
│   └── server/server.go        — HTTP/TLS server, route wiring, graceful shutdown
├── etc/
│   └── config.yaml             — Runtime configuration (DO NOT COMMIT — contains passwords)
├── scripts/
│   ├── systemd-setup.sh        — Systemd service installer with preflight checks
│   └── mcaster1-stream-proxy.service — Systemd unit template
├── build/                      — Compiled binary output
├── logs/                       — Runtime log files
├── Makefile                    — Build system
├── go.mod / go.sum             — Go module dependencies
├── README.md                   — Public documentation
├── CHANGELOG.md                — Version history
├── LICENSE.md                  — Proprietary license
└── CLAUDE.md                   — This file
```

---

## Key Patterns

```go
// Station lookup
station, err := db.LookupStation(ctx, stationID)

// ICY stripper
stripper := icy.NewStripper(metaint, func(title string) {
    // called on every StreamTitle change
})
stripper.Process(chunk) // returns clean audio bytes

// Stats logging
stats.LogStart(connID, stationID, stationName, mount, ip, ua, bufTier)
stats.LogProgress(connID, bytesSent, durationSecs)
stats.LogEnd(connID, bytesSent, durationSecs, staleClosed)
```

---

## Bugs Fixed

| Bug | File | Fix |
|-----|------|-----|
| PHP curl to Go proxy fails SSL hostname mismatch | proxy-stats.php, go_proxy.php | `CURLOPT_SSL_VERIFYHOST => 0` for localhost with wildcard cert |
