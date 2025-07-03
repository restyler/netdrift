package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

type SimpleProxy struct {
	port int
	name string
}

func (sp *SimpleProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("[%s] CONNECT request to %s from %s", sp.name, r.Host, r.RemoteAddr)

	// Connect directly to the target host
	targetConn, err := net.DialTimeout("tcp", r.Host, 30*time.Second)
	if err != nil {
		log.Printf("[%s] Failed to connect to target %s: %v", sp.name, r.Host, err)
		http.Error(w, "Failed to connect to target", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("[%s] ResponseWriter doesn't support hijacking", sp.name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[%s] Failed to hijack connection: %v", sp.name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established to client
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("[%s] Failed to send 200 to client: %v", sp.name, err)
		return
	}

	log.Printf("[%s] Established direct tunnel to %s", sp.name, r.Host)

	// Start bidirectional copying
	go func() {
		defer targetConn.Close()
		defer clientConn.Close()
		io.Copy(targetConn, clientConn)
	}()

	io.Copy(clientConn, targetConn)
}

func (sp *SimpleProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" {
		sp.handleConnect(w, r)
		return
	}

	// Simple status endpoint
	if r.URL.Path == "/status" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"proxy": "%s", "port": %d, "status": "running"}`, sp.name, sp.port)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func startProxy(port int, name string) {
	proxy := &SimpleProxy{
		port: port,
		name: name,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: proxy,
	}

	log.Printf("[%s] Starting simple proxy on %s", name, addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("[%s] Server failed: %v", name, err)
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Println("Usage: go run test_proxy.go <port1> [port2] ...")
		log.Println("Example: go run test_proxy.go 1025 1026")
		os.Exit(1)
	}

	ports := make([]int, 0)
	for i := 1; i < len(os.Args); i++ {
		port, err := strconv.Atoi(os.Args[i])
		if err != nil {
			log.Fatalf("Invalid port number: %s", os.Args[i])
		}
		ports = append(ports, port)
	}

	if len(ports) == 0 {
		log.Fatal("No valid ports specified")
	}

	// Start all proxies except the last one in goroutines
	for i := 0; i < len(ports)-1; i++ {
		port := ports[i]
		name := fmt.Sprintf("TestProxy-%d", port)
		go startProxy(port, name)
	}

	// Start the last proxy in the main goroutine to keep the program running
	lastPort := ports[len(ports)-1]
	lastName := fmt.Sprintf("TestProxy-%d", lastPort)
	startProxy(lastPort, lastName)
}
