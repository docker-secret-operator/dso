# Build Stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Download and cache dependencies separately for faster builds
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build DSO Core
# Use -ldflags to reduce binary size and remove symbol tables
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o docker-dso cmd/docker-dso/main.go

# Build Secret Provider Plugins
RUN mkdir -p /app/plugins
RUN cd cmd/plugins/dso-provider-vault && CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/plugins/dso-provider-vault main.go
# Add other plugins as they become available

# Final Stage
FROM alpine:3.20

# Install runtime dependencies and security certificates
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user for security (UID 10001 is a common convention)
RUN addgroup -S dso-group && adduser -S dso-user -G dso-group -u 10001

WORKDIR /home/dso-user

# Copy binaries from builder
COPY --from=builder /app/docker-dso /usr/local/bin/docker-dso

# Set up plugin directory
RUN mkdir -p /usr/local/lib/dso/plugins && \
    chown -R dso-user:dso-group /usr/local/lib/dso

# Copy plugins from builder
COPY --from=builder /app/plugins/ /usr/local/lib/dso/plugins/

# Ensure binaries are executable and owned by the dso-user
RUN chmod +x /usr/local/bin/docker-dso && \
    chmod +x /usr/local/lib/dso/plugins/*

# Switch to non-root user
USER dso-user

# Expose common volume mount points
VOLUME ["/var/run", "/etc/dso"]

# Default entrypoint
ENTRYPOINT ["docker-dso"]
CMD ["agent"]
