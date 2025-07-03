package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
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

type ProxyServer struct {
	config     *Config
	upstreams  []string
	currentIdx int
	mutex      sync.Mutex
	stats      struct {
		TotalRequests   int64
		SuccessRequests int64
		FailedRequests  int64
	}
}

func NewProxyServer(config *Config) *ProxyServer {
	ps := &ProxyServer{
		config: config,
	}

	// Build list of enabled upstream proxies
	for _, upstream := range config.UpstreamProxies {
		if upstream.Enabled {
			ps.upstreams = append(ps.upstreams, upstream.URL)
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
	ps.stats.TotalRequests++

	log.Printf("CONNECT request to %s from %s", r.Host, r.RemoteAddr)

	if !ps.authenticate(r) {
		log.Printf("Authentication failed for %s", r.RemoteAddr)
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"Proxy\"")
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		ps.stats.FailedRequests++
		return
	}

	upstream := ps.getNextUpstream()
	if upstream == "" {
		log.Printf("No upstream proxies available")
		http.Error(w, "No upstream proxies available", http.StatusBadGateway)
		ps.stats.FailedRequests++
		return
	}

	log.Printf("Using upstream proxy: %s", upstream)

	// Connect to upstream proxy
	upstreamConn, err := net.DialTimeout("tcp", strings.TrimPrefix(upstream, "http://"), 30*time.Second)
	if err != nil {
		log.Printf("Failed to connect to upstream %s: %v", upstream, err)
		http.Error(w, "Failed to connect to upstream proxy", http.StatusBadGateway)
		ps.stats.FailedRequests++
		return
	}
	defer upstreamConn.Close()

	// Send CONNECT request to upstream
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", r.Host, r.Host)
	if _, err := upstreamConn.Write([]byte(connectReq)); err != nil {
		log.Printf("Failed to send CONNECT to upstream: %v", err)
		http.Error(w, "Failed to connect", http.StatusBadGateway)
		ps.stats.FailedRequests++
		return
	}

	// Read response from upstream
	response := make([]byte, 1024)
	n, err := upstreamConn.Read(response)
	if err != nil {
		log.Printf("Failed to read response from upstream: %v", err)
		http.Error(w, "Failed to connect", http.StatusBadGateway)
		ps.stats.FailedRequests++
		return
	}

	responseStr := string(response[:n])
	if !strings.Contains(responseStr, "200") {
		log.Printf("Upstream proxy rejected connection: %s", strings.TrimSpace(responseStr))
		http.Error(w, "Upstream proxy rejected connection", http.StatusBadGateway)
		ps.stats.FailedRequests++
		return
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("ResponseWriter doesn't support hijacking")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		ps.stats.FailedRequests++
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("Failed to hijack connection: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		ps.stats.FailedRequests++
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established to client
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("Failed to send 200 to client: %v", err)
		ps.stats.FailedRequests++
		return
	}

	log.Printf("Established tunnel between client and %s via %s", r.Host, upstream)
	ps.stats.SuccessRequests++

	// Start bidirectional copying
	go func() {
		defer upstreamConn.Close()
		defer clientConn.Close()
		io.Copy(upstreamConn, clientConn)
	}()

	io.Copy(clientConn, upstreamConn)
}

func (ps *ProxyServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := map[string]interface{}{
		"total_requests":   ps.stats.TotalRequests,
		"success_requests": ps.stats.SuccessRequests,
		"failed_requests":  ps.stats.FailedRequests,
		"upstream_proxies": ps.upstreams,
		"current_upstream": ps.currentIdx,
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

func main() {
	configFile := "us.json"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	log.Printf("Loading configuration from %s", configFile)
	config, err := loadConfig(configFile)
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
