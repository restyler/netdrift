package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Mock IP resolver server for testing
func createMockIPResolverServer(responseIP string, statusCode int, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		
		if statusCode == 200 {
			response := IPResponse{IP: responseIP}
			json.NewEncoder(w).Encode(response)
		} else {
			fmt.Fprintf(w, `{"error": "service unavailable"}`)
		}
	}))
}

// Mock proxy server that tracks requests
type MockProxyServer struct {
	server       *httptest.Server
	requestCount int
	mutex        sync.Mutex
	shouldFail   bool
}

func createMockProxyServer(targetServer *httptest.Server) *MockProxyServer {
	mps := &MockProxyServer{}
	mps.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mps.mutex.Lock()
		mps.requestCount++
		shouldFail := mps.shouldFail
		mps.mutex.Unlock()
		
		if shouldFail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		
		// Act as a proxy - forward the request to the target server
		if targetServer != nil {
			resp, err := http.Get(targetServer.URL)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			
			// Copy headers
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			
			// Copy body
			body, _ := io.ReadAll(resp.Body)
			w.Write(body)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	return mps
}

func (mps *MockProxyServer) getRequestCount() int {
	mps.mutex.Lock()
	defer mps.mutex.Unlock()
	return mps.requestCount
}

func (mps *MockProxyServer) setShouldFail(fail bool) {
	mps.mutex.Lock()
	defer mps.mutex.Unlock()
	mps.shouldFail = fail
}

func (mps *MockProxyServer) close() {
	mps.server.Close()
}

// TestHealthCheckerBasicFunctionality tests basic health checker operations
func TestHealthCheckerBasicFunctionality(t *testing.T) {
	t.Run("CreateAndStartHealthChecker", func(t *testing.T) {
		// Create mock IP resolver
		ipServer := createMockIPResolverServer("1.2.3.4", 200, 0)
		defer ipServer.Close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9090", Enabled: true, Weight: 1},
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
				IntervalSeconds:  1, // 1 second for fast testing
				TimeoutSeconds:   5,
				FailureThreshold: 2,
				RecoveryThreshold: 1,
				Endpoints:        []string{ipServer.URL},
				EndpointRotation: false,
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Verify health checker was created and started
		if ps.healthChecker == nil {
			t.Fatal("Health checker should be created when enabled")
		}

		// Verify it's running
		ps.healthChecker.mutex.RLock()
		running := ps.healthChecker.running
		ps.healthChecker.mutex.RUnlock()

		if !running {
			t.Error("Health checker should be running")
		}
	})

	t.Run("StopHealthChecker", func(t *testing.T) {
		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled:         true,
				IntervalSeconds: 1,
				Endpoints:       []string{"https://httpbin.org/ip"},
			},
		}

		ps := NewProxyServer(config, "")

		// Start then stop
		ps.stopHealthChecker()

		if ps.healthChecker != nil {
			t.Error("Health checker should be nil after stopping")
		}
	})
}

// TestEndpointRotation tests the endpoint rotation functionality
func TestEndpointRotation(t *testing.T) {
	t.Run("RotationEnabled", func(t *testing.T) {
		// Create multiple mock servers
		server1 := createMockIPResolverServer("1.1.1.1", 200, 0)
		server2 := createMockIPResolverServer("2.2.2.2", 200, 0)
		server3 := createMockIPResolverServer("3.3.3.3", 200, 0)
		defer server1.Close()
		defer server2.Close()
		defer server3.Close()

		config := &Config{
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
				EndpointRotation: true,
				Endpoints:        []string{server1.URL, server2.URL, server3.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		// Get next endpoint multiple times and verify rotation
		endpoints := make([]string, 6)
		for i := 0; i < 6; i++ {
			endpoints[i] = hc.getNextEndpoint(config)
		}

		// Verify rotation pattern
		expectedPattern := []string{
			server2.URL, server3.URL, server1.URL, // First cycle
			server2.URL, server3.URL, server1.URL, // Second cycle
		}

		for i, expected := range expectedPattern {
			if endpoints[i] != expected {
				t.Errorf("Endpoint rotation failed at index %d: expected %s, got %s", i, expected, endpoints[i])
			}
		}
	})

	t.Run("RotationDisabled", func(t *testing.T) {
		server1 := createMockIPResolverServer("1.1.1.1", 200, 0)
		server2 := createMockIPResolverServer("2.2.2.2", 200, 0)
		defer server1.Close()
		defer server2.Close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				EndpointRotation: false,
				Endpoints:        []string{server1.URL, server2.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		// Should always return first endpoint
		for i := 0; i < 5; i++ {
			endpoint := hc.getNextEndpoint(config)
			if endpoint != server1.URL {
				t.Errorf("When rotation disabled, should always return first endpoint, got %s", endpoint)
			}
		}
	})
}

// TestHealthCheckResults tests various health check scenarios
func TestHealthCheckResults(t *testing.T) {
	t.Run("SuccessfulHealthCheck", func(t *testing.T) {
		ipServer := createMockIPResolverServer("192.168.1.1", 200, 0)
		defer ipServer.Close()

		proxyServer := createMockProxyServer(ipServer)
		defer proxyServer.close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				TimeoutSeconds: 5,
				Endpoints:      []string{ipServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)

		if !result.Success {
			t.Errorf("Health check should succeed, got error: %v", result.Error)
		}

		if result.Latency <= 0 {
			t.Error("Latency should be positive")
		}

		if result.Endpoint != ipServer.URL {
			t.Errorf("Expected endpoint %s, got %s", ipServer.URL, result.Endpoint)
		}
	})

	t.Run("FailedHealthCheckBadStatus", func(t *testing.T) {
		ipServer := createMockIPResolverServer("", 500, 0)
		defer ipServer.Close()

		proxyServer := createMockProxyServer(ipServer)
		defer proxyServer.close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				TimeoutSeconds: 5,
				Endpoints:      []string{ipServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)

		if result.Success {
			t.Error("Health check should fail with 500 status code")
		}

		if result.Error == nil {
			t.Error("Error should be set for failed health check")
		}
	})

	t.Run("FailedHealthCheckInvalidJSON", func(t *testing.T) {
		// Create server that returns invalid JSON
		invalidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			fmt.Fprintf(w, `{"invalid": json}`) // Invalid JSON
		}))
		defer invalidServer.Close()

		proxyServer := createMockProxyServer(invalidServer)
		defer proxyServer.close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				TimeoutSeconds: 5,
				Endpoints:      []string{invalidServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)

		if result.Success {
			t.Error("Health check should fail with invalid JSON")
		}
	})

	t.Run("FailedHealthCheckNoIP", func(t *testing.T) {
		// Create server that returns valid JSON but no IP
		noIPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			fmt.Fprintf(w, `{"status": "ok"}`) // No IP field
		}))
		defer noIPServer.Close()

		proxyServer := createMockProxyServer(noIPServer)
		defer proxyServer.close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				TimeoutSeconds: 5,
				Endpoints:      []string{noIPServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)

		if result.Success {
			t.Error("Health check should fail when no valid IP is returned")
		}
	})
}

