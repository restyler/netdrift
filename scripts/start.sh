#!/bin/bash

# Simple startup script for netdrift

set -e

echo "🚀 Starting netdrift..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or higher."
    exit 1
fi

# Build the applications
echo "🔨 Building applications..."
go build -o proxy main.go
go build -o test-proxy test_proxy.go

# Function to cleanup background processes
cleanup() {
    echo "🛑 Shutting down..."
    if [[ ! -z "$TEST_PROXY_PID" ]]; then
        kill $TEST_PROXY_PID 2>/dev/null || true
    fi
    if [[ ! -z "$MAIN_PROXY_PID" ]]; then
        kill $MAIN_PROXY_PID 2>/dev/null || true
    fi
    exit 0
}

# Set up signal handling
trap cleanup SIGINT SIGTERM

# Start test proxies in background
echo "🔧 Starting test proxy servers on ports 1025 and 1026..."
./test-proxy 1025 1026 &
TEST_PROXY_PID=$!

# Wait a moment for test proxies to start
sleep 2

# Start main proxy
echo "🎯 Starting main proxy server on port 3130..."
./proxy &
MAIN_PROXY_PID=$!

# Wait a moment for main proxy to start
sleep 2

echo ""
echo "✅ All services started successfully!"
echo ""
echo "📊 Test the proxy:"
echo "   curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net"
echo ""
echo "📈 Check statistics:"
echo "   curl http://127.0.0.1:3130/stats"
echo ""
echo "🛑 Press Ctrl+C to stop all services"

# Wait for user to stop
wait 