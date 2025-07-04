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

type UpstreamHealth struct {
	FailureCount      int64     `json:"failure_count"`
	SuccessCount      int64     `json:"success_count"`
	LastFailure       time.Time `json:"last_failure"`
	LastSuccess       time.Time `json:"last_success"`
	IsHealthy         bool      `json:"is_healthy"`
	FailureThreshold  int       `json:"failure_threshold"`
	RecoveryThreshold int       `json:"recovery_threshold"`
}

type WeightedUpstream struct {
	URL    string
	Weight int
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
	config           *Config
	configPath       string
	configModTime    time.Time
	upstreams        []string
	weightedUpstreams []WeightedUpstream
	totalWeight      int
	currentIdx       int
	mutex            sync.RWMutex
	reloadMutex      sync.Mutex
	healthMutex      sync.RWMutex
	upstreamHealth   map[string]*UpstreamHealth
	stats            struct {
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

func NewProxyServer(config *Config, configPath string) *ProxyServer {
	ps := &ProxyServer{
		config:         config,
		configPath:     configPath,
		upstreamHealth: make(map[string]*UpstreamHealth),
	}
	
	// Get initial config file modification time
	if stat, err := os.Stat(configPath); err == nil {
		ps.configModTime = stat.ModTime()
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

	// Build list of enabled upstream proxies with weights
	ps.buildUpstreamLists()

	log.Printf("Loaded %d upstream proxies (total weight: %d)", len(ps.upstreams), ps.totalWeight)
	return ps
}

func (ps *ProxyServer) reloadConfig() error {
	ps.reloadMutex.Lock()
	defer ps.reloadMutex.Unlock()
	
	// Check if config file has been modified
	stat, err := os.Stat(ps.configPath)
	if err != nil {
		return fmt.Errorf("failed to stat config file: %v", err)
	}
	
	if !stat.ModTime().After(ps.configModTime) {
		// File hasn't been modified
		return nil
	}
	
	log.Printf("Config file modified, reloading configuration from %s", ps.configPath)
	
	// Load new configuration
	newConfig, err := loadConfig(ps.configPath)
	if err != nil {
		log.Printf("Failed to reload config: %v", err)
		return fmt.Errorf("failed to reload config: %v", err)
	}
	
	// Update configuration with write lock
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	
	ps.config = newConfig
	ps.configModTime = stat.ModTime()
	
	// Rebuild upstream list
	oldUpstreams := ps.upstreams
	ps.currentIdx = 0
	
	// Use the new build method
	ps.buildUpstreamLists()
	
	log.Printf("Configuration reloaded successfully:")
	log.Printf("  - Server: %s", newConfig.Server.Name)
	log.Printf("  - Authentication: %t", newConfig.Authentication.Enabled)
	log.Printf("  - Upstream proxies: %d enabled (was %d)", len(ps.upstreams), len(oldUpstreams))
	
	// Log upstream changes
	for _, upstream := range ps.upstreams {
		found := false
		for _, oldUpstream := range oldUpstreams {
			if oldUpstream == upstream {
				found = true
				break
			}
		}
		if !found {
			log.Printf("  + Added upstream: %s", upstream)
		}
	}
	
	for _, oldUpstream := range oldUpstreams {
		found := false
		for _, upstream := range ps.upstreams {
			if upstream == oldUpstream {
				found = true
				break
			}
		}
		if !found {
			log.Printf("  - Removed upstream: %s", oldUpstream)
		}
	}
	
	return nil
}

func (ps *ProxyServer) startConfigWatcher() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if err := ps.reloadConfig(); err != nil {
				log.Printf("Config reload error: %v", err)
			}
		}
	}()
	log.Printf("Config file watcher started (checking every 1 minute)")
}

