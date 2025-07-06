package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHealthCheckScalability tests health check performance with many upstreams
func TestHealthCheckScalability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scalability test in short mode")
	}

	t.Run("ConcurrentHealthChecks", func(t *testing.T) {
		// Create a mock IP server
		requestCount := int64(0)
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&requestCount, 1)
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.123"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		// Create many upstream proxies (simulate large deployment)
		numUpstreams := 100
		var upstreamProxies []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			Note    string `json:"note,omitempty"`
		}

		// Create mock proxy servers
		var mockProxies []*httptest.Server
		for i := 0; i < numUpstreams; i++ {
			mockProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Forward to IP server
				resp, err := http.Get(ipServer.URL)
				if err != nil {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer resp.Body.Close()
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				body, _ := io.ReadAll(resp.Body)
				w.Write(body)
			}))
			mockProxies = append(mockProxies, mockProxy)
			
			upstreamProxies = append(upstreamProxies, struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				URL:     mockProxy.URL,
				Enabled: true,
				Weight:  1,
				Tag:     fmt.Sprintf("region-%d", i%5),
				Note:    fmt.Sprintf("Mock proxy %d", i),
			})
		}
		defer func() {
			for _, proxy := range mockProxies {
				proxy.Close()
			}
		}()

		config := &Config{
			UpstreamProxies: upstreamProxies,
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
				MaxConcurrency   int      `json:"max_concurrency,omitempty"`
				StaggerDelay     int      `json:"stagger_delay_ms,omitempty"`
			}{
				Enabled:          false, // Disable automatic health checker
				IntervalSeconds:  60, // 1 minute for this test
				TimeoutSeconds:   5,
				FailureThreshold: 3,
				RecoveryThreshold: 1,
				Endpoints:        []string{ipServer.URL},
				EndpointRotation: false,
				MaxConcurrency:   20, // Test concurrency control
				StaggerDelay:     5,  // 5ms stagger
			},
		}

		ps := NewProxyServer(config, "")

		// Record start time and initial request count
		startTime := time.Now()
		initialRequests := atomic.LoadInt64(&requestCount)

		// Set up config properly for manual health checker
		config.HealthCheck.Enabled = true
		
		// Create health checker manually for testing
		hc := NewHealthChecker(ps)
		hc.performHealthChecks()

		// Measure completion time
		elapsed := time.Since(startTime)
		finalRequests := atomic.LoadInt64(&requestCount)
		
		// Verify performance expectations
		t.Logf("Health check completed for %d upstreams in %v", numUpstreams, elapsed)
		t.Logf("Requests made: %d", finalRequests-initialRequests)

		// Should complete much faster than sequential execution would take
		maxExpectedTime := time.Duration(numUpstreams/config.HealthCheck.MaxConcurrency+1) * 
			time.Duration(config.HealthCheck.TimeoutSeconds) * time.Second
		if elapsed > maxExpectedTime {
			t.Errorf("Health checks took too long: %v (expected < %v)", elapsed, maxExpectedTime)
		}

		// Should have made exactly numUpstreams requests
		requestsMade := finalRequests - initialRequests
		if requestsMade != int64(numUpstreams) {
			t.Errorf("Expected %d health check requests, got %d", numUpstreams, requestsMade)
		}

		// Verify all upstreams are healthy
		healthyCount := 0
		for _, proxy := range mockProxies {
			if ps.isUpstreamHealthy(proxy.URL) {
				healthyCount++
			}
		}
		
		if healthyCount != numUpstreams {
			t.Errorf("Expected %d healthy upstreams, got %d", numUpstreams, healthyCount)
		}
	})

	t.Run("ConcurrencyLimiting", func(t *testing.T) {
		// Test that concurrency is properly limited
		maxConcurrency := 5
		activeConnections := int64(0)
		maxObservedConcurrency := int64(0)
		
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := atomic.AddInt64(&activeConnections, 1)
			defer atomic.AddInt64(&activeConnections, -1)
			
			// Track maximum observed concurrency
			for {
				observed := atomic.LoadInt64(&maxObservedConcurrency)
				if current <= observed || atomic.CompareAndSwapInt64(&maxObservedConcurrency, observed, current) {
					break
				}
			}
			
			// Simulate some processing time
			time.Sleep(100 * time.Millisecond)
			
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.156"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		// Create 20 upstream proxies
		numUpstreams := 20
		var upstreamProxies []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			Note    string `json:"note,omitempty"`
		}

		var mockProxies []*httptest.Server
		for i := 0; i < numUpstreams; i++ {
			mockProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp, err := http.Get(ipServer.URL)
				if err != nil {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer resp.Body.Close()
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				body, _ := io.ReadAll(resp.Body)
				w.Write(body)
			}))
			mockProxies = append(mockProxies, mockProxy)
			
			upstreamProxies = append(upstreamProxies, struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				URL:     mockProxy.URL,
				Enabled: true,
				Weight:  1,
			})
		}
		defer func() {
			for _, proxy := range mockProxies {
				proxy.Close()
			}
		}()

		config := &Config{
			UpstreamProxies: upstreamProxies,
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
				MaxConcurrency   int      `json:"max_concurrency,omitempty"`
				StaggerDelay     int      `json:"stagger_delay_ms,omitempty"`
			}{
				Enabled:        false, // Disable automatic health checker
				TimeoutSeconds: 5,
				Endpoints:      []string{ipServer.URL},
				MaxConcurrency: maxConcurrency,
			},
		}

		ps := NewProxyServer(config, "")
		
		// Set up config properly for manual health checker
		config.HealthCheck.Enabled = true
		
		// Create health checker manually for testing
		hc := NewHealthChecker(ps)
		hc.performHealthChecks()

		observedMax := atomic.LoadInt64(&maxObservedConcurrency)
		t.Logf("Maximum observed concurrency: %d (limit: %d)", observedMax, maxConcurrency)

		// Allow some tolerance for timing issues, but should be close to limit
		if observedMax > int64(maxConcurrency+2) {
			t.Errorf("Concurrency limit exceeded: observed %d, limit %d", observedMax, maxConcurrency)
		}

		if observedMax < int64(maxConcurrency-1) {
			t.Errorf("Concurrency too low: observed %d, limit %d (may indicate inefficient parallelization)", observedMax, maxConcurrency)
		}
	})

	t.Run("StaggeredExecution", func(t *testing.T) {
		// Test that staggered execution spreads load over time
		var requestTimes []time.Time
		var timeMutex sync.Mutex
		
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			timeMutex.Lock()
			requestTimes = append(requestTimes, time.Now())
			timeMutex.Unlock()
			
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.189"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		// Create 10 upstream proxies
		numUpstreams := 10
		var upstreamProxies []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			Note    string `json:"note,omitempty"`
		}

		var mockProxies []*httptest.Server
		for i := 0; i < numUpstreams; i++ {
			mockProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp, err := http.Get(ipServer.URL)
				if err != nil {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer resp.Body.Close()
				
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				body, _ := io.ReadAll(resp.Body)
				w.Write(body)
			}))
			mockProxies = append(mockProxies, mockProxy)
			
			upstreamProxies = append(upstreamProxies, struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
				Note    string `json:"note,omitempty"`
			}{
				URL:     mockProxy.URL,
				Enabled: true,
				Weight:  1,
			})
		}
		defer func() {
			for _, proxy := range mockProxies {
				proxy.Close()
			}
		}()

		config := &Config{
			UpstreamProxies: upstreamProxies,
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
				MaxConcurrency   int      `json:"max_concurrency,omitempty"`
				StaggerDelay     int      `json:"stagger_delay_ms,omitempty"`
			}{
				Enabled:        false, // Disable automatic health checker
				TimeoutSeconds: 2,
				Endpoints:      []string{ipServer.URL},
				MaxConcurrency: 20, // High concurrency
				StaggerDelay:   50, // 50ms stagger delay
			},
		}

		ps := NewProxyServer(config, "")
		
		// Set up config properly for manual health checker
		config.HealthCheck.Enabled = true
		
		// Create health checker manually for testing
		hc := NewHealthChecker(ps)
		hc.performHealthChecks()

		// Analyze request timing
		timeMutex.Lock()
		times := make([]time.Time, len(requestTimes))
		copy(times, requestTimes)
		timeMutex.Unlock()

		if len(times) != numUpstreams {
			t.Errorf("Expected %d requests, got %d", numUpstreams, len(times))
			return
		}

		// Verify that requests are staggered (not all simultaneous)
		var delays []time.Duration
		for i := 1; i < len(times); i++ {
			delay := times[i].Sub(times[0])
			delays = append(delays, delay)
		}

		// At least some requests should be delayed due to staggering
		delayedCount := 0
		for _, delay := range delays {
			if delay > 20*time.Millisecond { // Allow some tolerance
				delayedCount++
			}
		}

		if delayedCount < numUpstreams/2 {
			t.Errorf("Staggering not working properly: only %d/%d requests were delayed", delayedCount, numUpstreams)
		}

		t.Logf("Request timing spread: %v to %v (total span: %v)", 
			times[0].Sub(times[0]), times[len(times)-1].Sub(times[0]), times[len(times)-1].Sub(times[0]))
	})
}

