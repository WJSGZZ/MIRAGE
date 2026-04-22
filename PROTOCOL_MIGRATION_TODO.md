# MIRAGE Protocol Migration TODO

This repository currently contains a working prototype, not a complete
implementation of `MIRAGE_Protocol_Spec.md` version `1.0.2-draft`.

The goal of this plan is to migrate the codebase from the current
`post-TLS custom auth + custom mux + dashboard-first client` prototype into the
spec-defined architecture:

- raw TCP ClientHello parsing
- session_id based auth
- fallback-first server decision path
- TLS -> record.Conn -> Yamux
- PSK + cert_pin + padding seed based profile model
- core control API and desktop client split

## Status Model

- `done`: implemented in code and wired into runtime
- `partial`: scaffolding exists, but not yet fully wired
- `todo`: not started

## Track A: Config and Data Model

- `done` Add spec-aligned profile fields to the shared config model:
  - client PSK
  - cert pin(s)
  - client/server padding seed
  - control API listen address
  - fallback target
  - proxy mode
- `partial` Make generated `server.json` and `client.json` examples spec-first.
- `done` Enforce UID collision as config load error; upgrade to process-fatal startup path when the new server runtime lands.
- `done` Auto-generate missing seeds with `crypto/rand`.

## Track B: mirage:// Link Format

- `done` Support spec link fields:
  - credentials = `Base64Url(name:base64(psk))`
  - `sni`
  - `cert_pin`
  - `seed`
  - `name`
- `done` Preserve backward compatibility for legacy prototype links during migration.
- `todo` Add QR/export helpers after desktop client is stabilized.

## Track C: Cryptographic Derivation

- `partial` Implement:
  - UID derivation from PSK
  - padding parameter derivation
  - HMAC token derivation
  - replay key derivation
  - cert SPKI pin derivation
- `todo` Add test vectors against the spec.
- `todo` Add startup self-checks for malformed PSK and malformed seeds.

## Track D: Handshake Rewrite

- `partial` Replace current post-TLS auth message with ClientHello `session_id` auth.
- `done` Introduce raw ClientHello parser on bare TCP.
- `partial` Implement replay-safe fallback decision before calling any TLS API.
- `done` Introduce replayConn / first-bytes replay wrapper for the authenticated path.
- `done` Switch client TLS stack to uTLS Chrome profile and inject session_id bytes.
- `todo` Replace `InsecureSkipVerify` with pin-only verification using `VerifyPeerCertificate`.

## Track E: Record Layer

- `done` Create `record.Conn` package boundary and first implementation.
- `done` Implement frame parser with:
  - DATA
  - PADDING
  - HEARTBEAT
  - unknown type discard
- `done` Implement write slicing with the spec length limit `<= 16383`.
- `partial` Implement seed-derived padding strategy.
- `done` Implement background heartbeat goroutine lifecycle.
- `partial` Add invariants tests:
  - padding stripped output equals original byte stream
  - unknown type does not corrupt subsequent frames
  - no oversized DATA frames

## Track F: Multiplexing

- `done` Replace `internal/mux` custom implementation with `github.com/hashicorp/yamux`.
- `done` Ensure `EnableKeepAlive = false`.
- `done` Ensure Yamux sits above `record.Conn`, never directly above TLS.
- `partial` Add stream lifecycle integration tests.

## Track G: Server Runtime

- `partial` Replace current `tls.Listen` accept flow with:
  - bare TCP accept
  - ClientHello parse
  - auth/fallback branch
  - replayConn -> tls.Server
  - record.Conn
  - yamux.Server
- `partial` Add fallback raw byte replay and lifecycle ownership rules.
- `todo` Add startup cert pin logging.
- `todo` Add control API endpoints defined in spec section 13.4.

## Track H: Client Runtime

- `done` Replace current `crypto/tls` client handshake with uTLS Chrome profile.
- `done` Inject spec session_id auth bytes.
- `partial` Add pin-only certificate verification.
- `done` Wrap TLS connection in `record.Conn`.
- `done` Replace custom mux session with Yamux client.

