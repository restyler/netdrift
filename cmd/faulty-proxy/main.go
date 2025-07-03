package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"netdrift/pkg/faultyproxy"
)

func main() {
	var (
		port         = flag.Int("port", 8080, "Port to listen on")
		failureRate  = flag.Float64("failure-rate", 0.0, "Failure rate (0.0 to 1.0)")
		latency      = flag.Duration("latency", 0, "Base latency to add (e.g. 100ms, 2s)")
		jitter       = flag.Duration("jitter", 0, "Latency jitter (e.g. 50ms)")
		faultType    = flag.String("fault-type", "none", "Fault type: none, slow, reset, timeout, bad-gateway, internal-error")
		help         = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help {
		fmt.Println("Faulty Proxy - Configurable misbehaving proxy server")
		fmt.Println("")
		fmt.Println("Usage:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Fault Types:")
		fmt.Println("  none          - No faults, behaves normally")
		fmt.Println("  slow          - Slow responses (uses latency + jitter)")
		fmt.Println("  reset         - Reset connections randomly")
		fmt.Println("  timeout       - Hang connections (31s timeout)")
		fmt.Println("  bad-gateway   - Return 502 Bad Gateway")
		fmt.Println("  internal-error - Return 500 Internal Server Error")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  faulty-proxy -port 8081 -failure-rate 0.3 -fault-type reset")
		fmt.Println("  faulty-proxy -port 8082 -latency 2s -jitter 500ms -fault-type slow")
		fmt.Println("  faulty-proxy -port 8083 -failure-rate 0.1 -fault-type timeout")
		os.Exit(0)
	}

	// Parse fault type
	var ft faultyproxy.FaultType
	switch *faultType {
	case "none":
		ft = faultyproxy.NoFault
	case "slow":
		ft = faultyproxy.SlowResponse
	case "reset":
		ft = faultyproxy.ConnectionReset
	case "timeout":
		ft = faultyproxy.ConnectionTimeout
	case "bad-gateway":
		ft = faultyproxy.BadGateway
	case "internal-error":
		ft = faultyproxy.InternalError
	default:
		log.Fatalf("Unknown fault type: %s", *faultType)
	}

	// Create and configure faulty proxy
	faultyProxy := faultyproxy.NewFaultyProxy(*port)
	faultyProxy.FailureRate = *failureRate
	faultyProxy.Latency = *latency
	faultyProxy.LatencyJitter = *jitter
	faultyProxy.FaultType = ft

	// Start proxy
	if err := faultyProxy.Start(); err != nil {
		log.Fatalf("Failed to start faulty proxy: %v", err)
	}

	log.Printf("Faulty proxy started on port %d", *port)
	log.Printf("Configuration: failure-rate=%.2f, latency=%v, jitter=%v, fault-type=%s", 
		*failureRate, *latency, *jitter, *faultType)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down faulty proxy...")
	faultyProxy.Stop()
}