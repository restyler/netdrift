package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestUpstreamFailoverScenarios tests comprehensive failover scenarios
func TestUpstreamFailoverScenarios(t *testing.T) {
	t.Run("GradualUpstreamFailure", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9070", Enabled: true, Weight: 2},
				{URL: "http://127.0.0.1:9071", Enabled: true, Weight: 2},
				{URL: "http://127.0.0.1:9072", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Phase 1: All upstreams healthy
		upstreamCounts := make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// Should distribute according to weights (2:2:1)
		if len(upstreamCounts) != 3 {
			t.Errorf("Expected 3 upstreams initially, got %d", len(upstreamCounts))
		}

		// Phase 2: First upstream starts failing
		upstream1 := "http://127.0.0.1:9070"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream1)
		}

		// Should now exclude first upstream
		upstreamCounts = make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		if count, exists := upstreamCounts[upstream1]; exists && count > 0 {
			t.Errorf("Failed upstream %s should not be selected, got %d selections", upstream1, count)
		}

		// Phase 3: Second upstream also fails
		upstream2 := "http://127.0.0.1:9071"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream2)
		}

		// Should now only use third upstream
		upstreamCounts = make(map[string]int)
		for i := 0; i < 50; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		upstream3 := "http://127.0.0.1:9072"
		if upstreamCounts[upstream3] != 50 {
			t.Errorf("Only healthy upstream should get all traffic, got %d/50", upstreamCounts[upstream3])
		}

		// Phase 4: First upstream recovers
		for i := 0; i < 3; i++ {
			ps.recordUpstreamSuccess(upstream1)
		}

		// Should now use both first and third upstreams
		upstreamCounts = make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		if upstreamCounts[upstream1] == 0 || upstreamCounts[upstream3] == 0 {
			t.Error("Both recovered upstreams should receive traffic")
		}

		if count, exists := upstreamCounts[upstream2]; exists && count > 0 {
			t.Errorf("Still-failed upstream %s should not receive traffic", upstream2)
		}
	})

	t.Run("CascadingFailureRecovery", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9073", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9074", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9075", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9076", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		allUpstreams := []string{
			"http://127.0.0.1:9073",
			"http://127.0.0.1:9074",
			"http://127.0.0.1:9075",
			"http://127.0.0.1:9076",
		}

		// Simulate cascading failure: upstreams fail one by one
		for i, upstream := range allUpstreams[:3] { // Fail first 3
			t.Logf("Failing upstream %d: %s", i+1, upstream)
			
			for j := 0; j < 5; j++ {
				ps.recordUpstreamFailure(upstream)
			}

			// Check remaining healthy upstreams
			upstreamCounts := make(map[string]int)
			for k := 0; k < 40; k++ {
				selected := ps.getNextUpstream()
				upstreamCounts[selected]++
			}

			healthyCount := 0
			for _, upstream := range allUpstreams {
				if upstreamCounts[upstream] > 0 {
					healthyCount++
				}
			}

			expectedHealthy := 4 - (i + 1)
			if healthyCount != expectedHealthy {
				t.Errorf("After failing %d upstreams, expected %d healthy, got %d", i+1, expectedHealthy, healthyCount)
			}
		}

		// Now recover upstreams in reverse order
		for i := 2; i >= 0; i-- {
			upstream := allUpstreams[i]
			t.Logf("Recovering upstream: %s", upstream)
			
			for j := 0; j < 3; j++ {
				ps.recordUpstreamSuccess(upstream)
			}

			// Check that recovered upstream is being used
			upstreamCounts := make(map[string]int)
			for k := 0; k < 100; k++ {
				selected := ps.getNextUpstream()
				upstreamCounts[selected]++
			}

			if upstreamCounts[upstream] == 0 {
				t.Errorf("Recovered upstream %s should receive traffic", upstream)
			}
		}

		// Final check: all upstreams should be healthy and receive traffic
		finalCounts := make(map[string]int)
		for i := 0; i < 200; i++ {
			upstream := ps.getNextUpstream()
			finalCounts[upstream]++
		}

		for _, upstream := range allUpstreams {
			if finalCounts[upstream] == 0 {
				t.Errorf("Fully recovered upstream %s should receive traffic", upstream)
			}
		}
	})

	t.Run("PartialFailureLoadRedistribution", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9077", Enabled: true, Weight: 5}, // High capacity
				{URL: "http://127.0.0.1:9078", Enabled: true, Weight: 3}, // Medium capacity
				{URL: "http://127.0.0.1:9079", Enabled: true, Weight: 2}, // Low capacity
			},
		}

		ps := NewProxyServer(config, "")

		// Initial distribution check (5:3:2 ratio)
		initialCounts := make(map[string]int)
		for i := 0; i < 1000; i++ {
			upstream := ps.getNextUpstream()
			initialCounts[upstream]++
		}

		t.Logf("Initial distribution: %v", initialCounts)

		// Fail the high-capacity upstream
		highCapacity := "http://127.0.0.1:9077"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(highCapacity)
		}

		// Load should redistribute to remaining upstreams (3:2 ratio)
		redistributedCounts := make(map[string]int)
		for i := 0; i < 500; i++ {
			upstream := ps.getNextUpstream()
			redistributedCounts[upstream]++
		}

		// High capacity upstream should get no traffic
		if redistributedCounts[highCapacity] > 0 {
			t.Errorf("Failed high-capacity upstream should get no traffic, got %d", redistributedCounts[highCapacity])
		}

		// Remaining upstreams should get traffic in 3:2 ratio
		medium := "http://127.0.0.1:9078"
		low := "http://127.0.0.1:9079"
		
		if redistributedCounts[medium] == 0 || redistributedCounts[low] == 0 {
			t.Error("Remaining upstreams should both receive traffic")
		}

		ratio := float64(redistributedCounts[medium]) / float64(redistributedCounts[low])
		if ratio < 1.2 || ratio > 1.8 { // 3:2 = 1.5, allow some tolerance
			t.Errorf("Expected ratio ~1.5 (3:2), got %.2f", ratio)
		}

		t.Logf("Redistributed counts (after high-capacity failure): %v", redistributedCounts)
	})
}