// TestHealthCheckResourceUsage tests resource efficiency
func TestHealthCheckResourceUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resource usage test in short mode")
	}

	t.Run("ConnectionPooling", func(t *testing.T) {
		// Test that HTTP clients are properly pooled and reused
		ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			response := IPResponse{IP: "203.0.113.100"}
			json.NewEncoder(w).Encode(response)
		}))
		defer ipServer.Close()

		config := &Config{
			HealthCheck: struct {
				Enabled          bool     `json:"enabled"`
				IntervalSeconds  int      `json:"interval_seconds"`
				TimeoutSeconds   int      `json:"timeout_seconds"`
				FailureThreshold int      `json:"failure_threshold"`
				RecoveryThreshold int     `json:"recovery_threshold"`
				Endpoints        []string `json:"endpoints"`
				EndpointRotation bool     `json:"endpoint_rotation"`
				MaxConcurrency   int      `json:"max_concurrency,omitempty"`
				StaggerDelay     int      `json:"stagger_delay_ms,omitempty"`
			}{
				Enabled:        true,
				TimeoutSeconds: 5,
				Endpoints:      []string{ipServer.URL},
				MaxConcurrency: 10,
			},
		}

		ps := NewProxyServer(config, "")
		hc := NewHealthChecker(ps)

		// Test getting and returning clients from pool
		client1 := hc.getPooledClient("http://example.com:8080", config)
		if client1 == nil {
			t.Fatal("Should be able to get client from pool")
		}

		client2 := hc.getPooledClient("http://example.com:8080", config)
		if client2 == nil {
			t.Fatal("Should be able to get second client from pool")
		}

		// Return clients to pool
		hc.returnPooledClient(client1)
		hc.returnPooledClient(client2)

		// Get client again - should reuse from pool
		client3 := hc.getPooledClient("http://example.com:8080", config)
		if client3 == nil {
			t.Fatal("Should be able to reuse client from pool")
		}

		hc.returnPooledClient(client3)
		t.Logf("Connection pooling test passed")
	})
}