# MIRAGE

[English](#english) | [中文](#中文)

---

## English

### What MIRAGE is

MIRAGE is a censorship-resistant proxy project with:

- `miraged`: Linux server binary
- `miragec`: Windows local proxy core
- `MirageClient.WPF`: native Windows desktop client

The current direction is a mainstream desktop-client architecture:

- Go transport and local proxy core
- native Windows desktop UI
- explicit local control API between UI and core
- visible system proxy, WinHTTP, PAC, and environment coverage

### Current status

MIRAGE already supports:

- TLS-based client/server transport
- local SOCKS5 and HTTP proxy listeners
- Windows system proxy integration
- WinHTTP apply flow
- user-level proxy environment export
- spec-shaped control API endpoints
- real traffic counters on `/stats`
- native WPF desktop client with:
  - Chinese and English UI
  - follow-system-language default
  - tray integration
  - single-instance behavior
  - profile import via `mirage://`
  - proxy mode settings
  - copyable diagnostics report
  - automatic local core startup

MIRAGE still does **not** provide full capture equivalent to mature TUN-based clients. Software that ignores WinINet, WinHTTP, PAC, and proxy environment variables may still bypass MIRAGE until TUN or service mode is added.

### Architecture

```text
[Windows Desktop]
   MirageClient.WPF
          |
          v
  local control API :9099
          |
          v
       miragec
 HTTP :1081 / SOCKS5 :1080
          |
          v
  TLS transport + record layer + Yamux
          |
          v
[Linux VPS]
       miraged
          |
          v
    destination
```

### Repository layout

```text
cmd/
  miraged/                  server entry
  miragec/                  client/core entry
MirageClient.WPF/           native Windows desktop client
internal/
  client/                   MIRAGE client, SOCKS5, HTTP proxy
  server/                   MIRAGE server runtime
  daemon/                   local client daemon lifecycle
  dashboard/                local control API and diagnostics
  sysproxy/                 Windows proxy integration
  protocol/                 protocol helpers
  record/                   framed record transport
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

Run the server:

```bash
./miraged -c /path/to/config.json
```

### One-command bootstrap

You no longer need to manually stitch together `config.json`, `cert pin`, and the final `mirage://` link.

Build first:

```bash
go build -o miraged ./cmd/miraged
```

Then run bootstrap:

```bash
./miraged -bootstrap -bootstrap-dir /opt/miraged -listen 0.0.0.0:443
```

That command will:

- check whether the listen port is already occupied
- auto-generate server config
- auto-generate certificate and key
- derive the final `cert pin`
- auto-detect the public IP when possible
- write:
  - `/opt/miraged/config.json`
  - `/opt/miraged/client.json`
  - `/opt/miraged/mirage-cert.pem`
  - `/opt/miraged/mirage-key.pem`
- print the final importable `mirage://` URI

Useful overrides:

```bash
./miraged -bootstrap \
  -bootstrap-dir /opt/miraged \
  -public-host YOUR_DOMAIN_OR_IP \
  -listen 0.0.0.0:8443 \
  -fallback www.microsoft.com:443 \
  -sni www.microsoft.com \
  -name my-vps-1 \
  -user user1 \
  -overwrite
```

To inspect whether a port is already occupied on Linux:

```bash
ss -ltnp | grep ':443'
```

### Windows core

Build from the repository root:

```powershell
cd D:\Folder\App\MIRAGE
go build -o miragec.exe .\cmd\miragec
go build -o miraged.exe .\cmd\miraged
```

Run the standalone local core:

```powershell
.\miragec.exe --servers .\servers.json --no-browser
```

By default the core exposes:

- control API on `127.0.0.1:9099`
- SOCKS5 on `127.0.0.1:1080`
- HTTP proxy on `127.0.0.1:1081`

### Native Windows client

The primary Windows client lives in `MirageClient.WPF/`.

Build it:

```powershell
dotnet build .\MirageClient.WPF\MirageClient.WPF.csproj
```

Run it:

```powershell
.\MirageClient.WPF\bin\Debug\net8.0-windows\MirageClient.exe
```

Current desktop client behavior:

- native WPF window, not a WebView shell
- auto-starts the local `miragec` core when needed
- tray menu for open, connect, disconnect, proxy mode switching, reapply, and exit
- system proxy policy modes:
  - clear system proxy
  - auto system proxy
  - manual mode
  - PAC mode
- diagnostics page with copyable full report

### Proxy coverage

MIRAGE currently covers:

- Windows system proxy
- PAC URL mode
- WinHTTP
- process environment proxy
- manual SOCKS5 / HTTP proxy usage

This covers browsers and many tools, but not everything. If a program still bypasses MIRAGE, the remaining gap is usually full traffic capture such as:

- TUN
- WFP / service mode
- app-specific launch and capture helpers

### Development notes

- `MirageClient.WPF/bin` and `MirageClient.WPF/obj` are build output and should not be committed.
- Real local profile data such as `servers.json` should not be committed.
- Protocol migration work is tracked in [`PROTOCOL_MIGRATION_TODO.md`](PROTOCOL_MIGRATION_TODO.md).

---

## 中文

### MIRAGE 是什么

MIRAGE 是一个抗审查代理项目，目前包含：

- `miraged`：Linux 服务端二进制
- `miragec`：Windows 本地代理核心
- `MirageClient.WPF`：原生 Windows 桌面客户端

当前项目路线是逐步做成更接近主流客户端的软件架构：

- Go 实现传输层和本地代理核心
- 原生 Windows 桌面界面
- UI 与核心之间使用明确的本地控制 API
- 清晰展示系统代理、WinHTTP、PAC、环境变量这些接管层

### 当前状态

MIRAGE 目前已经支持：

- 基于 TLS 的客户端 / 服务端传输
- 本地 SOCKS5 和 HTTP 代理监听
- Windows 系统代理集成
- WinHTTP 应用流程
- 用户级环境变量代理导出
- 面向桌面客户端的规范化控制 API
- `/stats` 的真实流量统计
- 原生 WPF 客户端，具备：
  - 中英文界面
  - 默认跟随系统语言
  - 托盘集成
  - 单实例行为
  - `mirage://` 节点导入
  - 代理模式设置
  - 一键复制诊断报告
  - 自动拉起本地 `miragec` 核心

但它 **还没有** 达到成熟 TUN 客户端那种“几乎全局接管”的程度。  
如果某些软件不认 WinINet、WinHTTP、PAC、环境变量代理，它仍然可能绕过 MIRAGE，直到后续补上 TUN 或服务模式。

### 架构概览

```text
[Windows 桌面端]
   MirageClient.WPF
          |
          v
   本地控制 API :9099
          |
          v
       miragec
 HTTP :1081 / SOCKS5 :1080
          |
          v
   TLS 传输 + record 层 + Yamux
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
  miragec/                  客户端 / 本地核心入口
MirageClient.WPF/           原生 Windows 桌面客户端
internal/
  client/                   MIRAGE 客户端、SOCKS5、HTTP 代理
  server/                   MIRAGE 服务端运行时
  daemon/                   本地客户端核心生命周期
  dashboard/                本地控制 API 与诊断
  sysproxy/                 Windows 代理集成
  protocol/                 协议辅助模块
  record/                   分帧传输层
  tlspeek/                  ClientHello 解析
  replayconn/               服务端握手重放连接
  uri/                      mirage:// 链接解析
```

### 服务端部署

要求：

- Linux VPS
- Go 1.24+
- 一个开放的 TCP 端口，例如 `8443`

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

启动服务端：

```bash
./miraged -c /path/to/config.json
```

### 一键初始化

现在不需要再手动拼 `config.json`、`cert pin` 和最终 `mirage://` 了。

先编译：

```bash
go build -o miraged ./cmd/miraged
```

再执行一键初始化：

```bash
./miraged -bootstrap -bootstrap-dir /opt/miraged -listen 0.0.0.0:443
```

这个命令会自动：

- 检查监听端口是否已被占用
- 生成服务端配置
- 生成证书和私钥
- 计算最终 `cert pin`
- 尽量自动探测公网 IP
- 写出：
  - `/opt/miraged/config.json`
  - `/opt/miraged/client.json`
  - `/opt/miraged/mirage-cert.pem`
  - `/opt/miraged/mirage-key.pem`
- 直接打印最终可导入的 `mirage://`

常用覆盖参数：

```bash
./miraged -bootstrap \
  -bootstrap-dir /opt/miraged \
  -public-host 你的域名或公网IP \
  -listen 0.0.0.0:8443 \
  -fallback www.microsoft.com:443 \
  -sni www.microsoft.com \
  -name my-vps-1 \
  -user user1 \
  -overwrite
```

查看 Linux 上某个端口目前被谁占用：

```bash
ss -ltnp | grep ':443'
```

### Windows 本地核心

在仓库根目录编译：

```powershell
cd D:\Folder\App\MIRAGE
go build -o miragec.exe .\cmd\miragec
go build -o miraged.exe .\cmd\miraged
```

手动运行本地核心：

```powershell
.\miragec.exe --servers .\servers.json --no-browser
```

默认会暴露：

- 控制 API：`127.0.0.1:9099`
- SOCKS5：`127.0.0.1:1080`
- HTTP 代理：`127.0.0.1:1081`

### 原生 Windows 客户端

现在的主 Windows 客户端位于 `MirageClient.WPF/`。

编译：

```powershell
dotnet build .\MirageClient.WPF\MirageClient.WPF.csproj
```

运行：

```powershell
.\MirageClient.WPF\bin\Debug\net8.0-windows\MirageClient.exe
```

当前桌面客户端特性：

- 使用原生 WPF 窗口，而不是 WebView 外壳
- 需要时自动启动本地 `miragec` 核心
- 托盘菜单支持打开、连接、断开、切换代理模式、重新应用代理、退出
- 系统代理策略包含：
  - 清除系统代理
  - 自动配置系统代理
  - 手动模式
  - PAC 模式
- 诊断页支持复制完整报告

### 代理接管范围

当前 MIRAGE 已能覆盖：

- Windows 系统代理
- PAC 模式
- WinHTTP
- 进程环境变量代理
- 手动配置 SOCKS5 / HTTP 代理的软件

这已经能覆盖浏览器和不少工具，但还不是“全接管”。  
如果某个程序仍然绕过 MIRAGE，通常缺的是更底层的流量接管能力，例如：

- TUN
- WFP / 服务模式
- 面向特定应用的启动与接管辅助

### 开发说明

- `MirageClient.WPF/bin` 和 `MirageClient.WPF/obj` 属于构建产物，不应提交。
- `servers.json` 这类本地真实节点配置不应提交到仓库。
- 协议迁移进度见 [`PROTOCOL_MIGRATION_TODO.md`](PROTOCOL_MIGRATION_TODO.md)。
