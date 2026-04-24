# MIRAGE

<p align="center">
  <a href="#中文">中文</a> |
  <a href="#english">English</a>
</p>

> GitHub README files do not allow JavaScript, so MIRAGE uses reliable anchor-based language switching. Click a language above to jump to that section.

---

## 中文

MIRAGE 是一个自定义 TLS 伪装代理协议。当前推荐用法不是自研完整桌面客户端，而是：

```text
应用流量 -> Clash Verge Rev 系统代理 / TUN / 规则 -> MIRAGE 本地 SOCKS -> MIRAGE VPS
```

也就是说，MIRAGE 负责自己的协议和本地桥接；Clash Verge Rev / mihomo 负责成熟客户端能力，例如系统代理、TUN、规则、日志、连接面板和用户界面。

### 一句话流程

```text
VPS 执行 install.sh -> 得到 mirage:// 口令 -> Windows 运行 miragec 粘贴口令 -> Clash Verge 导入本地订阅 -> 可用
```

### 组件

- `miraged`：Linux VPS 服务端。
- `miragec`：Windows 本地 MIRAGE 桥接核心。
- Clash Verge Rev / mihomo：真正的桌面客户端、系统代理、TUN、规则和界面。

### 安全默认值

推荐的 Clash 桥接模式下，`miragec` 不会修改 Windows 代理状态：

- 不设置 Windows 系统代理。
- 不设置 WinHTTP 代理。
- 不写入用户级 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 环境变量。
- 只监听本机回环地址，并提供 Clash/mihomo 本地订阅。

流量接管交给 Clash Verge Rev。这样 MIRAGE 不会意外破坏你现有的 v2rayN、Clash 或 Windows 代理配置。

### VPS 一键部署

在 VPS 上使用 `root` 执行：

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 8443
```

脚本会自动完成：

- 安装 Go，如果系统缺少 Go。
- 编译 `miraged`。
- 生成证书、密钥和服务端配置。
- 安装并启动 `miraged` systemd 服务。
- 输出最终可导入的 `mirage://` 口令。

常用变体：

```bash
bash install.sh 443
PUBLIC_HOST=1.2.3.4 bash install.sh 8443
FALLBACK_ADDR=www.microsoft.com:443 SNI_NAME=www.microsoft.com bash install.sh 8443
```

查看服务状态：

```bash
systemctl status miraged --no-pager -l
journalctl -u miraged -f -l
```

如果重新生成服务端配置，请使用新的 `mirage://` 口令。旧口令可能出现 `uid not found`、`SPKI pin mismatch` 或 TLS EOF。

### Windows + Clash Verge Rev

编译 `miragec.exe`：

```powershell
cd D:\Folder\App\MIRAGE
go build -o miragec.exe .\cmd\miragec
```

启动 MIRAGE 桥接：

```powershell
.\miragec.exe
```

按提示粘贴 VPS 脚本输出的 `mirage://` 口令。连接成功后会看到：

```text
Clash URL   : http://127.0.0.1:9099/compat/mihomo.yaml
```

在 Clash Verge Rev 中：

1. 添加 Profile / 订阅 URL。
2. 填入 `http://127.0.0.1:9099/compat/mihomo.yaml`。
3. 每次重启 `miragec` 或更换口令后，更新这个订阅。
4. 在 Clash Verge Rev 里开启 System Proxy 或 TUN。

MIRAGE 生成的 mihomo 配置会自动给当前 VPS 地址加入 `DIRECT` 规则，避免 Clash TUN 把 MIRAGE 连接 VPS 的流量再次转回 MIRAGE，形成回环。

`miragec` 运行时的本地端点：

- SOCKS5：`127.0.0.1:1080`
- HTTP：`127.0.0.1:1081`
- 控制 API：`http://127.0.0.1:9099`
- Clash Verge / mihomo 订阅：`http://127.0.0.1:9099/compat/mihomo.yaml`
- v2rayN 自定义配置桥接：`http://127.0.0.1:9099/compat/v2rayn.json`

