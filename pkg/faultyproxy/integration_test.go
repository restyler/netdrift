package faultyproxy

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFaultyProxy_Integration tests the faulty proxy with real HTTP requests
func TestFaultyProxy_Integration(t *testing.T) {
	proxy := NewFaultyProxy(9200)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Create HTTP client with proxy
	proxyURL, _ := url.Parse("http://127.0.0.1:9200")
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	// Test actual HTTP request through proxy
	req, err := http.NewRequest("GET", "https://httpbin.org/ip", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestFaultyProxy_ConcurrentConnections tests handling multiple concurrent connections
func TestFaultyProxy_ConcurrentConnections(t *testing.T) {
	proxy := NewFaultyProxy(9201)
	proxy.FailureRate = 0.2 // 20% failure rate
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	const numConnections = 10
	var wg sync.WaitGroup
	successCount := make(chan int, numConnections)

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, err := net.Dial("tcp", "127.0.0.1:9201")
			if err != nil {
				t.Logf("Connection %d failed to connect: %v", id, err)
				return
			}
			defer conn.Close()

			// Send CONNECT request
			connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				t.Logf("Connection %d failed to send CONNECT: %v", id, err)
				return
			}

			// Try to read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				t.Logf("Connection %d failed to read response: %v", id, err)
				return
			}

			response := string(buffer[:n])
			if strings.Contains(response, "200 Connection Established") {
				successCount <- 1
			}
		}(i)
	}

	wg.Wait()
	close(successCount)

	// Count successful connections
	var successful int
	for range successCount {
		successful++
	}

	t.Logf("Successful connections: %d out of %d", successful, numConnections)

	// With 20% failure rate, we should have some successes but not all
	if successful == 0 {
		t.Error("Expected some successful connections, but got none")
	}
	if successful == numConnections {
		t.Error("Expected some failed connections due to 20% failure rate, but all succeeded")
	}
}

// TestFaultyProxy_LoadTesting performs basic load testing
func TestFaultyProxy_LoadTesting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	proxy := NewFaultyProxy(9202)
	proxy.FailureRate = 0.1 // 10% failure rate
	proxy.FaultType = SlowResponse
	proxy.Latency = 50 * time.Millisecond

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	const numRequests = 50
	const concurrency = 5

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)
	results := make(chan bool, numRequests)

	start := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			semaphore <- struct{}{} // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			conn, err := net.Dial("tcp", "127.0.0.1:9202")
			if err != nil {
				results <- false
				return
			}
			defer conn.Close()

			// Send CONNECT request
			connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				results <- false
				return
			}

			// Try to read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := conn.Read(buffer)
			if err != nil {
				results <- false
				return
			}

			response := string(buffer[:n])
			results <- strings.Contains(response, "200 Connection Established")
		}(i)
	}

	wg.Wait()
	close(results)

	duration := time.Since(start)

	// Count results
	var successful, failed int
	for success := range results {
		if success {
			successful++
		} else {
			failed++
		}
	}

	t.Logf("Load test completed in %v", duration)
	t.Logf("Successful: %d, Failed: %d (%.1f%% success rate)", 
		successful, failed, float64(successful)/float64(numRequests)*100)
	t.Logf("Average latency: %v per request", duration/time.Duration(numRequests))

	// Should have some successes with 10% failure rate
	if successful < numRequests/2 {
		t.Errorf("Success rate too low: %d/%d", successful, numRequests)
	}
}

// TestFaultyProxy_GracefulShutdown tests that the proxy shuts down cleanly
func TestFaultyProxy_GracefulShutdown(t *testing.T) {
	proxy := NewFaultyProxy(9203)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create a connection
	conn, err := net.Dial("tcp", "127.0.0.1:9203")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}

	// Check that we have an active connection
	if count := proxy.ActiveConnections(); count != 1 {
		t.Errorf("Expected 1 active connection, got %d", count)
	}

	// Stop the proxy
	proxy.Stop()

	// Connection should be closed
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Should not be able to connect to stopped proxy
	_, err = net.DialTimeout("tcp", "127.0.0.1:9203", 1*time.Second)
	if err == nil {
		t.Error("Expected connection to fail after proxy stop, but it succeeded")
	}
}

// TestFaultyProxy_TimeoutBehavior tests timeout fault mode specifically
func TestFaultyProxy_TimeoutBehavior(t *testing.T) {
	proxy := NewFaultyProxy(9204)
	proxy.FailureRate = 1.0 // Always timeout
	proxy.FaultType = ConnectionTimeout

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test timeout behavior
	start := time.Now()
	
	conn, err := net.Dial("tcp", "127.0.0.1:9204")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Try to read response with a reasonable timeout
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = conn.Read(buffer)
	
	elapsed := time.Since(start)
	
	// Should timeout
	if err == nil {
		t.Error("Expected timeout, but got successful read")
	}
	
	// Should take at least our read timeout
	if elapsed < 2*time.Second {
		t.Errorf("Expected timeout to take at least 2s, but took %v", elapsed)
	}
}

// TestFaultyProxy_ErrorRecovery tests that the proxy recovers from errors
func TestFaultyProxy_ErrorRecovery(t *testing.T) {
	proxy := NewFaultyProxy(9205)
	proxy.FailureRate = 0.8 // High failure rate initially
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test with high failure rate
	successCount := 0
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:9205")
		if err != nil {
			continue
		}

		connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			continue
		}

		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		conn.Close()

		if err == nil && strings.Contains(string(buffer[:n]), "200 Connection Established") {
			successCount++
		}
	}

	t.Logf("High failure rate phase: %d successes out of 5", successCount)

	// Now reduce failure rate
	proxy.FailureRate = 0.1 // Low failure rate
	time.Sleep(100 * time.Millisecond)

	// Test with low failure rate - should have more successes
	successCount = 0
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:9205")
		if err != nil {
			continue
		}

		connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			continue
		}

		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		conn.Close()

		if err == nil && strings.Contains(string(buffer[:n]), "200 Connection Established") {
			successCount++
		}
	}

	t.Logf("Low failure rate phase: %d successes out of 5", successCount)

	// Should have more successes with lower failure rate
	if successCount < 2 {
		t.Error("Expected more successes with low failure rate")
	}
}