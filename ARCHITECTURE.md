# NetDrift Architecture

## Overview

NetDrift is a high-performance HTTP forward proxy server written in Go that implements load balancing across multiple upstream proxies. It serves as a middleware layer between clients and upstream proxy servers, providing authentication, statistics tracking, and intelligent request routing.

## System Architecture

```mermaid
graph TB
    Client[Client Applications] --> Auth[Authentication Layer]
    Auth --> LB[Load Balancer]
    LB --> UP1[Upstream Proxy 1]
    LB --> UP2[Upstream Proxy 2]
    LB --> UP3[Upstream Proxy N]
    UP1 --> Target1[Target Server 1]
    UP2 --> Target2[Target Server 2]
    UP3 --> Target3[Target Server N]
    
    Stats[Statistics System] --> Auth
    Stats --> LB
    Config[Configuration Manager] --> Auth
    Config --> LB
    
    subgraph "NetDrift Core"
        Auth
        LB
        Stats
        Config
    end
```

## Request Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant N as NetDrift Proxy
    participant U as Upstream Proxy
    participant T as Target Server
    
    C->>N: CONNECT request with auth
    N->>N: Validate authentication
    N->>N: Select upstream proxy (round-robin)
    N->>U: CONNECT request to upstream
    U->>T: Establish connection
    T->>U: Connection established
    U->>N: 200 Connection Established
    N->>C: 200 Connection Established
    
    loop Data Transfer
        C->>N: Data
        N->>U: Data
        U->>T: Data
        T->>U: Response
        U->>N: Response
        N->>C: Response
    end
```

## Code Structure

### Core Components

#### 1. Main Proxy Server (`cmd/proxy/main.go`)
The central component that handles all proxy operations.

**Key Structures:**
- `Config`: Configuration structure for server settings, authentication, and upstream proxies
- `ProxyServer`: Main server structure with request handling, load balancing, and statistics
- `UpstreamStats`: Statistics tracking for individual upstream proxies
- `TimeWindowStats`: Time-based statistics aggregation

**Key Functions:**
- `NewProxyServer()`: Creates and initializes a new proxy server instance
- `handleConnect()`: Handles HTTP CONNECT requests and establishes upstream connections
- `ServeHTTP()`: Main HTTP handler that routes requests to appropriate handlers
- `getNextUpstream()`: Implements round-robin load balancing
- `authenticate()`: Validates client authentication credentials
- `parseUpstreamAuth()`: Parses upstream proxy URLs with authentication

#### 2. Test Proxy Server (`cmd/test-proxy/main.go`)
Simple proxy servers for development and testing purposes.

**Key Structures:**
- `SimpleProxy`: Basic proxy implementation for testing

**Key Functions:**
- `handleConnect()`: Handles CONNECT requests with direct target connection
- `copyData()`: Bidirectional data copying between client and target

#### 3. Faulty Proxy Package (`pkg/faultyproxy/`)
Specialized proxy for testing fault tolerance and error handling.

**Key Structures:**
- `FaultyProxy`: Proxy that can simulate various failure conditions
- `FaultType`: Enumeration of different fault types (slow, reset, timeout, etc.)

**Key Functions:**
- `NewFaultyProxy()`: Creates a faulty proxy with configurable fault injection
- `Start()`: Starts the proxy server with fault simulation
- `handleConnect()`: CONNECT handler with fault injection logic

### Package Structure

```
netdrift/
├── cmd/                    # Application entry points
│   ├── proxy/             # Main proxy server
│   ├── test-proxy/        # Test proxy servers
│   └── faulty-proxy/      # Faulty proxy for testing
├── pkg/                   # Reusable packages
│   ├── faultyproxy/       # Faulty proxy implementation
│   ├── proxy/             # Core proxy utilities (placeholder)
│   └── testproxy/         # Test proxy utilities (placeholder)
├── configs/               # Configuration templates
├── scripts/               # Build and test scripts
└── docs/                  # Documentation
```

## Key Features

### 1. Load Balancing
- **Algorithm**: Round-robin selection of upstream proxies
- **Weight Support**: Configurable weights for upstream proxies (future enhancement)
- **Health Tracking**: Per-upstream statistics and connection monitoring

### 2. Authentication
- **Basic Auth**: HTTP Basic authentication for client access
- **User Management**: Configurable user credentials in JSON configuration
- **Upstream Auth**: Support for authenticated upstream proxies (`http://user:pass@host:port`)

