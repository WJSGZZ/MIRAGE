<p align="center">
  <img src="assets/logo.png" width="160" alt="MIRAGE logo">
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-1.2-blue?style=flat-square" alt="version">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="go version">
  <img src="https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/license-MPL--2.0-green?style=flat-square" alt="license">
</p>

<h1 align="center">MIRAGE</h1>

<p align="center">
  <em>A transport protocol designed to be indistinguishable from normal HTTPS traffic.</em>
</p>

<p align="center">
  <a href="#中文">中文</a> &nbsp;·&nbsp;
  <a href="#english">English</a>
</p>

---

## 中文

### 概述

现代网络审查基础设施已从早期的端口封锁演进为对 TLS 握手指纹、流量熵值和协议行为的统计分析。Trojan、VLESS 等主流代理协议在设计上将"加密内容"视为充分条件，但在握手特征或数据包分布上仍存在可与普通浏览器流量区分的特征。

MIRAGE 的设计目标是：**封锁 MIRAGE 等价于封锁目标网站本身**。

协议由三层组成，每层职责严格单一：

```
应用流量
  → Clash Verge Rev（系统代理或 TUN 接管；规则 / 直连 / 代理分流）
    → MIRAGE 本地桥接核心（SOCKS5 / HTTP）
      → MIRAGE VPS（miraged）
        → 目标站
```

客户端无需独立 GUI：MIRAGE 在本机暴露标准 SOCKS5/HTTP 接口和 Clash 订阅端点，
由 **Clash Verge Rev / mihomo** 负责系统代理或 TUN 全流量接管，以及规则 / 直连 / 代理的路由分流。

---

### 协议设计

#### 分层架构

| 层 | 职责 |
| :--- | :--- |
| **Yamux** 多路复用 | 并发流管理 |
| **记录层**（Record Layer） | 切片 · 填充 · 心跳 |
| **TLS 1.3**（uTLS） | 唯一加密层，仿真浏览器握手指纹 |
| TCP | 底层传输，负责拥塞控制 |

各层职责严格单一：加密由 TLS 独立承担，流量整形由记录层独立承担，拥塞控制由 TCP 独立承担。

#### 外层不可区分性

GFW 自 2022 年起通过 TLS 握手指纹（JA3/JA3S）识别并封锁了所有使用 Go 标准库 TLS 的代理工具。MIRAGE 通过 **uTLS** 框架精确仿真真实 Chrome 浏览器的握手特征，包括密码套件偏好、扩展顺序、密钥协商参数，以及对后量子密码算法（X25519MLKEM768）的支持。在流量观察者看来，MIRAGE 连接与真实浏览器访问目标网站在握手层面无法区分。

#### 隐蔽认证

授权信息以密码学安全的方式嵌入 TLS ClientHello 的标准字段，**不新增任何 TLS 扩展，不修改握手包格式**。认证令牌由预共享密钥与时间窗口派生，具备防重放保护。用户标识每小时自动轮换，消除跨会话关联指纹。

服务端在接管 TLS 握手**之前**，于裸 TCP 层完成认证判定。认证失败的连接被**透明转发**至预配置的真实目标站。服务端在任何失败情形下均不发送协议错误响应，对外表现与真实 HTTPS 服务器无异，有效抵抗主动探测。

#### 记录层流量整形

记录层对 Yamux 输出的字节流进行系统性整形：

- **分块**：按参考于真实浏览器数据传输分布的随机权重切片，叠加随机抖动，避免固定大小特征。
- **填充帧**：在数据帧之间按密钥派生参数概率性插入随机内容的填充帧，干扰包长分布分析。
- **心跳帧**：连接空闲期间定期发送随机大小的心跳帧，消除流量静默时段特征。
- **死对端检测**：持续追踪最近一次有效帧时间，超时后主动关闭连接释放资源。

收发两端的整形参数完全独立，各自由密钥派生函数从对应的 `padding_seed` 产生，接收方无需了解对端的整形策略。

#### 安全性质

