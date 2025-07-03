.PHONY: build run-proxy run-test-proxies test clean

# Build the main proxy server
build:
	go build -o proxy ./cmd/proxy

# Build the test proxy servers
build-test:
	go build -o test-proxy ./pkg/proxy/test_proxy.go

# Run the main proxy server
run-proxy: build
	./proxy

# Run test proxy servers on ports 3025 and 3026
run-test-proxies: build-test
	./test-proxy 3025 3026

# Test the proxy with curl
test:
	@echo "Testing proxy with authentication..."
	curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net
	@echo "\n\nChecking stats..."
	curl http://127.0.0.1:3130/stats

# Clean build artifacts
clean:
	rm -f proxy test-proxy

# Docker build
docker-build:
	docker build -t netdrift .

# Docker run
docker-run:
	docker run -p 3130:3130 netdrift 