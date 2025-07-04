# Netdrift - Forward Proxy with Load Balancing

A high-performance HTTP CONNECT forward proxy server written in Go that implements advanced weighted load balancing across multiple upstream proxies with intelligent health monitoring and automatic failover. Features comprehensive statistics, authentication, fault tolerance, and production-ready monitoring.

## Features

- **HTTP CONNECT Support**: Full support for HTTPS tunneling
- **Advanced Load Balancing**: Weighted round-robin with health management and automatic failover
- **Upstream Health Monitoring**: Real-time health tracking with configurable failure thresholds
- **High Performance**: 4M+ operations/second with 227ns/op load balancing performance
- **Authentication**: Basic authentication with user management and upstream proxy auth support
- **Statistics & Monitoring**: Comprehensive metrics with time-window analytics and per-upstream tracking
- **Fault Tolerance**: Automatic failover, circuit breaker patterns, and graceful degradation
- **Configuration**: Flexible JSON-based configuration with live reload capability
- **Thread Safety**: Full concurrent operation support with stress-tested reliability
- **Process Management**: PID file support for production deployments
- **Testing Framework**: Comprehensive test suite with TDD-driven development
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
      "weight": 3
    },
    {
      "url": "http://user:pass@proxy.example.com:8080",
      "enabled": true,
      "weight": 2
    },
    {
      "url": "http://127.0.0.1:3026", 
      "enabled": true,
      "weight": 1
    }
  ]
}
```

## Load Balancing & Health Management

### Weight-Based Distribution

The proxy implements intelligent weighted round-robin load balancing:

- **Weight 3**: Receives 50% of traffic (3/6 ratio)
- **Weight 2**: Receives 33% of traffic (2/6 ratio)  
- **Weight 1**: Receives 17% of traffic (1/6 ratio)
- **Weight 0**: Excluded from selection (maintenance mode)

### Automatic Health Monitoring

- **Failure Tracking**: Real-time monitoring of upstream proxy health
- **Configurable Thresholds**: Default 3 failures trigger unhealthy status
- **Automatic Failover**: Traffic automatically routes to healthy upstreams
- **Instant Recovery**: First success after failure restores upstream to healthy pool
- **Graceful Degradation**: When all upstreams fail, routes to least-failed option

### Upstream Authentication Support

```bash
# Proxy with authentication in URL
"url": "http://username:password@proxy.example.com:3128"

# Proxy with special characters (URL encoded)
"url": "http://user%40domain:p%40ssw0rd@proxy.example.com:8080"
```

## Usage Examples

### Basic Usage
```bash
curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://httpbin.org/ip
```

### Testing Weight Distribution
```bash
# Send multiple requests to see weight-based distribution
for i in {1..12}; do
  echo "Request $i:"
  curl -s -x http://proxyuser:Proxy234@127.0.0.1:3130 https://httpbin.org/ip | jq -r '.origin'
done
```

### Health Status Monitoring
```bash
# Check upstream health via stats endpoint
curl -s http://127.0.0.1:3130/stats | jq '.total.upstream_metrics[] | {url, total_requests, failed_requests}'
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
┌─────────────────────────────────────────┐
│           Main Proxy Server             │
│           (127.0.0.1:3130)             │
│   ┌─────────────────────────────────┐   │
│   │      Authentication Layer       │   │
│   │   - Basic Auth Validation       │   │
│   │   - User Management            │   │
│   └─────────────────────────────────┘   │
│   ┌─────────────────────────────────┐   │
│   │    Weighted Load Balancer       │   │
│   │   - Health Status Filtering     │   │
│   │   - Weight-Based Selection      │   │
│   │   - Round-Robin Algorithm       │   │
│   └─────────────────────────────────┘   │
│   ┌─────────────────────────────────┐   │
│   │      Health Monitor             │   │
│   │   - Failure Count Tracking      │   │
│   │   - Automatic Failover          │   │
│   │   - Recovery Detection          │   │
│   └─────────────────────────────────┘   │
│   ┌─────────────────────────────────┐   │
│   │     Statistics System           │   │
│   │   - Real-time Metrics           │   │
│   │   - Per-Upstream Tracking       │   │
│   │   - Time-Window Analytics       │   │
│   └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
      ↓ Intelligent upstream selection
