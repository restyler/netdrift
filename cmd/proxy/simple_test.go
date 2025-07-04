package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBasicProxyFunctionality tests the core proxy functionality without complex upstream dependencies
func TestBasicProxyFunctionality(t *testing.T) {
	// Create simple test configuration
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3132",
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
		}{
			{URL: "http://127.0.0.1:9999", Enabled: true, Weight: 1}, // Non-existent upstream
		},
	}

	// Create and start main proxy
	ps := NewProxyServer(config, "")
	server := &http.Server{
		Addr:    config.Server.ListenAddress,
		Handler: ps,
	}
	go server.ListenAndServe()
	defer server.Close()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	t.Run("Authentication", func(t *testing.T) {
		// Test authentication rejection
		conn, err := net.Dial("tcp", "127.0.0.1:3132")
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Send CONNECT without auth
		connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			t.Fatalf("Failed to write: %v", err)
		}

		// Should get 407 Proxy Authentication Required
		buffer := make([]byte, 1024)
		n, err := conn.Read(buffer)
		if err != nil {
			t.Fatalf("Failed to read: %v", err)
		}

		response := string(buffer[:n])
		if !strings.Contains(response, "407") {
			t.Errorf("Expected 407 auth required, got: %s", response)
		}
	})

	t.Run("AuthenticationSuccess", func(t *testing.T) {
		// Test successful authentication (even if upstream fails)
		conn, err := net.Dial("tcp", "127.0.0.1:3132")
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Create auth header
		auth := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
		connectReq := fmt.Sprintf("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
		
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			t.Fatalf("Failed to write: %v", err)
		}

		// Should get response (even if it's an error due to upstream failure)
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil {
			t.Fatalf("Failed to read: %v", err)
		}

		response := string(buffer[:n])
		// Should not be 407 since we provided auth
		if strings.Contains(response, "407") {
			t.Errorf("Should not get 407 with valid auth, got: %s", response)
		}
	})

	t.Run("StatsEndpoint", func(t *testing.T) {
		// Test stats endpoint
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", "http://127.0.0.1:3132/stats", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200, got %d", resp.StatusCode)
		}

		var stats map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("Failed to decode stats: %v", err)
		}

		// Check basic stats structure
		if _, ok := stats["start_time"]; !ok {
			t.Error("Stats should have start_time")
		}
		if _, ok := stats["uptime"]; !ok {
			t.Error("Stats should have uptime")
		}
	})
}

// TestProxyServerCreation tests the basic proxy server creation and configuration
func TestProxyServerCreation(t *testing.T) {
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3133",
			StatsEndpoint: "/stats",
		},
		Authentication: struct {
			Enabled bool `json:"enabled"`
			Users   []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		}{
			Enabled: false, // Disable auth for simpler testing
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9998", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9997", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")
	if ps == nil {
		t.Fatal("NewProxyServer should not return nil")
	}

	// Check that upstreams were loaded
	if len(ps.upstreams) != 2 {
		t.Errorf("Expected 2 upstreams, got %d", len(ps.upstreams))
	}

	// Test round-robin selection
	first := ps.getNextUpstream()
	second := ps.getNextUpstream()
	third := ps.getNextUpstream()

	if first == second {
		t.Error("Round-robin should alternate between upstreams")
	}
	if first != third {
		t.Error("Round-robin should cycle back to first upstream")
	}
}

// TestConfigLoading tests configuration file loading
func TestConfigLoading(t *testing.T) {
	// Create a temporary config file
	configContent := `{
		"server": {
			"name": "Test Proxy",
			"listen_address": "127.0.0.1:3134",
			"stats_endpoint": "/stats"
		},
		"authentication": {
			"enabled": true,
			"users": [
				{
					"username": "user1",
					"password": "pass1"
				}
			]
		},
		"upstream_proxies": [
			{
				"url": "http://127.0.0.1:8081",
				"enabled": true,
				"weight": 1
			}
		]
	}`

	// Write to temporary file
	tmpFile := "/tmp/test_config.json"
	if err := writeFile(tmpFile, configContent); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}
	defer removeFile(tmpFile)

	// Load config
	config, err := loadConfig(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify config
	if config.Server.Name != "Test Proxy" {
		t.Errorf("Expected server name 'Test Proxy', got '%s'", config.Server.Name)
	}

	if !config.Authentication.Enabled {
		t.Error("Expected authentication to be enabled")
	}

	if len(config.Authentication.Users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(config.Authentication.Users))
	}

	if len(config.UpstreamProxies) != 1 {
		t.Errorf("Expected 1 upstream proxy, got %d", len(config.UpstreamProxies))
	}
}

