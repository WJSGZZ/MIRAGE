param(
  [string]$TargetTriple = "x86_64-pc-windows-msvc"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$binDir = Join-Path $repoRoot "desktop\src-tauri\binaries"
$outFile = Join-Path $binDir ("miragec-sidecar-" + $TargetTriple + ".exe")

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Push-Location $repoRoot
try {
  Write-Host "[*] Building MIRAGE sidecar..."
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  $env:CGO_ENABLED = "0"
  go build -trimpath -ldflags="-s -w -H=windowsgui" -o $outFile .\cmd\miragec
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
  }
  Write-Host "[+] Sidecar ready at $outFile"
} finally {
  Pop-Location
}