// TestHealthCheckIntegration tests integration with existing health management
func TestHealthCheckIntegration(t *testing.T) {
	t.Run("HealthCheckUpdatesUpstreamHealth", func(t *testing.T) {
		ipServer := createMockIPResolverServer("10.0.0.1", 200, 0)
		defer ipServer.Close()

		proxyServer := createMockProxyServer(ipServer)
		defer proxyServer.close()

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				{URL: proxyServer.server.URL, Enabled: true, Weight: 1},
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
				FailureThreshold: 2,
				TimeoutSeconds:   5,
				Endpoints:        []string{ipServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Initially should be healthy
		if !ps.isUpstreamHealthy(proxyServer.server.URL) {
			t.Error("Upstream should be initially healthy")
		}

		// Simulate failed health check
		proxyServer.setShouldFail(true)
		
		hc := ps.healthChecker
		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)
		hc.processHealthCheckResult(result)

		// After one failure, should still be healthy (threshold is 2)
		if !ps.isUpstreamHealthy(proxyServer.server.URL) {
			t.Error("Upstream should still be healthy after one failure")
		}

		// Simulate another failed health check
		result = hc.checkUpstreamHealth(proxyServer.server.URL, config)
		hc.processHealthCheckResult(result)

		// After two failures, should be unhealthy
		if ps.isUpstreamHealthy(proxyServer.server.URL) {
			t.Error("Upstream should be unhealthy after reaching failure threshold")
		}

		// Fix the proxy and check again
		proxyServer.setShouldFail(false)
		result = hc.checkUpstreamHealth(proxyServer.server.URL, config)
		hc.processHealthCheckResult(result)

		// Should be healthy again after successful check
		if !ps.isUpstreamHealthy(proxyServer.server.URL) {
			t.Error("Upstream should be healthy again after successful check")
		}
	})
}

// TestHealthCheckConfiguration tests configuration handling
func TestHealthCheckConfiguration(t *testing.T) {
	t.Run("DefaultValues", func(t *testing.T) {
		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				Enabled: true,
				// Leave other fields at zero to test defaults
			},
		}

		ps := NewProxyServer(config, "")
		defer ps.stopHealthChecker()

		// Check that defaults were set
		if len(config.HealthCheck.Endpoints) == 0 {
			t.Error("Default endpoints should be set")
		}

		if config.HealthCheck.FailureThreshold == 0 {
			t.Error("Default failure threshold should be set")
		}

		if config.HealthCheck.RecoveryThreshold == 0 {
			t.Error("Default recovery threshold should be set")
		}

		if config.HealthCheck.TimeoutSeconds == 0 {
			t.Error("Default timeout should be set")
		}
	})

	t.Run("DisabledHealthCheck", func(t *testing.T) {
		config := &Config{
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

		// Health checker should not be created
		if ps.healthChecker != nil {
			t.Error("Health checker should not be created when disabled")
		}
	})
}

// TestHealthCheckTimeout tests timeout handling
func TestHealthCheckTimeout(t *testing.T) {
	t.Run("TimeoutHandling", func(t *testing.T) {
		// Create slow server that takes longer than timeout
		slowServer := createMockIPResolverServer("1.1.1.1", 200, 2*time.Second)
		defer slowServer.Close()

		proxyServer := createMockProxyServer(slowServer)
		defer proxyServer.close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
			}{
				TimeoutSeconds: 1, // 1 second timeout
				Endpoints:      []string{slowServer.URL},
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		start := time.Now()
		result := hc.checkUpstreamHealth(proxyServer.server.URL, config)
		elapsed := time.Since(start)

		// Should fail due to timeout
		if result.Success {
			t.Error("Health check should fail due to timeout")
		}

		// Should not take much longer than timeout
		if elapsed > 3*time.Second {
			t.Errorf("Health check took too long: %v", elapsed)
		}
	})
}