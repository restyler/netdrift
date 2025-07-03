# Production Dockerfile - Main proxy server only
# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build only the main proxy
RUN mkdir -p bin && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/proxy ./cmd/proxy

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates curl

WORKDIR /app

# Copy the proxy binary and configs from builder stage
COPY --from=builder /app/bin/proxy ./bin/
COPY --from=builder /app/configs ./configs

# Make binary executable
RUN chmod +x ./bin/proxy

# Expose the proxy port
EXPOSE 3130

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3130/stats || exit 1

# Run the proxy server
CMD ["./bin/proxy", "-config", "/app/configs/docker.json"]