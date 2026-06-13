@echo off
setlocal enabledelayedexpansion

cd /d "%~dp0"

set "IMAGE=eaglestelle/swaratelle:latest"
set "LOCAL_ROOT=%CD%\.local"
set "LOCAL_DATA=%LOCAL_ROOT%\data"
set "LOCAL_MEDIA=%LOCAL_ROOT%\media"
set "LOCAL_SCRATCH=%LOCAL_ROOT%\scratch"

where docker >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Docker is not installed or not on PATH.
    echo Install Docker Desktop from https://www.docker.com/products/docker-desktop
    pause
    exit /b 1
)

docker info >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Docker is installed but not running.
    echo Start Docker Desktop and wait until it is ready, then run this again.
    pause
    exit /b 1
)

if not exist ".env" (
    if not exist ".env.example" (
        echo [ERROR] .env.example is missing.
        pause
        exit /b 1
    )

    echo Creating .env from .env.example with a generated API token...
    set "TOKEN="
    for /f "delims=" %%i in ('powershell -NoProfile -Command "[guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')"') do set "TOKEN=%%i"
    powershell -NoProfile -Command "$token='!TOKEN!'; $content=Get-Content -Raw '.env.example'; if ($content -match '(?m)^SWARATELLE_API_TOKEN=.*$') { $content=$content -replace '(?m)^SWARATELLE_API_TOKEN=.*$', ('SWARATELLE_API_TOKEN=' + $token) } else { $content=$content.TrimEnd() + [Environment]::NewLine + 'SWARATELLE_API_TOKEN=' + $token + [Environment]::NewLine }; Set-Content -Path '.env' -Value $content -NoNewline"
    if errorlevel 1 (
        echo [ERROR] Failed to create .env.
        pause
        exit /b 1
    )
    echo   .env created from .env.example with a generated API token.
) else (
    echo .env already exists, leaving it untouched.
)
echo.

if not exist ".local" (
    echo Creating .local folder...
    mkdir ".local"
)

if not exist ".local\data" (
    echo Creating .local\data folder...
    mkdir ".local\data"
)

if not exist ".local\media" (
    echo Creating .local\media folder...
    mkdir ".local\media"
)

if not exist ".local\scratch" (
    echo Creating .local\scratch folder...
    mkdir ".local\scratch"
)

echo.
echo Building image...
echo.
docker build -t "%IMAGE%" .
if errorlevel 1 (
    echo.
    echo [ERROR] docker build failed. See the output above.
    pause
    exit /b 1
)

echo.
echo Starting container...
docker rm -f swaratelle >nul 2>nul
docker run -d ^
    --name swaratelle ^
    --restart unless-stopped ^
    -p 8842:8842 ^
    --env-file "%CD%\.env" ^
    --mount "type=bind,source=%LOCAL_DATA%,target=/data" ^
    --mount "type=bind,source=%LOCAL_MEDIA%,target=/media" ^
    --mount "type=bind,source=%LOCAL_SCRATCH%,target=/scratch" ^
    "%IMAGE%"
if errorlevel 1 (
    echo.
    echo [ERROR] docker run failed. See the output above.
    pause
    exit /b 1
)

echo.
echo ============================================
echo   Running!
echo   Open http://localhost:8842 in your browser
echo   The UI logs in automatically - no token to paste.
echo ============================================
echo.
echo To stop:    docker stop swaratelle
echo To restart: docker start swaratelle
echo.
pause
