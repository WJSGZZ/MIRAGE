# MIRAGE Desktop

This directory contains the new desktop shell for MIRAGE.

## Architecture

- `miragec` stays the local Go core and dashboard server.
- Tauri starts `miragec` as a sidecar with `--no-browser`.
- The desktop window points its webview at `http://127.0.0.1:9099`.
- Closing the Tauri app also kills the Go sidecar.

This follows the same broad direction as mature desktop proxy clients:
an app shell plus a local core process, rather than a browser tab pretending
to be a desktop app.

## Prerequisites

- Node.js with npm
- Rust with Cargo
- Go 1.23+

## First-time setup

```powershell
cd desktop
npm install
powershell -ExecutionPolicy Bypass -File .\scripts\prepare-sidecar.ps1
```

## Development

```powershell
cd desktop
npm run dev
```

## Production build

```powershell
cd desktop
npm run prepare-sidecar
npm run build
```