| 性质 | 说明 |
| :--- | :--- |
| 外层不可区分 | 握手特征与真实 Chrome 浏览器一致，针对 TLS 指纹识别免疫 |
| 后量子安全 | 密钥交换支持 X25519MLKEM768 |
| 抗主动探测 | 认证失败时行为与真实目标站响应完全一致，无任何协议特征泄露 |
| 抗重放攻击 | 令牌绑定时间窗口与连接随机数，服务端维护防重放缓存 |
| 证书绑定 | 客户端以 SPKI 公钥指纹验证服务端证书，不依赖系统 CA 链 |
| 跨会话隐私 | 用户标识每小时轮换，历史握手记录无法跨会话关联到同一用户 |
| 无内层协议特征 | 无 TLS-in-TLS 嵌套；内层协议字节流对 TLS 层不透明，无可识别指纹 |

---

### 快速开始

#### 第一步：VPS 部署

在海外 VPS 上以 `root` 执行一键部署脚本：

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh
```

脚本完成后，终端将打印一条 `mirage://` 口令，保存好这条口令。

> 也可以直接指定端口：`bash install.sh 443`

#### 第二步：Windows 客户端

在 Windows 上运行 `build.bat` 即可从源码编译 `miragec.exe`（需提前安装 [Go 1.24+](https://go.dev/dl/)）：

```bat
build.bat
```

编译完成后运行 `miragec.exe`，按向导提示粘贴上一步获得的 `mirage://` 口令。

向导完成后会显示本地订阅地址，将其导入 **Clash Verge Rev** 即可开始使用。

---

### 本地端点

| 用途 | 地址 |
| :--- | :--- |
| SOCKS5 代理 | `127.0.0.1:1080` |
| HTTP 代理 | `127.0.0.1:1081` |
| Clash / mihomo 订阅 | `http://127.0.0.1:9099/compat/mihomo.yaml` |
| 控制 API | `http://127.0.0.1:9099` |

---

### 仓库结构

```
cmd/
  miraged/          服务端入口（Linux）
  miragec/          客户端桥接核心（Windows）
internal/
  protocol/         密钥派生、认证令牌、会话 ID 构造
  record/           记录层：切片、填充、心跳、死对端检测
  mux/              Yamux 多路复用封装
  server/           服务端认证与流量转发运行时
  client/           客户端拨号与 SOCKS5/HTTP 代理
  dashboard/        控制 API 与 Clash/v2rayN 兼容订阅
  tlspeek/          裸 TCP 层 ClientHello 解析
  replayconn/       握手字节回放包装器
  uri/              mirage:// 口令编解码
  tun/              TUN 全流量接管（实验性）
assets/             图标与 Logo 资源
install.sh          VPS 一键部署脚本
build.bat           Windows 本地编译脚本
go.mod / go.sum     Go 模块定义与依赖锁
LICENSE             MPL-2.0 许可证
```

---

## English

### Overview

Modern censorship infrastructure has evolved from simple port blocking to statistical analysis of TLS handshake fingerprints, traffic entropy, and protocol state machine behavior. Mainstream proxy protocols such as Trojan and VLESS treat content encryption as sufficient, yet remain distinguishable from normal browser traffic at the handshake or packet-distribution level.

MIRAGE's design goal is: **blocking MIRAGE is equivalent to blocking the target website itself**.

The protocol consists of three layers with strictly separated responsibilities:

```
Application traffic
  → Clash Verge Rev (system proxy or TUN capture; rule / direct / proxy routing)
    → MIRAGE local bridge core (SOCKS5 / HTTP)
      → MIRAGE VPS (miraged)
        → destination
```

No standalone GUI is required. MIRAGE exposes standard SOCKS5/HTTP interfaces and a local subscription endpoint. **Clash Verge Rev / mihomo** handles traffic capture (system proxy or TUN) and routing (rule / direct / proxy).

---

### Protocol Design

#### Layered Architecture

| Layer | Responsibility |
| :--- | :--- |
| **Yamux** multiplexing | concurrent stream management |
| **Record Layer** | chunking · padding · heartbeat |
| **TLS 1.3** (uTLS) | sole encryption layer, browser fingerprint emulation |
| TCP | transport, congestion control |

Each layer has a single, non-overlapping responsibility: encryption belongs to TLS, traffic shaping belongs to the record layer, and congestion control belongs to TCP.

#### Outer Indistinguishability

Since 2022, GFW has fingerprinted and blocked all proxy tools using Go's standard TLS library by identifying their JA3/JA3S handshake signatures. MIRAGE uses the **uTLS** framework to precisely emulate the TLS handshake of a real Chrome browser, including cipher suite preferences, extension ordering, key agreement parameters, and support for post-quantum cryptography (X25519MLKEM768). A MIRAGE connection is indistinguishable from a real browser accessing the configured destination at the handshake level.

#### Covert Authentication

Authorization material is embedded in a standard TLS ClientHello field using a cryptographically secure construction — **without adding any new TLS extensions or altering the handshake packet format**. Tokens are derived from the pre-shared key and a time window, carry replay protection, and rotate the user identifier hourly to eliminate cross-session correlation fingerprints.

The server completes authentication at the raw TCP layer, **before** entering the TLS handshake. Connections that fail authentication are **transparently forwarded** to a pre-configured real destination. The server never emits a protocol error response under any failure condition; from the outside it is indistinguishable from a real HTTPS server, providing robust resistance to active probing.

#### Record Layer Traffic Shaping

The record layer systematically shapes the byte stream produced by Yamux:

- **Chunking**: Data is sliced into variable-size frames using a weighted distribution referenced against real browser traffic, with additional random jitter to avoid fixed-size signatures.
- **Padding frames**: Random-content padding frames are probabilistically inserted between data frames according to key-derived parameters, disrupting packet-length distribution analysis.
- **Heartbeat frames**: During idle periods, heartbeat frames of random size are sent at randomized intervals, eliminating traffic-silence fingerprints.
- **Dead peer detection**: The receiver continuously tracks the last valid frame timestamp and closes the connection when it exceeds the inactivity threshold.

The sender and receiver operate with fully independent shaping parameters, each derived from their respective `padding_seed` via a key derivation function. Neither side requires any knowledge of the other's shaping strategy.

#### Security Properties

| Property | Description |
| :--- | :--- |
| Outer indistinguishability | Handshake matches real Chrome behavior; immune to TLS fingerprint identification |
| Post-quantum security | Key exchange supports X25519MLKEM768 |
| Active probe resistance | Authentication failure behavior is identical to the real destination; no protocol signature is ever emitted |
| Replay protection | Tokens are bound to a time window and connection nonce; a server-side cache prevents replay |
| Certificate binding | Client verifies the server certificate by SPKI public-key fingerprint, independently of the system CA store |
| Cross-session privacy | User identifiers rotate hourly; historical handshake records cannot be linked across sessions |
| No inner protocol fingerprint | No TLS-in-TLS nesting; the inner byte stream is opaque to the TLS layer and carries no identifiable structure |

---

### Quick Start

#### Step 1: Deploy the VPS Server

Run the one-command installer on your overseas VPS as `root`:

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh
```

When the script completes, a `mirage://` URI is printed to the terminal. Save it.

> You can also specify a port directly: `bash install.sh 443`

#### Step 2: Windows Client

Build `miragec.exe` from source on Windows (requires [Go 1.24+](https://go.dev/dl/)):

```bat
build.bat
```

Run the resulting `miragec.exe` and follow the on-screen wizard. Paste the `mirage://` URI from Step 1 when prompted.

The wizard will display the local subscription URL. Import it into **Clash Verge Rev** to complete setup.

---

### Local Endpoints

| Purpose | Address |
| :--- | :--- |
| SOCKS5 proxy | `127.0.0.1:1080` |
| HTTP proxy | `127.0.0.1:1081` |
| Clash / mihomo subscription | `http://127.0.0.1:9099/compat/mihomo.yaml` |
| Control API | `http://127.0.0.1:9099` |

---

### Repository Layout

```
cmd/
  miraged/          server entry point (Linux)
  miragec/          client bridge core (Windows)
internal/
  protocol/         key derivation, auth tokens, session ID construction
  record/           record layer: chunking, padding, heartbeat, dead peer detection
  mux/              Yamux multiplexing wrapper
  server/           server authentication and relay runtime
  client/           client dialer and SOCKS5/HTTP proxy
  dashboard/        control API and Clash/v2rayN compatibility subscriptions
  tlspeek/          raw-TCP ClientHello parser
  replayconn/       handshake byte replay wrapper
  uri/              mirage:// URI codec
  tun/              TUN full-traffic capture (experimental)
assets/             logo and icon assets
install.sh          one-command VPS installer
build.bat           Windows build script
go.mod / go.sum     Go module definition and dependency lock
LICENSE             MPL-2.0
```
