package main

import (
	"fmt"
	"math/rand"
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
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
				Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9047", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9048", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// Force multiple GC cycles to get a stable baseline
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	// Measure initial memory usage
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// More controlled load test parameters
	numGoroutines := 10             // Further reduced for more stable memory usage
	operationsPerGoroutine := 10000 // Increased operations per goroutine
	failureRate := 0.1              // 10% failure rate

	var wg sync.WaitGroup
	var totalOps int64

	// Buffered channel for failure logging to prevent goroutine blocking
	failureLogCh := make(chan struct{}, numGoroutines)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case <-failureLogCh:
				// Process one failure log per tick
			default:
				// Skip if no failures to log
			}
		}
	}()

	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				upstream := ps.getNextUpstream()

				// Simulate realistic success/failure pattern
				if rand.Float64() < failureRate {
					ps.recordUpstreamFailure("http://127.0.0.1:9048")
					// Try to log failure, but don't block if channel is full
					select {
					case failureLogCh <- struct{}{}:
					default:
					}
				} else {
					ps.recordUpstreamSuccess("http://127.0.0.1:9047")
				}

				_ = ps.isUpstreamHealthy(upstream)
				atomic.AddInt64(&totalOps, 1)

				// Small delay to prevent overwhelming the system
				if j%100 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Log progress periodically
	progressTicker := time.NewTicker(2 * time.Second)
	defer progressTicker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		t.Log("Test completed normally")
	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out after 30 seconds")
	}

	// Force GC and wait for memory to stabilize
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	// Measure final memory usage
	runtime.ReadMemStats(&m2)

	duration := time.Since(startTime)
	opsPerSecond := float64(totalOps) / duration.Seconds()

	// Calculate memory metrics
	var memoryChange int64
	if m2.Alloc > m1.Alloc {
		memoryChange = int64(m2.Alloc - m1.Alloc)
	} else {
		memoryChange = -int64(m1.Alloc - m2.Alloc)
	}

	opsPerformed := atomic.LoadInt64(&totalOps)

	t.Logf("Test duration: %v", duration)
	t.Logf("Operations per second: %.2f", opsPerSecond)
	t.Logf("Memory usage: initial=%d bytes, final=%d bytes, change=%+d bytes",
		m1.Alloc, m2.Alloc, memoryChange)
	t.Logf("Operations performed: %d", opsPerformed)

	// Memory increase should be reasonable (less than 5MB for this test)
	maxMemoryIncrease := int64(5 * 1024 * 1024) // 5MB
	if memoryChange > maxMemoryIncrease {
		t.Errorf("Memory increase too high: %+d bytes (max: %d bytes)", memoryChange, maxMemoryIncrease)
	}

	// Verify we performed the expected number of operations
	expectedOps := int64(numGoroutines * operationsPerGoroutine)
	if opsPerformed != expectedOps {
		t.Errorf("Expected %d operations, got %d", expectedOps, opsPerformed)
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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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

	// Create a rate limiter for health state logging
	logTicker := time.NewTicker(100 * time.Millisecond)
	defer logTicker.Stop()

	// Channel for health state changes
	healthStateChanges := make(chan string, 100)
	go func() {
		lastLogTime := make(map[string]time.Time)
		minLogInterval := time.Second // Minimum time between logs for the same upstream

		for upstream := range healthStateChanges {
			now := time.Now()
			if lastLog, exists := lastLogTime[upstream]; !exists || now.Sub(lastLog) >= minLogInterval {
				select {
				case <-logTicker.C:
					t.Logf("Health state change for %s", upstream)
					lastLogTime[upstream] = now
				default:
					// Skip logging if too frequent
				}
			}
		}
	}()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			operations := 0

			for time.Now().Before(stopTime) {
				// Varied operations
				_ = ps.getNextUpstream()
				operations++

				// Periodic health events with reduced logging
				if operations%1000 == 0 { // Reduced frequency
					ps.recordUpstreamSuccess("http://127.0.0.1:9049")
					select {
					case healthStateChanges <- "http://127.0.0.1:9049":
					default:
					}
				}
				if operations%1500 == 0 { // Reduced frequency
					ps.recordUpstreamFailure("http://127.0.0.1:9050")
					select {
					case healthStateChanges <- "http://127.0.0.1:9050":
					default:
					}
				}

				// Small delay to avoid busy loop
				time.Sleep(time.Microsecond * 100)
			}

			atomic.AddInt64(&totalOperations, int64(operations))
		}(i)
	}

	// Log progress periodically
	progressTicker := time.NewTicker(time.Second)
	defer progressTicker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		t.Log("Long-running test completed normally")
	case <-time.After(30 * time.Second):
		t.Fatal("Long-running test timed out after 30 seconds")
	}

	close(healthStateChanges)

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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
		}{
			{URL: "http://127.0.0.1:9052", Enabled: true, Weight: 1},
			{URL: "http://127.0.0.1:9053", Enabled: true, Weight: 1},
		},
	}

	ps := NewProxyServer(config, "")

	// High contention scenario with reduced noise
	numGoroutines := 100 // Reduced from 1000 to reduce noise
	operationsPerGoroutine := 100
	logThrottle := time.NewTicker(100 * time.Millisecond)
	defer logThrottle.Stop()

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)
	var totalOps int64

	// Create a buffered channel for health state changes to avoid blocking
	healthStateChanges := make(chan struct{}, 100)
	go func() {
		for range healthStateChanges {
			select {
			case <-logThrottle.C:
				// Only log health state changes periodically
				t.Log("Health state changes occurring...")
			default:
				// Skip logging if too frequent
			}
		}
	}()

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
				if j%2 == 0 {
					ps.recordUpstreamSuccess(upstream)
					select {
					case healthStateChanges <- struct{}{}:
					default:
					}
				} else {
					ps.recordUpstreamFailure(upstream)
					select {
					case healthStateChanges <- struct{}{}:
					default:
					}
				}

				_ = ps.isUpstreamHealthy(upstream)
				_ = ps.getUpstreamFailureCount(upstream)
				atomic.AddInt64(&totalOps, 1)
			}
		}(i)
	}

	// Log progress periodically
	progressTicker := time.NewTicker(time.Second)
	defer progressTicker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		t.Log("Race condition test completed normally")
	case <-time.After(30 * time.Second):
		t.Fatal("Race condition test timed out after 30 seconds")
	}

	close(errors)
	close(healthStateChanges)

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

	opsPerformed := atomic.LoadInt64(&totalOps)
	t.Logf("Completed %d operations", opsPerformed)
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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
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
