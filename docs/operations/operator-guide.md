# DSO Operator Guide

**Version:** 0.9.0-rc1  
**Date:** 2026-06-05  
**Status:** Release Candidate

---

## Introduction

This guide provides operators with step-by-step instructions for running, configuring, and operating the DSO Execution Platform. The platform provides a complete execution orchestration system with persistence, resilience, and operational visibility.

**Audience:** Platform operators, system administrators, DevOps engineers

---

## Prerequisites

### System Requirements
- **OS:** Linux or macOS (x86-64)
- **CPU:** 1 core minimum (2+ cores recommended)
- **RAM:** 256MB minimum (512MB+ recommended)
- **Disk:** 1GB minimum for binary and database
- **Go:** 1.21+ (for building from source)

### Network Requirements
- **Port:** 8080 (configurable, used for REST API and frontend)
- **Outbound HTTPS:** Optional (for future provider integrations)

### Knowledge Requirements
- Basic understanding of REST APIs
- Familiarity with JSON configuration
- Linux/Unix command line usage
- Understanding of execution/automation concepts

---

## Startup & Configuration

### Starting DSO

**Build from source:**
```bash
go build -o dso ./cmd/dso
```

**Run with default configuration:**
```bash
./dso
```

**Run with custom configuration:**
```bash
./dso --config /path/to/config.yaml
```

**Expected output:**
```
2026-06-05 10:00:00 INFO DSO starting
2026-06-05 10:00:00 INFO Database initialization (SQLite)
2026-06-05 10:00:00 INFO Running migrations...
2026-06-05 10:00:00 INFO Migration 0001: created tables
2026-06-05 10:00:00 INFO Migration 0002: added indexes
...
2026-06-05 10:00:00 INFO Migration 0011: audit event persistence
2026-06-05 10:00:00 INFO All migrations complete
2026-06-05 10:00:00 INFO REST API listening on :8080
2026-06-05 10:00:00 INFO Dashboard available at http://localhost:8080/operations
```

### Configuration File

**Default location:** `config.yaml` (current directory)

**Example configuration:**
```yaml
# Server configuration
server:
  port: 8080
  host: 0.0.0.0
  tls_enabled: false

# Database configuration
database:
  path: ./dso.db
  max_connections: 10
  timeout: 30s

# Worker configuration
workers:
  health_check_interval: 30s
  heartbeat_timeout: 60s
  max_concurrent_executions: 5

# Queue configuration
queue:
  max_retry_count: 3
  ttl_default: 24h
  max_queue_depth: 1000

# Execution configuration
execution:
  default_timeout: 60m
  step_timeout: 15m
  failure_injection:
    low_risk: 0.02    # 2%
    medium_risk: 0.05  # 5%
    high_risk: 0.10    # 10%

# Alert configuration
alerts:
  thresholds:
    failure_rate: 0.10       # Alert if >10% failures
    queue_depth: 500         # Alert if >500 queued
    worker_unhealthy: 0.50   # Alert if >50% unhealthy
```

### First Run Initialization

On first run, DSO automatically:
1. Creates SQLite database at configured path
2. Runs all 11 migrations to initialize schema
3. Validates database integrity
4. Initializes empty worker registry
5. Initializes empty execution queue
6. Starts REST API server
7. Loads frontend assets

**No manual initialization required.**

---

## Dashboard Navigation

### Accessing the Dashboard

Open your browser and navigate to:
```
http://localhost:8080/operations
```

**Note:** Replace `localhost` with your server's IP if accessing remotely.

### Dashboard Pages

**1. Overview Dashboard** (`/operations`)
- Main operational view
- Real-time KPI cards
- Queue and worker health
- Execution status distribution
- System health score

**KPI Cards:**
- **Success Rate** - Percentage of executions that completed successfully
- **Failure Rate** - Percentage of executions that failed
- **Throughput** - Executions per second
- **Avg Duration** - Average execution time
- **Queue Depth** - Current items in execution queue
- **Worker Health** - Percentage of healthy workers
- **System Health** - Overall system score (0-100)
- **Recovery Rate** - Percentage of automatic recoveries successful

**Auto-Refresh:** Dashboard updates every 10 seconds automatically.

**2. Alert Center** (`/operations/alerts`)
- Active alerts and notifications
- Severity filtering (critical, warning, info)
- Alert dismissal (persisted locally)
- Status summary cards
- Search capability

**Alert Types:**
- **Critical** - Immediate action required (red)
- **Warning** - Attention needed (orange)
- **Info** - Informational only (blue)

