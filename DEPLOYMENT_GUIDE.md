# DSO Deployment Guide

**Version:** 0.9.0-rc1  
**Date:** 2026-06-05  
**Status:** Release Candidate

---

## Overview

This guide provides step-by-step instructions for deploying DSO in production environments. DSO is distributed as a single self-contained binary with an embedded SQLite database and static frontend assets.

**Deployment model:** Single-binary, no external dependencies

---

## Architecture

### Deployment Components

```
dso (binary)
├─ REST API Server (port 8080)
├─ SQLite Database (./dso.db)
├─ Static Frontend Assets (embedded React SPA)
└─ Logs (stdout/stderr)
```

### Key Characteristics
- **Single binary** - No additional dependencies to install
- **Embedded database** - SQLite with ACID transactions
- **Static frontend** - React SPA compiled into binary
- **No external services** - No database server, no backend dependencies
- **Local state** - All state persisted to local disk

---

## Pre-Deployment Checklist

### System Requirements

**Hardware:**
- [ ] CPU: 1 core minimum (2+ cores recommended)
- [ ] RAM: 256MB minimum (512MB+ recommended)
- [ ] Disk: 1GB minimum available space
- [ ] Network: Outbound HTTPS capability (optional, for future integrations)

**Operating System:**
- [ ] Linux (x86-64) or macOS (x86-64)
- [ ] Kernel version 3.10+ (Linux)
- [ ] glibc 2.17+ (Linux)

**Network:**
- [ ] Port 8080 available (or configured custom port)
- [ ] Inbound access from operators/dashboards
- [ ] Outbound HTTPS optional

**Build Environment** (if building from source):
- [ ] Go 1.21 or later installed
- [ ] GOPATH/GOBIN in PATH
- [ ] Sufficient disk space for build artifacts

### Pre-Deployment Validation

**Validate system requirements:**
```bash
# Check OS
uname -a

# Check available disk
df -h /

# Check available RAM
free -h

# Check port availability
netstat -tuln | grep 8080
```

**Validate Go installation** (if building):
```bash
go version  # Should be 1.21+
```

---

## Building from Source

### Clone Repository

```bash
git clone https://github.com/antiersolutions/dso.git
cd dso
```

### Build Binary

**Standard build:**
```bash
go build -o dso ./cmd/dso
```

**Optimized build** (smaller binary):
```bash
go build -ldflags="-s -w" -o dso ./cmd/dso
```

**Build with version info:**
```bash
go build -ldflags="-s -w -X main.Version=0.9.0-rc1" -o dso ./cmd/dso
```

### Verify Build

```bash
# Check binary exists and is executable
ls -lh dso

# Get file info
file dso

# Test binary help
./dso --help
```

**Expected output:**
```
Usage: dso [options]
Options:
  --config string    Path to configuration file (default: config.yaml)
  --help             Show this help message
```

---

## Configuration

### Default Configuration

If no `config.yaml` is provided, DSO uses these defaults:

```yaml
server:
  port: 8080
  host: 0.0.0.0

database:
  path: ./dso.db

workers:
  health_check_interval: 30s
  heartbeat_timeout: 60s

queue:
  max_retry_count: 3
  ttl_default: 24h

execution:
  default_timeout: 60m
```

### Production Configuration

**Create `config.yaml` for production:**

```yaml
# Server configuration
server:
  port: 8080
  host: 0.0.0.0
  # TLS not yet supported in RC1
  # tls_enabled: true
  # tls_cert: /etc/dso/cert.pem
  # tls_key: /etc/dso/key.pem

# Database configuration
database:
  # Use absolute path for production
  path: /data/dso/dso.db
  max_connections: 10
  timeout: 30s
  # Enable WAL mode for better concurrency
  wal_enabled: true

# Worker configuration
workers:
  # Check worker health every 30 seconds
  health_check_interval: 30s
  # Mark worker unhealthy if no heartbeat for 60 seconds
  heartbeat_timeout: 60s
  # Max concurrent executions per worker
  max_concurrent_executions: 5

# Queue configuration
queue:
  # Retry failed executions up to 3 times
  max_retry_count: 3
  # Default TTL for queued items
  ttl_default: 24h
  # Maximum queue depth before alerts
  max_queue_depth: 1000

# Execution configuration
execution:
  # Default timeout for entire execution
  default_timeout: 60m
  # Default timeout per step
  step_timeout: 15m
  # Risk-based failure injection (for simulated execution)
  failure_injection:
    low_risk: 0.02      # 2%
    medium_risk: 0.05   # 5%
    high_risk: 0.10     # 10%

# Alert configuration
alerts:
  thresholds:
    # Alert if failure rate exceeds 10%
    failure_rate: 0.10
    # Alert if queue depth exceeds 500
    queue_depth: 500
    # Alert if >50% workers unhealthy
    worker_unhealthy: 0.50

# Logging configuration
logging:
  level: info              # debug, info, warn, error
  format: json             # json, text
  output: stdout           # stdout, file
  # file_path: /var/log/dso/dso.log
```

