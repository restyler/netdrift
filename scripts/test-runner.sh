#!/bin/bash

# Comprehensive test runner with PID management
# Runs test proxies, main proxy, performs tests, and cleans up by PIDs

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
TEST_PROXY_PORTS="3025 3026"
MAIN_PROXY_PORT="3130"
PROXY_CONFIG="configs/us.json"
TIMEOUT=30

# PID tracking
declare -a PIDS=()
declare -a PID_FILES=()

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')] $1${NC}"
}

success() {
    echo -e "${GREEN}[$(date '+%Y-%m-%d %H:%M:%S')] ✓ $1${NC}"
}

warning() {
    echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] ⚠ $1${NC}"
}

error() {
    echo -e "${RED}[$(date '+%Y-%m-%d %H:%M:%S')] ✗ $1${NC}"
}

cleanup() {
    log "Cleaning up processes..."
    
    # Kill processes by PID files
    for pid_file in "${PID_FILES[@]}"; do
        if [ -f "$pid_file" ]; then
            pid=$(cat "$pid_file" 2>/dev/null || echo "")
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                log "Killing process $pid from $pid_file"
                kill "$pid" 2>/dev/null || true
                # Give it time to shut down gracefully
                sleep 1
                # Force kill if still running
                if kill -0 "$pid" 2>/dev/null; then
                    warning "Force killing process $pid"
                    kill -9 "$pid" 2>/dev/null || true
                fi
            fi
            rm -f "$pid_file"
        fi
    done
    
    # Kill any remaining processes on our ports
    for port in $TEST_PROXY_PORTS $MAIN_PROXY_PORT; do
        pid=$(lsof -ti:$port 2>/dev/null || echo "")
        if [ -n "$pid" ]; then
            warning "Killing remaining process $pid on port $port"
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    
    # Clean up any remaining PID files
    rm -f proxy.pid test-proxy-*.pid
    
    success "Cleanup completed"
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

wait_for_port() {
    local port=$1
    local name=$2
    local timeout=$3
    
    log "Waiting for $name on port $port..."
    
    for i in $(seq 1 $timeout); do
        if nc -z 127.0.0.1 $port 2>/dev/null; then
            success "$name is ready on port $port"
            return 0
        fi
        sleep 1
    done
    
    error "$name failed to start on port $port within ${timeout}s"
    return 1
}

build_binaries() {
    log "Building binaries..."
    
    if ! make build-test; then
        error "Failed to build test-proxy"
        return 1
    fi
    
    if ! make build; then
        error "Failed to build main proxy"
        return 1
    fi
    
    success "Binaries built successfully"
}

start_test_proxies() {
    log "Starting test proxies on ports: $TEST_PROXY_PORTS"
    
    ./bin/test-proxy $TEST_PROXY_PORTS > test-proxies.log 2>&1 &
    local test_proxy_pid=$!
    PIDS+=($test_proxy_pid)
    
    # Track PID files that should be created
    for port in $TEST_PROXY_PORTS; do
        PID_FILES+=("test-proxy-${port}.pid")
    done
    
    log "Test proxies started with PID $test_proxy_pid"
    
    # Wait for all test proxy ports to be ready
    for port in $TEST_PROXY_PORTS; do
        if ! wait_for_port $port "Test proxy" 10; then
            return 1
        fi
    done
    
    success "All test proxies are running"
}

start_main_proxy() {
    log "Starting main proxy..."
    
    if [ ! -f "$PROXY_CONFIG" ]; then
        error "Configuration file $PROXY_CONFIG not found"
        return 1
    fi
    
    ./bin/proxy -config "$PROXY_CONFIG" > proxy.log 2>&1 &
    local proxy_pid=$!
    PIDS+=($proxy_pid)
    PID_FILES+=("proxy.pid")
    
    log "Main proxy started with PID $proxy_pid"
    
    if ! wait_for_port $MAIN_PROXY_PORT "Main proxy" 10; then
        return 1
    fi
    
    success "Main proxy is running"
}

run_tests() {
    log "Running tests..."
    
    # Test 1: Basic connectivity test (with auth if enabled)
    log "Test 1: Basic connectivity"
    local auth_url="http://127.0.0.1:$MAIN_PROXY_PORT"
    
    if grep -q '"enabled": true' "$PROXY_CONFIG"; then
        # Get credentials from config
        local username=$(grep -A 10 '"authentication"' "$PROXY_CONFIG" | grep '"username"' | head -1 | sed 's/.*"username": *"\([^"]*\)".*/\1/')
        local password=$(grep -A 10 '"authentication"' "$PROXY_CONFIG" | grep '"password"' | head -1 | sed 's/.*"password": *"\([^"]*\)".*/\1/')
        
        if [ -n "$username" ] && [ -n "$password" ]; then
            auth_url="http://${username}:${password}@127.0.0.1:$MAIN_PROXY_PORT"
        fi
    fi
    
    if curl -x "$auth_url" --connect-timeout 10 -s -o /dev/null -w "%{http_code}" https://httpbin.org/ip | grep -q "200"; then
        success "Basic connectivity test passed"
    else
        error "Basic connectivity test failed"
        return 1
    fi
    
    # Test 2: Test with authentication (if enabled)
    log "Test 2: Authentication test"
    if grep -q '"enabled": true' "$PROXY_CONFIG"; then
        # Get credentials from config
        local username=$(grep -A 10 '"authentication"' "$PROXY_CONFIG" | grep '"username"' | head -1 | sed 's/.*"username": *"\([^"]*\)".*/\1/')
        local password=$(grep -A 10 '"authentication"' "$PROXY_CONFIG" | grep '"password"' | head -1 | sed 's/.*"password": *"\([^"]*\)".*/\1/')
        
        if [ -n "$username" ] && [ -n "$password" ]; then
            if curl -x http://${username}:${password}@127.0.0.1:$MAIN_PROXY_PORT --connect-timeout 10 -s -o /dev/null -w "%{http_code}" https://httpbin.org/ip | grep -q "200"; then
                success "Authentication test passed"
            else
                error "Authentication test failed"
                return 1
            fi
        else
            warning "Could not extract credentials from config"
        fi
    else
        success "Authentication disabled, skipping auth test"
    fi
    
    # Test 3: Stats endpoint
    log "Test 3: Stats endpoint"
    if curl -s http://127.0.0.1:$MAIN_PROXY_PORT/stats | grep -q "total_requests"; then
        success "Stats endpoint test passed"
    else
        error "Stats endpoint test failed"
        return 1
    fi
    
    # Test 4: Load test (multiple concurrent requests)
    log "Test 4: Load test (5 concurrent requests)"
    local success_count=0
    local pids=()
    
    for i in {1..5}; do
        (
            if curl -x "$auth_url" --connect-timeout 10 --max-time 15 -s -o /dev/null -w "%{http_code}" https://httpbin.org/ip | grep -q "200"; then
                echo "success" > "/tmp/load_test_$i.result"
            else
                echo "failed" > "/tmp/load_test_$i.result"
            fi
        ) &
        pids+=($!)
    done
    
    # Wait for all background jobs with timeout
    for pid in "${pids[@]}"; do
        wait $pid
    done
    
    # Count successes
    for i in {1..5}; do
        if [ -f "/tmp/load_test_$i.result" ] && grep -q "success" "/tmp/load_test_$i.result"; then
            ((success_count++))
        fi
        rm -f "/tmp/load_test_$i.result"
    done
    
    if [ $success_count -ge 4 ]; then
        success "Load test passed ($success_count/5 requests succeeded)"
    else
        error "Load test failed ($success_count/5 requests succeeded)"
        return 1
    fi
    
    success "All tests passed!"
}

show_status() {
    log "System Status:"
    echo "Active processes:"
    
    for pid_file in "${PID_FILES[@]}"; do
        if [ -f "$pid_file" ]; then
            pid=$(cat "$pid_file" 2>/dev/null || echo "")
            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                echo "  - $pid_file: PID $pid (running)"
            else
                echo "  - $pid_file: PID $pid (not running)"
            fi
        else
            echo "  - $pid_file: not found"
        fi
    done
    
    echo
    echo "Port status:"
    for port in $TEST_PROXY_PORTS $MAIN_PROXY_PORT; do
        if nc -z 127.0.0.1 $port 2>/dev/null; then
            echo "  - Port $port: OPEN"
        else
            echo "  - Port $port: CLOSED"
        fi
    done
    
    echo
    echo "Log files:"
    for log_file in test-proxies.log proxy.log; do
        if [ -f "$log_file" ]; then
            echo "  - $log_file: $(wc -l < "$log_file") lines"
        fi
    done
}

main() {
    log "Starting comprehensive proxy test runner"
    
    # Check if we should just show status
    if [ "$1" = "status" ]; then
        show_status
        exit 0
    fi
    
    # Check if we should just cleanup
    if [ "$1" = "cleanup" ]; then
        cleanup
        exit 0
    fi
    
    # Build binaries
    if ! build_binaries; then
        error "Failed to build binaries"
        exit 1
    fi
    
    # Start test proxies
    if ! start_test_proxies; then
        error "Failed to start test proxies"
        exit 1
    fi
    
    # Start main proxy
    if ! start_main_proxy; then
        error "Failed to start main proxy"
        exit 1
    fi
    
    # Give everything a moment to stabilize
    sleep 2
    
    # Run tests
    if ! run_tests; then
        error "Tests failed"
        show_status
        exit 1
    fi
    
    success "All tests completed successfully!"
    show_status
}

# Show usage if help requested
if [ "$1" = "help" ] || [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "Usage: $0 [command]"
    echo
    echo "Commands:"
    echo "  (no args)  - Run full test suite"
    echo "  status     - Show current status"
    echo "  cleanup    - Clean up processes and PID files"
    echo "  help       - Show this help"
    echo
    echo "The script will:"
    echo "  1. Build test-proxy and proxy binaries"
    echo "  2. Start test proxies on ports: $TEST_PROXY_PORTS"
    echo "  3. Start main proxy on port: $MAIN_PROXY_PORT"
    echo "  4. Run comprehensive tests"
    echo "  5. Clean up all processes using PID files"
    echo
    echo "PID files created:"
    echo "  - proxy.pid (main proxy)"
    echo "  - test-proxy-XXXX.pid (test proxies)"
    echo
    echo "Log files created:"
    echo "  - test-proxies.log (test proxy output)"
    echo "  - proxy.log (main proxy output)"
    exit 0
fi

# Run main function
main "$@"