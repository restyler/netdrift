package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthCheckWithRealProxyServers tests health checks against actual proxy servers
func TestHealthCheckWithRealProxyServers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("HealthCheckWithWorkingProxy", func(t *testing.T) {
		// Create a mock IP resolver that returns different IPs for proxy vs direct access
		directIP := "203.0.113.1"
		proxyIP := "203.0.113.100"
		
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			
			// Simulate different IPs based on proxy usage
			// In a real scenario, the proxy would change the source IP
			ip := directIP
			if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Via") != "" {
				ip = proxyIP
			}
			
			response := IPResponse{IP: ip}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		// Create a simple HTTP proxy server for testing
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "CONNECT" {
				// Handle CONNECT requests for HTTPS
				w.WriteHeader(http.StatusOK)
				return
			}
			
			// For HTTP requests, act as a proxy
			resp, err := http.Get(r.URL.String())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			
			// Copy headers and status
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			
			// Copy body
			buf := make([]byte, 1024)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
		}))
		defer proxyServer.Close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.URL, Enabled: true, Weight: 1, Tag: "test"},
			},
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled:           true,
				IntervalSeconds:   2, // Fast for testing
				TimeoutSeconds:    5,
				FailureThreshold:  2,
				RecoveryThreshold: 1,
				Endpoints:         []string{ipServer.URL},
				EndpointRotation:  false,
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Wait for at least one health check cycle
		time.Sleep(3 * time.Second)

		// Proxy should be healthy
		if !ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should be healthy after successful health checks")
		}

		// Check health metrics
		metrics := ps.getHealthMetrics()
		upstreams, ok := metrics["upstreams"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected upstreams in health metrics")
		}

		proxyMetrics, ok := upstreams[proxyServer.URL].(map[string]interface{})
		if !ok {
			t.Fatal("Expected proxy metrics in health metrics")
		}

		healthy, ok := proxyMetrics["healthy"].(bool)
		if !ok || !healthy {
			t.Error("Proxy should be marked as healthy in metrics")
		}
	})

	t.Run("HealthCheckFailureRecovery", func(t *testing.T) {
		// Create IP server that can be toggled between working and failing
		serverWorking := true
		
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !serverWorking {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.50"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		// Simple proxy that always works
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simple echo proxy for testing
			w.WriteHeader(http.StatusOK)
		}))
		defer proxyServer.Close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.URL, Enabled: true, Weight: 1},
			},
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled:           true,
				IntervalSeconds:   1, // Very fast for testing
				TimeoutSeconds:    3,
				FailureThreshold:  2,
				RecoveryThreshold: 1,
				Endpoints:         []string{ipServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Initially should be healthy
		time.Sleep(2 * time.Second)
		if !ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should initially be healthy")
		}

		// Break the IP server
		serverWorking = false
		
		// Wait for failure detection (2 failures at 1s interval + processing time)
		time.Sleep(4 * time.Second)
		
		if ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should be unhealthy after IP server failures")
		}

		// Fix the IP server
		serverWorking = true
		
		// Wait for recovery (1 success needed)
		time.Sleep(3 * time.Second)
		
		if !ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should recover health after IP server recovery")
		}
	})

	t.Run("MultipleEndpointFallback", func(t *testing.T) {
		// Create multiple IP servers - some working, some failing
		workingServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.10"}
			json.NewEncoder(w).Encode(response)
		}))
		defer workingServer1.Close()

		failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer failingServer.Close()

		workingServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.20"}
			json.NewEncoder(w).Encode(response)
		}))
		defer workingServer2.Close()

		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer proxyServer.Close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.URL, Enabled: true, Weight: 1},
			},
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled:          true,
				IntervalSeconds:  1,
				TimeoutSeconds:   3,
				FailureThreshold: 3,
				RecoveryThreshold: 1,
				// Mix of working and failing endpoints
				Endpoints:        []string{workingServer1.URL, failingServer.URL, workingServer2.URL},
				EndpointRotation: true,
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Wait for several health check cycles
		time.Sleep(5 * time.Second)

		// Proxy should remain healthy because some endpoints work
		if !ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should remain healthy when some endpoints work")
		}

		// Check that we had some successes despite the failing endpoint
		successCount := ps.upstreamHealth[proxyServer.URL].SuccessCount
		if successCount == 0 {
			t.Error("Should have had some successful health checks")
		}
	})
}

