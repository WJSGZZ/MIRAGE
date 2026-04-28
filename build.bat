@echo off
REM Build MIRAGE server (Linux) and client (Windows) from Windows.
REM Requires Go: https://go.dev/dl/

echo [*] Downloading dependencies...
go mod tidy

echo.
echo [*] Embedding application icon...
where rsrc >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo     rsrc not found, installing...
    go install github.com/akavel/rsrc@latest
)
rsrc -ico assets/logo.ico -o cmd/miragec/rsrc.syso
if %ERRORLEVEL% NEQ 0 goto fail

echo [*] Building miragec.exe (Windows client core)...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags="-s -w" -o miragec.exe ./cmd/miragec
if %ERRORLEVEL% NEQ 0 goto fail

echo [*] Building miraged (Linux server)...
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o miraged ./cmd/miraged
if %ERRORLEVEL% NEQ 0 goto fail

echo.
echo [+] Done.
echo     miragec.exe  - MIRAGE core/sidecar for Clash bridge or headless mode
echo     miraged      - deploy to VPS, or run bash install.sh on VPS
goto end

:fail
echo [!] Build FAILED
exit /b 1

:end