// TestFailoverUnderLoad tests failover behavior during high concurrent load
func TestFailoverUnderLoad(t *testing.T) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		}{
			{URL: "http://127.0.0.1:9080", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9081", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9082", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	var wg sync.WaitGroup
	numGoroutines := 50
	requestsPerGoroutine := 200
	
	upstreamCounts := make(map[string]*int64)
	for _, proxy := range config.UpstreamProxies {
		count := int64(0)
		upstreamCounts[proxy.URL] = &count
	}

	// Start high load
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			for j := 0; j < requestsPerGoroutine; j++ {
				upstream := ps.getNextUpstream()
				if counter, exists := upstreamCounts[upstream]; exists {
					atomic.AddInt64(counter, 1)
				}
				
				// Small delay to simulate real load
				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	// Introduce failure during load
	go func() {
		time.Sleep(100 * time.Millisecond) // Let some load build up
		
		// Fail one upstream
		failingUpstream := "http://127.0.0.1:9081"
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(failingUpstream)
			time.Sleep(10 * time.Millisecond)
		}
		
		time.Sleep(200 * time.Millisecond)
		
		// Recover the upstream
		for i := 0; i < 3; i++ {
			ps.recordUpstreamSuccess(failingUpstream)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Analyze final distribution
	totalRequests := int64(numGoroutines * requestsPerGoroutine)
	actualTotal := int64(0)
	
	for upstream, counter := range upstreamCounts {
		count := atomic.LoadInt64(counter)
		actualTotal += count
		percentage := float64(count) / float64(totalRequests) * 100
		t.Logf("Upstream %s: %d requests (%.1f%%)", upstream, count, percentage)
	}

	if actualTotal != totalRequests {
		t.Errorf("Request count mismatch: expected %d, got %d", totalRequests, actualTotal)
	}

	// All upstreams should have received some traffic (since failover/recovery happened)
	for upstream, counter := range upstreamCounts {
		count := atomic.LoadInt64(counter)
		if count == 0 {
			t.Errorf("Upstream %s received no traffic during failover test", upstream)
		}
	}
}

// TestFailoverThresholds tests different failure thresholds for upstream health
func TestFailoverThresholds(t *testing.T) {
	t.Run("LowFailureThreshold", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9083", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9083"

		// Test with low failure threshold (should fail quickly)
		ps.setFailureThreshold(upstream, 2) // Only 2 failures needed

		// Initially healthy
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be healthy initially")
		}

		// One failure - should still be healthy
		ps.recordUpstreamFailure(upstream)
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should still be healthy after 1 failure")
		}

		// Second failure - should become unhealthy
		ps.recordUpstreamFailure(upstream)
		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be unhealthy after reaching threshold")
		}
	})

	t.Run("HighFailureThreshold", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9084", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9084"

		// Test with high failure threshold (more tolerant)
		ps.setFailureThreshold(upstream, 10)

		// Should remain healthy through multiple failures
		for i := 0; i < 8; i++ {
			ps.recordUpstreamFailure(upstream)
			if !ps.isUpstreamHealthy(upstream) {
				t.Errorf("Upstream should still be healthy after %d failures (threshold: 10)", i+1)
			}
		}

		// Should become unhealthy after threshold
		ps.recordUpstreamFailure(upstream) // 9th failure
		ps.recordUpstreamFailure(upstream) // 10th failure
		
		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be unhealthy after reaching high threshold")
		}
	})

	t.Run("DynamicThresholdAdjustment", func(t *testing.T) {
		t.Skip("Dynamic threshold adjustment not yet implemented - will be added during TDD")

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9085", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9085"

		// Start with default threshold
		initialThreshold := ps.getFailureThreshold(upstream)

		// Simulate adjusting threshold based on upstream performance
		ps.adjustFailureThreshold(upstream, 0.8) // 80% success rate observed

		newThreshold := ps.getFailureThreshold(upstream)
		if newThreshold == initialThreshold {
			t.Error("Threshold should adjust based on performance")
		}

		// Test threshold adjustment with high error rate
		ps.adjustFailureThreshold(upstream, 0.3) // 30% success rate

		strictThreshold := ps.getFailureThreshold(upstream)
		if strictThreshold >= newThreshold {
			t.Error("Threshold should become stricter with high error rate")
		}
	})
}

