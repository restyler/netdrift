# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based HTTP forward proxy server called "netdrift" that implements advanced weighted load balancing across multiple upstream proxies with intelligent health monitoring and automatic failover. It supports HTTP CONNECT tunneling for HTTPS traffic, basic authentication, upstream proxy authentication, and provides comprehensive statistics tracking.

**Performance**: 4.4M operations/second (227ns/op) with stress-tested concurrent handling and automatic health management.

**For detailed technical architecture, code structure, and system design, see [ARCHITECTURE.md](ARCHITECTURE.md)**

## Key Architecture

- **Main Proxy Server** (`cmd/proxy/main.go`): The core proxy server that handles CONNECT requests and load balances across upstream proxies
- **Test Proxy Servers** (`pkg/proxy/test_proxy.go`): Simple test proxy servers for development and testing
- **Configuration**: JSON-based configuration system (`configs/us.json`)
- **Statistics System**: Comprehensive metrics tracking with detailed per-upstream statistics and time-window analytics

The proxy works by:
1. Receiving CONNECT requests from clients with authentication
2. Selecting upstream proxy using round-robin algorithm
3. Establishing tunnel through selected upstream proxy
4. Handling bidirectional data copying between client and target

## Common Commands

### Build Commands
```bash
make build          # Build main proxy server
make build-test     # Build test proxy servers
make build-faulty   # Build faulty proxy server
make clean          # Clean build artifacts
```

### Running Services
```bash
make run-proxy           # Run main proxy server
make run-test-proxies    # Run test proxy servers on ports 3025 and 3026
make test               # Test proxy with curl
make test-unit          # Run unit tests for main proxy
make test-integration   # Run integration tests with real proxy servers
make test-faultyproxy   # Run faulty proxy tests only
make test-faultyproxy-full  # Run comprehensive faulty proxy test suite
make test-faultyproxy-bench # Run faulty proxy benchmarks
```

### Docker Commands
```bash
make docker-build    # Build Docker image
make docker-run      # Run Docker container
```

### Manual Commands
```bash
# Run main proxy
go run ./cmd/proxy

# Run test proxies
go run ./cmd/test-proxy 3025 3026

# Run faulty proxy with different configurations
./faulty-proxy -port 8081 -failure-rate 0.3 -fault-type reset
./faulty-proxy -port 8082 -latency 2s -jitter 500ms -fault-type slow
./faulty-proxy -port 8083 -failure-rate 0.1 -fault-type timeout

# Test with authentication
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net

# Check statistics
curl http://127.0.0.1:3130/stats
```

## Configuration

The proxy uses `us.json` for configuration (or file specified via `PROXY_CONFIG` environment variable). Key sections:
- **Server**: Listen address and stats endpoint
- **Authentication**: Enable/disable with user credentials
- **Upstream Proxies**: List of proxy servers with weights and enabled status

## Statistics System

The proxy provides detailed statistics at the `/stats` endpoint including:
- Total/success/failed request counts
- Per-upstream proxy metrics (latency, connections, requests)
- Time-window statistics (total lifetime and recent 15-minute windows)
- Current active connections and max concurrency

## Testing

**IMPORTANT: Always run integration tests via `make test-integration` before and after every feature implementation to ensure system stability.**

### Integration Testing
Run comprehensive integration tests with real proxy servers:
```bash
make test-integration   # Full integration test suite with server startup/teardown
```

### Regular Testing
Use the built-in test proxies for development:
1. Start test proxies: `make run-test-proxies`
2. Start main proxy: `make run-proxy`
3. Test functionality: `make test`

### Unit Testing
Run unit tests for the main proxy package:
```bash
make test-unit
```

### Faulty Proxy Testing
The faulty proxy has its own isolated test suite with comprehensive testing:

```bash
# Quick unit tests only
make test-faultyproxy

# Full test suite (unit, integration, benchmarks, coverage)
make test-faultyproxy-full

# Performance benchmarks only
make test-faultyproxy-bench

# Manual testing with different fault modes
make build-faulty
./faulty-proxy -help                                    # Show all options
./faulty-proxy -port 8081 -failure-rate 0.5 -fault-type reset
./faulty-proxy -port 8082 -latency 2s -fault-type slow
./faulty-proxy -port 8083 -failure-rate 0.2 -fault-type timeout

# Advanced test script usage
./scripts/test-faultyproxy.sh unit        # Unit tests only
./scripts/test-faultyproxy.sh integration # Integration tests only
./scripts/test-faultyproxy.sh coverage    # Coverage analysis
./scripts/test-faultyproxy.sh race        # Race condition detection
```

**Faulty Proxy Package Features:**
- **Isolated testing**: Separate from main netdrift package
- **Comprehensive test types**: Unit, integration, benchmarks, examples
- **Coverage analysis**: HTML coverage reports
- **Race detection**: Concurrent safety testing
- **Load testing**: Performance under stress
- **Example-driven documentation**: Runnable code examples

Available fault types:
- `none`: No faults (normal behavior)
- `slow`: Slow responses with configurable latency
- `reset`: Random connection resets
- `timeout`: Hang connections (31s timeout)
- `bad-gateway`: Return 502 errors
- `internal-error`: Return 500 errors

## Go Module

- Module name: `netdrift`
- GitHub repository: https://github.com/restyler/netdrift/
- Go version: 1.21+
- Dependencies: Uses only Go standard library