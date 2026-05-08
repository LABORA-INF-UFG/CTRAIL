# Use the official Go image as a build stage
FROM golang:1.24-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Install git (needed for some Go modules)
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the Go application
# Set STRIP_SYMBOLS=false to retain symbol table for pprof profiling
ARG STRIP_SYMBOLS=true
RUN if [ "$STRIP_SYMBOLS" = "true" ]; then \
      CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o ctrail ./cmd/server/main.go; \
    else \
      CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ctrail ./cmd/server/main.go; \
    fi

# Use a small alpine image for the final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -S -D -H -u 1001 -h /app -s /sbin/nologin -G appgroup -g appgroup appuser

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/ctrail .

# Copy configuration files
COPY --from=builder /app/config ./config

# Create logs directory
RUN mkdir -p /app/logs

# Change ownership of the application to the non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port (configurable via SERVER_PORT environment variable)
EXPOSE 8080

# Add health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Command to run the executable
CMD ["./ctrail"]
