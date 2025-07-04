package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHighConcurrencyLoadBalancing tests load balancing under high concurrent load
func TestHighConcurrencyLoadBalancing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Run("HighConcurrencyWeightedDistribution", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9040", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9041", Enabled: true, Weight: 2},
				{URL: "http://127.0.0.1:9042", Enabled: true, Weight: 3},
				{URL: "http://127.0.0.1:9043", Enabled: true, Weight: 4},
			},
		}

		ps := NewProxyServer(config, "")

		// Stress test parameters
		numGoroutines := 100
		requestsPerGoroutine := 1000
		totalRequests := numGoroutines * requestsPerGoroutine

		upstreamCounts := make(map[string]*int64)
		for _, proxy := range config.UpstreamProxies {
			count := int64(0)
			upstreamCounts[proxy.URL] = &count
		}

		var wg sync.WaitGroup
		startTime := time.Now()

		// Launch high concurrency load
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				
				for j := 0; j < requestsPerGoroutine; j++ {
					upstream := ps.getNextUpstream()
					if counter, exists := upstreamCounts[upstream]; exists {
						atomic.AddInt64(counter, 1)
					} else {
						t.Errorf("Unknown upstream returned: %s", upstream)
					}
				}
			}(i)
		}

		wg.Wait()
		duration := time.Since(startTime)

		// Performance metrics
		requestsPerSecond := float64(totalRequests) / duration.Seconds()
		t.Logf("Processed %d requests in %v (%.0f req/s)", totalRequests, duration, requestsPerSecond)

		// Verify weight distribution under high load
		// Expected ratios: 1:2:3:4 (total weight: 10)
		expectedDistribution := map[string]float64{
			"http://127.0.0.1:9040": 0.1, // 10%
			"http://127.0.0.1:9041": 0.2, // 20%
			"http://127.0.0.1:9042": 0.3, // 30%
			"http://127.0.0.1:9043": 0.4, // 40%
		}

		tolerance := 0.02 // 2% tolerance for high-load test
		actualTotal := int64(0)
		
		for upstream, counter := range upstreamCounts {
			count := atomic.LoadInt64(counter)
			actualTotal += count
			
			// expectedCount := int64(float64(totalRequests) * expectedDistribution[upstream])
			actualPercentage := float64(count) / float64(totalRequests)
			expectedPercentage := expectedDistribution[upstream]
			
			deviation := abs64(actualPercentage - expectedPercentage)
			
			if deviation > tolerance {
				t.Errorf("Upstream %s: expected %.1f%%, got %.1f%% (deviation: %.1f%%, tolerance: %.1f%%)",
					upstream, expectedPercentage*100, actualPercentage*100, deviation*100, tolerance*100)
			}
			
			t.Logf("Upstream %s: %d requests (%.1f%%)", upstream, count, actualPercentage*100)
		}

		if actualTotal != int64(totalRequests) {
			t.Errorf("Total requests mismatch: expected %d, got %d", totalRequests, actualTotal)
		}

		// Performance threshold (should handle at least 10k req/s)
		if requestsPerSecond < 10000 {
			t.Logf("Warning: Low performance %.0f req/s (expected >10k req/s)", requestsPerSecond)
		}
	})

	t.Run("ConcurrentHealthAndLoadBalancing", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			}{
				{URL: "http://127.0.0.1:9044", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9045", Enabled: true, Weight: 1},
				{URL: "http://127.0.0.1:9046", Enabled: true, Weight: 1},
			},
		}

		ps := NewProxyServer(config, "")

		numGoroutines := 50
		operationsPerGoroutine := 500
		var wg sync.WaitGroup

		// Concurrent load balancing + health management
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				
				for j := 0; j < operationsPerGoroutine; j++ {
					// Load balancing operations
					upstream := ps.getNextUpstream()
					
					// Simulate health events based on goroutine ID
					switch id % 3 {
					case 0:
						// Record failures for upstream1
						ps.recordUpstreamFailure("http://127.0.0.1:9044")
					case 1:
						// Record successes for upstream2
						ps.recordUpstreamSuccess("http://127.0.0.1:9045")
					case 2:
						// Mixed operations for upstream3
						if j%2 == 0 {
							ps.recordUpstreamSuccess("http://127.0.0.1:9046")
						} else {
							ps.recordUpstreamFailure("http://127.0.0.1:9046")
						}
					}
					
					// Health checks
					_ = ps.isUpstreamHealthy(upstream)
				}
			}(i)
		}

		wg.Wait()

		// Verify system remains stable under concurrent load
		// Check that we can still get upstreams
		for i := 0; i < 10; i++ {
			upstream := ps.getNextUpstream()
			if upstream == "" {
				t.Error("Load balancer should still return upstreams after stress test")
				break
			}
		}

		// Log final health states
		for _, proxy := range config.UpstreamProxies {
			healthy := ps.isUpstreamHealthy(proxy.URL)
			failures := ps.getUpstreamFailureCount(proxy.URL)
			t.Logf("Upstream %s: healthy=%v, failures=%d", proxy.URL, healthy, failures)
		}
	})
}

