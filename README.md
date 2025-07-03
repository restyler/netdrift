# 4proxy - Go Forward Proxy with Load Balancing

A simple HTTP CONNECT forward proxy server written in Go that load balances requests across multiple upstream proxies using round-robin algorithm.

## Features

- **HTTP CONNECT Support**: Full support for HTTPS tunneling
- **Load Balancing**: Round-robin distribution across upstream proxies
- **Authentication**: Basic authentication support
- **Statistics**: Built-in stats endpoint for monitoring
- **Configuration**: JSON-based configuration
- **Docker Ready**: Outputs logs to stdout/stderr for container deployment

## Configuration

The proxy reads configuration from `us.json`:

```json
{
  "server": {
    "name": "US Proxy Pool",
    "listen_address": "127.0.0.1:3130",
    "stats_endpoint": "/stats"
  },
  "authentication": {
    "enabled": true,
    "users": [
      {
        "username": "proxyuser",
        "password": "Proxy234"
      }
    ]
  },
  "upstream_proxies": [
    {
      "url": "http://127.0.0.1:1025",
      "enabled": true,
      "weight": 1
    },
    {
      "url": "http://127.0.0.1:1026",
      "enabled": true,
      "weight": 1
    }
  ]
}
```

## Quick Start

### 1. Start Test Upstream Proxies

In one terminal, start the test proxy servers:

```bash
make run-test-proxies
```

Or manually:
```bash
go run test_proxy.go 1025 1026
```

### 2. Start Main Proxy Server

In another terminal, start the main proxy:

```bash
make run-proxy
```

Or manually:
```bash
go run main.go
```

### 3. Test the Proxy

```bash
# Test with authentication
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net

# Check statistics
curl http://127.0.0.1:3130/stats
```

## Usage Examples

### Basic Usage
```bash
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://httpbin.org/ip
```

### With Custom User-Agent
```bash
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 \
     -H "User-Agent: MyApp/1.0" \
     https://httpbin.org/headers
```

### Testing Load Balancing
Run multiple requests to see round-robin in action:
```bash
for i in {1..4}; do
  echo "Request $i:"
  curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net
  echo ""
done
```

## Monitoring

Check proxy statistics:
```bash
curl http://127.0.0.1:3130/stats
```

Example response:
```json
{
  "current_upstream": 1,
  "failed_requests": 0,
  "success_requests": 5,
  "total_requests": 5,
  "upstream_proxies": [
    "http://127.0.0.1:1025",
    "http://127.0.0.1:1026"
  ]
}
```

## Building

### Binary
```bash
make build
```

### Docker
```bash
make docker-build
make docker-run
```

## Architecture

```
Client
  ↓ CONNECT request (with auth)
Main Proxy (127.0.0.1:3130)
  ↓ Round-robin selection
Upstream Proxy 1 (127.0.0.1:1025) or Upstream Proxy 2 (127.0.0.1:1026)
  ↓ Direct connection
Target Server (e.g., myip.scrapeninja.net)
```

## Log Output

All logs are written to stdout/stderr for Docker compatibility:

```
2024/01/15 10:30:15 Loading configuration from us.json
2024/01/15 10:30:15 Loaded 2 upstream proxies
2024/01/15 10:30:15 Starting US Proxy Pool on 127.0.0.1:3130
2024/01/15 10:30:15 Proxy server listening on 127.0.0.1:3130
2024/01/15 10:30:15 Authentication is enabled
2024/01/15 10:30:15 Stats endpoint available at /stats
2024/01/15 10:30:20 CONNECT request to myip.scrapeninja.net:443 from 127.0.0.1:54321
2024/01/15 10:30:20 Using upstream proxy: http://127.0.0.1:1025
2024/01/15 10:30:20 Established tunnel between client and myip.scrapeninja.net:443 via http://127.0.0.1:1025
```

## Dependencies

- Go 1.21+
- No external dependencies (uses only Go standard library)

## License

MIT License 