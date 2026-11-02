@echo off
chcp 65001 >nul
title Frame 一键启动
cd /d "%~dp0"

echo ============================================
echo   Frame AI 短剧创作 - 启动中
echo ============================================
echo.

REM 后端：先看端口是否已被占用，避免重复启动
netstat -ano | findstr ":8080" | findstr "LISTENING" >nul
if %errorlevel%==0 (
  echo [后端] 8080 已在运行，跳过
) else (
  echo [后端] 启动中...
  start "Frame 后端" cmd /k "cd /d "%~dp0backend" && bin\frame-server.exe"
)

REM 前端
netstat -ano | findstr ":5173" | findstr "LISTENING" >nul
if %errorlevel%==0 (
  echo [前端] 5173 已在运行，跳过
) else (
  echo [前端] 启动中...
  start "Frame 前端" cmd /k "cd /d "%~dp0frontend" && npm run dev"
)

echo.
echo 等待服务就绪...
timeout /t 6 /nobreak >nul

echo.
echo   前端  http://localhost:5173
echo   后端  http://127.0.0.1:8080
echo.
echo 两个窗口请保持开启；关掉窗口即停止对应服务。
echo ============================================
start "" http://localhost:5173
pause