// buildUpstreamLists builds the upstream lists with weights and health tracking
func (ps *ProxyServer) buildUpstreamLists() {
	ps.upstreams = nil
	ps.weightedUpstreams = nil
	ps.totalWeight = 0

	for _, upstream := range ps.config.UpstreamProxies {
		if upstream.Enabled {
			weight := upstream.Weight
			if weight < 0 {
				weight = 1 // Default weight for negative weights
			}
			// Allow zero weights (they should be excluded from selection)

			ps.upstreams = append(ps.upstreams, upstream.URL)
			ps.weightedUpstreams = append(ps.weightedUpstreams, WeightedUpstream{
				URL:    upstream.URL,
				Weight: weight,
			})
			ps.totalWeight += weight

			// Initialize upstream health if not exists
			if _, exists := ps.upstreamHealth[upstream.URL]; !exists {
				ps.upstreamHealth[upstream.URL] = &UpstreamHealth{
					IsHealthy:         true,
					FailureThreshold:  3, // Default failure threshold
					RecoveryThreshold: 1, // Default recovery threshold
				}
			}

			// Initialize stats if not exists
			if _, exists := ps.stats.UpstreamMetrics[upstream.URL]; !exists {
				ps.stats.UpstreamMetrics[upstream.URL] = &UpstreamStats{
					URL:         upstream.URL,
					LastRequest: time.Now(),
				}
			}
		}
	}
}

func (ps *ProxyServer) getNextUpstream() string {
	ps.mutex.RLock()
	defer ps.mutex.RUnlock()

	if len(ps.weightedUpstreams) == 0 {
		return ""
	}

	// Get healthy upstreams only
	healthyUpstreams := ps.getHealthyUpstreams()
	if len(healthyUpstreams) == 0 {
		// Fallback: return least failed upstream if all are unhealthy
		return ps.getLeastFailedUpstream()
	}

	// Use weighted round-robin selection
	return ps.selectWeightedUpstream(healthyUpstreams)
}

func (ps *ProxyServer) getHealthyUpstreams() []WeightedUpstream {
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()

	var healthy []WeightedUpstream
	for _, weighted := range ps.weightedUpstreams {
		// Skip zero-weight upstreams
		if weighted.Weight == 0 {
			continue
		}
		if health, exists := ps.upstreamHealth[weighted.URL]; exists && health.IsHealthy {
			healthy = append(healthy, weighted)
		}
	}
	return healthy
}

func (ps *ProxyServer) selectWeightedUpstream(upstreams []WeightedUpstream) string {
	if len(upstreams) == 0 {
		return ""
	}

	if len(upstreams) == 1 {
		return upstreams[0].URL
	}

	// Calculate total weight for healthy upstreams
	totalWeight := 0
	for _, upstream := range upstreams {
		totalWeight += upstream.Weight
	}

	if totalWeight == 0 {
		// All weights are zero, use simple round-robin
		// This should not happen since we filter zero weights in getHealthyUpstreams
		return upstreams[0].URL
	}

	// Get current index for weighted selection (thread-safe)
	ps.mutex.RUnlock()
	ps.mutex.Lock()
	ps.currentIdx = (ps.currentIdx + 1) % totalWeight
	targetWeight := ps.currentIdx
	ps.mutex.Unlock()
	ps.mutex.RLock()

	// Find upstream based on weight distribution
	currentWeight := 0
	for _, upstream := range upstreams {
		currentWeight += upstream.Weight
		if targetWeight < currentWeight {
			return upstream.URL
		}
	}

	// Fallback to first upstream
	return upstreams[0].URL
}

func (ps *ProxyServer) getLeastFailedUpstream() string {
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()

	if len(ps.upstreams) == 0 {
		return ""
	}

	leastFailed := ps.upstreams[0]
	minFailures := int64(999999)

	for _, upstream := range ps.upstreams {
		if health, exists := ps.upstreamHealth[upstream]; exists {
			if health.FailureCount < minFailures {
				minFailures = health.FailureCount
				leastFailed = upstream
			}
		}
	}

	return leastFailed
}

