package faultyproxy

import (
	"net"
	"strings"
	"testing"
	"time"
)

// BenchmarkFaultyProxy_NoFaults benchmarks the proxy without any faults
func BenchmarkFaultyProxy_NoFaults(b *testing.B) {
	proxy := NewFaultyProxy(9300)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := net.Dial("tcp", "127.0.0.1:9300")
			if err != nil {
				b.Errorf("Failed to connect: %v", err)
				continue
			}

			// Send CONNECT request
			connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				conn.Close()
				b.Errorf("Failed to write: %v", err)
				continue
			}

			// Read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, err = conn.Read(buffer)
			conn.Close()

			if err != nil {
				b.Errorf("Failed to read: %v", err)
			}
		}
	})
}

// BenchmarkFaultyProxy_WithLatency benchmarks the proxy with added latency
func BenchmarkFaultyProxy_WithLatency(b *testing.B) {
	proxy := NewFaultyProxy(9301)
	proxy.FailureRate = 0.0
	proxy.FaultType = SlowResponse
	proxy.Latency = 10 * time.Millisecond

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:9301")
		if err != nil {
			b.Errorf("Failed to connect: %v", err)
			continue
		}

		// Send CONNECT request
		connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			b.Errorf("Failed to write: %v", err)
			continue
		}

		// Read response
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, err = conn.Read(buffer)
		conn.Close()

		if err != nil {
			b.Errorf("Failed to read: %v", err)
		}
	}
}

// BenchmarkFaultyProxy_PartialFailures benchmarks the proxy with partial failures
func BenchmarkFaultyProxy_PartialFailures(b *testing.B) {
	proxy := NewFaultyProxy(9302)
	proxy.FailureRate = 0.3 // 30% failure rate
	proxy.FaultType = ConnectionReset

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1:9302")
		if err != nil {
			// Connection might fail due to fault injection
			continue
		}

		// Send CONNECT request
		connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			// Write might fail due to fault injection
			continue
		}

		// Read response
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, err = conn.Read(buffer)
		conn.Close()

		// Read might fail due to fault injection - that's expected
	}
}

// BenchmarkFaultyProxy_ConcurrentConnections benchmarks concurrent connection handling
func BenchmarkFaultyProxy_ConcurrentConnections(b *testing.B) {
	proxy := NewFaultyProxy(9303)
	proxy.FailureRate = 0.1 // 10% failure rate
	proxy.FaultType = SlowResponse
	proxy.Latency = 5 * time.Millisecond

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := net.Dial("tcp", "127.0.0.1:9303")
			if err != nil {
				continue
			}

			// Send CONNECT request
			connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
			if _, err := conn.Write([]byte(connectReq)); err != nil {
				conn.Close()
				continue
			}

			// Read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			_, err = conn.Read(buffer)
			conn.Close()

			// Errors are expected due to fault injection
		}
	})
}

// BenchmarkFaultyProxy_ConnectionLifecycle benchmarks the full connection lifecycle
func BenchmarkFaultyProxy_ConnectionLifecycle(b *testing.B) {
	proxy := NewFaultyProxy(9304)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Measure full lifecycle: connect, CONNECT request, response, close
		start := time.Now()

		conn, err := net.Dial("tcp", "127.0.0.1:9304")
		if err != nil {
			b.Errorf("Failed to connect: %v", err)
			continue
		}

		// Send CONNECT request
		connectReq := "CONNECT httpbin.org:443 HTTP/1.1\r\nHost: httpbin.org:443\r\n\r\n"
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			b.Errorf("Failed to write: %v", err)
			continue
		}

		// Read response
		buffer := make([]byte, 1024)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buffer)
		if err != nil {
			conn.Close()
			b.Errorf("Failed to read: %v", err)
			continue
		}

		// Verify response
		response := string(buffer[:n])
		if !strings.Contains(response, "200 Connection Established") {
			conn.Close()
			b.Errorf("Unexpected response: %s", response)
			continue
		}

		conn.Close()

		// Report timing
		elapsed := time.Since(start)
		b.ReportMetric(float64(elapsed.Nanoseconds()), "ns/connection")
	}
}

// BenchmarkFaultyProxy_MemoryUsage benchmarks memory usage under load
func BenchmarkFaultyProxy_MemoryUsage(b *testing.B) {
	proxy := NewFaultyProxy(9305)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Pre-allocate connections slice to avoid allocation overhead in benchmark
	connections := make([]net.Conn, 0, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create multiple connections to simulate memory pressure
		for j := 0; j < 10; j++ {
			conn, err := net.Dial("tcp", "127.0.0.1:9305")
			if err != nil {
				continue
			}

			connections = append(connections, conn)

			// Send CONNECT request
			connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
			conn.Write([]byte(connectReq))
		}

		// Close all connections
		for _, conn := range connections {
			conn.Close()
		}
		connections = connections[:0] // Reset slice but keep capacity
	}
}

// BenchmarkFaultyProxy_ThroughputTest benchmarks maximum throughput
func BenchmarkFaultyProxy_ThroughputTest(b *testing.B) {
	proxy := NewFaultyProxy(9306)
	proxy.FailureRate = 0.0
	proxy.FaultType = NoFault

	if err := proxy.Start(); err != nil {
		b.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Track bytes processed
	var totalBytes int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := net.Dial("tcp", "127.0.0.1:9306")
			if err != nil {
				continue
			}

			// Send CONNECT request
			connectReq := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
			bytesWritten, err := conn.Write([]byte(connectReq))
			if err != nil {
				conn.Close()
				continue
			}

			// Read response
			buffer := make([]byte, 1024)
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			bytesRead, err := conn.Read(buffer)
			conn.Close()

			if err == nil {
				totalBytes += int64(bytesWritten + bytesRead)
			}
		}
	})

	b.ReportMetric(float64(totalBytes), "bytes")
	b.ReportMetric(float64(totalBytes)/b.Elapsed().Seconds(), "bytes/sec")
}