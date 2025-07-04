package main

import (
	"sync"
	"testing"
)

// TestWeightedRoundRobin tests weight-based load balancing
func TestWeightedRoundRobin(t *testing.T) {
	t.Run("BasicWeightDistribution", func(t *testing.T) {
		config := &Config{
			Server: struct {
				Name          string `json:"name"`
				ListenAddress string `json:"listen_address"`
				StatsEndpoint string `json:"stats_endpoint"`
			}{
				Name:          "Test Proxy",
				ListenAddress: "127.0.0.1:3140",
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
			}{
				{URL: "http://127.0.0.1:9001", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9002", Enabled: true, Weight: 2},
				{URL: "http://127.0.0.1:9003", Enabled: true, Weight: 3},
			},
		}

		ps := NewProxyServer(config, "")

		// Test weight distribution over many requests
		// Expected ratio: 1:2:3 (total weight: 6)
		// Over 600 requests: proxy1=100, proxy2=200, proxy3=300
		requestCount := 600
		upstreamCounts := make(map[string]int)
		
		for i := 0; i < requestCount; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// Verify we have exactly 3 upstreams
		if len(upstreamCounts) != 3 {
			t.Errorf("Expected 3 upstreams, got %d", len(upstreamCounts))
		}

		// Check weight distribution (allowing 5% tolerance)
		expectedCounts := map[string]int{
			"http://127.0.0.1:9001": 100, // weight 1/6 = 16.67%
			"http://127.0.0.1:9002": 200, // weight 2/6 = 33.33%
			"http://127.0.0.1:9003": 300, // weight 3/6 = 50.00%
		}

		tolerance := 0.05 // 5% tolerance
		for upstream, expected := range expectedCounts {
			actual := upstreamCounts[upstream]
			minExpected := int(float64(expected) * (1.0 - tolerance))
			maxExpected := int(float64(expected) * (1.0 + tolerance))
			
			if actual < minExpected || actual > maxExpected {
				t.Errorf("Upstream %s: expected %d±%d%%, got %d", 
					upstream, expected, int(tolerance*100), actual)
			}
		}
	})

	t.Run("SingleWeightUpstream", func(t *testing.T) {
		config := &Config{
			Server: struct {
				Name          string `json:"name"`
				ListenAddress string `json:"listen_address"`
				StatsEndpoint string `json:"stats_endpoint"`
			}{
				Name:          "Test Proxy",
				ListenAddress: "127.0.0.1:3141",
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
			}{
				{URL: "http://127.0.0.1:9004", Enabled: true, Weight: 5},
			},
		}

		ps := NewProxyServer(config, "")

		// With single upstream, should always return same upstream
		for i := 0; i < 10; i++ {
			upstream := ps.getNextUpstream()
			if upstream != "http://127.0.0.1:9004" {
				t.Errorf("Expected http://127.0.0.1:9004, got %s", upstream)
			}
		}
	})

	t.Run("ZeroWeightHandling", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9005", Enabled: true, Weight: 0}, // Zero weight
				{URL: "http://127.0.0.1:9006", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9007", Enabled: true, Weight: 2},
			},
		}

		ps := NewProxyServer(config, "")

		// Zero weight upstreams should never be selected
		upstreamCounts := make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// Zero weight upstream should not be selected
		if count, exists := upstreamCounts["http://127.0.0.1:9005"]; exists && count > 0 {
			t.Errorf("Zero weight upstream was selected %d times", count)
		}

		// Other upstreams should be selected in 1:2 ratio
		count1 := upstreamCounts["http://127.0.0.1:9006"]
		count2 := upstreamCounts["http://127.0.0.1:9007"]
		
		if count1 == 0 || count2 == 0 {
			t.Error("Non-zero weight upstreams should be selected")
		}

		// Rough ratio check (weight 1:2)
		ratio := float64(count2) / float64(count1)
		if ratio < 1.5 || ratio > 2.5 {
			t.Errorf("Expected ratio ~2.0, got %.2f (counts: %d:%d)", ratio, count1, count2)
		}
	})
}