// TestMemoryUsageUnderLoad tests memory consumption during high load
func TestMemoryUsageUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9047", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9048", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Measure initial memory usage
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Generate high load with many operations
	numGoroutines := 200
	operationsPerGoroutine := 5000

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				_ = ps.getNextUpstream()
				ps.recordUpstreamSuccess("http://127.0.0.1:9047")
				ps.recordUpstreamFailure("http://127.0.0.1:9048")
				_ = ps.isUpstreamHealthy("http://127.0.0.1:9047")
			}
		}()
	}

	wg.Wait()

	// Measure final memory usage
	runtime.GC()
	runtime.ReadMemStats(&m2)

	memoryIncrease := m2.Alloc - m1.Alloc
	t.Logf("Memory usage: initial=%d bytes, final=%d bytes, increase=%d bytes",
		m1.Alloc, m2.Alloc, memoryIncrease)

	// Memory increase should be reasonable (less than 10MB for this test)
	maxMemoryIncrease := uint64(10 * 1024 * 1024) // 10MB
	if memoryIncrease > maxMemoryIncrease {
		t.Errorf("Memory increase too high: %d bytes (max: %d bytes)", memoryIncrease, maxMemoryIncrease)
	}
}

// TestLongRunningStressTest tests stability over extended periods
func TestLongRunningStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9049", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9050", Enabled: true, Weight: 2},
			{URL: "http://127.0.0.1:9051", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Run for 30 seconds with moderate concurrent load
	duration := 30 * time.Second
	numGoroutines := 20
	
	var wg sync.WaitGroup
	var totalOperations int64
	stopTime := time.Now().Add(duration)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			operations := 0
			
			for time.Now().Before(stopTime) {
				// Varied operations
				_ = ps.getNextUpstream()
				operations++
				
				// Periodic health events
				if operations%100 == 0 {
					ps.recordUpstreamSuccess("http://127.0.0.1:9049")
				}
				if operations%150 == 0 {
					ps.recordUpstreamFailure("http://127.0.0.1:9050")
				}
				
				// Small delay to avoid busy loop
				time.Sleep(time.Microsecond * 100)
			}
			
			atomic.AddInt64(&totalOperations, int64(operations))
		}(i)
	}

	wg.Wait()

	// Verify system is still functional
	upstreamCounts := make(map[string]int)
	for i := 0; i < 100; i++ {
		upstream := ps.getNextUpstream()
		upstreamCounts[upstream]++
	}

	// Should still have proper distribution
	if len(upstreamCounts) == 0 {
		t.Error("Load balancer stopped working after long-running test")
	}

	// Log performance
	opsPerSecond := float64(totalOperations) / duration.Seconds()
	t.Logf("Long-running test: %d operations in %v (%.0f ops/s)", totalOperations, duration, opsPerSecond)

	// Verify final upstream distribution
	for upstream, count := range upstreamCounts {
		t.Logf("Final distribution - %s: %d selections", upstream, count)
	}
}

// TestRaceConditionDetection tests for race conditions under high concurrency
func TestRaceConditionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9052", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9053", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// High contention scenario
	numGoroutines := 1000
	operationsPerGoroutine := 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			defer func() {
				if r := recover(); r != nil {
					errors <- fmt.Errorf("goroutine %d panicked: %v", id, r)
				}
			}()
			
			for j := 0; j < operationsPerGoroutine; j++ {
				// Mix of operations that could cause race conditions
				upstream := ps.getNextUpstream()
				if upstream == "" {
					errors <- fmt.Errorf("goroutine %d: got empty upstream at operation %d", id, j)
					return
				}
				
				// Concurrent health operations
				ps.recordUpstreamSuccess(upstream)
				ps.recordUpstreamFailure(upstream)
				_ = ps.isUpstreamHealthy(upstream)
				_ = ps.getUpstreamFailureCount(upstream)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors or panics
	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
		if errorCount > 10 { // Limit error output
			t.Log("... (more errors truncated)")
			break
		}
	}

	if errorCount > 0 {
		t.Errorf("Detected %d race condition errors", errorCount)
	}
}

// Helper function for int64 absolute value
func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}


// TestBenchmarkLoadBalancing provides benchmark tests for performance regression
func BenchmarkLoadBalancing(b *testing.B) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9060", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9061", Enabled: true, Weight: 2},
			{URL: "http://127.0.0.1:9062", Enabled: true, Weight: 3},
		},
	}

	ps := NewProxyServer(config, "")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ps.getNextUpstream()
		}
	})
}

func BenchmarkHealthTracking(b *testing.B) {
	config := &Config{
		UpstreamProxies: []struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
			Weight  int    `json:"weight"`
		}{
			{URL: "http://127.0.0.1:9063", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")
	upstream := "http://127.0.0.1:9063"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				ps.recordUpstreamSuccess(upstream)
			} else {
				ps.recordUpstreamFailure(upstream)
			}
			_ = ps.isUpstreamHealthy(upstream)
			i++
		}
	})
}