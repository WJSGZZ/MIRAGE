# MIRAGE

[English](#english) | [中文](#中文)

---

## English

### What MIRAGE is

MIRAGE is a censorship-resistant proxy. It disguises traffic as ordinary TLS by embedding auth material inside the ClientHello `session_id` field — no extra round-trips, no distinguishable handshake.

- **`miraged`** — Linux VPS server
- **`miragec`** — Windows proxy core (headless CLI or dashboard)

---

### VPS deployment (one command)

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 443
```

The script will:

1. Install Go if needed
2. Build `miraged` from source
3. Auto-generate config, TLS certificate, PSK, and cert pin
4. Auto-detect the public IP
5. Print the final `mirage://` import token
6. Install and start a systemd service

**Port options** — pass as first argument or use the interactive menu:

```bash
bash install.sh 443    # standard HTTPS port (recommended)
bash install.sh 8443   # alternative TLS port
bash install.sh 80     # HTTP port, bypasses some firewalls
bash install.sh        # interactive menu
```

> Run as root. Make sure the chosen port is not already occupied.

At the end you will see:

```
╔══════════════════════════════════════════════════════════════╗
║            MIRAGE:// IMPORT TOKEN (save this!)              ║
╠══════════════════════════════════════════════════════════════╣
║  mirage://xxxx@1.2.3.4:443?sni=www.microsoft.com&cert_pin=...
╚══════════════════════════════════════════════════════════════╝
```

Save that token — it encodes the server address, PSK, cert pin, and padding seed.

---

### Windows client

Build from the repository root (requires Go):

```powershell
go build -o miragec.exe .\cmd\miragec
```

**Headless mode** (recommended — no UI, just the proxy):

```powershell
# from a mirage:// token
.\miragec.exe -uri "mirage://xxxx@1.2.3.4:443?..."

# from a client.json file
.\miragec.exe -c client.json
```

This starts:
- SOCKS5 proxy on `127.0.0.1:1080`
- HTTP proxy on `127.0.0.1:1081`

**Dashboard mode** (web UI at `http://127.0.0.1:9099`):

```powershell
.\miragec.exe
```

**Flag reference:**

| Flag | Default | Purpose |
|------|---------|---------|
| `-c path` | — | Load client.json, run headless |
| `-uri mirage://...` | — | Parse URI directly, run headless |
| `-socks5 addr` | from config | Override SOCKS5 listen address |
| `-http addr` | from config | Override HTTP listen address |
| `-servers path` | `servers.json` next to exe | Dashboard mode profile file |
| `-no-browser` | false | Suppress browser auto-open |

---

### v2rayN integration

Run `miragec.exe` in headless mode (SOCKS5 on `:1080`), then in v2rayN add a custom config:

```json
{
  "inbounds": [
    {"protocol": "socks", "port": 10808, "listen": "127.0.0.1",
     "settings": {"auth": "noauth", "udp": false}}
  ],
  "outbounds": [
    {"protocol": "socks",
     "settings": {"servers": [{"address": "127.0.0.1", "port": 1080}]}}
  ]
}
```

v2rayN handles routing rules; MIRAGE handles the tunnel.

---

### Architecture

```
[Windows]
  miragec.exe
  SOCKS5 :1080 / HTTP :1081
       |
       | TLS (uTLS Chrome profile)
       | session_id = UID(4) + HMAC-token(28)
       | record layer + Yamux
       v
[Linux VPS]
  miraged
       |
       v
  destination
```

---

### Repository layout

```
cmd/
  miraged/        server entry point
  miragec/        client entry point (headless + dashboard)
internal/
  client/         TLS dial, SOCKS5, HTTP proxy
  server/         connection accept, auth routing, relay
  protocol/       PSK → UID, HMAC token, session_id assembly
  record/         framed transport with padding and heartbeat
  mux/            Yamux multiplexer wrapper
  uri/            mirage:// encode / decode
  config/         JSON config loading and field parsing
  dashboard/      web UI and REST control API
  sysproxy/       Windows system proxy integration
  tlspeek/        raw ClientHello parsing
  replayconn/     ClientHello replay for server handshake
  certutil/       TLS cert load/generate and SPKI pin
install.sh        VPS one-command installer
build.bat         Windows cross-compile helper
```

---

## 中文

### MIRAGE 是什么

MIRAGE 是一个抗审查代理。它把认证信息嵌进 TLS ClientHello 的 `session_id` 字段，流量看起来和普通 HTTPS 没有区别——没有额外握手，没有可识别的特征。

- **`miraged`** — Linux VPS 服务端
- **`miragec`** — Windows 代理核心（无界面 CLI 或 Web 面板）

---

### VPS 部署（一条命令）

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 443
```

脚本会自动：

1. 如果没有 Go 则自动安装
2. 从源码编译 `miraged`
3. 自动生成配置、TLS 证书、PSK 和 cert pin
4. 自动探测公网 IP
5. 打印最终 `mirage://` 口令
6. 安装并启动 systemd 服务

**端口选择** — 作为第一个参数传入，或不传进入交互菜单：

```bash
bash install.sh 443    # 标准 HTTPS 端口（推荐）
bash install.sh 8443   # 备用 TLS 端口
bash install.sh 80     # HTTP 端口，可绕过部分防火墙
bash install.sh        # 交互菜单
```

> 需要 root 权限运行，且所选端口不能被占用。

脚本结束时会显示：

```
╔══════════════════════════════════════════════════════════════╗
║            MIRAGE:// IMPORT TOKEN (save this!)              ║
╠══════════════════════════════════════════════════════════════╣
║  mirage://xxxx@1.2.3.4:443?sni=www.microsoft.com&cert_pin=...
╚══════════════════════════════════════════════════════════════╝
```

保存这行口令，它包含服务器地址、PSK、cert pin 和 padding seed。

---

### Windows 客户端

在仓库根目录编译（需要 Go）：

```powershell
go build -o miragec.exe .\cmd\miragec
```

**无界面模式**（推荐——不开 UI，直接跑代理）：

```powershell
# 用 mirage:// 口令
.\miragec.exe -uri "mirage://xxxx@1.2.3.4:443?..."

# 用 client.json 文件
.\miragec.exe -c client.json
```

启动后提供：
- SOCKS5 代理 `127.0.0.1:1080`
- HTTP 代理 `127.0.0.1:1081`

**面板模式**（Web UI，访问 `http://127.0.0.1:9099`）：

```powershell
.\miragec.exe
```

**参数说明：**

| 参数 | 默认值 | 用途 |
|------|--------|------|
| `-c 路径` | — | 加载 client.json，无界面运行 |
| `-uri mirage://...` | — | 直接解析口令，无界面运行 |
| `-socks5 地址` | 配置文件中的值 | 覆盖 SOCKS5 监听地址 |
| `-http 地址` | 配置文件中的值 | 覆盖 HTTP 监听地址 |
| `-servers 路径` | exe 同目录的 `servers.json` | 面板模式节点文件 |
| `-no-browser` | false | 不自动打开浏览器 |

---

### 配合 v2rayN 使用

后台运行 `miragec.exe` 无界面模式（SOCKS5 在 `:1080`），然后在 v2rayN 中添加自定义配置：

```json
{
  "inbounds": [
    {"protocol": "socks", "port": 10808, "listen": "127.0.0.1",
     "settings": {"auth": "noauth", "udp": false}}
  ],
  "outbounds": [
    {"protocol": "socks",
     "settings": {"servers": [{"address": "127.0.0.1", "port": 1080}]}}
  ]
}
```

v2rayN 负责路由规则，MIRAGE 负责隧道传输。

---

### 架构

```
[Windows]
  miragec.exe
  SOCKS5 :1080 / HTTP :1081
       |
       | TLS（uTLS Chrome 指纹）
       | session_id = UID(4字节) + HMAC-token(28字节)
       | record 层 + Yamux
       v
[Linux VPS]
  miraged
       |
       v
    目标站点
```

---

### 仓库结构

```
cmd/
  miraged/        服务端入口
  miragec/        客户端入口（无界面 + 面板）
internal/
  client/         TLS 拨号、SOCKS5、HTTP 代理
  server/         连接接受、认证路由、中继
  protocol/       PSK → UID、HMAC token、session_id 组装
  record/         带 padding 和心跳的分帧传输层
  mux/            Yamux 多路复用封装
  uri/            mirage:// 编解码
  config/         JSON 配置加载与字段解析
  dashboard/      Web UI 与 REST 控制 API
  sysproxy/       Windows 系统代理集成
  tlspeek/        原始 ClientHello 解析
  replayconn/     服务端握手重放连接
  certutil/       TLS 证书加载/生成与 SPKI pin
install.sh        VPS 一键安装脚本
build.bat         Windows 交叉编译辅助脚本
```
