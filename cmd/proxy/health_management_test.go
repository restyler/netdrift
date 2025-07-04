package main

import (
	"sync"
	"testing"
	"time"
)

// TestUpstreamHealthTracking tests upstream health monitoring and failure tracking
func TestUpstreamHealthTracking(t *testing.T) {
	t.Run("FailureCountTracking", func(t *testing.T) {
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
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9020", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9021", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Simulate upstream failures
		upstream1 := "http://127.0.0.1:9020"
		upstream2 := "http://127.0.0.1:9021"

		// Record multiple failures for upstream1
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream1)
		}

		// Record single failure for upstream2
		ps.recordUpstreamFailure(upstream2)

		// Check failure counts
		failures1 := ps.getUpstreamFailureCount(upstream1)
		failures2 := ps.getUpstreamFailureCount(upstream2)

		if failures1 != 5 {
			t.Errorf("Expected 5 failures for upstream1, got %d", failures1)
		}

		if failures2 != 1 {
			t.Errorf("Expected 1 failure for upstream2, got %d", failures2)
		}
	})

	t.Run("HealthStatusTracking", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9022", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9022"

		// Initially should be healthy
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("New upstream should be healthy initially")
		}

		// After success, should remain healthy
		ps.recordUpstreamSuccess(upstream)
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be healthy after success")
		}

		// After multiple failures, should become unhealthy
		failureThreshold := 3
		for i := 0; i < failureThreshold; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be unhealthy after multiple failures")
		}
	})

	t.Run("HealthRecovery", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9023", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9023"

		// Make upstream unhealthy
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be unhealthy after failures")
		}

		// Record successful requests to recover health
		successThreshold := 2
		for i := 0; i < successThreshold; i++ {
			ps.recordUpstreamSuccess(upstream)
		}

		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should recover health after successful requests")
		}
	})
}

// TestUpstreamFailover tests automatic failover to healthy upstreams
func TestUpstreamFailover(t *testing.T) {
	t.Run("SkipUnhealthyUpstreams", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9024", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9025", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9026", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Make middle upstream unhealthy
		unhealthyUpstream := "http://127.0.0.1:9025"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(unhealthyUpstream)
		}

		// Track upstream selections
		upstreamCounts := make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// Unhealthy upstream should not be selected
		if count, exists := upstreamCounts[unhealthyUpstream]; exists && count > 0 {
			t.Errorf("Unhealthy upstream was selected %d times", count)
		}

		// Only healthy upstreams should be selected
		healthyCount := upstreamCounts["http://127.0.0.1:9024"] + upstreamCounts["http://127.0.0.1:9026"]
		if healthyCount != 100 {
			t.Errorf("Expected 100 selections from healthy upstreams, got %d", healthyCount)
		}
	})

	t.Run("AllUpstreamsUnhealthy", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9027", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9028", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Make all upstreams unhealthy
		for _, proxy := range config.UpstreamProxies {
			for i := 0; i < 5; i++ {
				ps.recordUpstreamFailure(proxy.URL)
			}
		}

		// Should either fallback to least-failed upstream or implement circuit breaker
		upstream := ps.getNextUpstream()
		
		// Implementation decision during TDD: 
		// - Return least-failed upstream?
		// - Return empty string to indicate no healthy upstreams?
		// - Implement circuit breaker with timeout recovery?
		if upstream == "" {
			t.Log("No upstream selected when all are unhealthy (circuit breaker behavior)")
		} else {
			t.Logf("Selected upstream %s when all are unhealthy (fallback behavior)", upstream)
		}
	})

	t.Run("FailoverWithWeights", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9029", Enabled: true, Weight: 3}, // High weight
				{URL: "http://127.0.0.1:9030", Enabled: true, Weight: 1}, // Low weight
				{URL: "http://127.0.0.1:9031", Enabled: true, Weight: 2}, // Medium weight
			},
		}

		ps := NewProxyServer(config, "")

		// Make the high-weight upstream unhealthy
		highWeightUpstream := "http://127.0.0.1:9029"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(highWeightUpstream)
		}

		// Track selections among remaining healthy upstreams
		upstreamCounts := make(map[string]int)
		for i := 0; i < 300; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// High weight upstream should not be selected
		if count, exists := upstreamCounts[highWeightUpstream]; exists && count > 0 {
			t.Errorf("Unhealthy high-weight upstream was selected %d times", count)
		}

		// Remaining upstreams should be selected according to their weights (1:2 ratio)
		lowWeightCount := upstreamCounts["http://127.0.0.1:9030"]
		mediumWeightCount := upstreamCounts["http://127.0.0.1:9031"]

		if lowWeightCount == 0 || mediumWeightCount == 0 {
			t.Error("Healthy upstreams should be selected")
		}

		// Check 1:2 ratio (allowing tolerance)
		ratio := float64(mediumWeightCount) / float64(lowWeightCount)
		if ratio < 1.5 || ratio > 2.5 {
			t.Errorf("Expected weight ratio ~2.0, got %.2f", ratio)
		}
	})
}

// TestHealthCheckInterval tests periodic health checking
func TestHealthCheckInterval(t *testing.T) {
	t.Skip("Periodic health checks not yet implemented - will be added during TDD")

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9032", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Start health checker with 100ms interval
	ps.startHealthChecker(100 * time.Millisecond)
	defer ps.stopHealthChecker()

	upstream := "http://127.0.0.1:9032"

	// Make upstream unhealthy
	for i := 0; i < 5; i++ {
		ps.recordUpstreamFailure(upstream)
	}

	initialHealth := ps.isUpstreamHealthy(upstream)

	// Wait for health check cycles
	time.Sleep(300 * time.Millisecond)

	// Health status might have changed based on periodic checks
	finalHealth := ps.isUpstreamHealthy(upstream)

	t.Logf("Initial health: %v, Final health: %v", initialHealth, finalHealth)

	// Implementation will determine exact behavior:
	// - Active health checks with HTTP requests?
	// - Passive health based on request success/failure?
	// - Circuit breaker with automatic recovery timer?
}