### Configuration File Locations

DSO searches for configuration in this order:
1. `--config` flag (if provided)
2. `./config.yaml` (current directory)
3. Built-in defaults

**Best practice:** Use absolute paths in production configuration.

---

## Database Setup

### Database Initialization

DSO automatically initializes the database on first run:

```bash
./dso --config config.yaml
```

**First run output:**
```
2026-06-05 10:00:00 INFO DSO starting
2026-06-05 10:00:00 INFO Database initialization (SQLite)
2026-06-05 10:00:00 INFO Running migrations...
2026-06-05 10:00:00 INFO Migration 0001: execution_requests table
2026-06-05 10:00:00 INFO Migration 0002: execution_plans table
...
2026-06-05 10:00:00 INFO Migration 0011: audit_events table
2026-06-05 10:00:00 INFO All migrations complete
2026-06-05 10:00:00 INFO REST API listening on :8080
```

### Database Location

**Default location:** `./dso.db` (current working directory)

**Recommended production location:** `/data/dso/dso.db` or `/var/lib/dso/dso.db`

**Set in config:**
```yaml
database:
  path: /data/dso/dso.db
```

### Database Features

- **SQLite 3.x** embedded database
- **ACID transactions** for data integrity
- **WAL mode** for concurrent reads
- **11 migrations** for schema management
- **Automatic backups** via application
- **Optimistic locking** via version fields

### Database Permissions

Set proper file permissions:
```bash
chmod 600 /data/dso/dso.db      # Owner read/write only
chown dso:dso /data/dso/dso.db  # Owned by dso user
```

### Database Backups

**Backup database:**
```bash
# Stop DSO first
systemctl stop dso

# Backup
cp /data/dso/dso.db /backups/dso.db.backup.$(date +%Y%m%d_%H%M%S)

# Restart
systemctl start dso
```

**Verify backup integrity:**
```bash
sqlite3 /backups/dso.db.backup.* "PRAGMA integrity_check;"
```

### Database Recovery

**Restore from backup:**
```bash
# Stop DSO
systemctl stop dso

# Restore
cp /backups/dso.db.backup.20260605_100000 /data/dso/dso.db

# Restart
systemctl start dso

# Verify
curl http://localhost:8080/api/orchestration/overview
```

---

## Asset Embedding

### Frontend Assets

DSO includes static React SPA frontend assets:
- Compiled TypeScript/JavaScript
- CSS and styling
- All dependencies bundled
- Served from `/operations` route

**No separate web server needed.**

### Asset Serving

Assets are embedded in the binary and served by the Go HTTP server:
- `/` - Static asset serving
- `/api/*` - REST API endpoints
- `/operations` - Operations Dashboard
- `/operations/alerts` - Alert Center
- `/operations/recovery` - Recovery Dashboard
- `/operations/trace` - Trace Explorer
- `/operations/dlq` - DLQ Console
- `/operations/reports` - Export Center

### Verifying Assets

**Check embedded assets:**
```bash
curl http://localhost:8080/operations
# Should return HTML page
```

**Check API availability:**
```bash
curl http://localhost:8080/api/orchestration/overview
# Should return JSON
```

---

## Deployment Methods

### Method 1: Systemd Service (Recommended for Production)

**Create service file `/etc/systemd/system/dso.service`:**

```ini
[Unit]
Description=DSO Execution Platform
After=network.target
StartLimitInterval=0

[Service]
Type=simple
Restart=always
RestartSec=10
StartLimitBurst=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=dso

# User configuration
User=dso
Group=dso

# Working directory
WorkingDirectory=/opt/dso

# Command
ExecStart=/opt/dso/dso --config /etc/dso/config.yaml

# Process limits
LimitNOFILE=65536
LimitNPROC=32768

# Security settings
PrivateTmp=yes
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/data/dso /var/log/dso

[Install]
WantedBy=multi-user.target
```

**Setup and start service:**
```bash
# Create dso user
useradd -r -m -d /opt/dso -s /bin/false dso

# Copy binary
cp dso /opt/dso/dso
chown dso:dso /opt/dso/dso
chmod 755 /opt/dso/dso

# Copy config
mkdir -p /etc/dso
cp config.yaml /etc/dso/config.yaml
chown dso:dso /etc/dso/config.yaml
chmod 600 /etc/dso/config.yaml

# Create data directory
mkdir -p /data/dso
chown dso:dso /data/dso
chmod 700 /data/dso

# Create log directory
mkdir -p /var/log/dso
chown dso:dso /var/log/dso
chmod 700 /var/log/dso

# Enable and start service
systemctl daemon-reload
systemctl enable dso
systemctl start dso

# Verify
systemctl status dso
```