**To dismiss an alert:** Click the alert card, then click "Dismiss". Dismissed alerts are remembered in browser localStorage.

**3. Recovery Dashboard** (`/operations/recovery`)
- Complete recovery event timeline
- Event type filtering
- Time range filtering
- Search by Correlation ID or Execution ID
- Detailed event information

**Event Types:**
- **Worker Failure** (🔴) - Worker health check failed
- **Queue Recovery** (🔄) - Queue state restored after restart
- **Execution Cancelled** (❌) - Execution was manually cancelled
- **Execution Paused** (⏸️) - Execution was manually paused
- **Execution Resumed** (▶️) - Paused execution was resumed

**Filters:**
- **Event Type** - Filter by specific event category
- **Time Range** - All time, Today, or Last 7 days
- **Search** - Find events by Correlation ID or Execution ID

**To view event details:** Click "Details" button or click the event card. The detail drawer shows:
- Event ID
- Complete event description
- Associated Correlation ID
- Associated Execution ID
- Associated Worker ID (if applicable)
- Exact timestamp
- Link to full execution trace

**4. Trace Explorer** (`/operations/trace`)
- End-to-end execution tracing
- Search by Correlation ID
- Timeline visualization
- Status transition tracking
- Failure details and root cause

**To search for a trace:**
1. Click on the search box
2. Enter Correlation ID (example: `corr-123abc`)
3. Click "Search" or press Enter

**Timeline shows:**
- All events related to the execution
- Event timestamps and ordering
- Status transitions (pending → validated → planned → queued → running → completed)
- Event details for each item
- Failure information if execution failed

**Failure details include:**
- Failure type
- Failure reason
- Stack trace (if available)
- Step that failed
- Recommendations for recovery

**5. DLQ Console** (`/operations/dlq`)
- Dead Letter Queue (failed executions)
- Failure reason breakdown
- Search and sort controls
- Retryable vs permanent status
- JSON export capability

**DLQ Statistics:**
- **Total Items** - Total failed executions in DLQ
- **Retryable** - Items that can be retried
- **Permanent** - Items that cannot be retried
- **Oldest Item** - Age of oldest failed execution
- **Status** - Current DLQ health

**Search & Sort:**
- Search by Execution ID or Correlation ID
- Sort by failure reason, timestamp, or age
- Filter by retryable status

**Export DLQ:**
1. Click "Export" button
2. JSON file downloads with timestamp
3. Use for compliance reporting or analysis

**6. Export Center** (`/operations/reports`)
- Export operational data
- Four export types available
- JSON format with timestamps
- Use for compliance and analysis

**Export Types:**

**Operations Summary**
- Current KPIs snapshot
- Queue health metrics
- Worker health metrics
- System health score
- Execution status distribution

**Recovery Report**
- Recovery event history
- Worker failure statistics
- Queue recovery count
- Execution cancellation count
- Recovery success rate

**Dead Letter Queue Report**
- DLQ item count
- Failure reason breakdown
- Retryable vs permanent split
- Age analysis
- Recommendations

**Alert Report**
- Active alerts
- Alert counts by severity
- Alert history
- Threshold configurations
- Remediation steps

**To export:**
1. Click "Export JSON" on desired report type
2. JSON file downloads to Downloads folder
3. Filename includes timestamp for versioning

---

## Operation Procedures

### Creating Executions

**Via API:**
```bash
curl -X POST http://localhost:8080/api/executions \
  -H "Content-Type: application/json" \
  -d '{
    "correlation_id": "corr-unique-id",
    "priority": "normal",
    "steps": [
      {
        "name": "step-1",
        "action": "verify",
        "timeout": "5m",
        "risk_level": "low"
      },
      {
        "name": "step-2",
        "action": "execute",
        "timeout": "10m",
        "risk_level": "medium"
      }
    ]
  }'
```

**Expected response:**
```json
{
  "execution_id": "exec-123abc",
  "correlation_id": "corr-unique-id",
  "status": "validated",
  "created_at": "2026-06-05T10:00:00Z"
}
```

**Monitor in dashboard:** Once created, execution appears in the Overview dashboard within 10 seconds.

### Monitoring Executions

**In Dashboard:**
1. Open Overview dashboard
2. Check "Queue Depth" card
3. Watch "Execution Status Distribution"
4. Monitor success/failure rates

**Via API:**
```bash
curl http://localhost:8080/api/executions
```

**View execution details:**
1. Go to Trace Explorer
2. Enter Correlation ID
3. View complete timeline and status

### Cancelling Executions

**Via Dashboard:**
1. Go to Trace Explorer
2. Search for Correlation ID
3. Click "Cancel" (if execution is queued/running)

