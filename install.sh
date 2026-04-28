#!/usr/bin/env bash
set -euo pipefail

# ---------------------------------------------------------------------------
# MIRAGE server installer
# Usage:
#   bash install.sh [PORT]
# Environment overrides (all optional):
#   DOMAIN        Domain name for ACME/Let's Encrypt certificate.
#                 Leave unset to be prompted, or set to "" to skip ACME.
#   PORT          Listen port (overrides interactive selection and $1).
#   PUBLIC_HOST   Public IP or domain for the generated client URI.
#   FALLBACK_ADDR Fallback target for unauthenticated connections.
#   INSTALL_DIR   Installation directory (default: /opt/miraged).
#   USER_NAME     First user name (default: user1).
#   PROFILE_NAME  Profile name embedded in the mirage:// URI.
# ---------------------------------------------------------------------------

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
PROFILE_NAME=${PROFILE_NAME:-my-vps-1}
USER_NAME=${USER_NAME:-user1}
ARCH=$(uname -m)

# ---------------------------------------------------------------------------
# Certificate selection: ACME (Let's Encrypt) or self-signed
# ---------------------------------------------------------------------------

# Allow DOMAIN to be pre-set via environment; prompt only when unset.
if [[ ! -v DOMAIN ]]; then
    echo "" >&2
    echo "  [Optional] Domain name for a trusted ACME/Let's Encrypt certificate." >&2
    echo "  Requirements: DNS A record already points here; port 80 must be free." >&2
    echo "  Leave blank to use a self-signed certificate (cert pin still protects" >&2
    echo "  the connection, but self-signed certs are easier to fingerprint)." >&2
    read -rp "  Domain (e.g. proxy.example.com) [blank = self-signed]: " DOMAIN </dev/tty || true
fi
DOMAIN="${DOMAIN:-}"

USE_ACME=false
SNI_NAME="${SNI_NAME:-}"

if [[ -n "$DOMAIN" ]]; then
    if [[ "$PORT" == "80" ]]; then
        echo "[!] Port 80 is reserved for the ACME HTTP challenge; cannot use ACME" >&2
        echo "    when the MIRAGE listen port is also 80. Falling back to self-signed." >&2
        DOMAIN=""
    else
        USE_ACME=true
        # When using a real cert, SNI defaults to the domain so the TLS
        # handshake looks identical to a real HTTPS connection to that domain.
        : "${SNI_NAME:=$DOMAIN}"
    fi
fi
: "${SNI_NAME:=www.microsoft.com}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

if port_busy "$PORT"; then
    echo "[!] Port ${PORT} is already occupied." >&2
    ss -ltnp | grep ":${PORT}" || true
    exit 1
fi

if [[ "$USE_ACME" == "true" ]] && port_busy 80; then
    echo "[!] Port 80 is required for the ACME HTTP challenge but is already" >&2
    echo "    in use. Free port 80 or re-run without a domain name." >&2
    ss -ltnp | grep ":80" || true
    exit 1
fi

# ---------------------------------------------------------------------------
# Install Go if needed
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

echo "[*] Building miraged from ${SRC}"
cd "$SRC"
go mod tidy
go build -trimpath -ldflags="-s -w" -o miraged ./cmd/miraged

mkdir -p "$INSTALL_DIR"
cp miraged "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/miraged"

# ---------------------------------------------------------------------------
# ACME certificate (placed before bootstrap so bootstrap computes the
# correct SPKI pin for the real cert and embeds it in the mirage:// URI)
# ---------------------------------------------------------------------------

CERT_PATH="$INSTALL_DIR/mirage-cert.pem"
KEY_PATH="$INSTALL_DIR/mirage-key.pem"

if [[ "$USE_ACME" == "true" ]]; then
    echo "[*] Obtaining ACME certificate for ${DOMAIN}..."

    if ! command -v certbot >/dev/null 2>&1; then
        echo "[*] certbot not found, attempting to install..."
        if command -v apt-get >/dev/null 2>&1; then
            apt-get install -y certbot
        elif command -v yum >/dev/null 2>&1; then
            yum install -y certbot
        elif command -v snap >/dev/null 2>&1; then
            snap install --classic certbot
            ln -sf /snap/bin/certbot /usr/bin/certbot
        else
            echo "[!] Could not install certbot automatically." >&2
            echo "    Install certbot manually and re-run, or leave domain blank." >&2
            exit 1
        fi
    fi

    certbot certonly \
        --standalone \
        --non-interactive \
        --agree-tos \
        --register-unsafely-without-email \
        -d "$DOMAIN"

    cp "/etc/letsencrypt/live/${DOMAIN}/fullchain.pem" "$CERT_PATH"
    cp "/etc/letsencrypt/live/${DOMAIN}/privkey.pem"   "$KEY_PATH"
    chmod 600 "$KEY_PATH"
    echo "[+] ACME certificate obtained and installed."
fi

# ---------------------------------------------------------------------------
# Bootstrap: generate config.json, client.json, and mirage:// URI.
# certutil.LoadOrGenerate will use the ACME cert if already present above,
# or generate a self-signed cert otherwise.
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# systemd service
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# ACME renewal hook: keeps cert files in sync after auto-renewal
# ---------------------------------------------------------------------------

if [[ "$USE_ACME" == "true" ]]; then
    HOOK_DIR="/etc/letsencrypt/renewal-hooks/deploy"
    mkdir -p "$HOOK_DIR"
    cat > "${HOOK_DIR}/miraged.sh" <<HOOK
#!/usr/bin/env bash
# Auto-generated by MIRAGE install.sh — do not edit manually.
set -euo pipefail
LIVE="/etc/letsencrypt/live/${DOMAIN}"
cp "\${LIVE}/fullchain.pem" "${CERT_PATH}"
cp "\${LIVE}/privkey.pem"   "${KEY_PATH}"
chmod 600 "${KEY_PATH}"
systemctl reload ${SERVICE} 2>/dev/null || systemctl restart ${SERVICE}
HOOK
    chmod +x "${HOOK_DIR}/miraged.sh"
    echo "[+] Certbot renewal hook installed: ${HOOK_DIR}/miraged.sh"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo
echo "[+] Installed and started."
echo "    Config : ${INSTALL_DIR}/config.json"
echo "    Client : ${INSTALL_DIR}/client.json"
echo "    Cert   : ${CERT_PATH}"
if [[ "$USE_ACME" == "true" ]]; then
    echo "    CA     : Let's Encrypt (${DOMAIN})"
else
    echo "    CA     : self-signed (cert pin provides authentication)"
fi
echo "    Logs   : journalctl -u ${SERVICE} -f"
echo

if [[ -n "$MIRAGE_URI" ]]; then
    echo "============================================================"
    echo "MIRAGE IMPORT URI"
    echo "============================================================"
    echo "$MIRAGE_URI"
    echo "============================================================"
    echo
    echo "Paste this URI into miragec (the on-screen wizard accepts it directly)."
else
    echo "[!] Could not extract mirage:// URI. Check ${INSTALL_DIR}/bootstrap.txt" >&2
    exit 1
fi
