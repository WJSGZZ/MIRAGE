@echo off
REM Build MIRAGE server (Linux) and client (Windows) from Windows.
REM Requires Go: https://go.dev/dl/

echo [*] Downloading dependencies...
go mod tidy

echo.
echo [*] Building miragec.exe (Windows client)...
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
echo     miragec.exe  — run on this Windows machine
echo     miraged      — scp to VPS, then: bash install.sh
goto end

:fail
echo [!] Build FAILED
exit /b 1

:end
