@echo off
echo ========================================================
echo       pBFT CONSENSUS NETWORK (WINDOWS LAUNCHER)
echo ========================================================

:: --- 1. CLEANUP (Giết tiến trình cũ) ---
echo [1/4] Cleaning up old processes...
taskkill /F /IM node-app.exe >nul 2>&1
taskkill /F /IM dashboard-app.exe >nul 2>&1
del ledger.db >nul 2>&1
del dashboard.log >nul 2>&1

:: --- 2. BUILD ---
echo [2/4] Building project...

:: Build Dashboard
go build -o dashboard-app.exe dashboard/server.go
IF %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Build Dashboard Failed!
    pause
    exit /b
)

:: Build Node (Sửa đường dẫn pBFT/main.go tùy theo cấu trúc folder thật của bạn)
:: Giả sử file main nằm ở folder pBFT
go build -o node-app.exe pBFT/main.go
IF %ERRORLEVEL% NEQ 0 (
    echo [ERROR] Build Node Failed!
    pause
    exit /b
)

:: --- 3. START DASHBOARD ---
echo [3/4] Starting Dashboard...
start /B dashboard-app.exe > dashboard.log 2>&1

:: --- 4. START NODES ---
echo [4/4] Starting 5 pBFT Nodes...

:: Dùng start /B để chạy ngầm (Background)
start /B node-app.exe -id=node1 -port=50051
start /B node-app.exe -id=node2 -port=50052
start /B node-app.exe -id=node3 -port=50053
start /B node-app.exe -id=node4 -port=50054
start /B node-app.exe -id=node5 -port=50055

echo.
echo ✅ SYSTEM STARTED SUCCESSFULLY!
echo 👉 Dashboard: http://localhost:8080
echo 👉 Logs: Type 'type dashboard.log' to view logs.
echo.
echo ⚠️  PRESS ANY KEY TO STOP THE SYSTEM AND KILL ALL PROCESSES...
pause >nul

:: --- SHUTDOWN ---
echo.
echo 🛑 Shutting down...
taskkill /F /IM node-app.exe >nul 2>&1
taskkill /F /IM dashboard-app.exe >nul 2>&1
echo Done.