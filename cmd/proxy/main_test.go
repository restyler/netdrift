package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
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
			ListenAddress: "127.0.0.1:3131",
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
		}{
			{URL: "http://127.0.0.1:9991", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9990", Enabled: true, Weight: 1},
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

	// Create authentication header
	auth := base64.StdEncoding.EncodeToString([]byte("proxyuser:Proxy234"))

	// Test stats endpoint
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://127.0.0.1:3131/stats", nil)
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
		StartTime       time.Time `json:"start_time"`
		Uptime          string    `json:"uptime"`
		TotalStats      interface{} `json:"total"`
		RecentStats     interface{} `json:"recent_15m"`
		CurrentRequests int64     `json:"current_requests"`
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

	// Current requests should be reasonable (0 or positive)
	if stats.CurrentRequests < 0 {
		t.Errorf("Current requests should not be negative, got %d", stats.CurrentRequests)
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
		}{
			{URL: "http://127.0.0.1:9989", Enabled: true, Weight: 1},
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

	// Test stats endpoint without auth (should work since auth is disabled)
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://127.0.0.1:3138/stats", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	defer resp.Body.Close()

	// Should get successful response even without auth
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify it's valid JSON
	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode stats JSON: %v", err)
	}

	// Should have basic structure
	if _, ok := stats["start_time"]; !ok {
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