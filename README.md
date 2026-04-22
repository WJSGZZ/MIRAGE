# MIRAGE

[English](#english) | [中文](#中文)

---

<a name="english"></a>

## English

### What MIRAGE is

MIRAGE is a censorship-resistant proxy project with:

- a Linux server binary: `miraged`
- a Windows client/core binary: `miragec`
- a new desktop shell under [`desktop/`](desktop/README.md)

The project is moving away from a browser-first control panel and toward a mainstream desktop-client architecture:

- local Go proxy core
- packaged desktop shell
- tray behavior and desktop-owned UX
- explicit system proxy, WinHTTP, and environment-proxy visibility

### Current status

MIRAGE already supports:

- TLS-based client/server transport
- local SOCKS5 and HTTP proxy listeners
- Windows system proxy integration
- WinHTTP apply flow
- proxy environment export for tools that read `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`
- desktop shell bootstrap with tray support
- diagnostics for listener health and outbound proxy checks

MIRAGE does **not** yet provide full traffic capture equivalent to mature TUN-based clients.  
Some applications may still bypass the proxy if they ignore WinINet, WinHTTP, and proxy environment variables.

### Architecture

```text
[Windows Desktop]
  MIRAGE Desktop (Tauri shell)
           |
           v
      miragec sidecar
   HTTP :1081 / SOCKS5 :1080
           |
           v
      TLS transport + auth + record layer + mux
           |
           v
[Linux VPS]
        miraged
           |
           v
     destination
```

### Repo layout

```text
cmd/
  miraged/                  server entry
  miragec/                  client/core entry
desktop/
  app/                      desktop UI
  src-tauri/                Tauri shell
internal/
  client/                   MIRAGE client, SOCKS5, HTTP proxy
  server/                   MIRAGE server runtime
  daemon/                   local client daemon lifecycle
  dashboard/                local backend API and diagnostics
  sysproxy/                 Windows proxy integration
  protocol/                 key derivation and protocol helpers
  record/                   framed record transport
  mux/                      multiplexing session layer
  tlspeek/                  ClientHello parsing helpers
  replayconn/               replayable conn for server handshake path
  uri/                      mirage:// link parsing
```

### Server deployment

Requirements:

- Linux VPS
- Go 1.24+
- one open TCP port, for example `8443`

Clone and build:

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git /opt/mirage-src
cd /opt/mirage-src
go build -o miraged ./cmd/miraged
```

Generate a sample config:

```bash
./miraged -genkey
```

That command prints a server config template and a client config template.  
For the legacy path you can still use the printed `serverPubKey` / `shortId` style config.  
For the newer spec-aligned path, use fields such as:

- `users[].psk`
- `cert_pin`
- `client_padding_seed`
- `server_padding_seed`

Run the server:

```bash
./miraged -c /path/to/config.json
```

For systemd:

```bash
cat >/etc/systemd/system/miraged.service <<'EOF'
[Unit]
Description=MIRAGE proxy server
After=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/mirage
ExecStart=/opt/mirage/miraged -c /opt/mirage/config.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
```

Then:

```bash
systemctl daemon-reload
systemctl enable miraged
systemctl start miraged
```

### Windows client/core

Build the Go binaries from the repo root:

```powershell
cd D:\Folder\App\MIRAGE
.\build.bat
```

That produces:

- `miragec.exe` for Windows
- `miraged` / `miraged.exe` depending on your local build target

Run the standalone core:

```powershell
.\miragec.exe --servers .\servers.json
```

By default, the local backend exposes:

- dashboard/backend API on `127.0.0.1:9099`
- SOCKS5 on `127.0.0.1:1080`
- HTTP proxy on `127.0.0.1:1081`

### Desktop shell

The desktop client lives under [`desktop/`](desktop/README.md).

Install frontend dependencies:

```powershell
cd D:\Folder\App\MIRAGE\desktop
npm.cmd install
```

Start the desktop shell in development mode:

```powershell
npm.cmd run dev
```

Notes:

- the Tauri shell launches `miragec` as a sidecar
- the window now behaves like a desktop app, not just a browser tab
- tray support and hide-to-tray are included
- the desktop UI talks to the local backend API instead of redirecting the user to the old web dashboard

### Proxy coverage

Right now MIRAGE can cover several layers:

- Windows system proxy
- WinHTTP
- process environment proxy
- manual SOCKS5 / HTTP proxy usage

This is enough for browsers and many tools, but not all applications.  
If a program still bypasses MIRAGE, the missing piece is usually full capture mode, such as:

- TUN
- WFP/service mode
- app-specific launch and injection strategies

### Development notes

- The repo now tracks the desktop shell source, but ignores heavy build artifacts.
- Real local profile data such as `servers.json` is ignored and should not be committed.
- The migration work is tracked in [`PROTOCOL_MIGRATION_TODO.md`](PROTOCOL_MIGRATION_TODO.md).

---

<a name="中文"></a>

## 中文

### MIRAGE 是什么

MIRAGE 是一个抗审查代理项目，目前包含三部分：

- Linux 服务端二进制：`miraged`
- Windows 客户端核心：`miragec`
- 新的桌面客户端外壳：[`desktop/`](desktop/README.md)

项目现在的方向已经不是“打开一个本地网页控制面板”了，而是逐步对标主流桌面代理客户端：

- 本地 Go 代理核心
- 桌面应用外壳
- 托盘和桌面端交互
- 清晰展示系统代理、WinHTTP、环境变量代理这些接管层

### 当前能力

MIRAGE 目前已经支持：

- 基于 TLS 的客户端 / 服务端传输
- 本地 SOCKS5 代理和 HTTP 代理
- Windows 系统代理设置
- WinHTTP 应用流程
- 导出 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`
- 带托盘的桌面端启动外壳
- 本地监听、连通性、代理通道的诊断功能

但它还**没有**做到成熟 TUN 客户端那种“几乎全局接管”。  
如果某些软件既不认 WinINet，也不认 WinHTTP，也不认代理环境变量，它还是可能绕过 MIRAGE。

### 架构概览

```text
[Windows 桌面端]
   MIRAGE Desktop (Tauri)
            |
            v
       miragec sidecar
   HTTP :1081 / SOCKS5 :1080
            |
            v
   TLS 传输 + 鉴权 + record 层 + mux
            |
            v
[Linux VPS]
         miraged
            |
            v
         目标站点
```

### 仓库结构

```text
cmd/
  miraged/                  服务端入口
  miragec/                  客户端核心入口
desktop/
  app/                      桌面 UI
  src-tauri/                Tauri 外壳
internal/
  client/                   MIRAGE 客户端、SOCKS5、HTTP 代理
  server/                   MIRAGE 服务端运行时
  daemon/                   本地客户端代理生命周期
  dashboard/                本地后端 API 与诊断
  sysproxy/                 Windows 代理设置
  protocol/                 密钥派生与协议辅助
  record/                   分帧传输层
  mux/                      多路复用层
  tlspeek/                  ClientHello 解析
  replayconn/               服务端握手重放连接
  uri/                      mirage:// 链接解析
```

### 服务端部署

要求：

- Linux VPS
- Go 1.24+
- 一个可开放的 TCP 端口，例如 `8443`

克隆并编译：

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git /opt/mirage-src
cd /opt/mirage-src
go build -o miraged ./cmd/miraged
```

生成示例配置：

```bash
./miraged -genkey
```

这个命令会打印服务端和客户端配置模板。

目前仓库同时兼容两种路径：

- 旧的兼容配置：`serverPubKey` / `shortId`
- 新的 spec 对齐配置：`psk` / `cert_pin` / `client_padding_seed` / `server_padding_seed`

启动服务端：

```bash
./miraged -c /path/to/config.json
```

如果你要挂成 systemd：

```bash
cat >/etc/systemd/system/miraged.service <<'EOF'
[Unit]
Description=MIRAGE proxy server
After=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/mirage
ExecStart=/opt/mirage/miraged -c /opt/mirage/config.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
```

然后执行：

```bash
systemctl daemon-reload
systemctl enable miraged
systemctl start miraged
```

### Windows 客户端核心

在仓库根目录编译：

```powershell
cd D:\Folder\App\MIRAGE
.\build.bat
```

构建完成后会得到客户端和服务端可执行文件。

运行本地核心：

```powershell
.\miragec.exe --servers .\servers.json
```

默认情况下会暴露：

- 本地后端 / 诊断接口：`127.0.0.1:9099`
- SOCKS5：`127.0.0.1:1080`
- HTTP 代理：`127.0.0.1:1081`

### 桌面客户端

桌面客户端说明在 [`desktop/README.md`](desktop/README.md)。

安装依赖：

```powershell
cd D:\Folder\App\MIRAGE\desktop
npm.cmd install
```

开发模式启动：

```powershell
npm.cmd run dev
```

现在桌面端的设计思路是：

- Tauri 外壳负责真正的桌面窗口
- `miragec` 作为 sidecar 本地运行
- 桌面 UI 直接请求本地 API
- 不再把旧网页面板当作唯一主界面

### 代理接管范围

现在 MIRAGE 可以覆盖这些层：

- Windows 系统代理
- WinHTTP
- 进程环境变量代理
- 手动配置 SOCKS5 / HTTP 代理的软件

这已经足够覆盖浏览器和不少工具，但还不是“完全体”。  
如果某个程序仍然绕过 MIRAGE，通常缺的是更底层的流量接管能力，比如：

- TUN
- WFP / 服务模式
- 针对特定应用的启动接管策略

### 开发说明

- 桌面端源码已经纳入仓库，但构建产物和依赖目录已忽略。
- `servers.json` 这类本地真实节点配置不会提交到仓库。
- 协议迁移计划见 [`PROTOCOL_MIGRATION_TODO.md`](PROTOCOL_MIGRATION_TODO.md)。
