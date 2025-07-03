.PHONY: build run-proxy run-test-proxies test test-integration clean docker-build docker-run docker-prod docker-test docker-prod-up docker-prod-down docker-test-up docker-test-down docker-logs

# Build the main proxy server
build:
	mkdir -p bin
	go build -o bin/proxy ./cmd/proxy

# Build the test proxy servers
build-test:
	mkdir -p bin
	go build -o bin/test-proxy ./cmd/test-proxy

# Build the faulty proxy server
build-faulty:
	mkdir -p bin
	go build -o bin/faulty-proxy ./cmd/faulty-proxy

# Run the main proxy server
run-proxy: build
	./bin/proxy -config configs/us.json

# Run test proxy servers on ports 3025 and 3026
run-test-proxies: build-test
	./bin/test-proxy 3025 3026

# Test the proxy with curl
test:
	@echo "Testing proxy with authentication..."
	curl -x http://proxyuser:Proxy234@127.0.0.1:3130 https://myip.scrapeninja.net
	@echo "\n\nChecking stats..."
	curl http://127.0.0.1:3130/stats

# Run Go tests for the main proxy
test-unit:
	go test -v ./cmd/proxy/...

# Run comprehensive integration test (build, start, test, cleanup)
test-integration:
	./scripts/test-runner.sh

# Run tests for faulty proxy package only
test-faultyproxy:
	go test -v ./pkg/faultyproxy

# Run comprehensive faulty proxy test suite
test-faultyproxy-full:
	./scripts/test-faultyproxy.sh all

# Run faulty proxy benchmarks
test-faultyproxy-bench:
	./scripts/test-faultyproxy.sh benchmarks

# Clean build artifacts
clean:
	rm -rf bin
	rm -f proxy test-proxy faulty-proxy  # Legacy cleanup

# Production Docker commands
docker-build:
	docker build -t netdrift .

docker-run:
	docker run -p 3130:3130 netdrift

docker-prod:
	docker build -t netdrift-prod .

docker-prod-up:
	docker compose -f docker-compose.prod.yml up -d

docker-prod-down:
	docker compose -f docker-compose.prod.yml down

docker-prod-logs:
	docker compose -f docker-compose.prod.yml logs -f

# Test Docker commands  
docker-test:
	docker build -f Dockerfile.test -t netdrift-test .

docker-test-up:
	docker compose -f docker-compose.test.yml up -d

docker-test-down:
	docker compose -f docker-compose.test.yml down

docker-test-logs:
	docker compose -f docker-compose.test.yml logs -f

# Legacy/convenience commands (use production)
docker-up: docker-prod-up
docker-down: docker-prod-down
docker-logs: docker-prod-logs

# Cleanup commands
docker-clean:
	docker compose -f docker-compose.prod.yml down -v --remove-orphans || true
	docker compose -f docker-compose.test.yml down -v --remove-orphans || true
	docker system prune -f 