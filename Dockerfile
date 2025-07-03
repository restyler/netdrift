# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./
COPY us.json ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o proxy main.go

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary and config from builder stage
COPY --from=builder /app/proxy .
COPY --from=builder /app/us.json .

# Expose the proxy port
EXPOSE 3130

# Run the proxy server
CMD ["./proxy"] 