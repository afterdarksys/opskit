#!/bin/bash
# install.sh — Install statsagent on Rocky Linux, Debian, or Ubuntu
# Usage: sudo bash install.sh [--port 9100] [--interval 15s] [--labels dc=iad1,env=prod]
#
# Downloads or uses a local binary, writes a systemd unit, and starts the service.

set -euo pipefail

# ── Config ──────────────────────────────────────────────────────────────────
SERVICE_NAME="statsagent"
INSTALL_DIR="/usr/local/bin"
UNIT_DIR="/etc/systemd/system"
BINARY_SRC="${STATSAGENT_BIN:-}"  # set to path of local binary, or we'll build/download

PORT=9100
INTERVAL="15s"
LABELS=""
UNINSTALL=false

# ── Args ─────────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --port)      PORT="$2";     shift 2 ;;
        --interval)  INTERVAL="$2"; shift 2 ;;
        --labels)    LABELS="$2";   shift 2 ;;
        --bin)       BINARY_SRC="$2"; shift 2 ;;
        --uninstall) UNINSTALL=true; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# ── Must run as root ──────────────────────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    echo "ERROR: This script must be run as root (use sudo)."
    exit 1
fi

# ── Detect distro ─────────────────────────────────────────────────────────────
detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        echo "${ID:-unknown}"
    else
        echo "unknown"
    fi
}

DISTRO=$(detect_distro)
echo "Detected distro: $DISTRO"

# ── Uninstall ─────────────────────────────────────────────────────────────────
if $UNINSTALL; then
    echo "Uninstalling $SERVICE_NAME..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "$UNIT_DIR/$SERVICE_NAME.service"
    rm -f "$INSTALL_DIR/$SERVICE_NAME"
    systemctl daemon-reload
    echo "✓ $SERVICE_NAME uninstalled"
    exit 0
fi

# ── Locate or build binary ────────────────────────────────────────────────────
DEST_BIN="$INSTALL_DIR/$SERVICE_NAME"

if [ -n "$BINARY_SRC" ] && [ -f "$BINARY_SRC" ]; then
    echo "Using local binary: $BINARY_SRC"
    cp "$BINARY_SRC" "$DEST_BIN"
elif command -v go &>/dev/null && [ -f "$(dirname "$0")/../go.mod" ]; then
    echo "Building from source..."
    cd "$(dirname "$0")/.."
    CGO_ENABLED=0 go build -ldflags="-s -w" -o "$DEST_BIN" .
else
    echo "ERROR: No binary found. Either:"
    echo "  - Set STATSAGENT_BIN=/path/to/statsagent"
    echo "  - Run from the source directory with Go installed"
    exit 1
fi

chmod 755 "$DEST_BIN"
echo "Installed binary: $DEST_BIN"

# ── Write systemd unit ────────────────────────────────────────────────────────
ENV_BLOCK="Environment=STATSAGENT_PORT=$PORT
Environment=STATSAGENT_INTERVAL=$INTERVAL"

if [ -n "$LABELS" ]; then
    ENV_BLOCK="$ENV_BLOCK
Environment=STATSAGENT_LABELS=$LABELS"
fi

# Add Docker socket if available
if [ -S /var/run/docker.sock ]; then
    ENV_BLOCK="$ENV_BLOCK
Environment=STATSAGENT_DOCKER_SOCKET=/var/run/docker.sock"
fi

# Add k3s kubeconfig if present
if [ -f /etc/rancher/k3s/k3s.yaml ]; then
    ENV_BLOCK="$ENV_BLOCK
Environment=STATSAGENT_K3S_KUBECONFIG=/etc/rancher/k3s/k3s.yaml"
fi

cat > "$UNIT_DIR/$SERVICE_NAME.service" <<EOF
[Unit]
Description=StatsAgent - Lightweight System Stats Collector
After=network.target
Wants=network.target

[Service]
Type=simple
User=root
ExecStart=$DEST_BIN serve
Restart=on-failure
RestartSec=5s
$ENV_BLOCK

ProtectSystem=full
ReadWritePaths=/tmp

[Install]
WantedBy=multi-user.target
EOF

echo "Wrote: $UNIT_DIR/$SERVICE_NAME.service"

# ── Enable and start ──────────────────────────────────────────────────────────
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

sleep 2
STATUS=$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || echo "unknown")

if [ "$STATUS" = "active" ]; then
    echo ""
    echo "✓ $SERVICE_NAME is running"
    echo "  Status:  systemctl status $SERVICE_NAME"
    echo "  Logs:    journalctl -u $SERVICE_NAME -f"
    echo "  Metrics: curl http://localhost:$PORT/metrics"
    echo "  JSON:    curl http://localhost:$PORT/stats"
    echo "  Health:  curl http://localhost:$PORT/health"
else
    echo ""
    echo "⚠ $SERVICE_NAME started but status is: $STATUS"
    echo "  Check: journalctl -u $SERVICE_NAME -n 50"
    exit 1
fi
