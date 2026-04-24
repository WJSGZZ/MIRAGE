# MIRAGE

<p align="center">
  <a href="#中文">中文</a> |
  <a href="#english">English</a>
</p>

<p align="center">
  <strong>A lightweight private transport core for Clash Verge Rev / mihomo.</strong>
</p>

---

## 中文

MIRAGE 是一个轻量的私有传输协议与本地桥接核心。它不尝试重新实现一个完整桌面客户端，而是把成熟的客户端体验交给 Clash Verge Rev / mihomo。

```text
应用流量 -> Clash Verge Rev 系统代理 / TUN / 规则 -> MIRAGE 本地桥接 -> MIRAGE VPS
```

### 快速流程

```text
VPS 一键部署 -> 得到 mirage:// 口令 -> Windows 运行 miragec 粘贴口令 -> Clash Verge 导入本地订阅 -> 开始使用
```

### 协议介绍

MIRAGE 的重点在协议层，而不是客户端外壳。v1.0 版本围绕“像正常网络连接一样工作、尽量少暴露独有特征、部署和使用足够简单”来设计。

| 方向 | 说明 |
|---|---|
| 外观自然 | 传输层尽量贴近常见浏览器连接形态，减少自定义隧道的突兀特征。 |
| 私有授权 | 节点口令包含连接所需的用户材料，服务端只接受授权客户端。 |
| 失败回落 | 非授权或异常连接会按配置回落到真实站点，降低可见差异。 |
| 流量整形 | 内部传输会做分片与填充，让数据流不只表现为固定模式。 |
| 多路复用 | 单条远程连接可以承载多个本地代理请求，减少连接管理成本。 |
| Clash 桥接 | MIRAGE 专注协议本身，系统代理、TUN、规则和日志交给 Clash Verge Rev / mihomo。 |

更深入的协议细节请参考本地技术报告。公开 README 只保留必要的产品级说明，避免暴露过多实现细节。

### 当前能力

| 能力 | 状态 |
|---|---|
| Linux VPS 服务端 | 可用 |
| Windows 本地桥接核心 | 可用 |
| `mirage://` 口令导入 | 可用 |
| Clash Verge / mihomo 本地订阅 | 可用 |
| VPS 一键部署脚本 | 可用 |
| 自动保护 VPS 直连，避免 Clash TUN 回环 | 可用 |
| 自研图形客户端 | 暂不提供，推荐使用 Clash Verge Rev |
| MIRAGE 自带系统代理/TUN 接管 | 暂不提供，交给 Clash Verge Rev / mihomo |

### 安全默认值

推荐模式下，`miragec` 不会主动修改 Windows 代理配置：

- 不设置 Windows 系统代理。
- 不设置 WinHTTP 代理。
- 不写入用户级代理环境变量。
- 只监听本机回环地址，并提供本地订阅。

流量接管由 Clash Verge Rev 完成，这样可以避免影响你现有的 v2rayN、Clash 或 Windows 代理设置。

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
- 生成服务端配置和证书材料。
- 安装并启动 systemd 服务。
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

`miragec` 运行时的本地端点：

| 用途 | 地址 |
|---|---|
| SOCKS5 | `127.0.0.1:1080` |
| HTTP | `127.0.0.1:1081` |
| 控制 API | `http://127.0.0.1:9099` |
| Clash/mihomo 订阅 | `http://127.0.0.1:9099/compat/mihomo.yaml` |
| v2rayN 自定义配置桥接 | `http://127.0.0.1:9099/compat/v2rayn.json` |

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

### 排错

如果 Clash 导入成功但无法联网：

- 重启 `miragec` 后，在 Clash Verge Rev 里更新订阅。
- 打开 `http://127.0.0.1:9099/compat/mihomo.yaml`，确认 VPS 地址被设置为直连。
- 如果日志出现证书 pin 不匹配，请重新使用 VPS 当前输出的新口令。
- 如果 VPS 重新部署或重新生成配置，旧口令需要替换。

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

MIRAGE is a lightweight private transport protocol and local bridge core. It does not try to replace a full desktop client. Instead, it delegates the mature client experience to Clash Verge Rev / mihomo.

```text
Apps -> Clash Verge Rev system proxy / TUN / rules -> MIRAGE local bridge -> MIRAGE VPS
```

### Quick Flow

```text
One-command VPS install -> get mirage:// URI -> run miragec on Windows and paste URI -> import local subscription into Clash Verge -> ready
```

### Protocol Overview

MIRAGE is primarily a protocol project, not a desktop-shell project. v1.0 is designed around a simple idea: behave like an ordinary network connection, avoid unnecessary distinctive traits, and stay easy to deploy.

| Direction | Description |
|---|---|
| Natural profile | The transport aims to stay close to common browser-style connection behavior instead of looking like a bespoke tunnel. |
| Private authorization | Import URIs carry the user material needed for access, and the server only accepts authorized clients. |
| Graceful fallback | Unknown or invalid connections can fall back to a real configured site, reducing visible differences. |
| Traffic shaping | Internal transport uses chunking and padding so traffic does not follow one fixed pattern. |
| Multiplexing | One remote connection can carry multiple local proxy requests, reducing connection-management overhead. |
| Clash bridge | MIRAGE focuses on the protocol. Clash Verge Rev / mihomo handles system proxy, TUN, rules, logs, and UI. |

For deeper protocol details, refer to the local technical report. The public README intentionally keeps only product-level information and avoids exposing unnecessary implementation details.

### Current Capabilities

| Capability | Status |
|---|---|
| Linux VPS server | Available |
| Windows local bridge core | Available |
| `mirage://` URI import | Available |
| Clash Verge / mihomo local subscription | Available |
| One-command VPS installer | Available |
| Automatic VPS direct rule to avoid Clash TUN loops | Available |
| Custom graphical client | Not included; Clash Verge Rev is recommended |
| MIRAGE-owned system proxy / TUN capture | Not included; delegated to Clash Verge Rev / mihomo |

### Safe Defaults

In the recommended mode, `miragec` does not modify Windows proxy configuration:

- It does not set Windows System Proxy.
- It does not set WinHTTP proxy.
- It does not write user-level proxy environment variables.
- It only listens on loopback addresses and serves a local subscription.

Traffic capture is handled by Clash Verge Rev. This avoids unexpected changes to existing v2rayN, Clash, or Windows proxy settings.

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
- generate server config and certificate material,
- install and start the systemd service,
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

Local endpoints while `miragec` is running:

| Purpose | Address |
|---|---|
| SOCKS5 | `127.0.0.1:1080` |
| HTTP | `127.0.0.1:1081` |
| Control API | `http://127.0.0.1:9099` |
| Clash/mihomo subscription | `http://127.0.0.1:9099/compat/mihomo.yaml` |
| v2rayN custom config bridge | `http://127.0.0.1:9099/compat/v2rayn.json` |

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

### Troubleshooting

If Clash imports the profile but traffic fails:

- Refresh the Clash profile after restarting `miragec`.
- Open `http://127.0.0.1:9099/compat/mihomo.yaml` and confirm the VPS address is set to direct.
- If logs show a certificate pin mismatch, use a fresh URI from the current VPS deployment.
- If the VPS is reinstalled or its config is regenerated, replace old URIs.

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
