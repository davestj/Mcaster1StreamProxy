# Changelog

All notable changes to Mcaster1StreamProxy are documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/).

---

## [1.0.0] — 2026-03-11

### Added
- Initial release of Mcaster1StreamProxy — high-performance Go HTTPS streaming proxy
- ICY 1.x metadata strip finite state machine (3-state: AUDIO → META_LEN → META_BODY)
- SSE (Server-Sent Events) metadata side-channel at `?meta=1` for real-time track title updates
- 4-tier adaptive pre-buffering: small (16KB), medium (32KB), large (56KB), high (64KB)
- Concurrency semaphore limiting max concurrent streams (default: 5,000)
- SSRF protection — blocks proxy requests to private/loopback IP ranges
- MariaDB integration — logs all sessions to `ypman_proxy_stats` table (shared with PHP proxy)
- Connection ID prefix `go_` to distinguish from PHP proxy sessions (`php_`)
- TLS support using CasterClub wildcard certificates
- Health endpoint (`/health`) returning JSON with active streams, max capacity, uptime
- Stats endpoint (`/stats`) returning JSON proxy statistics
- ICY response header parser with forwardable header filtering
- Client IP extraction from `X-Real-IP` / `X-Forwarded-For` / direct connection
- Graceful shutdown with configurable drain timeout
- Systemd service unit with auto-restart, LimitNOFILE=65535
- Setup script (`scripts/systemd-setup.sh`) for automated service installation
- Makefile with build, test, install, run, fmt, vet targets
- YAML configuration (`etc/config.yaml`) with validation and sensible defaults

### Integration
- nginx reverse proxy routing: `/stream` → Go proxy (port 9877)
- PHP fallback preserved at `/proxy-stream.php` for URL-based streams
- Mcaster1YPMan dashboard integration: health metrics, Go/PHP session breakdown, start/stop/restart controls
- API endpoint `go_proxy.php` for service management via YPMan admin panel
- Passwordless sudo entry for `systemctl start/stop/restart/status mcaster1-stream-proxy`

### Performance
- Goroutine-per-listener model: ~8KB per stream vs ~50MB per PHP-FPM worker
- Designed for 500–5,000+ concurrent listeners on a single server
- Atomic counters for thread-safe statistics without mutex contention
- Connection pool for MariaDB with configurable max open connections
