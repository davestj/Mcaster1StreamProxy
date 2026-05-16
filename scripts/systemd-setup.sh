#!/bin/bash
# systemd-setup.sh — Install and enable Mcaster1StreamProxy as a systemd service
# Owner: MCaster1 LLC / David St John <davestj@mcaster1.com>
# Usage: sudo bash scripts/systemd-setup.sh

set -euo pipefail

PROJECT_ROOT="/var/www/mcaster1.com/Mcaster1StreamProxy"
BINARY="${PROJECT_ROOT}/build/mcaster1-stream-proxy"
CONFIG="${PROJECT_ROOT}/etc/config.yaml"
SERVICE_NAME="mcaster1-stream-proxy"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
RUN_USER="mediacast1"
RUN_GROUP="www-data"
LOG_DIR="${PROJECT_ROOT}/logs"

# ── Preflight checks ────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This script must be run as root (sudo)."
    exit 1
fi

if [[ ! -f "$BINARY" ]]; then
    echo "[ERROR] Binary not found: $BINARY"
    echo "        Run 'make' first to build the project."
    exit 1
fi

if [[ ! -f "$CONFIG" ]]; then
    echo "[ERROR] Config not found: $CONFIG"
    exit 1
fi

echo "=============================================="
echo " Mcaster1StreamProxy — systemd service setup"
echo "=============================================="
echo ""

# ── Stop existing service if running ─────────────────────────────────────────
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[INFO] Stopping existing ${SERVICE_NAME} service..."
    systemctl stop "$SERVICE_NAME"
fi

# ── Kill any stray processes on port 9877 ────────────────────────────────────
STRAY_PID=$(lsof -i :9877 -t 2>/dev/null || true)
if [[ -n "$STRAY_PID" ]]; then
    echo "[INFO] Killing stray process on port 9877 (PID: $STRAY_PID)..."
    kill "$STRAY_PID" 2>/dev/null || true
    sleep 1
fi

# ── Ensure log directory exists with correct ownership ───────────────────────
mkdir -p "$LOG_DIR"
chown "${RUN_USER}:${RUN_GROUP}" "$LOG_DIR"
echo "[OK]   Log directory: $LOG_DIR"

# ── Write the systemd unit file ──────────────────────────────────────────────
cat > "$SERVICE_FILE" << UNIT
[Unit]
Description=Mcaster1StreamProxy — ICY Stream Proxy for CasterClub YP Directory
Documentation=https://yp.casterclub.com:9689/docs/
After=network-online.target mariadb.service
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_GROUP}

ExecStart=${BINARY} -c ${CONFIG}
ExecReload=/bin/kill -HUP \$MAINPID

WorkingDirectory=${PROJECT_ROOT}
Restart=on-failure
RestartSec=5

# Resource limits — tuned for 5,000+ concurrent streams
LimitNOFILE=65535
LimitNPROC=4096

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
UNIT

echo "[OK]   Service file written: $SERVICE_FILE"

# ── Reload systemd and enable ────────────────────────────────────────────────
systemctl daemon-reload
echo "[OK]   systemd daemon reloaded"

systemctl enable "$SERVICE_NAME"
echo "[OK]   Service enabled (starts on boot)"

# ── Start the service ────────────────────────────────────────────────────────
systemctl start "$SERVICE_NAME"
sleep 2

# ── Verify ───────────────────────────────────────────────────────────────────
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "[OK]   Service is RUNNING"
else
    echo "[FAIL] Service failed to start!"
    journalctl -u "$SERVICE_NAME" --no-pager -n 20
    exit 1
fi

# ── Health check ─────────────────────────────────────────────────────────────
HEALTH=$(curl -sk --max-time 5 https://localhost:9877/health 2>/dev/null || echo "FAIL")
if echo "$HEALTH" | grep -q '"status":"ok"'; then
    echo "[OK]   Health check passed: $HEALTH"
else
    echo "[WARN] Health check returned: $HEALTH"
fi

echo ""
echo "=============================================="
echo " Setup complete!"
echo "=============================================="
echo ""
echo " Service commands:"
echo "   sudo systemctl status  ${SERVICE_NAME}"
echo "   sudo systemctl stop    ${SERVICE_NAME}"
echo "   sudo systemctl start   ${SERVICE_NAME}"
echo "   sudo systemctl restart ${SERVICE_NAME}"
echo "   sudo journalctl -fu    ${SERVICE_NAME}"
echo ""
echo " Test URLs:"
echo "   curl -sk https://localhost:9877/health"
echo "   curl -sk https://yp.casterclub.com/stream?id=8442 | file -"
echo ""
