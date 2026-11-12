@echo off
rem KILLER 大盘监控系统启动脚本
rem 双击运行即可在独立窗口启动服务（脱离 IDE 会话常驻）

cd /d "%~dp0"
echo [KILLER] 正在启动大盘监控服务...
echo [KILLER] 面板地址: http://localhost:8080
echo [KILLER] 关闭本窗口即停止服务

killer.exe

echo.
echo [KILLER] 服务已停止。
pause
