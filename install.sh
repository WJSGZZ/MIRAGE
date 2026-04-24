#!/usr/bin/env bash
set -euo pipefail

select_port() {
    if [[ -n "${1:-}" ]]; then
        echo "$1"
        return
    fi
    echo "" >&2
    echo "  Select listen port:" >&2
    echo "    1) 443   standard HTTPS" >&2
    echo "    2) 8443  alternative TLS port (recommended if 443 is occupied)" >&2
    echo "    3) 80    HTTP port" >&2
    echo "    4) Custom" >&2
    read -rp "  Choice [2]: " choice </dev/tty
    case "${choice:-2}" in
        1) echo 443 ;;
        2) echo 8443 ;;
        3) echo 80 ;;
        4) read -rp "  Enter port: " p </dev/tty; echo "$p" ;;
        *) echo 8443 ;;
    esac
}

PORT=$(select_port "${1:-}")
LISTEN_ADDR=${LISTEN_ADDR:-"0.0.0.0:${PORT}"}
INSTALL_DIR=${INSTALL_DIR:-/opt/miraged}
SERVICE=${SERVICE:-miraged}
GO_VERSION=${GO_VERSION:-1.24.3}
PUBLIC_HOST=${PUBLIC_HOST:-}
FALLBACK_ADDR=${FALLBACK_ADDR:-www.microsoft.com:443}
SNI_NAME=${SNI_NAME:-www.microsoft.com}
PROFILE_NAME=${PROFILE_NAME:-my-vps-1}
USER_NAME=${USER_NAME:-user1}
ARCH=$(uname -m)

go_arch() {
    case "$ARCH" in
        x86_64|amd64) echo amd64 ;;
        aarch64|arm64) echo arm64 ;;
        *) echo unsupported ;;
    esac
}

need_go() {
    if ! command -v go >/dev/null 2>&1; then
        return 0
    fi
    local have
    have=$(go version | awk '{print $3}' | sed 's/^go//')
    [[ "$have" < "$GO_VERSION" ]]
}

detect_public_host() {
    if [[ -n "$PUBLIC_HOST" ]]; then
        echo "$PUBLIC_HOST"
        return
    fi
    curl -4fsS --max-time 5 https://api.ipify.org || hostname -I | awk '{print $1}'
}

port_busy() {
    local port="$1"
    ss -ltn "( sport = :${port} )" | grep -q ":${port}"
}

if port_busy "$PORT"; then
    echo "[!] Port ${PORT} is already occupied." >&2
    ss -ltnp | grep ":${PORT}" || true
    exit 1
fi

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
fi

export PATH=/usr/local/go/bin:/snap/bin:$PATH
echo "[+] $(go version)"

SRC="$(cd "$(dirname "$0")" && pwd)"
PUBLIC_HOST=$(detect_public_host)

echo "[*] Building miraged from ${SRC}"
cd "$SRC"
go mod tidy
go build -trimpath -ldflags="-s -w" -o miraged ./cmd/miraged

mkdir -p "$INSTALL_DIR"
cp miraged "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/miraged"

BOOTSTRAP_ARGS=(
    -bootstrap
    -bootstrap-dir "$INSTALL_DIR"
    -listen "$LISTEN_ADDR"
    -public-host "$PUBLIC_HOST"
    -fallback "$FALLBACK_ADDR"
    -sni "$SNI_NAME"
    -name "$PROFILE_NAME"
    -user "$USER_NAME"
    -overwrite
)

echo "[*] Generating server config and import URI..."
BOOTSTRAP_OUT=$("$INSTALL_DIR/miraged" "${BOOTSTRAP_ARGS[@]}" 2>&1)
echo "$BOOTSTRAP_OUT"
echo "$BOOTSTRAP_OUT" > "$INSTALL_DIR/bootstrap.txt"
MIRAGE_URI=$(grep '^mirage://' "$INSTALL_DIR/bootstrap.txt" | tail -n 1 || true)

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
systemctl restart "$SERVICE"

echo
echo "[+] Installed and started."
echo "    Config : ${INSTALL_DIR}/config.json"
echo "    Client : ${INSTALL_DIR}/client.json"
echo "    Logs   : journalctl -u ${SERVICE} -f"
echo

if [[ -n "$MIRAGE_URI" ]]; then
    echo "============================================================"
    echo "MIRAGE IMPORT URI"
    echo "============================================================"
    echo "$MIRAGE_URI"
    echo "============================================================"
    echo
    echo "Use this URI in MirageClient.Tauri Profiles -> Import."
else
    echo "[!] Could not extract mirage:// URI. Check ${INSTALL_DIR}/bootstrap.txt" >&2
    exit 1
fi
