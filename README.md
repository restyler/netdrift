# Netdrift - Forward Proxy with Load Balancing

A high-performance HTTP CONNECT forward proxy server written in Go that implements load balancing across multiple upstream proxies using a round-robin algorithm. Features comprehensive monitoring, authentication, and Docker support.

## Features

- **HTTP CONNECT Support**: Full support for HTTPS tunneling
- **Load Balancing**: Round-robin distribution across upstream proxies
- **Authentication**: Basic authentication with user management
- **Statistics & Monitoring**: Detailed metrics with time-window analytics
- **Configuration**: Flexible JSON-based configuration with multiple input methods
- **Process Management**: PID file support for production deployments
- **Testing Framework**: Comprehensive integration test suite
- **Docker Ready**: Full Docker and Docker Compose support
- **Production Ready**: Built-in logging, error handling, and graceful shutdown

## Quick Start

### Using Make Commands (Recommended)

```bash
# Build and run integration tests
make test-integration

# Run services manually
make run-test-proxies    # Start test proxies on ports 3025, 3026
make run-proxy           # Start main proxy on port 3130

# Test the setup
make test               # Basic connectivity test
```

### Using Docker Compose

#### Production (Proxy Only)
```bash
# Start production proxy
docker compose -f docker-compose.prod.yml up -d

# View logs
docker compose -f docker-compose.prod.yml logs -f

# Stop services
docker compose -f docker-compose.prod.yml down
```

#### Testing (Full Stack with Mock Proxies)
```bash
# Start test environment with mock proxies
docker compose -f docker-compose.test.yml up -d

# View logs
docker compose -f docker-compose.test.yml logs -f

# Stop services
docker compose -f docker-compose.test.yml down
```

## Configuration

### Command Line Options

```bash
# Using flags (recommended)
./bin/proxy -config configs/us.json
./bin/proxy -help

# Using environment variables (container-friendly)
PROXY_CONFIG=configs/us.json ./bin/proxy

# Test proxies
./bin/test-proxy 3025 3026
./bin/test-proxy -help
```

### Configuration Priority

1. **PROXY_CONFIG environment variable** (highest priority)
2. **-config command line flag** (middle priority)
3. **Default value** `configs/us.json` (lowest priority)

### Sample Configuration

The proxy reads configuration from `configs/us.json`:

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
      "url": "http://127.0.0.1:3025",
      "enabled": true,
      "weight": 1
    },
    {
      "url": "http://127.0.0.1:3026",
      "enabled": true,
      "weight": 1
    }
  ]
}
```

## Usage Examples

### Basic Usage
```bash
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://httpbin.org/ip
```

### Testing Load Balancing
```bash
for i in {1..4}; do
  echo "Request $i:"
  curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://httpbin.org/ip
  echo ""
done
```

### With Custom Headers
```bash
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 \
     -H "User-Agent: MyApp/1.0" \
     https://httpbin.org/headers
```

## Monitoring & Statistics

### Stats Endpoint
```bash
curl http://127.0.0.1:3130/stats
```

### Example Response
```json
{
  "start_time": "2025-07-04T00:00:00Z",
  "uptime": "1h30m45s",
  "current_requests": 2,
  "total": {
    "window": "total",
    "total_requests": 150,
    "success_requests": 147,
    "failed_requests": 3,
    "avg_latency_ms": 245.6,
    "max_concurrency": 8,
    "upstream_metrics": [...]
  },
  "recent_15m": {
    "window": "15m0s",
    "total_requests": 45,
    "success_requests": 44,
    "failed_requests": 1,
    "avg_latency_ms": 189.2,
    "max_concurrency": 5,
    "upstream_metrics": [...]
  }
}
```

## Available Make Commands

### Build Commands
```bash
make build              # Build main proxy server
make build-test         # Build test proxy servers
make build-faulty       # Build faulty proxy server (for testing)
make clean              # Clean build artifacts
```

### Running Services
```bash
make run-proxy          # Run main proxy server
make run-test-proxies   # Run test proxy servers
make test               # Basic connectivity test
make test-unit          # Run unit tests
make test-integration   # Full integration test suite
```

### Docker Commands

#### Production Docker
```bash
make docker-prod                                      # Build production image (proxy only)
docker compose -f docker-compose.prod.yml up -d      # Start production stack
docker compose -f docker-compose.prod.yml down       # Stop production stack
docker compose -f docker-compose.prod.yml logs -f    # View production logs
```

#### Test Docker  
```bash
make docker-test                                      # Build test image (with mock proxies)
docker compose -f docker-compose.test.yml up -d      # Start test stack with mock proxies
docker compose -f docker-compose.test.yml down       # Stop test stack
docker compose -f docker-compose.test.yml logs -f    # View test logs
```

#### Single Container
```bash
make docker-build       # Build production image
make docker-run         # Run single container
make docker-clean       # Clean up all Docker resources
```

### Testing Commands
```bash
make test-faultyproxy         # Unit tests for faulty proxy
make test-faultyproxy-full    # Comprehensive faulty proxy test suite
make test-faultyproxy-bench   # Performance benchmarks
```

## Architecture

```
Client Request
      ↓ CONNECT with Basic Auth