// TestHealthCheckConfigurationIntegration tests configuration changes
func TestHealthCheckConfigurationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("DynamicConfigurationChanges", func(t *testing.T) {
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.99"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer proxyServer.Close()

		// Start with health checks disabled
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.URL, Enabled: true, Weight: 1},
			},
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled: false,
			},
		}

		ps := NewProxyServer(config, "")
		defer func() {
			if ps.healthChecker != nil {
				ps.stopHealthChecker()
			}
		}()

		// Should not have health checker initially
		if ps.healthChecker != nil {
			t.Error("Should not have health checker when disabled")
		}

		// Enable health checks
		config.HealthCheck.Enabled = true
		config.HealthCheck.IntervalSeconds = 1
		config.HealthCheck.Endpoints = []string{ipServer.URL}

		// Restart proxy server with new config (simulates config reload)
		ps.stopHealthChecker()
		if config.HealthCheck.Enabled {
			interval := time.Duration(config.HealthCheck.IntervalSeconds) * time.Second
			ps.startHealthChecker(interval)
		}

		// Should now have health checker
		if ps.healthChecker == nil {
			t.Error("Should have health checker after enabling")
		}

		// Wait for health checks
		time.Sleep(3 * time.Second)

		if !ps.isUpstreamHealthy(proxyServer.URL) {
			t.Error("Proxy should be healthy after enabling health checks")
		}
	})
}

// TestHealthCheckStatsIntegration tests health check statistics
func TestHealthCheckStatsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("HealthCheckStatsReporting", func(t *testing.T) {
		requestCount := 0
		
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			
			// Fail every third request to test mixed results
			if requestCount%3 == 0 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			
			response := IPResponse{IP: fmt.Sprintf("203.0.113.%d", requestCount)}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer proxyServer.Close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.URL, Enabled: true, Weight: 1, Tag: "integration-test"},
			},
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled:           true,
				IntervalSeconds:   1,
				TimeoutSeconds:    3,
				FailureThreshold: 5, // High threshold so it doesn't go unhealthy
				RecoveryThreshold: 1,
				Endpoints:         []string{ipServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Wait for several health check cycles
		time.Sleep(6 * time.Second)

		// Check health metrics
		metrics := ps.getHealthMetrics()
		
		upstreams, ok := metrics["upstreams"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected upstreams in health metrics")
		}

		proxyMetrics, ok := upstreams[proxyServer.URL].(map[string]interface{})
		if !ok {
			t.Fatal("Expected proxy metrics in health metrics")
		}

		successCount, ok := proxyMetrics["success_count"].(int64)
		if !ok {
			t.Fatal("Expected success_count in proxy metrics")
		}

		failureCount, ok := proxyMetrics["failure_count"].(int64)
		if !ok {
			t.Fatal("Expected failure_count in proxy metrics")
		}

		if successCount == 0 {
			t.Error("Should have some successful health checks")
		}

		if failureCount == 0 {
			t.Error("Should have some failed health checks (every 3rd request fails)")
		}

		// Check tag groups
		if tagGroups, ok := metrics["tag_groups"].(map[string]interface{}); ok {
			if testGroup, ok := tagGroups["integration-test"].(map[string]interface{}); ok {
				if healthyCount, ok := testGroup["healthy_upstreams"].(int); ok && healthyCount != 1 {
					t.Errorf("Expected 1 healthy upstream in tag group, got %d", healthyCount)
				}
			}
		}

		t.Logf("Health check results: %d successes, %d failures", successCount, failureCount)
	})
}