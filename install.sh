#!/usr/bin/env bash
# MIRAGE server installer for Ubuntu/Debian VPS.
# Run as root: bash install.sh
set -euo pipefail

INSTALL_DIR=/opt/miraged
SERVICE=miraged
GO_VERSION=1.23.4
ARCH=$(dpkg --print-architecture 2>/dev/null || echo amd64)

# ── 1. Install Go if needed ──────────────────────────────────────────────────
need_go() {
    command -v go &>/dev/null || return 0
    local v; v=$(go version | awk '{print $3}' | tr -d 'go')
    [[ "$v" < "$GO_VERSION" ]]
}
if need_go; then
    echo "[*] Installing Go ${GO_VERSION}..."
    cd /tmp
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${ARCH}.tar.gz"
    export PATH="$PATH:/usr/local/go/bin"
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
fi
export PATH="$PATH:/usr/local/go/bin"
echo "[+] $(go version)"

# ── 2. Build ─────────────────────────────────────────────────────────────────
SRC="$(cd "$(dirname "$0")" && pwd)"
echo "[*] Building from ${SRC}..."
cd "$SRC"
go mod tidy
go build -trimpath -ldflags="-s -w" -o miraged ./cmd/miraged
echo "[+] Build OK"

# ── 3. Install ───────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"
cp miraged "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/miraged"

if [[ ! -f "$INSTALL_DIR/config.json" ]]; then
    echo "[*] Generating keys..."
    "$INSTALL_DIR/miraged" -genkey | tee /tmp/mirage_keygen.txt
    echo ""
    echo "┌────────────────────────────────────────────────────────────────────┐"
    echo "│  Copy the client.json fragment above into your Windows client.     │"
    echo "│  Then paste the server config into $INSTALL_DIR/config.json        │"
    echo "│  and replace YOUR_VPS_IP in client.json with this server's IP.     │"
    echo "└────────────────────────────────────────────────────────────────────┘"
fi

# ── 4. systemd unit ──────────────────────────────────────────────────────────
cat > "/etc/systemd/system/${SERVICE}.service" <<EOF
[Unit]
Description=MIRAGE anti-censorship proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/miraged -c ${INSTALL_DIR}/config.json
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE"

echo ""
echo "[+] Installed. Next steps:"
echo "    1. Edit   $INSTALL_DIR/config.json"
echo "    2. Run:   systemctl start $SERVICE"
echo "    3. Watch: journalctl -u $SERVICE -f"