**Via API:**
```bash
curl -X POST http://localhost:8080/api/executions/{execution_id}/cancel
```

**Result:**
- Execution status changes to "cancelled"
- Audit event logged
- Recovery event created
- Resources cleaned up

### Pausing & Resuming Executions

**Pause execution:**
```bash
curl -X POST http://localhost:8080/api/executions/{execution_id}/pause
```

**Resume execution:**
```bash
curl -X POST http://localhost:8080/api/executions/{execution_id}/resume
```

**Use cases:**
- Pause before risky step for manual review
- Resume after external validation
- Investigate failure before continuing

### Handling Failed Executions

**Detect failures:**
1. Check Alert Center for failure alerts
2. Check DLQ Console for failed items
3. Check Dashboard for failure rate spike

**Investigate failure:**
1. Go to DLQ Console
2. Find execution in list
3. Click to view failure details
4. Check failure reason and recommendations

**Remediate:**
1. Review failure reason (transient vs permanent)
2. Check worker health (was worker unhealthy?)
3. Check queue depth (was system overloaded?)
4. Check logs for detailed error
5. Retry if transient, escalate if permanent

### Worker Health Monitoring

**View worker status:**
1. Go to Overview dashboard
2. Check "Worker Health" card
3. View healthy vs unhealthy count

**Investigate unhealthy worker:**
```bash
curl http://localhost:8080/api/orchestration/workers
```

**Response includes:**
- Worker ID and registration time
- Last heartbeat time
- Health status
- Current load
- Failure count

**If worker is unhealthy:**
1. Check Recovery Dashboard for worker failure events
2. Review system logs
3. Consider restart if persistent
4. Check for resource exhaustion

### Queue Management

**Monitor queue depth:**
1. Dashboard shows current queue depth
2. Alerts fire if depth exceeds threshold
3. Average wait time shown

**If queue is backing up:**
1. Check if workers are healthy
2. Check if executions are hanging
3. Investigate slow executions
4. Consider increasing worker capacity

**Queue recovery on restart:**
- Queued items are automatically restored
- High-priority items requeued first
- No manual intervention required

---

## Recovery & Resilience

### Automatic Recovery Features

**Worker Failure Recovery**
- Workers with failed heartbeats are marked unhealthy
- Executions automatically requeue if worker fails
- Recovery events logged to Recovery Dashboard

**Queue Recovery on Restart**
- Queued/running executions restored on startup
- High-priority items restored first
- State validated before restart

**Timeout Handling**
- Step timeouts: Individual step timeout
- Execution timeouts: Entire execution timeout
- Worker timeouts: 30-second heartbeat window

**Automatic Rollback**
- In-progress executions older than 5 minutes auto-rolled back
- Orphaned containers detected and cleaned
- State marked for operator review if needed

### Manual Recovery Procedures

**Restart DSO after crash:**
1. Check logs for error details
2. Address root cause if known
3. Restart: `./dso --config config.yaml`
4. Verify startup: Check logs for "REST API listening"
5. Check Recovery Dashboard for recovery events
6. Verify executions restored from queue

**Recover single execution:**
1. Go to DLQ Console
2. Find execution in failed items
3. Review failure reason
4. If transient, retry
5. If permanent, escalate or manual intervention

**Clear stuck execution:**
1. Identify stuck execution (no status change for hours)
2. Get Execution ID
3. Call: `curl -X POST /api/executions/{id}/cancel`
4. Verify in Recovery Dashboard
5. Investigate root cause in logs

### Audit & Compliance

**View complete audit trail:**
1. Go to Trace Explorer
2. Search for Correlation ID
3. View all events in timeline
4. Export for compliance (Export Center)

**Generate compliance reports:**
1. Go to Export Center
2. Select appropriate report type
3. Download JSON
4. Archive for audit trail

**Audit events tracked:**
- Execution created
- Execution validated
- Execution planned
- Execution queued
- Execution started
- Execution completed
- Execution failed
- Execution cancelled
- Execution paused
- Execution resumed
- Worker registered
- Worker health status changed
- Recovery event

---

## Performance Tuning

### Configuration Tuning

**Increase throughput:**
```yaml
workers:
  max_concurrent_executions: 10  # Increase if CPU available

queue:
  max_queue_depth: 5000  # Increase if memory available
```

**Decrease latency:**
```yaml
server:
  port: 8080
  # Add caching layer in reverse proxy

workers:
  health_check_interval: 15s  # More frequent checks
```