// TestFailoverRecoveryPatterns tests different recovery patterns
func TestFailoverRecoveryPatterns(t *testing.T) {
	t.Run("ImmediateRecovery", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9086", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9086"

		// Make upstream unhealthy
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should be unhealthy")
		}

		// Single success should recover immediately
		ps.recordUpstreamSuccess(upstream)
		
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should recover immediately after success")
		}
	})

	t.Run("GradualRecovery", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9087", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9087"

		// Configure gradual recovery (requires multiple successes)
		ps.setRecoveryThreshold(upstream, 3)

		// Make upstream unhealthy
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		// Should require multiple successes to recover
		ps.recordUpstreamSuccess(upstream)
		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should not recover after single success")
		}

		ps.recordUpstreamSuccess(upstream)
		if ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should not recover after two successes")
		}

		ps.recordUpstreamSuccess(upstream)
		if !ps.isUpstreamHealthy(upstream) {
			t.Error("Upstream should recover after three successes")
		}
	})

	t.Run("ExponentialBackoffRecovery", func(t *testing.T) {
		t.Skip("Exponential backoff recovery not yet implemented - will be added during TDD")

		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9088", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")
		upstream := "http://127.0.0.1:9088"

		// Enable exponential backoff recovery
		ps.enableExponentialBackoff(upstream, true)

		// Make upstream unhealthy
		for i := 0; i < 5; i++ {
			ps.recordUpstreamFailure(upstream)
		}

		// First retry should be immediate
		retryTime := ps.getNextRetryTime(upstream)
		if time.Until(retryTime) > time.Second {
			t.Error("First retry should be immediate or very soon")
		}

		// Simulate failed retry
		ps.recordUpstreamFailure(upstream)

		// Second retry should be delayed
		retryTime = ps.getNextRetryTime(upstream)
		if time.Until(retryTime) < time.Second {
			t.Error("Second retry should be delayed")
		}

		// Third retry should be delayed even more
		ps.recordUpstreamFailure(upstream)
		newRetryTime := ps.getNextRetryTime(upstream)
		
		if newRetryTime.Before(retryTime) {
			t.Error("Retry delay should increase exponentially")
		}
	})
}