// Health management methods
func (ps *ProxyServer) recordUpstreamFailure(upstream string) {
	ps.healthMutex.Lock()
	defer ps.healthMutex.Unlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		health = &UpstreamHealth{
			IsHealthy:         true,
			FailureThreshold:  3,
			RecoveryThreshold: 1,
		}
		ps.upstreamHealth[upstream] = health
	}

	health.FailureCount++
	health.LastFailure = time.Now()

	// Check if upstream should be marked unhealthy
	if health.FailureCount >= int64(health.FailureThreshold) {
		health.IsHealthy = false
	}
}

func (ps *ProxyServer) recordUpstreamSuccess(upstream string) {
	ps.healthMutex.Lock()
	defer ps.healthMutex.Unlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		health = &UpstreamHealth{
			IsHealthy:         true,
			FailureThreshold:  3,
			RecoveryThreshold: 1,
		}
		ps.upstreamHealth[upstream] = health
	}

	health.SuccessCount++
	health.LastSuccess = time.Now()

	// Check if upstream should recover
	if !health.IsHealthy {
		// Reset failure count on success to allow recovery
		health.FailureCount = 0
		health.IsHealthy = true
	}
}

func (ps *ProxyServer) isUpstreamHealthy(upstream string) bool {
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		return true // Assume healthy if no health record
	}

	return health.IsHealthy
}

func (ps *ProxyServer) getUpstreamFailureCount(upstream string) int {
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		return 0
	}

	return int(health.FailureCount)
}

// Configuration methods for testing
func (ps *ProxyServer) setFailureThreshold(upstream string, threshold int) {
	ps.healthMutex.Lock()
	defer ps.healthMutex.Unlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		health = &UpstreamHealth{
			IsHealthy:         true,
			FailureThreshold:  threshold,
			RecoveryThreshold: 1,
		}
		ps.upstreamHealth[upstream] = health
	} else {
		health.FailureThreshold = threshold
	}
}

func (ps *ProxyServer) setRecoveryThreshold(upstream string, threshold int) {
	ps.healthMutex.Lock()
	defer ps.healthMutex.Unlock()

	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		health = &UpstreamHealth{
			IsHealthy:         true,
			FailureThreshold:  3,
			RecoveryThreshold: threshold,
		}
		ps.upstreamHealth[upstream] = health
	} else {
		health.RecoveryThreshold = threshold
	}
}

// Stub methods for advanced features (to be implemented later)
func (ps *ProxyServer) startHealthChecker(interval time.Duration) {
	// TODO: Implement periodic health checks
}

func (ps *ProxyServer) stopHealthChecker() {
	// TODO: Implement health checker stopping
}

func (ps *ProxyServer) getCircuitBreakerState(upstream string) string {
	// TODO: Implement circuit breaker states
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()
	
	health, exists := ps.upstreamHealth[upstream]
	if !exists || health.IsHealthy {
		return "CLOSED"
	}
	return "OPEN"
}

func (ps *ProxyServer) getHealthMetrics() map[string]interface{} {
	// TODO: Implement comprehensive health metrics export
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()
	
	metrics := make(map[string]interface{})
	upstreams := make(map[string]interface{})
	
	for url, health := range ps.upstreamHealth {
		upstreams[url] = map[string]interface{}{
			"healthy":       health.IsHealthy,
			"failure_count": health.FailureCount,
			"success_count": health.SuccessCount,
		}
	}
	
	metrics["upstreams"] = upstreams
	return metrics
}

// Additional stub methods for advanced failover features
func (ps *ProxyServer) getFailureThreshold(upstream string) int {
	ps.healthMutex.RLock()
	defer ps.healthMutex.RUnlock()
	
	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		return 3 // Default threshold
	}
	return health.FailureThreshold
}

func (ps *ProxyServer) adjustFailureThreshold(upstream string, successRate float64) {
	// TODO: Implement dynamic threshold adjustment based on success rate
	ps.healthMutex.Lock()
	defer ps.healthMutex.Unlock()
	
	health, exists := ps.upstreamHealth[upstream]
	if !exists {
		return
	}
	
	// Simple adjustment logic: lower success rate = stricter threshold
	if successRate < 0.5 {
		health.FailureThreshold = 2 // Stricter
	} else if successRate > 0.8 {
		health.FailureThreshold = 5 // More tolerant
	}
}