// TestUpstreamProxyAuthentication tests that upstream proxies with authentication
// in the format http://user:pass@host:port are properly handled
func TestUpstreamProxyAuthentication(t *testing.T) {
	t.Run("ParseUpstreamAuthURL", func(t *testing.T) {
		testCases := []struct {
			name     string
			url      string
			wantHost string
			wantAuth string
			wantErr  bool
		}{
			{
				name:     "Basic auth URL",
				url:      "http://user:pass@127.0.0.1:3128",
				wantHost: "127.0.0.1:3128",
				wantAuth: "Basic dXNlcjpwYXNz", // base64("user:pass")
				wantErr:  false,
			},
			{
				name:     "Auth URL with special characters",
				url:      "http://test%40user:p%40ssw0rd@proxy.example.com:8080",
				wantHost: "proxy.example.com:8080",
				wantAuth: "Basic dGVzdEB1c2VyOnBAc3N3MHJk", // base64("test@user:p@ssw0rd")
				wantErr:  false,
			},
			{
				name:     "URL without auth",
				url:      "http://127.0.0.1:3128",
				wantHost: "127.0.0.1:3128",
				wantAuth: "",
				wantErr:  false,
			},
			{
				name:     "HTTPS URL with auth",
				url:      "https://user:pass@secure-proxy.com:443",
				wantHost: "secure-proxy.com:443",
				wantAuth: "Basic dXNlcjpwYXNz",
				wantErr:  false,
			},
			{
				name:     "Invalid URL",
				url:      "not-a-url",
				wantHost: "",
				wantAuth: "",
				wantErr:  true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				host, auth, err := parseUpstreamAuth(tc.url)
				
				if tc.wantErr {
					if err == nil {
						t.Errorf("Expected error for URL %s, but got none", tc.url)
					}
					return
				}
				
				if err != nil {
					t.Errorf("Unexpected error for URL %s: %v", tc.url, err)
					return
				}
				
				if host != tc.wantHost {
					t.Errorf("Expected host %s, got %s", tc.wantHost, host)
				}
				
				if auth != tc.wantAuth {
					t.Errorf("Expected auth %s, got %s", tc.wantAuth, auth)
				}
			})
		}
	})

	t.Run("UpstreamConnectRequestWithAuth", func(t *testing.T) {
		// Test that CONNECT requests to upstream proxies include authentication
		testCases := []struct {
			name        string
			upstreamURL string
			targetHost  string
			wantContains []string
		}{
			{
				name:        "CONNECT with upstream auth",
				upstreamURL: "http://proxyuser:proxypass@127.0.0.1:3128",
				targetHost:  "example.com:443",
				wantContains: []string{
					"CONNECT example.com:443 HTTP/1.1",
					"Host: example.com:443",
					"Proxy-Authorization: Basic cHJveHl1c2VyOnByb3h5cGFzcw==", // base64("proxyuser:proxypass")
				},
			},
			{
				name:        "CONNECT without upstream auth",
				upstreamURL: "http://127.0.0.1:3128",
				targetHost:  "example.com:443",
				wantContains: []string{
					"CONNECT example.com:443 HTTP/1.1",
					"Host: example.com:443",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				connectReq := buildUpstreamConnectRequest(tc.upstreamURL, tc.targetHost)
				
				for _, want := range tc.wantContains {
					if !strings.Contains(connectReq, want) {
						t.Errorf("CONNECT request should contain %q, but got:\n%s", want, connectReq)
					}
				}
				
				// If no auth expected, ensure no Proxy-Authorization header
				if !strings.Contains(tc.upstreamURL, "@") {
					if strings.Contains(connectReq, "Proxy-Authorization") {
						t.Errorf("CONNECT request should not contain Proxy-Authorization header when no auth, but got:\n%s", connectReq)
					}
				}
			})
		}
	})

	t.Run("ConfigurationParsing", func(t *testing.T) {
		// Test configuration parsing with upstream authentication
		configContent := `{
			"server": {
				"name": "Test Proxy",
				"listen_address": "127.0.0.1:3135",
				"stats_endpoint": "/stats"
			},
			"authentication": {
				"enabled": false
			},
			"upstream_proxies": [
				{
					"url": "http://user1:pass1@proxy1.example.com:3128",
					"enabled": true,
					"weight": 1
				},
				{
					"url": "http://user2:pass2@proxy2.example.com:8080",
					"enabled": true,
					"weight": 2
				},
				{
					"url": "http://proxy3.example.com:3128",
					"enabled": true,
					"weight": 1
				}
			]
		}`

		tmpFile := "/tmp/test_upstream_auth_config.json"
		if err := writeFile(tmpFile, configContent); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}
		defer removeFile(tmpFile)

		config, err := loadConfig(tmpFile)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		if len(config.UpstreamProxies) != 3 {
			t.Errorf("Expected 3 upstream proxies, got %d", len(config.UpstreamProxies))
		}

		// Verify URLs are preserved with authentication
		expectedURLs := []string{
			"http://user1:pass1@proxy1.example.com:3128",
			"http://user2:pass2@proxy2.example.com:8080",
			"http://proxy3.example.com:3128",
		}

		for i, expected := range expectedURLs {
			if config.UpstreamProxies[i].URL != expected {
				t.Errorf("Expected upstream URL %s, got %s", expected, config.UpstreamProxies[i].URL)
			}
		}
	})
}

// Helper functions for upstream authentication parsing

// buildUpstreamConnectRequest builds a CONNECT request for upstream proxy
func buildUpstreamConnectRequest(upstreamURL, targetHost string) string {
	_, auth, err := parseUpstreamAuth(upstreamURL)
	if err != nil {
		return fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetHost, targetHost)
	}

	if auth != "" {
		return fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n", targetHost, targetHost, auth)
	}

	return fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetHost, targetHost)
}

// Helper functions for file operations

func writeFile(filename, content string) error {
	return os.WriteFile(filename, []byte(content), 0644)
}

func removeFile(filename string) error {
	return os.Remove(filename)
}