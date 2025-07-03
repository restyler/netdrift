package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Server struct {
		Name          string `json:"name"`
		ListenAddress string `json:"listen_address"`
		StatsEndpoint string `json:"stats_endpoint"`
	} `json:"server"`
	Authentication struct {
		Enabled bool `json:"enabled"`
		Users   []struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"users"`
	} `json:"authentication"`
	UpstreamProxies []struct {
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
		Weight  int    `json:"weight"`
	} `json:"upstream_proxies"`
}

type UpstreamStats struct {
	URL                string    `json:"url"`
	TotalRequests      int64     `json:"total_requests"`
	SuccessRequests    int64     `json:"success_requests"`
	FailedRequests     int64     `json:"failed_requests"`
	TotalLatency       int64     `json:"total_latency_ms"`
	AvgLatency         float64   `json:"avg_latency_ms"`
	CurrentConnections int64     `json:"current_connections"`
	LastRequest        time.Time `json:"last_request"`
}

type TimeWindowStats struct {
	Window          string          `json:"window"`
	TotalRequests   int64           `json:"total_requests"`
	SuccessRequests int64           `json:"success_requests"`
	FailedRequests  int64           `json:"failed_requests"`
	AvgLatency      float64         `json:"avg_latency_ms"`
	MaxConcurrency  int64           `json:"max_concurrency"`
	UpstreamMetrics []UpstreamStats `json:"upstream_metrics"`
}

type ProxyServer struct {
	config     *Config
	upstreams  []string
	currentIdx int
	mutex      sync.Mutex
	stats      struct {
		StartTime       time.Time
		TotalRequests   int64
		SuccessRequests int64
		FailedRequests  int64
		CurrentRequests int64
		MaxConcurrency  int64
		UpstreamMetrics map[string]*UpstreamStats
		RecentRequests  []struct {
			Timestamp time.Time
			Upstream  string
			Latency   int64
			Success   bool
		}
	}
}

func NewProxyServer(config *Config) *ProxyServer {
	ps := &ProxyServer{
		config: config,
	}

	// Initialize stats
	ps.stats.StartTime = time.Now()
	ps.stats.UpstreamMetrics = make(map[string]*UpstreamStats)
	ps.stats.RecentRequests = make([]struct {
		Timestamp time.Time
		Upstream  string
		Latency   int64
		Success   bool
	}, 0)

	// Build list of enabled upstream proxies
	for _, upstream := range config.UpstreamProxies {
		if upstream.Enabled {
			ps.upstreams = append(ps.upstreams, upstream.URL)
			ps.stats.UpstreamMetrics[upstream.URL] = &UpstreamStats{
				URL:         upstream.URL,
				LastRequest: time.Now(),
			}
		}
	}

	log.Printf("Loaded %d upstream proxies", len(ps.upstreams))
	return ps
}

func (ps *ProxyServer) getNextUpstream() string {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if len(ps.upstreams) == 0 {
		return ""
	}

	upstream := ps.upstreams[ps.currentIdx]
	ps.currentIdx = (ps.currentIdx + 1) % len(ps.upstreams)
	return upstream
}

func (ps *ProxyServer) authenticate(r *http.Request) bool {
	if !ps.config.Authentication.Enabled {
		log.Printf("Authentication disabled, allowing request")
		return true
	}

	// For CONNECT requests, we need to check Proxy-Authorization header
	proxyAuth := r.Header.Get("Proxy-Authorization")
	if proxyAuth == "" {
		log.Printf("No proxy auth credentials provided")
		return false
	}

	// Parse Basic authentication
	if !strings.HasPrefix(proxyAuth, "Basic ") {
		log.Printf("Proxy auth is not Basic authentication")
		return false
	}

	// Decode base64 credentials
	encoded := proxyAuth[6:] // Remove "Basic " prefix
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Printf("Failed to decode proxy auth: %v", err)
		return false
	}

	// Split username:password
	credentials := string(decoded)
	parts := strings.SplitN(credentials, ":", 2)
	if len(parts) != 2 {
		log.Printf("Invalid credential format")
		return false
	}

	username, password := parts[0], parts[1]
	log.Printf("Authentication attempt for user: %s", username)

	for _, user := range ps.config.Authentication.Users {
		if user.Username == username && user.Password == password {
			log.Printf("Authentication successful for user: %s", username)
			return true
		}
	}

	log.Printf("Authentication failed for user: %s", username)
	return false
}

func (ps *ProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Increment current requests and update max concurrency
	currentReqs := atomic.AddInt64(&ps.stats.CurrentRequests, 1)
	for {
		maxConcurrency := atomic.LoadInt64(&ps.stats.MaxConcurrency)
		if currentReqs <= maxConcurrency || atomic.CompareAndSwapInt64(&ps.stats.MaxConcurrency, maxConcurrency, currentReqs) {
			break
		}
	}
	defer atomic.AddInt64(&ps.stats.CurrentRequests, -1)

	atomic.AddInt64(&ps.stats.TotalRequests, 1)

	if !ps.authenticate(r) {
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"Proxy\"")
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	upstream := ps.getNextUpstream()
	if upstream == "" {
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		http.Error(w, "No upstream proxies available", http.StatusBadGateway)
		return
	}

	// Update upstream stats
	upstreamStats := ps.stats.UpstreamMetrics[upstream]
	atomic.AddInt64(&upstreamStats.TotalRequests, 1)
	atomic.AddInt64(&upstreamStats.CurrentConnections, 1)
	defer atomic.AddInt64(&upstreamStats.CurrentConnections, -1)

	// Connect to upstream proxy
	upstreamConn, err := net.DialTimeout("tcp", strings.TrimPrefix(upstream, "http://"), 30*time.Second)
	if err != nil {
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		http.Error(w, "Failed to connect to upstream proxy", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	// Send CONNECT request to upstream
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", r.Host, r.Host)
	if _, err := upstreamConn.Write([]byte(connectReq)); err != nil {
		log.Printf("Failed to send CONNECT to upstream: %v", err)
		http.Error(w, "Failed to connect", http.StatusBadGateway)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}

	// Read response from upstream
	response := make([]byte, 1024)
	n, err := upstreamConn.Read(response)
	if err != nil {
		log.Printf("Failed to read response from upstream: %v", err)
		http.Error(w, "Failed to connect", http.StatusBadGateway)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}

	responseStr := string(response[:n])
	if !strings.Contains(responseStr, "200") {
		log.Printf("Upstream proxy rejected connection: %s", strings.TrimSpace(responseStr))
		http.Error(w, "Upstream proxy rejected connection", http.StatusBadGateway)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("ResponseWriter doesn't support hijacking")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("Failed to hijack connection: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established to client
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("Failed to send 200 to client: %v", err)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		return
	}

	log.Printf("Established tunnel between client and %s via %s", r.Host, upstream)
	atomic.AddInt64(&ps.stats.SuccessRequests, 1)
	atomic.AddInt64(&upstreamStats.SuccessRequests, 1)

	// Update stats after successful connection
	elapsed := time.Since(startTime).Milliseconds()
	atomic.AddInt64(&upstreamStats.TotalLatency, elapsed)
	atomic.AddInt64(&upstreamStats.TotalLatency, elapsed)

	ps.mutex.Lock()
	upstreamStats.LastRequest = time.Now()
	upstreamStats.AvgLatency = float64(upstreamStats.TotalLatency) / float64(upstreamStats.SuccessRequests)

	// Add to recent requests
	ps.stats.RecentRequests = append(ps.stats.RecentRequests, struct {
		Timestamp time.Time
		Upstream  string
		Latency   int64
		Success   bool
	}{
		Timestamp: time.Now(),
		Upstream:  upstream,
		Latency:   elapsed,
		Success:   true,
	})

	// Trim old requests (keep last 15 minutes)
	cutoff := time.Now().Add(-15 * time.Minute)
	for i, req := range ps.stats.RecentRequests {
		if req.Timestamp.After(cutoff) {
			ps.stats.RecentRequests = ps.stats.RecentRequests[i:]
			break
		}
	}
	ps.mutex.Unlock()

	// Start bidirectional copying
	go func() {
		defer upstreamConn.Close()
		defer clientConn.Close()
		io.Copy(upstreamConn, clientConn)
	}()

	io.Copy(clientConn, upstreamConn)
}

func (ps *ProxyServer) getTimeWindowStats(window time.Duration) TimeWindowStats {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	cutoff := time.Now().Add(-window)
	stats := TimeWindowStats{
		Window:          window.String(),
		UpstreamMetrics: make([]UpstreamStats, 0),
	}

	// Initialize per-upstream stats
	upstreamStats := make(map[string]*UpstreamStats)
	for _, upstream := range ps.upstreams {
		upstreamStats[upstream] = &UpstreamStats{
			URL: upstream,
		}
	}

	var totalLatency int64
	maxConcurrent := int64(0)

	// Process recent requests
	for _, req := range ps.stats.RecentRequests {
		if req.Timestamp.Before(cutoff) {
			continue
		}

		stats.TotalRequests++
		if req.Success {
			stats.SuccessRequests++
			totalLatency += req.Latency
		} else {
			stats.FailedRequests++
		}

		// Update upstream-specific stats
		upstream := upstreamStats[req.Upstream]
		upstream.TotalRequests++
		if req.Success {
			upstream.SuccessRequests++
			upstream.TotalLatency += req.Latency
		} else {
			upstream.FailedRequests++
		}
	}

	// Calculate averages and add upstream stats
	if stats.SuccessRequests > 0 {
		stats.AvgLatency = float64(totalLatency) / float64(stats.SuccessRequests)
	}
	stats.MaxConcurrency = maxConcurrent

	for _, upstream := range ps.upstreams {
		us := upstreamStats[upstream]
		if us.SuccessRequests > 0 {
			us.AvgLatency = float64(us.TotalLatency) / float64(us.SuccessRequests)
		}
		us.CurrentConnections = ps.stats.UpstreamMetrics[upstream].CurrentConnections
		stats.UpstreamMetrics = append(stats.UpstreamMetrics, *us)
	}

	return stats
}

func (ps *ProxyServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats := struct {
		StartTime       time.Time       `json:"start_time"`
		Uptime          string          `json:"uptime"`
		TotalStats      TimeWindowStats `json:"total"`
		RecentStats     TimeWindowStats `json:"recent_15m"`
		CurrentRequests int64           `json:"current_requests"`
	}{
		StartTime:       ps.stats.StartTime,
		Uptime:          time.Since(ps.stats.StartTime).String(),
		TotalStats:      ps.getTimeWindowStats(time.Since(ps.stats.StartTime)),
		RecentStats:     ps.getTimeWindowStats(15 * time.Minute),
		CurrentRequests: atomic.LoadInt64(&ps.stats.CurrentRequests),
	}

	json.NewEncoder(w).Encode(stats)
}

func (ps *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == ps.config.Server.StatsEndpoint {
		ps.handleStats(w, r)
		return
	}

	if r.Method == "CONNECT" {
		ps.handleConnect(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func loadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}

	return &config, nil
}

func writePidFile() {
	pidFile := "proxy.pid"
	file, err := os.Create(pidFile)
	if err != nil {
		log.Printf("Failed to create PID file %s: %v", pidFile, err)
		return
	}
	defer file.Close()
	
	fmt.Fprintf(file, "%d\n", os.Getpid())
	log.Printf("PID file created: %s", pidFile)
}

var (
	configFile = flag.String("config", "configs/us.json", "Path to configuration file")
	showHelp   = flag.Bool("help", false, "Show help message")
)

func main() {
	flag.Parse()
	
	if *showHelp {
		fmt.Printf("Usage: %s [options]\n", os.Args[0])
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		fmt.Println("\nEnvironment variables:")
		fmt.Println("  PROXY_CONFIG - Path to configuration file (overrides -config)")
		os.Exit(0)
	}
	
	writePidFile()
	
	// Priority: Environment variable > Command line flag > Default
	configPath := *configFile
	if envConfig := os.Getenv("PROXY_CONFIG"); envConfig != "" {
		configPath = envConfig
	}

	log.Printf("Loading configuration from %s", configPath)
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting %s on %s", config.Server.Name, config.Server.ListenAddress)

	proxyServer := NewProxyServer(config)

	server := &http.Server{
		Addr:    config.Server.ListenAddress,
		Handler: proxyServer,
	}

	log.Printf("Proxy server listening on %s", config.Server.ListenAddress)
	if config.Authentication.Enabled {
		log.Printf("Authentication is enabled")
	}
	log.Printf("Stats endpoint available at %s", config.Server.StatsEndpoint)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
