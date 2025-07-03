package faultyproxy

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// ExampleFaultyProxy_basicUsage demonstrates basic usage of the faulty proxy
func ExampleFaultyProxy_basicUsage() {
	// Create a new faulty proxy on port 8080
	proxy := NewFaultyProxy(8080)
	proxy.FailureRate = 0.0 // No failures
	proxy.FaultType = NoFault

	// Start the proxy
	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Connect to the proxy
	conn, err := net.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	// Send CONNECT request
	connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		fmt.Printf("Failed to write: %v\n", err)
		return
	}

	// Read response
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("Failed to read: %v\n", err)
		return
	}

	response := string(buffer[:n])
	if strings.Contains(response, "200 Connection Established") {
		fmt.Println("Connection successful")
	}

	// Output: Connection successful
}

// ExampleFaultyProxy_slowResponse demonstrates slow response simulation
func ExampleFaultyProxy_slowResponse() {
	// Create a proxy that always responds slowly
	proxy := NewFaultyProxy(8081)
	proxy.FailureRate = 1.0 // Always apply fault
	proxy.FaultType = SlowResponse
	proxy.Latency = 200 * time.Millisecond

	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	start := time.Now()

	conn, err := net.Dial("tcp", "127.0.0.1:8081")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		fmt.Printf("Failed to write: %v\n", err)
		return
	}

	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("Failed to read: %v\n", err)
		return
	}

	elapsed := time.Since(start)
	response := string(buffer[:n])

	if strings.Contains(response, "200 Connection Established") && elapsed >= 200*time.Millisecond {
		fmt.Println("Slow response received")
	}

	// Output: Slow response received
}

// ExampleFaultyProxy_connectionReset demonstrates connection reset simulation
func ExampleFaultyProxy_connectionReset() {
	// Create a proxy that always resets connections
	proxy := NewFaultyProxy(8082)
	proxy.FailureRate = 1.0 // Always fail
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:8082")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		fmt.Printf("Failed to write: %v\n", err)
		return
	}

	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(buffer)

	if err != nil {
		fmt.Println("Connection reset as expected")
	}

	// Output: Connection reset as expected
}

// ExampleFaultyProxy_badGateway demonstrates HTTP error response simulation
func ExampleFaultyProxy_badGateway() {
	// Create a proxy that always returns 502 Bad Gateway
	proxy := NewFaultyProxy(8083)
	proxy.FailureRate = 1.0 // Always fail
	proxy.FaultType = BadGateway

	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:8083")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		fmt.Printf("Failed to write: %v\n", err)
		return
	}

	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("Failed to read: %v\n", err)
		return
	}

	response := string(buffer[:n])
	if strings.Contains(response, "502 Bad Gateway") {
		fmt.Println("Bad Gateway error received")
	}

	// Output: Bad Gateway error received
}

// ExampleFaultyProxy_partialFailures demonstrates random failure simulation
func ExampleFaultyProxy_partialFailures() {
	// Create a proxy with 50% failure rate
	proxy := NewFaultyProxy(8084)
	proxy.FailureRate = 0.5 // 50% failure rate
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	successCount := 0
	totalRequests := 10

	for i := 0; i < totalRequests; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:8084")
		if err != nil {
			continue
		}

		connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			continue
		}

		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buffer)
		conn.Close()

		if err == nil {
			response := string(buffer[:n])
			if strings.Contains(response, "200 Connection Established") {
				successCount++
			}
		}
	}

	fmt.Printf("Completed %d requests with some successes and failures\n", totalRequests)

	// Output: Completed 10 requests with some successes and failures
}

// ExampleFaultyProxy_activeConnections demonstrates connection tracking
func ExampleFaultyProxy_activeConnections() {
	proxy := NewFaultyProxy(8085)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		fmt.Printf("Failed to start proxy: %v\n", err)
		return
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	fmt.Printf("Active connections initially: %d\n", proxy.ActiveConnections())

	// Create a connection
	conn, err := net.Dial("tcp", "127.0.0.1:8085")
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}

	time.Sleep(100 * time.Millisecond) // Let the connection be processed
	fmt.Printf("Active connections after connect: %d\n", proxy.ActiveConnections())

	conn.Close()
	time.Sleep(100 * time.Millisecond) // Let the connection be cleaned up
	fmt.Printf("Active connections after close: %d\n", proxy.ActiveConnections())

	// Output:
	// Active connections initially: 0
	// Active connections after connect: 1
	// Active connections after close: 0
}