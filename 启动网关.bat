@echo off
chcp 65001 >nul
title Free Model Gateway - AI Model Gateway
color 0A

echo.
echo ============================================================
echo            Free Model Gateway v1.0.0
echo            AI Model Smart Routing Gateway
echo ============================================================
echo.

cd /d "%~dp0"

if exist ".env" (
    echo [INFO] Loading .env...
    for /f "usebackq tokens=1,2 delims==" %%a in (".env") do (
        if not "%%a"=="" if not "%%a"=="#" (
            set "%%a=%%b"
        )
    )
)

if not exist "bin\fmg.exe" (
    echo [ERROR] Binary not found: bin\fmg.exe
    echo Run: go build -o bin\fmg.exe .\cmd\fmg\
    pause
    exit /b 1
)

if not exist "config.yaml" (
    echo [ERROR] Config not found: config.yaml
    echo Copy config.example.yaml to config.yaml first.
    pause
    exit /b 1
)

if not exist "logs" mkdir logs

echo.
echo [INFO] Starting Free Model Gateway...
echo [INFO] Listen: http://localhost:10086
echo [INFO] Press Ctrl+C to stop
echo.
echo ------------------------------------------------------------

bin\fmg.exe -c config.yaml -l info 2>&1 | tee logs\fmg.log

pause
