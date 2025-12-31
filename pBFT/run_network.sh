#!/bin/bash

# --- FUNCTION DỌN DẸP MẠNH MẼ ---
cleanup() {
    echo ""
    echo "🛑 Shutting down pBFT network..."
    
    # Giết tiến trình bằng tên (Force Kill)
    pkill -f "node-app"
    pkill -f "dashboard-app"
    
    # Xóa file rác (Database & Log cũ)
    rm ledger.db 2>/dev/null
    rm dashboard.log 2>/dev/null
    
    echo "✅ Cleanup done."
    exit
}

# Bắt mọi tín hiệu thoát (Ctrl+C, Error, Kill)
trap cleanup SIGINT SIGTERM EXIT

# --- START ---

echo "🧹 Pre-cleaning..."
pkill -f "node-app"
pkill -f "dashboard-app"

echo "🔨 Building..."
# 1. Build Node
go build -o node-app main.go
if [ $? -ne 0 ]; then echo "❌ Build Node Failed"; exit 1; fi

# 2. Build Dashboard 
go build -o dashboard-app dashboard/server.go
if [ $? -ne 0 ]; then echo "❌ Build Dashboard Failed"; exit 1; fi

echo "🚀 Starting Dashboard (Logs -> dashboard.log)..."
./dashboard-app > dashboard.log 2>&1 &
DASH_PID=$!

echo "🚀 Starting 5 pBFT Nodes..."
./node-app -id=node1 -port=50051 &
./node-app -id=node2 -port=50052 &
./node-app -id=node3 -port=50053 &
./node-app -id=node4 -port=50054 &
./node-app -id=node5 -port=50055 &

echo "✅ SYSTEM STARTED!"
echo "👉 Dashboard Log: tail -f dashboard.log"
echo "👉 Web UI: http://localhost:8080"
echo "⌨️  Press Ctrl+C to stop everything."

# Giữ script chạy theo Dashboard PID
wait $DASH_PID