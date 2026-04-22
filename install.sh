#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR=${INSTALL_DIR:-/opt/miraged}
SERVICE=${SERVICE:-miraged}
GO_VERSION=${GO_VERSION:-1.24.3}
LISTEN_ADDR=${LISTEN_ADDR:-0.0.0.0:443}
PUBLIC_HOST=${PUBLIC_HOST:-}
FALLBACK_ADDR=${FALLBACK_ADDR:-www.microsoft.com:443}
SNI_NAME=${SNI_NAME:-www.microsoft.com}
PROFILE_NAME=${PROFILE_NAME:-my-vps-1}
USER_NAME=${USER_NAME:-user1}
ARCH=$(uname -m)

need_go() {
    if ! command -v go >/dev/null 2>&1; then
        return 0
    fi
    local have
    have=$(go version | awk '{print $3}' | sed 's/^go//')
    [[ "$have" < "$GO_VERSION" ]]
}

go_arch() {
    case "$ARCH" in
        x86_64|amd64) echo amd64 ;;
        aarch64|arm64) echo arm64 ;;
        *) echo unsupported ;;
    esac
}

if need_go; then
    TAR_ARCH=$(go_arch)
    if [[ "$TAR_ARCH" == unsupported ]]; then
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
    fi
    echo "[*] Installing Go ${GO_VERSION}..."
    cd /tmp
    curl -fsSLO "https://go.dev/dl/go${GO_VERSION}.linux-${TAR_ARCH}.tar.gz"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TAR_ARCH}.tar.gz"
    export PATH=/usr/local/go/bin:$PATH
    if ! grep -q '/usr/local/go/bin' /root/.bashrc 2>/dev/null; then
        echo 'export PATH=/usr/local/go/bin:$PATH' >> /root/.bashrc
    fi
fi

export PATH=/usr/local/go/bin:/snap/bin:$PATH
echo "[+] $(go version)"

SRC="$(cd "$(dirname "$0")" && pwd)"
echo "[*] Building from ${SRC}..."
cd "$SRC"
go mod tidy
go build -trimpath -ldflags="-s -w" -o miraged ./cmd/miraged
echo "[+] Build OK"

mkdir -p "$INSTALL_DIR"
cp miraged "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/miraged"

echo "[*] Running bootstrap..."
BOOTSTRAP_ARGS=(
    -bootstrap
    -bootstrap-dir "$INSTALL_DIR"
    -listen "$LISTEN_ADDR"
    -fallback "$FALLBACK_ADDR"
    -sni "$SNI_NAME"
    -name "$PROFILE_NAME"
    -user "$USER_NAME"
    -overwrite
)
if [[ -n "$PUBLIC_HOST" ]]; then
    BOOTSTRAP_ARGS+=(-public-host "$PUBLIC_HOST")
fi

"$INSTALL_DIR/miraged" "${BOOTSTRAP_ARGS[@]}" | tee "$INSTALL_DIR/bootstrap.txt"

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

echo
echo "[+] Installed."
echo "    Config:      ${INSTALL_DIR}/config.json"
echo "    Client:      ${INSTALL_DIR}/client.json"
echo "    Bootstrap:   ${INSTALL_DIR}/bootstrap.txt"
echo "    Start with:  systemctl start ${SERVICE}"
echo "    Logs with:   journalctl -u ${SERVICE} -f"
