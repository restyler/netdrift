package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestBasicConcurrency tests basic concurrent connection handling without complex fault injection
func TestBasicConcurrency(t *testing.T) {
	// Create simple test configuration
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3135",
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
		}{
			{URL: "http://127.0.0.1:9996", Enabled: true, Weight: 1}, // Non-existent upstream
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

	// Test concurrent connections
	var wg sync.WaitGroup
	numRequests := 10
	wg.Add(numRequests)

	// Create authentication header
	auth := base64.StdEncoding.EncodeToString([]byte("proxyuser:Proxy234"))
	authHeader := fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)

	// Track connection attempts
	connectionsMade := 0
	var connectionsMutex sync.Mutex

	// Launch concurrent requests
	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()

			// Create a direct TCP connection to the proxy
			conn, err := net.Dial("tcp", "127.0.0.1:3135")
			if err != nil {
				t.Logf("Request %d: Failed to connect to proxy: %v", id, err)
				return
			}
			defer conn.Close()

			connectionsMutex.Lock()
			connectionsMade++
			connectionsMutex.Unlock()

			// Send CONNECT request with authentication
			connectReq := fmt.Sprintf("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n%s\r\n", authHeader)
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				t.Logf("Request %d: Failed to send CONNECT request: %v", id, err)
				return
			}

			// Try to read response with short timeout since upstream will fail
			buf := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				t.Logf("Request %d: Failed to read response (expected): %v", id, err)
				return
			}

			response := string(buf[:n])
			t.Logf("Request %d: Received response: %q", id, response)
		}(i)
	}

	// Wait for all requests to complete
	wg.Wait()

	t.Logf("Successfully made %d connections out of %d attempts", connectionsMade, numRequests)

	// We should have been able to establish connections to the proxy
	// (even if the upstream connections fail)
	if connectionsMade == 0 {
		t.Error("Expected to establish at least some connections to the proxy")
	}
}

// TestProxyRoundRobin tests the round-robin upstream selection
func TestProxyRoundRobin(t *testing.T) {
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3136",
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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9995", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9994", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9993", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Test round-robin selection
	seen := make(map[string]int)
	iterations := 9 // 3 full cycles

	for i := 0; i < iterations; i++ {
		upstream := ps.getNextUpstream()
		seen[upstream]++
	}

	// Each upstream should have been selected exactly 3 times
	expectedCount := 3
	for upstream, count := range seen {
		if count != expectedCount {
			t.Errorf("Upstream %s selected %d times, expected %d", upstream, count, expectedCount)
		}
	}

	// Should have exactly 3 different upstreams
	if len(seen) != 3 {
		t.Errorf("Expected 3 different upstreams, got %d", len(seen))
	}
}

// TestAuthenticationFlow tests the authentication mechanism in detail
func TestAuthenticationFlow(t *testing.T) {
	config := &Config{
		Server: struct {
			Name          string `json:"name"`
			ListenAddress string `json:"listen_address"`
			StatsEndpoint string `json:"stats_endpoint"`
		}{
			Name:          "Test Proxy",
			ListenAddress: "127.0.0.1:3137",
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
				{Username: "user1", Password: "pass1"},
				{Username: "user2", Password: "pass2"},
			},
		},
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9992", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Test authentication function directly
	t.Run("ValidUser1", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		auth := base64.StdEncoding.EncodeToString([]byte("user1:pass1"))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)

		if !ps.authenticate(req) {
			t.Error("Valid user1 should authenticate successfully")
		}
	})

	t.Run("ValidUser2", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		auth := base64.StdEncoding.EncodeToString([]byte("user2:pass2"))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)

		if !ps.authenticate(req) {
			t.Error("Valid user2 should authenticate successfully")
		}
	})

	t.Run("InvalidUser", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		auth := base64.StdEncoding.EncodeToString([]byte("invalid:invalid"))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)

		if ps.authenticate(req) {
			t.Error("Invalid user should not authenticate")
		}
	})

	t.Run("NoAuth", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}

		if ps.authenticate(req) {
			t.Error("Request without auth should not authenticate")
		}
	})

	t.Run("MalformedAuth", func(t *testing.T) {
		req := &http.Request{
			Header: make(http.Header),
		}
		req.Header.Set("Proxy-Authorization", "Basic invalidbase64")

		if ps.authenticate(req) {
			t.Error("Request with malformed auth should not authenticate")
		}
	})
}