// TestDisabledUpstreamHandling tests handling of disabled upstreams
func TestDisabledUpstreamHandling(t *testing.T) {
	t.Run("SkipDisabledUpstreams", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9008", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9009", Enabled: false, Weight: 1}, // Disabled
				{URL: "http://127.0.0.1:9010", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Disabled upstreams should never be selected
		upstreamCounts := make(map[string]int)
		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++
		}

		// Disabled upstream should not be selected
		if count, exists := upstreamCounts["http://127.0.0.1:9009"]; exists && count > 0 {
			t.Errorf("Disabled upstream was selected %d times", count)
		}

		// Only enabled upstreams should be selected
		enabledCount := upstreamCounts["http://127.0.0.1:9008"] + upstreamCounts["http://127.0.0.1:9010"]
		if enabledCount != 100 {
			t.Errorf("Expected 100 selections from enabled upstreams, got %d", enabledCount)
		}
	})

	t.Run("AllUpstreamsDisabled", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
			}{
				{URL: "http://127.0.0.1:9011", Enabled: false, Weight: 1},
				{URL: "http://127.0.0.1:9012", Enabled: false, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		// Should handle gracefully when all upstreams are disabled
		upstream := ps.getNextUpstream()
		
		// Should either return empty string or fallback behavior
		// Implementation detail to be determined during TDD
		if upstream != "" {
			t.Logf("With all upstreams disabled, got: %s", upstream)
		}
	})
}

// TestConcurrentWeightedLoadBalancing tests weighted load balancing under concurrent access
func TestConcurrentWeightedLoadBalancing(t *testing.T) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		}{
			{URL: "http://127.0.0.1:9013", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9014", Enabled: true, Weight: 3},
			{URL: "http://127.0.0.1:9015", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Concurrent upstream selection
	var wg sync.WaitGroup
	numGoroutines := 10
	requestsPerGoroutine := 100
	
	upstreamCounts := make(map[string]int)
	var mutex sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localCounts := make(map[string]int)
			
			for j := 0; j < requestsPerGoroutine; j++ {
				upstream := ps.getNextUpstream()
				localCounts[upstream]++
			}
			
			mutex.Lock()
			for upstream, count := range localCounts {
				upstreamCounts[upstream] += count
			}
			mutex.Unlock()
		}()
	}

	wg.Wait()

	totalRequests := numGoroutines * requestsPerGoroutine
	
	// Verify weight distribution with concurrent access
	// Expected ratio: 1:3:1 (total weight: 5)
	expectedCounts := map[string]int{
		"http://127.0.0.1:9013": totalRequests / 5,     // 20%
		"http://127.0.0.1:9014": totalRequests * 3 / 5, // 60%
		"http://127.0.0.1:9015": totalRequests / 5,     // 20%
	}

	tolerance := 0.15 // 15% tolerance for concurrent access
	for upstream, expected := range expectedCounts {
		actual := upstreamCounts[upstream]
		minExpected := int(float64(expected) * (1.0 - tolerance))
		maxExpected := int(float64(expected) * (1.0 + tolerance))
		
		if actual < minExpected || actual > maxExpected {
			t.Errorf("Concurrent test - Upstream %s: expected %d±%d%%, got %d", 
				upstream, expected, int(tolerance*100), actual)
		}
	}
}

// TestDynamicWeightChanges tests behavior when weights change during runtime
func TestDynamicWeightChanges(t *testing.T) {
	t.Skip("Dynamic weight changes not yet implemented - will be added during TDD")
	
	// This test will drive implementation of runtime weight updates
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		}{
			{URL: "http://127.0.0.1:9016", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9017", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Test initial distribution
	upstreamCounts := make(map[string]int)
	for i := 0; i < 100; i++ {
		upstream := ps.getNextUpstream()
		upstreamCounts[upstream]++
	}

	// Verify initial 1:1 distribution
	count1 := upstreamCounts["http://127.0.0.1:9016"]
	count2 := upstreamCounts["http://127.0.0.1:9017"]
	
	if abs(count1-count2) > 10 {
		t.Errorf("Initial distribution should be ~1:1, got %d:%d", count1, count2)
	}

	// TODO: Implement weight update functionality
	// ps.UpdateUpstreamWeight("http://127.0.0.1:9017", 3)

	// Test new distribution after weight change
	upstreamCounts = make(map[string]int)
	for i := 0; i < 100; i++ {
		upstream := ps.getNextUpstream()
		upstreamCounts[upstream]++
	}

	// Should now be 1:3 distribution
	newCount1 := upstreamCounts["http://127.0.0.1:9016"]
	newCount2 := upstreamCounts["http://127.0.0.1:9017"]
	
	// expectedRatio := 3.0
	actualRatio := float64(newCount2) / float64(newCount1)
	
	if actualRatio < 2.0 || actualRatio > 4.0 {
		t.Errorf("After weight change, expected ratio ~3.0, got %.2f", actualRatio)
	}
}

// Helper function for absolute value
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}