**Optimize resource usage:**
```yaml
database:
  max_connections: 5  # Reduce if memory constrained

execution:
  default_timeout: 30m  # Shorter for lower resource usage
```

### Database Optimization

**Monitor database size:**
```bash
du -h ./dso.db
```

**Vacuum database (cleanup):**
```bash
sqlite3 ./dso.db "VACUUM;"
```

**Check database integrity:**
```bash
sqlite3 ./dso.db "PRAGMA integrity_check;"
```

### API Response Time

**Dashboard APIs typically respond in <500ms:**
- Overview: ~300ms
- Trace: ~80-90ms
- Metrics: ~150ms
- Workers: ~50ms

**If response time degrading:**
1. Check database file size
2. Check system CPU/RAM usage
3. Consider database cleanup
4. Check for stuck executions

---

## Troubleshooting

### DSO fails to start

**Error: "database is locked"**
- Another instance running
- Solution: Kill other processes or change db path

**Error: "port already in use"**
- Port 8080 in use
- Solution: `./dso --config config.yaml` with different port

**Error: "migration failed"**
- Database corruption
- Solution: Back up `dso.db`, delete it, restart to reinitialize

### Dashboard not loading

**Blank page or 404:**
- API not running
- Solution: Check API logs, restart DSO

**Data not updating:**
- Auto-refresh stalled
- Solution: Refresh browser page (F5)

**Alerts not appearing:**
- Threshold not met
- Solution: Check threshold configuration, verify execution failure

### Executions not progressing

**Stuck in "queued" state:**
- No healthy workers
- Solution: Check Worker Health card, verify worker status

**Stuck in "running" state:**
- Worker crash or hang
- Solution: Check Recovery Dashboard, restart worker, cancel execution

**Constant failures:**
- Check failure reason in DLQ
- Check system resources (CPU, RAM, disk)
- Check worker logs

---

## Operational Best Practices

### Backup & Recovery

**Backup database:**
```bash
cp dso.db dso.db.backup.$(date +%Y%m%d_%H%M%S)
```

**Keep backups for at least 7 days for compliance.**

**Restore from backup:**
1. Stop DSO
2. Replace `dso.db` with backup
3. Start DSO
4. Verify data in dashboard

### Monitoring & Alerting

**Key metrics to watch:**
- Failure rate (should be <5% under normal conditions)
- Queue depth (should be <100 most of the time)
- Worker health (should be >95% healthy)
- Average execution duration

**Set up external monitoring:**
- Poll `/api/orchestration/metrics` every 60 seconds
- Export metrics to monitoring system (Prometheus, DataDog, etc.)
- Set alerts for threshold violations

### Regular Maintenance

**Daily:**
- Check Alert Center
- Monitor Dashboard KPIs
- Review failure rate

**Weekly:**
- Export reports for compliance
- Review DLQ for patterns
- Check database size

**Monthly:**
- Backup database
- Review audit trail
- Plan capacity for Phase 5

---

## Upgrading DSO

**RC1 to GA (General Availability):**
1. Back up `dso.db`
2. Stop current instance
3. Update binary: `go build -o dso ./cmd/dso`
4. Start new version: `./dso --config config.yaml`
5. Verify migrations applied automatically
6. Test critical executions

**No breaking changes between RC1 and GA expected.**

---

## Security Considerations

### Network Access

**Dashboard access:**
- DSO listens on port 8080 by default
- Use firewall to restrict access to trusted IPs
- Use reverse proxy with authentication for production

**API access:**
- All APIs are currently unauthenticated (Phase 5 will add auth)
- Use network isolation or VPN for production
- Authentication/authorization coming in Phase 5

### Data Protection

**Database:**
- `dso.db` contains execution history and audit trail
- Restrict file permissions: `chmod 600 dso.db`
- Back up regularly for compliance

**Configuration:**
- `config.yaml` may contain sensitive settings
- Restrict file permissions: `chmod 600 config.yaml`
- Do not commit to version control

---

## Support & Resources

### Documentation
- See RELEASE_NOTES.md for features
- See DEPLOYMENT_GUIDE.md for production setup
- See CHANGELOG.md for version history

### Troubleshooting
1. Check logs (stdout/stderr)
2. Review Recovery Dashboard for events
3. Check DLQ Console for failure reasons
4. Review Trace Explorer for execution timeline

### Reporting Issues
- Collect logs: All startup messages and errors
- Capture error time and Correlation ID
- Note system configuration
- Provide reproduction steps

---

**DSO Operator Guide - v0.9.0-rc1 Edition**

For more information, see: DEPLOYMENT_GUIDE.md, RELEASE_NOTES.md, CHANGELOG.md