┌─────────────────────────┐
│   Main Proxy Server     │
│   (127.0.0.1:3130)     │
│   - Authentication     │
│   - Load Balancing     │
│   - Statistics        │
└─────────────────────────┘
      ↓ Round-robin selection
┌─────────────────────────┐
│   Upstream Proxies      │
│   - Test Proxy 3025     │
│   - Test Proxy 3026     │
│   - Or External Proxies │
└─────────────────────────┘
      ↓ Direct connection
Target Server (HTTPS tunnel)
```

## Development

### Project Structure
```
netdrift/
├── bin/                        # Compiled binaries (gitignored)
├── cmd/                        # Main applications
│   ├── proxy/                 # Main proxy server
│   ├── test-proxy/            # Test proxy servers
│   └── faulty-proxy/          # Faulty proxy for testing
├── configs/                    # Configuration files
│   ├── us.json               # Development config
│   └── docker.json           # Docker config
├── pkg/                        # Library packages
├── scripts/                    # Build and test scripts
├── Dockerfile                 # Production Docker image
├── Dockerfile.test            # Test Docker image (with test proxies)
├── docker-compose.prod.yml    # Production Docker Compose
├── docker-compose.test.yml    # Test Docker Compose (full stack)
└── Makefile                   # Build automation
```

### Testing Framework

The project includes a comprehensive test runner with PID management:

```bash
# Full integration test
./scripts/test-runner.sh

# Show service status
./scripts/test-runner.sh status

# Clean up processes
./scripts/test-runner.sh cleanup

# Show help
./scripts/test-runner.sh help
```

### Process Management

Both proxy applications create PID files for production deployment:

- `proxy.pid` - Main proxy server
- `test-proxy-3025.pid`, `test-proxy-3026.pid` - Test proxies

## Docker Support

### Building Docker Image
```bash
make docker-build
```

### Running with Docker

#### Production Deployment
```bash
# Single container
make docker-run

# Full production stack
docker compose -f docker-compose.prod.yml up -d
```

#### Development/Testing
```bash
# Test environment with mock proxies
docker compose -f docker-compose.test.yml up -d
```

### Environment Variables for Docker
```bash
PROXY_CONFIG=/app/configs/us.json
```

## Requirements

- **Go**: 1.21+ (for building from source)
- **Docker**: For containerized deployment
- **Docker Compose**: For multi-service orchestration
- **Make**: For build automation

## Dependencies

- Uses only Go standard library
- No external runtime dependencies
- Self-contained binaries

## Performance

- **Concurrent Connections**: Handles thousands of simultaneous connections
- **Load Balancing**: Efficient round-robin with connection tracking
- **Memory Usage**: Minimal memory footprint
- **Latency**: Low overhead proxy with detailed latency tracking

## License

MIT License

## Contributing

1. Fork the repository
2. Create a feature branch
3. Run the test suite: `make test-integration`
4. Commit your changes
5. Push and create a Pull Request

For detailed development guidance, see [CLAUDE.md](./CLAUDE.md).