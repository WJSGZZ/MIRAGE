[English](#english) | [中文](#中文)

---

<a name="english"></a>

# MIRAGE

A censorship-resistant proxy protocol with a dedicated client and server.

MIRAGE is distinct from Xray/V2Ray in three ways:

- Authentication uses BLAKE3 key derivation instead of HKDF-SHA256
- The inner transport is a custom multiplexing protocol, not VLESS
- Authentication happens after the TLS handshake, over the encrypted channel

Active probers who reach the server without valid credentials receive an HTTP 400 response, indistinguishable from a normal web server rejecting a bad request.

## How it works

```
[Windows PC]                          [VPS]
  miragec.exe                         miraged
  SOCKS5 :1080  <-->  TLS 1.3  <-->  mux server  <-->  destination
                      BLAKE3 auth
                      custom mux frames
```

1. The client exposes a local SOCKS5 proxy on `127.0.0.1:1080`.
2. Each SOCKS5 request opens a mux stream over a shared TLS session to the server.
3. The server dials the destination and relays traffic bidirectionally.

## Server deployment

**Requirements:** Linux VPS, Go 1.23+, an open TCP port (default 8443).

```bash
git clone https://github.com/YOUR_USERNAME/MIRAGE /opt/miraged-src
cd /opt/miraged-src
go mod tidy
go build -o miraged ./cmd/miraged

mkdir -p /opt/miraged
cp miraged /opt/miraged/
cd /opt/miraged

# Generate key pair and sample configs
./miraged -genkey
```

Copy the printed `config.json` block into `/opt/miraged/config.json`.
Change `"listen"` to `"0.0.0.0:8443"` if port 443 is already in use.

```bash
# Create systemd service
cat > /etc/systemd/system/miraged.service <<EOF
[Unit]
Description=MIRAGE proxy server
After=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/miraged
ExecStart=/opt/miraged/miraged -c /opt/miraged/config.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable miraged
systemctl start miraged

# Open firewall
ufw allow 8443/tcp
```

Verify: `journalctl -u miraged -f` should print `miraged: listening on 0.0.0.0:8443`.

## Client setup (Windows)

**Requirements:** Go 1.23+ (only needed to build once).

```bat
cd MIRAGE
build.bat
```

This produces `miragec.exe` (Windows) and `miraged` (Linux) in the project root.

Copy the `client.json` block printed by `miraged -genkey` into `client.json`.
Fill in:

| Field | Value |
|---|---|
| `server` | `YOUR_VPS_IP:8443` |
| `serverPubKey` | public key from `miraged -genkey` |
| `shortId` | shortId from `miraged -genkey` |
| `insecureSkipVerify` | `true` (self-signed cert) or `false` (real cert) |

```bat
miragec.exe -c client.json
```

Then point your browser or system proxy to `SOCKS5 127.0.0.1:1080`.

## TLS certificates

If `certFile` and `keyFile` are left empty in `config.json`, the server
auto-generates a self-signed certificate on first run and saves it as
`mirage-cert.pem` / `mirage-key.pem` next to the binary.

For production, provide a real certificate (e.g. Let's Encrypt) and set
`insecureSkipVerify: false` on the client.

## File structure

```
cmd/
  miraged/        server binary
  miragec/        client binary
internal/
  auth/           BLAKE3 post-handshake authentication
  mux/            custom inner multiplexing protocol
  config/         server and client config loading
  server/         TLS listener + auth + mux server
  client/         mux client + SOCKS5 proxy
  certutil/       self-signed TLS certificate generation
config.example.json   server config template
client.example.json   client config template
build.bat             build script (Windows)
install.sh            server install script (Linux)
```

## Authentication wire format

```
Client sends 80 bytes immediately after TLS handshake:

  [0:32]  auth_pub   client ephemeral X25519 public key
  [32:40] timestamp  big-endian int64 Unix seconds
  [40:48] short_id   user shortId, zero-padded to 8 bytes
  [48:80] mac        BLAKE3.derive_key("MIRAGE-v1-client-auth",
                       ecdhe_secret || timestamp || short_id)

Server replies with 1 byte:
  0x00 = accepted, enter mux mode
  (connection close = rejected)
```

---

<a name="中文"></a>

# MIRAGE

一个带有独立客户端和服务端的抗审查代理协议。

MIRAGE 与 Xray/V2Ray 的核心区别：

- 认证使用 BLAKE3 密钥派生，而非 HKDF-SHA256
- 内层传输是自研多路复用协议，而非 VLESS
- 认证在 TLS 握手完成后、在加密信道内进行

没有有效凭证的主动探测者连接到服务器后，只会收到 HTTP 400 响应，与普通 Web 服务器拒绝非法请求的行为完全一致。

## 工作原理

```
[Windows 电脑]                        [VPS]
  miragec.exe                         miraged
  SOCKS5 :1080  <-->  TLS 1.3  <-->  mux 服务端  <-->  目标网站
                      BLAKE3 认证
                      自研 mux 帧
```

1. 客户端在本机 `127.0.0.1:1080` 暴露一个 SOCKS5 代理。
2. 每个 SOCKS5 请求通过复用的 TLS 会话向服务端开一条 mux stream。
3. 服务端连接目标地址，双向中继流量。

## 服务端部署

**要求：** Linux VPS、Go 1.23+、一个开放的 TCP 端口（默认 8443）。

```bash
git clone https://github.com/YOUR_USERNAME/MIRAGE /opt/miraged-src
cd /opt/miraged-src
go mod tidy
go build -o miraged ./cmd/miraged

mkdir -p /opt/miraged
cp miraged /opt/miraged/
cd /opt/miraged

# 生成密钥对和示例配置
./miraged -genkey
```

将输出的 `config.json` 内容写入 `/opt/miraged/config.json`。
如果 443 端口已被占用，将 `"listen"` 改为 `"0.0.0.0:8443"`。

```bash
# 创建 systemd 服务
cat > /etc/systemd/system/miraged.service <<EOF
[Unit]
Description=MIRAGE proxy server
After=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/miraged
ExecStart=/opt/miraged/miraged -c /opt/miraged/config.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable miraged
systemctl start miraged

# 开放防火墙
ufw allow 8443/tcp
```

验证：`journalctl -u miraged -f` 应输出 `miraged: listening on 0.0.0.0:8443`。

## 客户端使用（Windows）

**要求：** Go 1.23+（只需编译一次）。

```bat
cd MIRAGE
build.bat
```

生成 `miragec.exe`（Windows 客户端）和 `miraged`（Linux 服务端二进制）。

将 `miraged -genkey` 输出的 `client.json` 内容写入 `client.json`，填入：

| 字段 | 说明 |
|---|---|
| `server` | `VPS公网IP:8443` |
| `serverPubKey` | `miraged -genkey` 输出的公钥 |
| `shortId` | `miraged -genkey` 输出的 shortId |
| `insecureSkipVerify` | 自签证书填 `true`，真实证书填 `false` |

```bat
miragec.exe -c client.json
```

将浏览器或系统代理设置为 `SOCKS5 127.0.0.1:1080` 即可使用。

## TLS 证书

`config.json` 中 `certFile` 和 `keyFile` 留空时，服务端首次运行会自动生成自签证书，
保存为二进制同目录下的 `mirage-cert.pem` / `mirage-key.pem`。

生产环境建议使用真实证书（如 Let's Encrypt），并将客户端 `insecureSkipVerify` 设为 `false`。

## 文件结构

```
cmd/
  miraged/        服务端二进制
  miragec/        客户端二进制
internal/
  auth/           BLAKE3 认证（TLS 握手后）
  mux/            自研内层多路复用协议
  config/         配置加载
  server/         TLS 监听 + 认证 + mux 服务端
  client/         mux 客户端 + SOCKS5 代理
  certutil/       自签 TLS 证书生成
config.example.json   服务端配置模板
client.example.json   客户端配置模板
build.bat             编译脚本（Windows）
install.sh            服务端安装脚本（Linux）
```

## 认证报文格式

```
TLS 握手完成后，客户端立即发送 80 字节：

  [0:32]  auth_pub   客户端临时 X25519 公钥
  [32:40] timestamp  大端 int64 Unix 秒时间戳
  [40:48] short_id   用户 shortId，不足 8 字节补零
  [48:80] mac        BLAKE3.derive_key("MIRAGE-v1-client-auth",
                       ecdhe_secret || timestamp || short_id)

服务端回复 1 字节：
  0x00 = 认证通过，进入 mux 模式
  （关闭连接 = 认证失败）
```