### 3. Statistics System
- **Real-time Metrics**: Request counts, latency, success/failure rates
- **Per-Upstream Tracking**: Individual statistics for each upstream proxy
- **Time Windows**: Both lifetime and recent (15-minute) statistics
- **Concurrency Monitoring**: Current active connections and peak concurrency

### 4. Configuration Management
- **JSON Configuration**: Human-readable configuration files
- **Live Reload**: Dynamic configuration updates without restart
- **Template System**: Configuration templates for different environments

### 5. HTTP CONNECT Tunneling
- **HTTPS Support**: Secure tunneling for HTTPS traffic
- **Connection Hijacking**: Low-level connection handling for performance
- **Bidirectional Data Transfer**: Efficient data copying between client and target

## Data Flow

### 1. Client Request Processing
1. Client sends CONNECT request with authentication
2. NetDrift validates credentials against configuration
3. Request is logged and statistics updated
4. Upstream proxy is selected using round-robin algorithm

### 2. Upstream Connection
1. NetDrift establishes connection to selected upstream proxy
2. CONNECT request is forwarded to upstream (with auth if required)
3. Upstream establishes connection to target server
4. Success/failure is tracked in statistics

### 3. Data Tunneling
1. Client connection is hijacked for direct data transfer
2. Goroutines handle bidirectional data copying
3. Connection statistics are updated in real-time
4. Cleanup occurs when either side closes connection

## Performance Characteristics

### Concurrency Model
- **Goroutines**: One goroutine per client connection
- **Non-blocking I/O**: Efficient handling of multiple concurrent connections
- **Atomic Operations**: Lock-free statistics updates where possible
- **Connection Pooling**: Reuse of upstream connections (future enhancement)

### Resource Usage
- **Memory**: Minimal per-connection overhead
- **CPU**: Efficient round-robin selection and statistics calculation
- **Network**: Direct data copying with minimal buffering

## Testing Architecture

### 1. Unit Tests
- **Core Logic**: Authentication, load balancing, configuration parsing
- **Isolated Testing**: Mock upstream proxies and controlled environments
- **Coverage**: Comprehensive test coverage for all major components

### 2. Integration Tests
- **Real Proxies**: Testing with actual proxy servers
- **End-to-End**: Complete request flow validation
- **Fault Injection**: Testing error handling and recovery

### 3. Performance Tests
- **Load Testing**: Concurrent connection handling
- **Stress Testing**: Resource limits and degradation patterns
- **Benchmark Suite**: Performance regression testing

## Security Considerations

### Authentication
- **Credential Storage**: Secure configuration file handling
- **Transport Security**: HTTPS tunneling support
- **Access Control**: User-based authentication and authorization

### Network Security
- **Connection Validation**: Proper upstream proxy validation
- **Error Handling**: Secure error responses without information leakage
- **Resource Limits**: Protection against resource exhaustion attacks

## Future Enhancements

### Load Balancing
- **Weighted Round-Robin**: Full implementation of proxy weights
- **Health Checks**: Active health monitoring of upstream proxies
- **Failover**: Automatic failover to healthy upstream proxies

### Performance
- **Connection Pooling**: Reuse of upstream connections
- **Caching**: Response caching for repeated requests
- **Compression**: Data compression for improved bandwidth usage

### Monitoring
- **Metrics Export**: Prometheus/OpenTelemetry integration
- **Alerting**: Threshold-based alerting for operational issues
- **Logging**: Structured logging with configurable levels