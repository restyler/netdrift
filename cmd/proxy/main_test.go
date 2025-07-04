package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStatsEndpoint(t *testing.T) {
	// Create test configuration
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3150",
			StatsEndpoint: "/stats",
		},
		Authentication: struct {
			Enabled bool `json:"enabled"`
			Users   []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		}{
			Enabled: true,
			Users: []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				{Username: "proxyuser", Password: "Proxy234"},
			},
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{}, // Empty upstream proxies list
	}

	// Create proxy server
	ps := NewProxyServer(config, "")

	// Start server with timeouts
	server := &http.Server{
		Addr:              config.Server.ListenAddress,
		Handler:           ps,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go server.ListenAndServe()
	defer server.Close()

	// Wait for server to start and verify it's listening
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:3150", time.Second)
		if err == nil {
			conn.Close()
			break
		}
		if i == maxRetries-1 {
			t.Fatalf("Server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Create authentication header
	auth := base64.StdEncoding.EncodeToString([]byte("proxyuser:Proxy234"))

	// Test stats endpoint with a timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest("GET", "http://127.0.0.1:3150/stats", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Add("Proxy-Authorization", fmt.Sprintf("Basic %s", auth))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	defer resp.Body.Close()

	// Should get successful response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Decode stats
	var stats struct {
		StartTime          time.Time   `json:"start_time"`
		Uptime             string      `json:"uptime"`
		TotalStats         interface{} `json:"total"`
		RecentStats        interface{} `json:"recent_15m"`
		CurrentConcurrency int64       `json:"current_concurrency"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode stats: %v", err)
	}

	// Basic validation - just check structure exists
	if stats.StartTime.IsZero() {
		t.Error("Stats should have a valid start time")
	}

	if stats.Uptime == "" {
		t.Error("Stats should have uptime information")
	}

	if stats.TotalStats == nil {
		t.Error("Stats should have total stats")
	}

	if stats.RecentStats == nil {
		t.Error("Stats should have recent stats")
	}

	// Current concurrency should be reasonable (0 or positive)
	if stats.CurrentConcurrency < 0 {
		t.Errorf("Current concurrency should not be negative, got %d", stats.CurrentConcurrency)
	}
}

func TestStatsEndpointNoAuth(t *testing.T) {
	// Create test configuration with auth disabled
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3138",
			StatsEndpoint: "/stats",
		},
		Authentication: struct {
			Enabled bool `json:"enabled"`
			Users   []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		}{
			Enabled: false, // Disable auth
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9989", Enabled: true, Weight: 1},
		},
	}

	// Create proxy server
	ps := NewProxyServer(config, "")

	// Start server with timeouts
	server := &http.Server{
		Addr:              config.Server.ListenAddress,
		Handler:           ps,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}

	// Create channels for server startup synchronization
	serverReady := make(chan struct{})
	serverError := make(chan error, 1)

	go func() {
		// Signal that we're about to start listening
		close(serverReady)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			serverError <- err
		}
	}()

	// Wait for server to start or fail
	select {
	case err := <-serverError:
		t.Fatalf("Server failed to start: %v", err)
	case <-serverReady:
		// Continue with test
	}

	// Ensure server cleanup
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Server shutdown error: %v", err)
		}
	}()

	// Wait for server to start and verify it's listening
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:3138", time.Second)
		if err == nil {
			conn.Close()
			break
		}
		if i == maxRetries-1 {
			t.Fatalf("Server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Test stats endpoint without auth (should work since auth is disabled)
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   1 * time.Second,
				KeepAlive: 1 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 1 * time.Second,
			IdleConnTimeout:       1 * time.Second,
			DisableKeepAlives:     true, // Disable keep-alives to prevent connection reuse
		},
	}

	req, err := http.NewRequest("GET", "http://127.0.0.1:3138/stats", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Add context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	defer resp.Body.Close()

	// Should get successful response even without auth
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Read body with timeout
	type statsResponse struct {
		StartTime interface{} `json:"start_time"`
	}
	var stats statsResponse

	// Create a timer for body read timeout
	bodyTimer := time.NewTimer(2 * time.Second)
	defer bodyTimer.Stop()

	// Create a channel for the decoding operation
	done := make(chan error, 1)
	go func() {
		done <- json.NewDecoder(resp.Body).Decode(&stats)
	}()

	// Wait for either completion or timeout
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Failed to decode stats JSON: %v", err)
		}
	case <-bodyTimer.C:
		t.Fatal("Timeout while reading response body")
	}

	// Should have basic structure
	if stats.StartTime == nil {
		t.Error("Stats should contain start_time")
	}
}

func TestInvalidEndpoint(t *testing.T) {
	// Create test configuration
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3139",
			StatsEndpoint: "/stats",
		},
		Authentication: struct {
			Enabled bool `json:"enabled"`
			Users   []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		}{
			Enabled: false,
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9988", Enabled: true, Weight: 1},
		},
	}

	// Create proxy server
	ps := NewProxyServer(config, "")

	// Start server
	server := &http.Server{
		Addr:    config.Server.ListenAddress,
		Handler: ps,
	}
	go server.ListenAndServe()
	defer server.Close()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Test invalid endpoint
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://127.0.0.1:3139/invalid", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Should get Method Not Allowed for non-CONNECT, non-stats requests
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d for invalid endpoint, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

func TestStatsEndpointHTTPAuth(t *testing.T) {
	// Create test configuration
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3149",
			StatsEndpoint: "/stats",
		},
		Authentication: struct {
			Enabled bool `json:"enabled"`
			Users   []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		}{
			Enabled: true,
			Users: []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}{
				{Username: "testuser", Password: "testpass"},
			},
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{}, // Empty upstream proxies list
	}

	// Create proxy server
	ps := NewProxyServer(config, "")

	// Start server with timeouts
	server := &http.Server{
		Addr:              config.Server.ListenAddress,
		Handler:           ps,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go server.ListenAndServe()
	defer server.Close()

	// Wait for server to start and verify it's listening
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:3149", time.Second)
		if err == nil {
			conn.Close()
			break
		}
		if i == maxRetries-1 {
			t.Fatalf("Server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Create authentication header
	auth := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))

	// Test stats endpoint with standard Authorization header
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	
	t.Run("StandardAuthorizationHeader", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:3149/stats", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Add("Authorization", fmt.Sprintf("Basic %s", auth))
		
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		defer resp.Body.Close()

		// Should get successful response
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var stats map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode stats: %v", err)
		}

		// Check basic stats structure
		if _, ok := stats["start_time"]; !ok {
			t.Error("Stats should have start_time")
		}
	})

	t.Run("ProxyAuthorizationHeader", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:3149/stats", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Add("Proxy-Authorization", fmt.Sprintf("Basic %s", auth))
		
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		defer resp.Body.Close()

		// Should get successful response
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var stats map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode stats: %v", err)
		}

		// Check basic stats structure
		if _, ok := stats["start_time"]; !ok {
			t.Error("Stats should have start_time")
		}
	})

	t.Run("NoAuthHeader", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:3149/stats", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		defer resp.Body.Close()

		// Should get 401 Unauthorized
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		// Should have WWW-Authenticate header
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if wwwAuth == "" {
			t.Error("Expected WWW-Authenticate header")
		}
		if !strings.Contains(wwwAuth, "Basic") {
			t.Errorf("Expected Basic auth in WWW-Authenticate header, got: %s", wwwAuth)
		}
	})
}