**View logs:**
```bash
journalctl -u dso -f          # Follow logs
journalctl -u dso --since=-1h # Last hour
```

### Method 2: Manual Startup

**For development or manual operation:**

```bash
# Navigate to install directory
cd /opt/dso

# Start DSO
./dso --config /etc/dso/config.yaml

# Logs output to stdout
```

**Control-C to stop.**

### Method 3: Docker (Optional)

**Dockerfile for containerization:**

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o dso ./cmd/dso

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/dso .
EXPOSE 8080
VOLUME ["/data/dso"]
ENTRYPOINT ["./dso", "--config", "/etc/dso/config.yaml"]
```

**Build and run:**
```bash
docker build -t dso:0.9.0-rc1 .
docker run -d \
  --name dso \
  -p 8080:8080 \
  -v /data/dso:/data/dso \
  -v /etc/dso:/etc/dso \
  dso:0.9.0-rc1
```

---

## Upgrade & Backup Procedures

### Pre-Upgrade Backup

**Always backup before upgrading:**

```bash
# Stop DSO
systemctl stop dso

# Backup database
cp /data/dso/dso.db /backups/dso.db.backup.pre-upgrade.$(date +%Y%m%d_%H%M%S)

# Verify backup
sqlite3 /backups/dso.db.backup.pre-upgrade.* "PRAGMA integrity_check;"

# Restart
systemctl start dso
```

### Upgrade Procedure

**To upgrade to a new version:**

```bash
# 1. Backup database (see above)

# 2. Stop DSO
systemctl stop dso

# 3. Build/download new binary
go build -o dso-new ./cmd/dso
cp dso-new /opt/dso/dso

# 4. Restart DSO
systemctl start dso

# 5. Verify startup
sleep 5
systemctl status dso
curl http://localhost:8080/api/orchestration/overview

# 6. Verify functionality
# Open dashboard at http://localhost:8080/operations
# Check that executions can be created and run
```

### Rollback Procedure

**If upgrade fails:**

```bash
# 1. Stop DSO
systemctl stop dso

# 2. Restore database from backup
cp /backups/dso.db.backup.pre-upgrade.* /data/dso/dso.db

# 3. Restore previous binary
cp /opt/dso/dso.bak /opt/dso/dso  # If you saved it

# 4. Restart DSO
systemctl start dso

# 5. Verify
systemctl status dso
curl http://localhost:8080/api/orchestration/overview
```

**Alternatively, in case of severe issues:**
```bash
# Reinitialize empty database
rm /data/dso/dso.db
systemctl restart dso
# All state will be lost
```

---

## Reverse Proxy Configuration

### Nginx Configuration

**For production, use reverse proxy with TLS:**

```nginx
upstream dso_backend {
    server localhost:8080;
}