## Track I: Control Plane

- `partial` Existing dashboard already covers some state/diagnostics flows.
- `done` Normalize control API to the spec paths:
  - `/health`
  - `/version`
  - `/state`
  - `/stats`
  - `/profiles`
  - `/connect`
  - `/disconnect`
  - `/reload-config`
  - `/logs`
- `partial` Keep legacy `/api/*` routes alive while the desktop client migrates to spec-shaped endpoints.
- `todo` Upgrade `/logs` from JSON snapshot to long-poll or SSE.
- `done` Replace placeholder `/stats` counters with real traffic accounting.
- `todo` Split core API from dashboard presentation API if needed.

## Track J: Desktop Client

- `done` Native WPF client exists in `MirageClient.WPF/` and now compiles, launches, and auto-starts the local core.
- `done` Add bilingual UI support:
  - follow system language by default
  - manual `zh-CN` / `en-US` switch in native settings
- `partial` Make WPF the primary Windows startup/build/distribution path.
- `partial` Add tray, single-instance, startup options, profile management, and native import/export flows.
- `partial` Add system proxy policy management in the native client:
  - off
  - system
  - manual
  - pac
- `partial` Remove remaining browser-first assumptions and stop treating the dashboard as the main client.

## Track K: TUN and UDP

- `todo` Introduce TUN mode architecture per spec section 12.
- `todo` Define separate AmneziaWG path for latency-sensitive UDP.
- `todo` Add DNS leak prevention and VPS loop prevention.

## Immediate Sequence

1. `done` Freeze the current prototype behavior as `legacy`.
2. `done` Land spec-aligned config, URI, and crypto primitives.
3. `todo` Land record layer package and tests.
4. `todo` Swap custom mux for Yamux.
5. `todo` Rewrite handshake to raw TCP + session_id auth + fallback.
6. `todo` Switch client handshake to uTLS and pin-only trust.
7. `partial` Stabilize control API and native desktop shell.

## Current Gap Snapshot

- The server now accepts raw TCP, parses ClientHello, and can authenticate spec `session_id` on the bare connection before calling `tls.Server`.
- The repository now runs `uTLS/ TLS -> record.Conn -> Yamux`, and both client and server have a spec `session_id` path; the remaining handshake work is mostly around hardening and removing the legacy fallback auth path when migration is complete.
- The server is now dual-stack during migration: spec `session_id` auth on the new path, legacy post-TLS auth still available for existing clients.
- New `mirage://` links can now reach the spec client handshake path, but end-to-end runtime verification against a real MIRAGE server is still needed.
- The server can now compute and print SPKI pin values, and the client will enforce SPKI pin matching when `cert_pin` is configured.
- The dashboard/core API now exposes the spec-shaped control endpoints alongside the older `/api/*` compatibility surface.
- The core now exposes real upload/download counters and per-second rates through `/stats`, so native clients can render traffic instead of placeholder cards.
- A native WPF Windows client now builds and starts successfully, with Chinese/English localization, tray integration, copyable diagnostics, proxy mode settings, and automatic local core startup.
- This means the migration foundation is in place, but the protocol implementation is not yet `1.0.2-draft` complete.

## Repository Mapping

- `internal/config/`:
  profile fields, seeds, cert pin, control listen, proxy mode
- `internal/uri/`:
  new mirage:// format and backward compatibility
- `internal/certutil/`:
  SPKI pin derivation
- `internal/auth/`:
  migrate from legacy post-TLS auth to spec session_id auth
- `internal/record/`:
  new package for record layer framing
- `internal/mux/`:
  remove after Yamux migration
- `internal/server/`:
  raw TCP parse, fallback, replay cache, TLS handoff
- `internal/client/`:
  uTLS, session_id, pin verify, record.Conn, yamux client
- `internal/dashboard/`:
  control API normalization and GUI-facing presentation
- `MirageClient.WPF/`:
  primary native Windows client shell
