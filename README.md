<p align="center">
  <img src="https://img.shields.io/badge/version-1.0-blue?style=flat-square" alt="version">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="go version">
  <img src="https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/license-private-red?style=flat-square" alt="license">
</p>

<h1 align="center">MIRAGE</h1>

<p align="center">
  <em>A private transport protocol engineered for hostile network environments.</em>
</p>

<p align="center">
  <a href="#中文说明">中文</a> &nbsp;·&nbsp;
  <a href="#english">English</a>
</p>

---

## 中文说明

### 概述

MIRAGE 是一套面向受限网络环境的私有传输协议及其实现。协议在设计层面系统性地应对现代深度包检测（DPI）技术，而不依赖于任何单一混淆手段的侥幸。

客户端无需独立 GUI：MIRAGE 在本机暴露标准 SOCKS5/HTTP 接口和本地订阅端点，与 **Clash Verge Rev / mihomo** 配合完成系统代理、TUN 全流量接管和规则分流。

```
应用流量
  → Clash Verge Rev（系统代理 / TUN / 规则）
    → MIRAGE 本地桥接核心
      → MIRAGE VPS
        → 目标站
```

---

### 协议设计

#### 分层架构

MIRAGE 协议栈由三个明确分工的层次组成：

```
┌─────────────────────────────────┐
│        多路复用层（Yamux）        │  并发流管理，零协议特征暴露
├─────────────────────────────────┤
│        记录层（Record Layer）     │  切片 · 填充 · 心跳 · 流量整形
├─────────────────────────────────┤
│      外层加密（TLS 1.3）          │  唯一加密层，仿真真实浏览器指纹
└─────────────────────────────────┘
               │ TCP
```

各层职责严格单一：加密由 TLS 独立承担，流量混淆由记录层独立承担，拥塞控制由 TCP 独立承担，任何层不跨越边界承担其他层的职责。

#### 外层不可区分性

MIRAGE 的 TLS 握手通过 **uTLS** 框架精确仿真主流浏览器的握手特征，包括密码套件偏好、扩展顺序、密钥协商参数，以及对**后量子密码算法**（X25519MLKEM768）的支持。在流量观察者看来，MIRAGE 连接与正常浏览器访问目标网站在握手层面无法区分。

#### 隐蔽认证

授权信息以密码学安全的方式嵌入 TLS 握手的标准字段，**不新增任何 TLS 扩展，不修改握手包格式**。认证令牌基于预共享密钥与时间窗口派生，具备防重放保护，且每小时自动更换用户标识，消除跨会话关联指纹。

服务端在裸 TCP 层完成认证判定，认证通过后才接管 TLS 握手。认证失败的连接被**透明转发**至预配置的真实目标站，服务端在任何情况下均不主动发送协议错误响应，从外部看与真实 HTTPS 服务器无异。

#### 记录层流量整形

记录层对 Yamux 输出的字节流进行系统性整形：

- **分块策略**：按参考于真实浏览器数据传输分布的随机权重切片，同时叠加随机抖动，避免固定大小特征。
- **填充帧**：在数据帧之间按密钥派生参数概率性插入随机填充，干扰长度分布分析。每帧填充内容均由密码学随机数生成器产生。
- **心跳帧**：连接空闲期间定期发送随机大小的心跳帧，维持连接活跃性并消除流量静默时段指纹。
- **死对端检测**：接收方持续追踪最近一次有效帧时间，超时后主动关闭连接释放资源。

发送端和接收端的填充参数相互独立，由各自配置的 `padding_seed` 经密钥派生函数产生，接收方无需了解对端的整形策略。

#### 安全性质

| 性质 | 说明 |
|---|---|
| 外层不可区分 | 握手特征与真实 Chrome 浏览器一致 |
| 后量子安全 | 密钥交换支持 X25519MLKEM768 |
| 抗主动探测 | 认证失败路径与真实目标站响应一致，无协议泄露 |
| 抗重放攻击 | 令牌绑定时间窗口与连接随机数，并维护服务端防重放缓存 |
| 证书绑定 | 客户端以 SPKI 公钥指纹验证服务端证书，不依赖系统 CA 链 |
| 跨会话隐私 | 用户标识每小时轮换，历史握手记录无法关联到当前用户 |
| 内层无特征 | 无 TLS-in-TLS 嵌套，内层协议字节流对 TLS 层不透明 |

---

### 快速开始

#### 第一步：VPS 部署

在海外 VPS 上以 `root` 执行一键部署脚本：

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 8443
```

脚本完成后，终端将打印一条 `mirage://` 口令，保存好这条口令。

> 端口 `8443` 可替换为 `443` 或其他可用端口。

#### 第二步：Windows 客户端

运行 `miragec.exe`，按向导提示粘贴上一步获得的 `mirage://` 口令，即可完成连接配置。