### CLI 模式

交互式导入口令并启动桥接：

```powershell
.\miragec.exe
```

脚本化导入口令并启动桥接：

```powershell
.\miragec.exe -core -servers servers.json -import-uri "mirage://..." -connect-last
```

使用已有 `servers.json` 启动桥接：

```powershell
.\miragec.exe -core -servers servers.json -dashboard 127.0.0.1:9099 -connect-last
```

旧的直连本地代理模式：

```powershell
.\miragec.exe -uri "mirage://..."
```

旧模式默认也不会修改系统代理。只有显式传入 `-set-system-proxy` 时，MIRAGE 才会尝试写 Windows 代理设置。

### Clash 排错

如果 Clash 导入成功但无法联网：

- 重启 `miragec` 后，请在 Clash Verge Rev 里更新订阅。
- 打开 `http://127.0.0.1:9099/compat/mihomo.yaml`，确认你的 VPS IP 在 `MATCH,PROXY` 前面以 `DIRECT` 规则出现。
- 如果日志出现 `dial <你的 VPS IP>:<端口>`，通常说明 Clash 订阅太旧，或者 VPS 直连保护规则缺失。
- 如果日志出现 `SPKI pin mismatch`，说明口令里的证书 pin 与服务端证书不匹配。
- 如果 VPS 日志出现 `uid not found`，说明口令里的用户/PSK 与当前服务端配置不匹配。

本地检查命令：

```powershell
curl.exe --socks5-hostname 127.0.0.1:1080 https://www.google.com -I
curl.exe --proxy http://127.0.0.1:1081 https://api.openai.com -I
curl.exe http://127.0.0.1:9099/compat/mihomo.yaml
```

### 仓库结构

```text
cmd/
  miraged/              Linux 服务端入口
  miragec/              Windows core/headless 桥接入口
internal/
  client/               本地 SOCKS5/HTTP 代理和 MIRAGE 拨号器
  dashboard/            本地控制 API 和 Clash/v2rayN 兼容订阅
  daemon/               本地代理生命周期和统计
  server/               服务端认证和转发运行时
  sysproxy/             旧 Windows 代理辅助代码，推荐模式默认禁用
  tun/                  实验代码，推荐 Clash 流程不使用
  uri/                  mirage:// 导入导出
install.sh              VPS 一键安装脚本
build.bat               Windows 构建辅助脚本
```

### 构建检查

```powershell
go test ./...
go build -o miragec.exe .\cmd\miragec
go build -o miraged.exe .\cmd\miraged
```

Linux：

```bash
go test ./...
go build -o miraged ./cmd/miraged
```

---

## English

MIRAGE is a custom TLS-disguised proxy protocol. The recommended workflow is not a custom full desktop client. Instead, MIRAGE works as a small local bridge for Clash Verge Rev / mihomo:

```text
Apps -> Clash Verge Rev system proxy / TUN / rules -> MIRAGE local SOCKS -> MIRAGE VPS
```

MIRAGE handles its own protocol and local bridge. Clash Verge Rev / mihomo handles mature desktop-client features such as system proxy, TUN, rules, logs, connection views, and UI.

### Quick Flow

```text
Run install.sh on VPS -> get mirage:// URI -> run miragec on Windows and paste URI -> import local subscription into Clash Verge -> ready
```

### Components

- `miraged`: Linux VPS server.
- `miragec`: Windows local MIRAGE bridge/core.
- Clash Verge Rev / mihomo: desktop client, system proxy, TUN, rules, and UI.

### Safe Defaults

In the recommended Clash bridge flow, `miragec` does not modify Windows proxy state:

- It does not set Windows System Proxy.
- It does not set WinHTTP proxy.
- It does not write user-level `HTTP_PROXY`, `HTTPS_PROXY`, or `ALL_PROXY` environment variables.
- It only listens on loopback addresses and serves a local Clash/mihomo subscription.