server {
    listen 443 ssl http2;
    server_name dso.example.com;

    ssl_certificate /etc/ssl/certs/dso.crt;
    ssl_certificate_key /etc/ssl/private/dso.key;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "SAMEORIGIN" always;

    # Logging
    access_log /var/log/nginx/dso_access.log;
    error_log /var/log/nginx/dso_error.log;

    # DSO proxy
    location / {
        proxy_pass http://dso_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # Buffering
        proxy_buffering on;
        proxy_buffer_size 4k;
        proxy_buffers 8 4k;
    }

    # WebSocket support (if needed in Phase 5)
    location /ws {
        proxy_pass http://dso_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}

# HTTP redirect to HTTPS
server {
    listen 80;
    server_name dso.example.com;
    return 301 https://$server_name$request_uri;
}
```

### Apache Configuration

**Alternative with Apache:**

```apache
<VirtualHost *:443>
    ServerName dso.example.com

    SSLEngine on
    SSLCertificateFile /etc/ssl/certs/dso.crt
    SSLCertificateKeyFile /etc/ssl/private/dso.key

    ProxyPreserveHost On
    ProxyPass / http://localhost:8080/
    ProxyPassReverse / http://localhost:8080/

    # Logging
    ErrorLog ${APACHE_LOG_DIR}/dso_error.log
    CustomLog ${APACHE_LOG_DIR}/dso_access.log combined
</VirtualHost>

<VirtualHost *:80>
    ServerName dso.example.com
    Redirect permanent / https://dso.example.com/
</VirtualHost>
```

---

## Monitoring & Health Checks

### Health Check Endpoint

**Check DSO is running:**

```bash
curl http://localhost:8080/api/orchestration/overview
```

**Expected response:** JSON with orchestration metrics

**Health check interval:** Every 30 seconds

### Systemd Health Checks

**Systemd monitors service automatically:**

```bash
systemctl status dso
```

### External Monitoring

**Integrate with monitoring systems:**

```bash
# Prometheus-style metrics
curl http://localhost:8080/api/orchestration/metrics
```

---

## Resource Usage

### Typical Resource Consumption

**Memory:**
- Base: ~50MB
- Per 100 queued executions: +10MB
- Per connected client: +1-2MB

**Disk:**
- Binary: ~15-20MB (depending on build options)
- Database: Grows with execution history
- Typical: 100-500MB per million executions

**CPU:**
- Idle: <1%
- Processing queue: 5-30%
- Dashboard queries: 2-5%

### Resource Limits (Systemd)

Set in systemd service file:

```ini
# Max open file descriptors
LimitNOFILE=65536

# Max processes
LimitNPROC=32768
```

---

## Troubleshooting Deployment

### Port Already in Use

**Error:** "listen tcp :8080: bind: address already in use"

**Solution:**
```bash
# Find process using port
lsof -i :8080

# Kill process (if not DSO)
kill -9 <PID>

# Or use different port in config.yaml
```

### Database Locked

**Error:** "database is locked"

**Solution:**
1. Check if multiple instances running
2. Stop all instances: `systemctl stop dso`
3. Wait 30 seconds
4. Check process: `lsof | grep dso.db`
5. Restart: `systemctl start dso`

### Permission Denied

**Error:** "permission denied: /data/dso/dso.db"

**Solution:**
```bash
# Verify ownership
ls -l /data/dso/

# Fix permissions
chown dso:dso /data/dso/dso.db
chmod 600 /data/dso/dso.db
```

### Out of Memory

**If DSO crashes due to memory:**

```yaml
# Reduce in config.yaml
database:
  max_connections: 5  # Was 10

queue:
  max_queue_depth: 100  # Reduce queue size
```

---

## Performance Validation

### Load Testing

**Basic performance test:**

```bash
# Check dashboard response time
time curl http://localhost:8080/api/operations/dashboard

# Check trace response time
time curl http://localhost:8080/api/orchestration/trace/test-id

# Check worker lookup time
time curl http://localhost:8080/api/orchestration/workers
```

**Expected times:**
- Dashboard: <500ms
- Trace: <200ms
- Workers: <100ms

---

## Security Hardening

### Network Isolation

**Use firewall to restrict access:**

```bash
# UFW (Ubuntu/Debian)
ufw allow 22/tcp      # SSH
ufw allow 443/tcp     # HTTPS (reverse proxy)
ufw deny 8080         # Block direct DSO access

# iptables (Linux)
iptables -A INPUT -p tcp --dport 8080 -j DROP
```

### File Permissions

**Secure configuration and database:**

```bash
chmod 600 /etc/dso/config.yaml
chmod 600 /data/dso/dso.db
chown dso:dso /etc/dso/config.yaml
chown dso:dso /data/dso/dso.db
```

### TLS/HTTPS

**Phase 5 will add native TLS support.**

For now, use reverse proxy (Nginx/Apache) with TLS certificates.

---

## Compliance & Auditing

### Audit Trail

DSO maintains complete audit trail:
- All executions logged
- All state changes tracked
- CorrelationID linking for traceability
- Exported via Export Center

### Log Retention

**Configure log rotation:**

```bash
# /etc/logrotate.d/dso
/var/log/dso/*.log {
    daily
    rotate 30
    compress
    delaycompress
    notifempty
    create 0600 dso dso
    sharedscripts
    postrotate
        systemctl reload dso > /dev/null 2>&1 || true
    endscript
}
```

### Data Retention

**Manage database size:**

```bash
# Export and archive old data
./dso-export-tool --since=2026-01-01 --output=archive.json

# Clean up old data (in Phase 5)
# Currently all data retained
```

---

## Support & Troubleshooting

### Common Issues Checklist

- [ ] Port 8080 available?
- [ ] `/data/dso` directory writable?
- [ ] Sufficient disk space?
- [ ] Correct permissions?
- [ ] Database not corrupted?
- [ ] No other DSO instance running?

### Getting Help

1. Check logs: `journalctl -u dso -n 100`
2. Check database: `sqlite3 /data/dso/dso.db ".tables"`
3. Verify API: `curl http://localhost:8080/api/orchestration/overview`
4. Report issue with logs and config (without secrets)

---

**DSO Deployment Guide - v0.9.0-rc1 Edition**

For more information, see: OPERATOR_GUIDE.md, RELEASE_NOTES.md