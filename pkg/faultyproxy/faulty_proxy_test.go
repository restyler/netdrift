package faultyproxy

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestFaultyProxy_NoFaults(t *testing.T) {
	proxy := NewFaultyProxy(9101)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Wait for proxy to start
	time.Sleep(100 * time.Millisecond)

	// Test normal connection
	conn, err := net.Dial("tcp", "127.0.0.1:9101")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	response := string(buffer[:n])
	if !strings.Contains(response, "200 Connection Established") {
		t.Errorf("Expected 200 Connection Established, got: %s", response)
	}
}

func TestFaultyProxy_ConnectionReset(t *testing.T) {
	proxy := NewFaultyProxy(9102)
	proxy.FailureRate = 1.0 // Always fail
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test connection reset
	conn, err := net.Dial("tcp", "127.0.0.1:9102")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Try to read response - should get connection reset
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(buffer)
	
	// Should get EOF or connection reset error
	if err == nil {
		t.Error("Expected connection to be reset, but got successful read")
	}
}

func TestFaultyProxy_BadGateway(t *testing.T) {
	proxy := NewFaultyProxy(9103)
	proxy.FailureRate = 1.0 // Always fail
	proxy.FaultType = BadGateway

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test bad gateway response
	conn, err := net.Dial("tcp", "127.0.0.1:9103")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	response := string(buffer[:n])
	if !strings.Contains(response, "502 Bad Gateway") {
		t.Errorf("Expected 502 Bad Gateway, got: %s", response)
	}
}

func TestFaultyProxy_SlowResponse(t *testing.T) {
	proxy := NewFaultyProxy(9105)
	proxy.FailureRate = 1.0 // Always be slow
	proxy.FaultType = SlowResponse
	proxy.Latency = 200 * time.Millisecond

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test slow response
	start := time.Now()
	
	conn, err := net.Dial("tcp", "127.0.0.1:9105")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	elapsed := time.Since(start)
	response := string(buffer[:n])
	
	if !strings.Contains(response, "200 Connection Established") {
		t.Errorf("Expected 200 Connection Established, got: %s", response)
	}
	
	if elapsed < 200*time.Millisecond {
		t.Errorf("Expected response to take at least 200ms, but took %v", elapsed)
	}
}

func TestFaultyProxy_ConnectionTimeout(t *testing.T) {
	proxy := NewFaultyProxy(9106)
	proxy.FailureRate = 1.0 // Always timeout
	proxy.FaultType = ConnectionTimeout

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test timeout
	conn, err := net.Dial("tcp", "127.0.0.1:9106")
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		t.Fatalf("Failed to send CONNECT request: %v", err)
	}

	// Try to read response with short timeout
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(buffer)
	
	// Should timeout
	if err == nil {
		t.Error("Expected timeout, but got successful read")
	}
}

func TestFaultyProxy_PartialFailure(t *testing.T) {
	proxy := NewFaultyProxy(9107)
	proxy.FailureRate = 0.5 // 50% failure rate
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test multiple connections to verify random behavior
	successCount := 0
	failureCount := 0
	totalTests := 20

	for i := 0; i < totalTests; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:9107")
		if err != nil {
			t.Fatalf("Failed to connect to proxy: %v", err)
		}

		// Send CONNECT request
		connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			failureCount++
			continue
		}

		// Try to read response
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		conn.Close()

		if err != nil {
			failureCount++
		} else {
			response := string(buffer[:n])
			if strings.Contains(response, "200 Connection Established") {
				successCount++
			} else {
				failureCount++
			}
		}
	}

	t.Logf("Success: %d, Failures: %d out of %d tests", successCount, failureCount, totalTests)
	
	// With 50% failure rate, we should have both successes and failures
	if successCount == 0 {
		t.Error("Expected some successful connections, but got none")
	}
	if failureCount == 0 {
		t.Error("Expected some failed connections, but got none")
	}
}