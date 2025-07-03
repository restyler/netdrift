# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based HTTP forward proxy server called "netdrift" that implements load balancing across multiple upstream proxies using a round-robin algorithm. It supports HTTP CONNECT tunneling for HTTPS traffic, basic authentication, and provides comprehensive statistics tracking.

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
make clean          # Clean build artifacts
```

### Running Services
```bash
make run-proxy           # Run main proxy server
make run-test-proxies    # Run test proxy servers on ports 3025 and 3026
make test               # Test proxy with curl
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
go run ./pkg/proxy/test_proxy.go 3025 3026

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

Use the built-in test proxies for development:
1. Start test proxies: `make run-test-proxies`
2. Start main proxy: `make run-proxy`
3. Test functionality: `make test`

## Go Module

- Module name: `netdrift`
- GitHub repository: https://github.com/restyler/netdrift/
- Go version: 1.21+
- Dependencies: Uses only Go standard library