Traffic capture is handled by Clash Verge Rev. This keeps MIRAGE small and avoids unexpected changes to your existing v2rayN, Clash, or Windows proxy configuration.

### One-Command VPS Install

Run as root on the VPS:

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 8443
```

The script will:

- install Go if missing,
- build `miraged`,
- generate cert, key, and server config,
- install and start the `miraged` systemd service,
- print the final `mirage://` import URI.

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

If you regenerate server config, use the new `mirage://` URI. Old URIs may fail with `uid not found`, `SPKI pin mismatch`, or TLS EOF errors.

### Windows + Clash Verge Rev

Build `miragec.exe`:

```powershell
cd D:\Folder\App\MIRAGE
go build -o miragec.exe .\cmd\miragec
```

Start the MIRAGE bridge:

```powershell
.\miragec.exe
```

Paste the `mirage://` URI printed by the VPS installer. After connection, `miragec` prints:

```text
Clash URL   : http://127.0.0.1:9099/compat/mihomo.yaml
```

In Clash Verge Rev:

1. Add a Profile from URL.
2. Use `http://127.0.0.1:9099/compat/mihomo.yaml`.
3. Update the profile after each MIRAGE restart or URI change.
4. Enable Clash Verge Rev System Proxy or TUN.

The generated mihomo profile automatically adds a `DIRECT` rule for the active MIRAGE VPS address. This prevents Clash TUN from routing MIRAGE's own VPS connection back into MIRAGE.

Local endpoints while `miragec` is running:

- SOCKS5: `127.0.0.1:1080`
- HTTP: `127.0.0.1:1081`
- Control API: `http://127.0.0.1:9099`
- Clash Verge / mihomo subscription: `http://127.0.0.1:9099/compat/mihomo.yaml`
- v2rayN custom config bridge: `http://127.0.0.1:9099/compat/v2rayn.json`

### CLI Modes

Interactive URI import and bridge mode:

```powershell
.\miragec.exe
```

Scripted URI import and bridge mode:

```powershell
.\miragec.exe -core -servers servers.json -import-uri "mirage://..." -connect-last
```

Run the bridge with an existing `servers.json`:

```powershell
.\miragec.exe -core -servers servers.json -dashboard 127.0.0.1:9099 -connect-last
```

Legacy direct local proxy mode:

```powershell
.\miragec.exe -uri "mirage://..."
```

Legacy direct mode also avoids system proxy changes by default. MIRAGE only writes Windows proxy settings when `-set-system-proxy` is explicitly provided.

### Clash Troubleshooting

If Clash imports the profile but traffic fails:

- Refresh the Clash profile after restarting `miragec`.
- Open `http://127.0.0.1:9099/compat/mihomo.yaml` and confirm your VPS IP appears before `MATCH,PROXY` as a `DIRECT` rule.
- If logs show `dial <your-vps-ip>:<port>`, the Clash profile is stale or the VPS bypass rule is missing.
- If logs show `SPKI pin mismatch`, the URI cert pin does not match the server certificate.
- If VPS logs show `uid not found`, the URI user/PSK material does not match the current server config.

Basic local checks:

```powershell
curl.exe --socks5-hostname 127.0.0.1:1080 https://www.google.com -I
curl.exe --proxy http://127.0.0.1:1081 https://api.openai.com -I
curl.exe http://127.0.0.1:9099/compat/mihomo.yaml
```

### Repository Layout

```text
cmd/
  miraged/              Linux server entry point
  miragec/              Windows core/headless bridge entry point
internal/
  client/               local SOCKS5/HTTP proxy and MIRAGE dialer
  dashboard/            local control API and Clash/v2rayN compatibility subscriptions
  daemon/               local proxy runtime lifecycle and stats
  server/               server accept/auth/relay runtime
  sysproxy/             legacy Windows proxy helpers, disabled by default
  tun/                  experimental code, not used by the recommended Clash flow
  uri/                  mirage:// import/export
install.sh              one-command VPS installer
build.bat               Windows helper build script
```

### Build Checks

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
