# MIRAGE

MIRAGE is a custom TLS-disguised proxy protocol with a small local bridge for Clash Verge Rev / mihomo.

The recommended client workflow is intentionally simple:

```text
Apps -> Clash Verge Rev system proxy / TUN / rules -> MIRAGE local SOCKS -> MIRAGE VPS
```

MIRAGE no longer tries to be a full desktop proxy client. Clash Verge Rev provides the mature UI, rules, system proxy, TUN, logs, and traffic capture. MIRAGE only provides the custom protocol core and a local Clash-compatible subscription.

## Components

- `miraged`: Linux VPS server.
- `miragec`: Windows local bridge/core.
- Clash Verge Rev / mihomo: desktop client, system proxy, TUN, rules, and UI.

## Safety Defaults

In the recommended Clash bridge flow, `miragec` does not modify Windows proxy state:

- It does not set Windows System Proxy.
- It does not set WinHTTP proxy.
- It does not write user `HTTP_PROXY`, `HTTPS_PROXY`, or `ALL_PROXY` environment variables.
- It only listens on local loopback ports and serves a local subscription.

Traffic capture is handled by Clash Verge Rev. This keeps MIRAGE small and avoids unexpected changes to your existing v2rayN, Clash, or Windows proxy configuration.

## VPS Install

Run as root on the VPS:

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 8443
```

The installer will:

- install Go if needed,
- build `miraged`,
- generate cert/key/config material,
- install and start a `miraged` systemd service,
- print a final `mirage://` import URI.

Useful variants:

```bash
bash install.sh 443
PUBLIC_HOST=1.2.3.4 bash install.sh 8443
FALLBACK_ADDR=www.microsoft.com:443 SNI_NAME=www.microsoft.com bash install.sh 8443
```

Check server status:

```bash
systemctl status miraged --no-pager -l
journalctl -u miraged -f -l
```

If you regenerate server config, use the new `mirage://` URI. Old URIs can fail with `uid not found`, `SPKI pin mismatch`, or TLS EOF errors.

## Windows + Clash Verge Rev

Build `miragec.exe`:

```powershell
cd D:\Folder\App\MIRAGE
go build -o miragec.exe .\cmd\miragec
```

Start MIRAGE bridge:

```powershell
.\miragec.exe
```

Paste the `mirage://` URI from your VPS installer when prompted. After connection, `miragec` prints:

```text
Clash URL   : http://127.0.0.1:9099/compat/mihomo.yaml
```

In Clash Verge Rev:

1. Add a profile from URL.
2. Use `http://127.0.0.1:9099/compat/mihomo.yaml`.
3. Update the profile after every MIRAGE restart or URI change.
4. Enable Clash Verge Rev System Proxy or TUN.

The generated mihomo profile automatically adds a `DIRECT` rule for the active MIRAGE VPS address. This prevents Clash TUN from routing MIRAGE's own VPS connection back into MIRAGE.

Local endpoints while `miragec` is running:

- SOCKS5: `127.0.0.1:1080`
- HTTP: `127.0.0.1:1081`
- Control API: `http://127.0.0.1:9099`
- Clash Verge / mihomo profile: `http://127.0.0.1:9099/compat/mihomo.yaml`
- v2rayN custom config bridge: `http://127.0.0.1:9099/compat/v2rayn.json`

## CLI Modes

Interactive import and bridge mode:

```powershell
.\miragec.exe
```

Scripted import and bridge mode:

```powershell
.\miragec.exe -core -servers servers.json -import-uri "mirage://..." -connect-last
```

Run the bridge using existing `servers.json`:

```powershell
.\miragec.exe -core -servers servers.json -dashboard 127.0.0.1:9099 -connect-last
```

Legacy direct local proxy mode:

```powershell
.\miragec.exe -uri "mirage://..."
```

Legacy direct mode also avoids system proxy changes by default. Only use `-set-system-proxy` if you explicitly want MIRAGE itself to write Windows proxy settings.

## Clash Troubleshooting

If Clash imports the profile but traffic fails:

- Refresh the Clash profile URL after restarting `miragec`.
- Open `http://127.0.0.1:9099/compat/mihomo.yaml` and confirm your VPS IP appears before `MATCH,PROXY` as `DIRECT`.
- If logs show `dial <your-vps-ip>:<port>`, the Clash profile is stale or the VPS bypass rule is missing.
- If logs show `SPKI pin mismatch`, the URI cert pin does not match the server certificate.
- If VPS logs show `uid not found`, the URI PSK/user material does not match the current server config.

Basic local checks:

```powershell
curl.exe --socks5-hostname 127.0.0.1:1080 https://www.google.com -I
curl.exe --proxy http://127.0.0.1:1081 https://api.openai.com -I
curl.exe http://127.0.0.1:9099/compat/mihomo.yaml
```

## Repository Layout

```text
cmd/
  miraged/              Linux server entry point
  miragec/              Windows core/headless bridge entry point
internal/
  client/               local SOCKS5/HTTP proxy and MIRAGE dialer
  dashboard/            local control API and compatibility subscriptions
  daemon/               local proxy runtime lifecycle and stats
  server/               server accept/auth/relay runtime
  sysproxy/             legacy Windows proxy helpers, disabled by default
  tun/                  experimental code, not used by the recommended Clash flow
  uri/                  mirage:// import/export
install.sh              one-command VPS installer
build.bat               Windows helper build script
```

## Build Checks

```powershell
go test ./...
go build -o miragec.exe .\cmd\miragec
go build -o miraged.exe .\cmd\miraged
```

On Linux:

```bash
go test ./...
go build -o miraged ./cmd/miraged
```