向导完成后会显示本地订阅地址，将其导入 **Clash Verge Rev** 即可开始使用。

---

### 本地端点

| 用途 | 地址 |
|---|---|
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
install.sh          VPS 一键部署脚本
```

---

## English

### Overview

MIRAGE is a private transport protocol and implementation designed for adversarial network environments. The protocol addresses modern deep packet inspection (DPI) techniques systematically at the design level, rather than relying on any single obfuscation trick.

No standalone GUI is required on the client side. MIRAGE exposes standard SOCKS5/HTTP interfaces and a local subscription endpoint on the local machine, delegating system proxy management, TUN capture, and traffic rules to **Clash Verge Rev / mihomo**.

```
Application traffic
  → Clash Verge Rev (system proxy / TUN / rules)
    → MIRAGE local bridge core
      → MIRAGE VPS
        → destination
```

---

### Protocol Design

#### Layered Architecture

The MIRAGE protocol stack consists of three layers with strictly separated responsibilities:

```
┌─────────────────────────────────┐
│      Multiplexing (Yamux)        │  concurrent stream management, zero protocol exposure
├─────────────────────────────────┤
│      Record Layer                │  chunking · padding · heartbeat · traffic shaping
├─────────────────────────────────┤
│      Outer Encryption (TLS 1.3)  │  sole encryption layer, emulating real browser fingerprint
└─────────────────────────────────┘
               │ TCP
```

Each layer has a single, non-overlapping responsibility: encryption belongs exclusively to TLS, traffic shaping belongs exclusively to the record layer, and congestion control belongs exclusively to TCP.

#### Outer Indistinguishability

MIRAGE uses the **uTLS** framework to precisely emulate the TLS handshake fingerprint of mainstream browsers, including cipher suite preferences, extension ordering, key agreement parameters, and support for **post-quantum cryptography** (X25519MLKEM768). To a passive observer, a MIRAGE connection is indistinguishable from a real browser accessing the configured destination at the handshake level.

#### Covert Authentication

Authorization material is embedded in standard TLS handshake fields using a cryptographically secure construction — **without adding any new TLS extensions or modifying handshake packet structure**. Authentication tokens are derived from the pre-shared key and a time window, carry replay protection, and rotate the user identifier hourly to eliminate cross-session correlation fingerprints.

The server performs authentication at the raw TCP layer, before entering the TLS handshake. Connections that fail authentication are **transparently forwarded** to a pre-configured real destination. The server never emits a protocol error response of any kind under any failure condition; from the outside it is indistinguishable from a real HTTPS server.

#### Record Layer Traffic Shaping

The record layer systematically shapes the byte stream produced by Yamux:

- **Chunking**: Data is sliced into variable-size frames using a weighted distribution referenced against real browser traffic, with additional random jitter to avoid any fixed-size signature.
- **Padding frames**: Random-length padding frames are probabilistically inserted between data frames according to key-derived parameters, disrupting length distribution analysis. Each padding frame is independently filled with cryptographically random bytes.
- **Heartbeat frames**: During idle periods, heartbeat frames of random size are sent at randomized intervals, maintaining connection liveness and eliminating traffic-silence fingerprints.
- **Dead peer detection**: The receiver continuously tracks the last valid frame timestamp and proactively closes the connection when it exceeds the inactivity threshold.

The sender and receiver operate with fully independent shaping parameters, each derived from their respective `padding_seed` via a key derivation function. Neither side needs any knowledge of the other's shaping strategy.

#### Security Properties

| Property | Description |
|---|---|
| Outer indistinguishability | Handshake characteristics match real Chrome browser behavior |
| Post-quantum security | Key exchange supports X25519MLKEM768 |
| Active probe resistance | Authentication failure path is identical to the real destination response; no protocol leakage |
| Replay protection | Tokens are bound to a time window and connection nonce; a server-side cache prevents replay |
| Certificate binding | Client verifies the server certificate by SPKI public-key fingerprint, independently of the system CA store |
| Cross-session privacy | User identifiers rotate hourly; historical handshake records cannot be linked to the current session |
| No inner fingerprint | No TLS-in-TLS nesting; the inner protocol byte stream is opaque to the TLS layer |

---

### Quick Start

#### Step 1: Deploy the VPS Server

Run the one-command installer on your overseas VPS as `root`:

```bash
git clone https://github.com/WJSGZZ/MIRAGE.git
cd MIRAGE
bash install.sh 8443
```

When the script completes, a `mirage://` URI is printed to the terminal. Save it.

> Replace `8443` with `443` or any other available port.

#### Step 2: Windows Client

Run `miragec.exe` and follow the on-screen wizard. When prompted, paste the `mirage://` URI from the previous step.

The wizard will confirm the connection and display the local subscription URL. Import that URL into **Clash Verge Rev** to complete setup.

---

### Local Endpoints

| Purpose | Address |
|---|---|
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
install.sh          one-command VPS installer
```
