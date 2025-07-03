package faultyproxy

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type FaultType int

const (
	NoFault FaultType = iota
	SlowResponse
	ConnectionReset
	ConnectionTimeout
	BadGateway
	InternalError
)

type FaultyProxy struct {
	Port           int
	FailureRate    float64 // 0.0 to 1.0
	Latency        time.Duration
	LatencyJitter  time.Duration
	FaultType      FaultType
	connections    int64
	server         *http.Server
	listener       net.Listener
	shutdownSignal chan struct{}
}

func NewFaultyProxy(port int) *FaultyProxy {
	return &FaultyProxy{
		Port:           port,
		FailureRate:    0.0,
		Latency:        0,
		LatencyJitter:  0,
		FaultType:      NoFault,
		shutdownSignal: make(chan struct{}),
	}
}

func (fp *FaultyProxy) ActiveConnections() int64 {
	return atomic.LoadInt64(&fp.connections)
}

func (fp *FaultyProxy) simulateLatency() {
	if fp.Latency > 0 {
		jitter := time.Duration(0)
		if fp.LatencyJitter > 0 {
			jitter = time.Duration(rand.Int63n(int64(fp.LatencyJitter)))
		}
		time.Sleep(fp.Latency + jitter)
	}
}

func (fp *FaultyProxy) shouldFail() bool {
	return rand.Float64() < fp.FailureRate
}

func (fp *FaultyProxy) handleConnection(conn net.Conn) {
	atomic.AddInt64(&fp.connections, 1)
	defer atomic.AddInt64(&fp.connections, -1)
	defer conn.Close()

	log.Printf("[FaultyProxy-%d] New connection from %s", fp.Port, conn.RemoteAddr())

	// Simulate latency before any processing
	fp.simulateLatency()

	// Check if we should fail this request
	if fp.shouldFail() {
		log.Printf("[FaultyProxy-%d] Simulating failure type %v", fp.Port, fp.FaultType)
		switch fp.FaultType {
		case ConnectionReset:
			log.Printf("[FaultyProxy-%d] Simulating connection reset", fp.Port)
			return
		case ConnectionTimeout:
			log.Printf("[FaultyProxy-%d] Simulating timeout (hanging for 31s)", fp.Port)
			time.Sleep(31 * time.Second) // Most clients timeout at 30s
			return
		case BadGateway:
			log.Printf("[FaultyProxy-%d] Simulating bad gateway", fp.Port)
			conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		case InternalError:
			log.Printf("[FaultyProxy-%d] Simulating internal error", fp.Port)
			conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\n\r\n"))
			return
		}
	}

	// Read the CONNECT request
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("[FaultyProxy-%d] Failed to read request: %v", fp.Port, err)
		return
	}

	log.Printf("[FaultyProxy-%d] Received request: %s", fp.Port, string(buffer[:n]))

	// For SlowResponse type, add extra delay before responding
	if fp.FaultType == SlowResponse {
		log.Printf("[FaultyProxy-%d] Simulating slow response", fp.Port)
		jitter := time.Duration(0)
		if fp.LatencyJitter > 0 {
			jitter = time.Duration(rand.Int63n(int64(fp.LatencyJitter)))
		}
		time.Sleep(fp.Latency + jitter)
	}

	// Send 200 Connection Established
	resp := "HTTP/1.1 200 Connection Established\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		log.Printf("[FaultyProxy-%d] Failed to write response: %v", fp.Port, err)
		return
	}

	log.Printf("[FaultyProxy-%d] Sent 200 Connection Established", fp.Port)

	// Handle data tunneling with potential faults
	targetAddr := fp.extractTargetFromConnect(string(buffer[:n]))
	if targetAddr == "" {
		log.Printf("[FaultyProxy-%d] Could not extract target address", fp.Port)
		return
	}

	// Connect to the actual target
	targetConn, err := net.DialTimeout("tcp", targetAddr, 30*time.Second)
	if err != nil {
		log.Printf("[FaultyProxy-%d] Failed to connect to target %s: %v", fp.Port, targetAddr, err)
		return
	}
	defer targetConn.Close()

	log.Printf("[FaultyProxy-%d] Connected to target %s", fp.Port, targetAddr)

	// Start bidirectional copying with fault injection
	go func() {
		defer targetConn.Close()
		defer conn.Close()
		fp.copyWithFaults(targetConn, conn, "target->client")
	}()

	fp.copyWithFaults(conn, targetConn, "client->target")
}

func (fp *FaultyProxy) extractTargetFromConnect(request string) string {
	// Parse CONNECT request to extract target address
	lines := strings.Split(request, "\r\n")
	if len(lines) > 0 {
		parts := strings.Split(lines[0], " ")
		if len(parts) >= 2 && parts[0] == "CONNECT" {
			return parts[1]
		}
	}
	return ""
}

func (fp *FaultyProxy) copyWithFaults(dst, src net.Conn, direction string) {
	buffer := make([]byte, 32*1024) // 32KB buffer
	for {
		select {
		case <-fp.shutdownSignal:
			return
		default:
			// Set read timeout to avoid hanging indefinitely
			src.SetReadDeadline(time.Now().Add(30 * time.Second))
			
			n, err := src.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Printf("[FaultyProxy-%d] Error reading from %s: %v", fp.Port, direction, err)
				}
				return
			}

			// Simulate random connection drops during data transfer
			if fp.shouldFail() && fp.FaultType == ConnectionReset {
				log.Printf("[FaultyProxy-%d] Simulating connection reset during %s", fp.Port, direction)
				return
			}

			// Simulate latency for each chunk
			if fp.FaultType == SlowResponse || fp.Latency > 0 {
				fp.simulateLatency()
			}

			// Write data
			dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := dst.Write(buffer[:n]); err != nil {
				log.Printf("[FaultyProxy-%d] Failed to write to %s: %v", fp.Port, direction, err)
				return
			}
		}
	}
}

func (fp *FaultyProxy) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fp.Port))
	if err != nil {
		return fmt.Errorf("failed to start proxy on port %d: %v", fp.Port, err)
	}
	fp.listener = listener

	log.Printf("[FaultyProxy-%d] Starting proxy with failure rate %.2f", fp.Port, fp.FailureRate)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-fp.shutdownSignal:
					return
				default:
					log.Printf("[FaultyProxy-%d] Failed to accept connection: %v", fp.Port, err)
					continue
				}
			}
			go fp.handleConnection(conn)
		}
	}()

	return nil
}

func (fp *FaultyProxy) Stop() {
	close(fp.shutdownSignal)
	if fp.listener != nil {
		fp.listener.Close()
	}
}
