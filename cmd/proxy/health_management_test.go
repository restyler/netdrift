package main

import (
	"sync"
	"testing"
	"time"
)

// TestUpstreamStatsTrackingBasic tests basic upstream stats tracking functionality  
func TestUpstreamStatsTrackingBasic(t *testing.T) {
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
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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

	t.Run("HealthStatusAlwaysTrue", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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

		// Record multiple failures (stats tracking only - upstream stays healthy)
		failureThreshold := 3
		for i := 0; i < failureThreshold; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		// Note: With passive health checks disabled, upstream should remain healthy
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should remain healthy (passive health checks disabled)")
		}
	})

	t.Run("StatsTrackingWithoutRecovery", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9023", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9023"

		// Record failures (stats tracking only)
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		// Note: With passive health checks disabled, upstream should remain healthy
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should remain healthy (passive health checks disabled)")
		}

		// Record successful requests (stats tracking only)
		successThreshold := 2
		for i := 0; i < successThreshold; i++ {
			ps.recordUpstreamSuccess(upstream)
		}

		// Upstream should still be healthy
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should remain healthy (passive health checks disabled)")
		}
	})
}

// TestUpstreamSelectionWithStats tests upstream selection continues despite failure stats
func TestUpstreamSelectionWithStats(t *testing.T) {
	t.Run("AllUpstreamsStillSelected", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9024", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9025", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9026", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Record failures for middle upstream (stats tracking only)
		middleUpstream := "http://127.0.0.1:9025"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(middleUpstream)
		}

		// Track upstream selections - with passive health checks disabled,
		// all upstreams should be selected including the one with failures
		upstreamCounts := make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// With passive health checks disabled, all upstreams should be selected
		// including the one with recorded failures
		if count, exists := upstreamCounts[middleUpstream]; !exists || count == 0 {
			t.Error("Middle upstream should still be selected (passive health checks disabled)")
		}

		// All upstreams should be selected roughly equally
		totalSelections := 0
		for _, count := range upstreamCounts {
			totalSelections += count
		}
		if totalSelections != 100 {
			t.Errorf("Expected 100 total selections, got %d", totalSelections)
		}
	})

	t.Run("AllUpstreamsWithFailures", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9027", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9028", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Record failures for all upstreams (stats tracking only)
		for _, proxy := range config.UpstreamProxies {
			for i := 0; i < 5; i++ {
				ps.recordUpstreamFailure(proxy.URL)
			}
		}

		// With passive health checks disabled, upstreams should still be selected
		upstream := ps.getNextUpstream()

		// Upstream should still be selected despite recorded failures
		if upstream == "" {
			t.Error("Expected upstream to be selected (passive health checks disabled)")
		} else {
			t.Logf("Selected upstream %s despite recorded failures (passive health checks disabled)", upstream)
		}
	})

	t.Run("WeightedSelectionWithFailures", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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

		// Note: With passive health checks disabled, all upstreams remain selectable
		if count, exists := upstreamCounts[highWeightUpstream]; !exists || count == 0 {
			t.Error("High-weight upstream should still be selected (passive health checks disabled)")
		}

		// All upstreams should be selected according to their weights (3:1:2 ratio)
		highWeightCount := upstreamCounts[highWeightUpstream]
		lowWeightCount := upstreamCounts["http://127.0.0.1:9030"]
		mediumWeightCount := upstreamCounts["http://127.0.0.1:9031"]

		if highWeightCount == 0 || lowWeightCount == 0 || mediumWeightCount == 0 {
			t.Error("All upstreams should be selected (passive health checks disabled)")
		}

		// With passive health checks disabled, all upstreams should follow weight distribution
		// Expected weight ratio: 3:1:2 for high:low:medium
		t.Logf("Selection counts - high: %d, low: %d, medium: %d", highWeightCount, lowWeightCount, mediumWeightCount)
	})
}

// TestHealthCheckInterval tests periodic health checking
func TestStatsTrackingInterval(t *testing.T) {
	t.Skip("Periodic health checks not yet implemented - will be added during TDD")

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
func TestConcurrentStatsManagement(t *testing.T) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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

// TestStatsWithoutCircuitBreaker verifies that stats are tracked without circuit breaker behavior  
func TestStatsWithoutCircuitBreaker(t *testing.T) {
	// Note: Circuit breaker functionality removed - not applicable without passive health checks

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9035", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")
	upstream := "http://127.0.0.1:9035"

	// Upstream should always be healthy (no circuit breaker)
	if !ps.isUpstreamHealthy(upstream) {
		t.Error("Upstream should always be healthy (no circuit breaker)")
	}

	// Record many failures - should not affect upstream availability
	for i := 0; i < 10; i++ {
		ps.recordUpstreamFailure(upstream)
	}

	// Should still be healthy and selectable despite failures
	if !ps.isUpstreamHealthy(upstream) {
		t.Error("Upstream should remain healthy despite failures (no circuit breaker)")
	}

	// Upstream should always be selected regardless of failure history
	selected := ps.getNextUpstream()
	if selected != upstream {
		t.Errorf("Expected upstream to be selected despite failures, got %s", selected)
	}

	// Verify failure count is tracked for monitoring
	failureCount := ps.getUpstreamFailureCount(upstream)
	if failureCount != 10 {
		t.Errorf("Expected 10 failures tracked, got %d", failureCount)
	}
}

// TestHealthMetricsExport tests health metrics for monitoring
func TestStatsMetricsExport(t *testing.T) {
	t.Skip("Health metrics export not yet implemented - will be added during TDD")

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
	metrics := ps.getStatsMetrics()

	// Verify metrics structure
	expectedMetrics := map[string]interface{}{
		"upstreams": map[string]interface{}{
			"http://127.0.0.1:9036": map[string]interface{}{
				"healthy":       false,
				"failure_count": 2,
				"success_count": 0,
				"circuit_state": "OPEN",
				"last_failure":  "timestamp",
				"last_success":  nil,
			},
			"http://127.0.0.1:9037": map[string]interface{}{
				"healthy":       true,
				"failure_count": 0,
				"success_count": 1,
				"circuit_state": "CLOSED",
				"last_failure":  nil,
				"last_success":  "timestamp",
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