// TestConcurrentHealthManagement tests health tracking under concurrent load
func TestConcurrentHealthManagement(t *testing.T) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9033", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9034", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	var wg sync.WaitGroup
	numGoroutines := 10
	operationsPerGoroutine := 50

	upstream1 := "http://127.0.0.1:9033"
	upstream2 := "http://127.0.0.1:9034"

	// Concurrent health state changes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			for j := 0; j < operationsPerGoroutine; j++ {
				if id%2 == 0 {
					// Even goroutines record failures
					ps.recordUpstreamFailure(upstream1)
					ps.recordUpstreamSuccess(upstream2)
				} else {
					// Odd goroutines record successes
					ps.recordUpstreamSuccess(upstream1)
					ps.recordUpstreamFailure(upstream2)
				}
				
				// Also test concurrent upstream selection
				_ = ps.getNextUpstream()
				
				// Check health status
				_ = ps.isUpstreamHealthy(upstream1)
				_ = ps.isUpstreamHealthy(upstream2)
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	failures1 := ps.getUpstreamFailureCount(upstream1)
	failures2 := ps.getUpstreamFailureCount(upstream2)
	
	expectedFailures := (numGoroutines / 2) * operationsPerGoroutine
	
	if failures1 != expectedFailures {
		t.Errorf("Expected %d failures for upstream1, got %d", expectedFailures, failures1)
	}
	
	if failures2 != expectedFailures {
		t.Errorf("Expected %d failures for upstream2, got %d", expectedFailures, failures2)
	}

	// Health status should be deterministic based on failure counts
	health1 := ps.isUpstreamHealthy(upstream1)
	health2 := ps.isUpstreamHealthy(upstream2)
	
	t.Logf("Final health status - upstream1: %v, upstream2: %v", health1, health2)
	t.Logf("Final failure counts - upstream1: %d, upstream2: %d", failures1, failures2)
}

// TestCircuitBreakerBehavior tests circuit breaker pattern implementation
func TestCircuitBreakerBehavior(t *testing.T) {
	t.Skip("Circuit breaker not yet implemented - will be added during TDD")

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9035", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")
	upstream := "http://127.0.0.1:9035"

	// Test circuit breaker states: CLOSED -> OPEN -> HALF_OPEN -> CLOSED

	// 1. Start in CLOSED state (normal operation)
	state := ps.getCircuitBreakerState(upstream)
	if state != "CLOSED" {
		t.Errorf("Initial state should be CLOSED, got %s", state)
	}

	// 2. Trigger failures to open circuit
	failureThreshold := 5
	for i := 0; i < failureThreshold; i++ {
		ps.recordUpstreamFailure(upstream)
	}

	state = ps.getCircuitBreakerState(upstream)
	if state != "OPEN" {
		t.Errorf("State should be OPEN after failures, got %s", state)
	}

	// 3. In OPEN state, requests should be rejected immediately
	selected := ps.getNextUpstream()
	if selected == upstream {
		t.Error("Upstream should not be selected when circuit is OPEN")
	}

	// 4. After timeout, should transition to HALF_OPEN
	time.Sleep(1 * time.Second) // Circuit breaker timeout

	// Next request should trigger HALF_OPEN state
	ps.getNextUpstream()
	state = ps.getCircuitBreakerState(upstream)
	if state != "HALF_OPEN" {
		t.Errorf("State should be HALF_OPEN after timeout, got %s", state)
	}

	// 5. Success in HALF_OPEN should close circuit
	ps.recordUpstreamSuccess(upstream)
	state = ps.getCircuitBreakerState(upstream)
	if state != "CLOSED" {
		t.Errorf("State should be CLOSED after success in HALF_OPEN, got %s", state)
	}

	// 6. Failure in HALF_OPEN should reopen circuit
	// Reset to HALF_OPEN state and test failure
	// ... (implementation-specific test logic)
}

// TestHealthMetricsExport tests health metrics for monitoring
func TestHealthMetricsExport(t *testing.T) {
	t.Skip("Health metrics export not yet implemented - will be added during TDD")

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9036", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9037", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Generate some health events
	ps.recordUpstreamFailure("http://127.0.0.1:9036")
	ps.recordUpstreamFailure("http://127.0.0.1:9036")
	ps.recordUpstreamSuccess("http://127.0.0.1:9037")

	// Export health metrics
	metrics := ps.getHealthMetrics()

	// Verify metrics structure
	expectedMetrics := map[string]interface{}{
		"upstreams": map[string]interface{}{
			"http://127.0.0.1:9036": map[string]interface{}{
				"healthy":        false,
				"failure_count":  2,
				"success_count":  0,
				"circuit_state":  "OPEN",
				"last_failure":   "timestamp",
				"last_success":   nil,
			},
			"http://127.0.0.1:9037": map[string]interface{}{
				"healthy":        true,
				"failure_count":  0,
				"success_count":  1,
				"circuit_state":  "CLOSED",
				"last_failure":   nil,
				"last_success":   "timestamp",
			},
		},
		"total_healthy_upstreams":   1,
		"total_unhealthy_upstreams": 1,
	}

	// Validate metrics (implementation-specific assertions)
	if metrics == nil {
		t.Error("Health metrics should not be nil")
	}

	t.Logf("Health metrics: %+v", metrics)
	t.Logf("Expected structure: %+v", expectedMetrics)
}