func (ps *ProxyServer) enableExponentialBackoff(upstream string, enabled bool) {
	// TODO: Implement exponential backoff for retry timing
}

func (ps *ProxyServer) getNextRetryTime(upstream string) time.Time {
	// TODO: Implement exponential backoff timing
	return time.Now().Add(1 * time.Second) // Simple 1-second delay for now
}

func (ps *ProxyServer) authenticate(r *http.Request) bool {
	ps.mutex.RLock()
	config := ps.config
	ps.mutex.RUnlock()
	
	if !config.Authentication.Enabled {
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

	for _, user := range config.Authentication.Users {
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

	// Parse upstream URL for authentication
	upstreamHost, upstreamAuth, err := parseUpstreamAuth(upstream)
	if err != nil {
		log.Printf("Failed to parse upstream URL %s: %v", upstream, err)
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		http.Error(w, "Invalid upstream proxy configuration", http.StatusBadGateway)
		return
	}

	// Connect to upstream proxy
	upstreamConn, err := net.DialTimeout("tcp", upstreamHost, 30*time.Second)
	if err != nil {
		atomic.AddInt64(&ps.stats.FailedRequests, 1)
		atomic.AddInt64(&upstreamStats.FailedRequests, 1)
		http.Error(w, "Failed to connect to upstream proxy", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	// Send CONNECT request to upstream with authentication if present
	var connectReq string
	if upstreamAuth != "" {
		connectReq = fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n", r.Host, r.Host, upstreamAuth)
	} else {
		connectReq = fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", r.Host, r.Host)
	}
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

	ps.mutex.RLock()
	startTime := ps.stats.StartTime
	ps.mutex.RUnlock()

	stats := struct {
		StartTime       time.Time       `json:"start_time"`
		Uptime          string          `json:"uptime"`
		TotalStats      TimeWindowStats `json:"total"`
		RecentStats     TimeWindowStats `json:"recent_15m"`
		CurrentRequests int64           `json:"current_requests"`
	}{
		StartTime:       startTime,
		Uptime:          time.Since(startTime).String(),
		TotalStats:      ps.getTimeWindowStats(time.Since(startTime)),
		RecentStats:     ps.getTimeWindowStats(15 * time.Minute),
		CurrentRequests: atomic.LoadInt64(&ps.stats.CurrentRequests),
	}

	json.NewEncoder(w).Encode(stats)
}

func (ps *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ps.mutex.RLock()
	statsEndpoint := ps.config.Server.StatsEndpoint
	ps.mutex.RUnlock()
	
	if r.URL.Path == statsEndpoint {
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

// parseUpstreamAuth parses an upstream proxy URL and extracts host and auth header
func parseUpstreamAuth(upstreamURL string) (host, auth string, err error) {
	if !strings.HasPrefix(upstreamURL, "http://") && !strings.HasPrefix(upstreamURL, "https://") {
		return "", "", fmt.Errorf("invalid URL scheme")
	}

	// Remove http:// or https:// prefix
	urlPart := strings.TrimPrefix(upstreamURL, "http://")
	urlPart = strings.TrimPrefix(urlPart, "https://")

	// Check if auth is present
	if strings.Contains(urlPart, "@") {
		parts := strings.Split(urlPart, "@")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid URL format")
		}
		
		authPart := parts[0]
		host = parts[1]
		
		// URL decode the auth part for common cases
		if strings.Contains(authPart, "%40") {
			authPart = strings.ReplaceAll(authPart, "%40", "@")
		}
		
		// Create basic auth header
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(authPart))
	} else {
		host = urlPart
	}

	return host, auth, nil
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

	proxyServer := NewProxyServer(config, configPath)

	// Start config file watcher
	proxyServer.startConfigWatcher()

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