┌─────────────────────────────────────────┐
│           Upstream Proxies              │
│   ┌─────────┐ ┌─────────┐ ┌─────────┐   │
│   │Proxy A  │ │Proxy B  │ │Proxy C  │   │
│   │Weight:3 │ │Weight:2 │ │Weight:1 │   │
│   │Healthy ✓│ │Failed ✗ │ │Healthy ✓│   │
│   │50% load │ │0% load  │ │50% load │   │
│   └─────────┘ └─────────┘ └─────────┘   │
└─────────────────────────────────────────┘
      ↓ HTTPS tunnel establishment
Target Server (End-to-end encryption)
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

The project includes a comprehensive test-driven development (TDD) suite:

#### Integration Tests
```bash
# Full integration test with real proxies
make test-integration

# Manual test runner with PID management
./scripts/test-runner.sh

# Show service status  
./scripts/test-runner.sh status

# Clean up processes
./scripts/test-runner.sh cleanup
```

#### Unit & Load Balancing Tests
```bash
# Core load balancing functionality
go test -v ./cmd/proxy -run="TestWeightedRoundRobin"

# Health management system
go test -v ./cmd/proxy -run="TestUpstreamHealthTracking"

# Failover scenarios
go test -v ./cmd/proxy -run="TestUpstreamFailoverScenarios"

# High-concurrency stress testing
go test -v ./cmd/proxy -run="TestHighConcurrencyLoadBalancing"
```

#### Performance Benchmarks
```bash
# Load balancing performance
go test -bench=BenchmarkLoadBalancing ./cmd/proxy

# Health tracking performance  
go test -bench=BenchmarkHealthTracking ./cmd/proxy

# Memory usage under load
go test -run=TestMemoryUsageUnderLoad ./cmd/proxy
```

#### Fault Injection Testing
```bash
# Faulty proxy for testing resilience
make test-faultyproxy-full

# Race condition detection
go test -race ./cmd/proxy

# Long-running stability tests
go test -run=TestLongRunningStressTest ./cmd/proxy
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

### Benchmark Results

- **Load Balancing Performance**: **227.9 ns/op** (4.4M operations/second)
- **Stress Test**: 100,000 concurrent requests with perfect weight distribution
- **Throughput**: 3.1M+ requests/second in high-concurrency scenarios
- **Memory Efficiency**: <10MB memory increase under 1M operations load
- **Thread Safety**: Race-condition free with comprehensive concurrent testing

### Production Metrics

- **Concurrent Connections**: Handles 10,000+ simultaneous connections
- **Load Balancing**: Sub-microsecond upstream selection with health filtering
- **Memory Usage**: Minimal memory footprint with efficient health tracking
- **Latency**: Ultra-low overhead proxy with detailed per-upstream latency tracking
- **Failover Time**: Instant failover on upstream health state changes
- **Recovery**: Immediate upstream recovery on first successful request

### Real-World Performance

```bash
# Benchmark load balancing performance
go test -bench=BenchmarkLoadBalancing ./cmd/proxy
# Result: BenchmarkLoadBalancing-10   4855418   227.9 ns/op

# Stress test with 100k requests across 100 goroutines
go test -run=TestHighConcurrencyLoadBalancing ./cmd/proxy
# Result: Perfect weight distribution (10%/20%/30%/40%) at 3.1M req/s
```

## License

MIT License

## Contributing

1. Fork the repository
2. Create a feature branch
3. Run the test suite: `make test-integration`
4. Commit your changes
5. Push and create a Pull Request

For detailed development guidance, see [CLAUDE.md](./CLAUDE.md).