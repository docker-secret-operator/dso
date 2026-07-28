# Build Stage
FROM golang:1.25-alpine AS builder

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
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o docker-dso ./cmd/dso

# Build Secret Provider Plugins
RUN mkdir -p /app/plugins
RUN cd cmd/plugins/dso-provider-vault && CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/plugins/dso-provider-vault main.go
RUN cd cmd/plugins/dso-provider-aws && CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/plugins/dso-provider-aws main.go
RUN cd cmd/plugins/dso-provider-azure && CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/plugins/dso-provider-azure main.go
RUN cd cmd/plugins/dso-provider-huawei && CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/plugins/dso-provider-huawei main.go

# Final Stage
FROM alpine:3.20

# Install runtime dependencies and security certificates
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user for security (UID 10001 is a common convention)
RUN addgroup -S dso-group && adduser -S dso-user -G dso-group -u 10001

WORKDIR /home/dso-user

# Copy binaries from builder
COPY --from=builder /app/docker-dso /usr/local/bin/docker-dso

# Set up plugin directory. [SEC-3] Deliberately NOT chown'd to dso-user: the
# directory and its contents stay root-owned (default for a RUN step
# executed before the USER switch below) with 0755, giving dso-user
# read+execute access (sufficient to exec and hash-verify plugins) but not
# write. If dso-user owned this directory, a compromised daemon process
# could chmod it back to writable itself (ownership, not just permission
# bits, is what a future compromise could exploit) and overwrite plugin
# binaries plus the SEC-2 hash manifest that is meant to verify them.
RUN mkdir -p /usr/local/lib/dso/plugins && \
    chmod 0755 /usr/local/lib/dso /usr/local/lib/dso/plugins

# Copy plugins from builder
COPY --from=builder /app/plugins/ /usr/local/lib/dso/plugins/

# Ensure binaries are executable. [SEC-3] Deliberately NOT chowned to
# dso-user -- they stay root-owned, matching the plugin directory itself.
RUN chmod +x /usr/local/bin/docker-dso && \
    chmod +x /usr/local/lib/dso/plugins/*

# [SEC-2] Generate the plugin hash manifest at build time. The image never
# runs `docker dso system setup` (which normally writes this file), so
# without this step plugin hash verification -- mandatory by default as of
# SEC-2 -- would reject every plugin in this image with "cannot open hash
# manifest", breaking the container as the primary deployment method.
RUN cd /usr/local/lib/dso/plugins && \
    { echo "# Plugin Hash Manifest (auto-generated at image build time)"; \
      echo "# Format: plugin_name=sha256_hash"; \
      echo ""; \
      for f in dso-provider-*; do \
        echo "$f=$(sha256sum "$f" | cut -d' ' -f1)"; \
      done; \
    } > hashes.txt

# Switch to non-root user
USER dso-user

# Expose common volume mount points
VOLUME ["/var/run", "/etc/dso"]

# Default entrypoint
ENTRYPOINT ["docker-dso"]
CMD ["legacy-agent"]
