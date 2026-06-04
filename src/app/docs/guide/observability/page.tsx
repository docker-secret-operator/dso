import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Observability & Monitoring",
  description: "Monitoring, metrics, logging, and observability setup for Docker Secret Operator."
};

export default function ObservabilityPage() {
  return (
    <div>
      <h1>Observability & Monitoring</h1>

      <p>
        Setup monitoring, logging, metrics, and alerting for Docker Secret Operator to ensure visibility into secret rotation operations.
      </p>

      <h2>Three Pillars of Observability</h2>

      <table>
        <thead>
          <tr>
            <th>Pillar</th>
            <th>Purpose</th>
            <th>Tools</th>
            <th>Retention</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Logging</strong></td>
            <td>Detailed events and errors</td>
            <td>journald, ELK, Splunk, CloudWatch</td>
            <td>7-30 days (operational)</td>
          </tr>
          <tr>
            <td><strong>Metrics</strong></td>
            <td>Time-series performance data</td>
            <td>Prometheus, Grafana, CloudWatch</td>
            <td>1-2 years (historical)</td>
          </tr>
          <tr>
            <td><strong>Tracing</strong></td>
            <td>Request flow across components</td>
            <td>Jaeger, Zipkin, DataDog (optional)</td>
            <td>7-30 days (debug)</td>
          </tr>
        </tbody>
      </table>

      <h2>Structured Logging</h2>

      <h3>Enable Structured Logging</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  structured_logging: true
  log_format: "json"  # JSON format for parsing
  log_output: "journald"  # systemd journal
  log_level: "info"  # debug, info, warn, error
      </code></pre>

      <h3>Log Entries</h3>

      <p>
        Example structured log entry:
      </p>

      <pre><code className="language-json">
{
  "timestamp": "2025-03-15T10:30:45.123Z",
  "level": "info",
  "component": "rotation",
  "event": "rotation_started",
  "rotation_id": "rot-abc123",
  "service": "api",
  "secrets_count": 2,
  "duration_ms": 0,
  "status": "pending",
  "traces": {
    "provider": "aws-secrets-manager",
    "region": "us-east-1"
  }
}
      </code></pre>

      <h3>Log Levels</h3>

      <ul>
        <li><strong>DEBUG:</strong> Detailed diagnostic info (don't use in production)</li>
        <li><strong>INFO:</strong> Normal operational messages (default)</li>
        <li><strong>WARN:</strong> Warning conditions (needs investigation)</li>
        <li><strong>ERROR:</strong> Error conditions (needs immediate attention)</li>
      </ul>

      <h3>Accessing Logs</h3>

      <pre><code className="language-bash">
# View recent logs
sudo journalctl -u dso-agent -n 20

# Follow logs (tail -f)
sudo journalctl -u dso-agent -f

# Filter by level
sudo journalctl -u dso-agent -p err  # Errors only
sudo journalctl -u dso-agent -p warn  # Warnings and above

# Filter by time
sudo journalctl -u dso-agent --since "2 hours ago"
sudo journalctl -u dso-agent -S 2025-03-15

# Search logs
sudo journalctl -u dso-agent | grep "rotation_failed"

# Output as JSON (for parsing)
sudo journalctl -u dso-agent -o json | jq '.MESSAGE'
      </code></pre>

      <h2>Centralized Logging</h2>

      <h3>Setup: Systemd Log Forwarding</h3>

      <p>
        Forward DSO logs to external logging system.
      </p>

      <h4>Option 1: Rsyslog</h4>

      <pre><code className="language-bash">
# /etc/rsyslog.d/60-dso.conf
:programname, isequal, "dso-agent" @@logs.example.com:514

# Test configuration
sudo rsyslogd -N1

# Restart
sudo systemctl restart rsyslog
      </code></pre>

      <h4>Option 2: Filebeat (ELK)</h4>

      <pre><code className="language-yaml">
# /etc/filebeat/filebeat.yml
filebeat.inputs:
  - type: journald
    enabled: true

processors:
  - add_fields:
      target: dso
      fields:
        service: dso-agent
        environment: production

output.elasticsearch:
  hosts: ["elasticsearch.internal:9200"]
  index: "dso-logs-%{+yyyy.MM.dd}"
      </code></pre>

      <h4>Option 3: CloudWatch (AWS)</h4>

      <pre><code className="language-bash">
# Install CloudWatch agent
wget https://s3.amazonaws.com/amazoncloudwatch-agent/linux/amd64/latest/amazon-cloudwatch-agent.rpm
sudo rpm -U ./amazon-cloudwatch-agent.rpm

# Configure
sudo tee /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json << 'EOF'
{
  "logs": {
    "logs_collected": {
      "files": {
        "collect_list": [
          {
            "file_path": "/var/log/dso/*.log",
            "log_group_name": "/dso/agent",
            "log_stream_name": "production"
          }
        ]
      }
    }
  }
}
EOF

# Start agent
sudo systemctl start amazon-cloudwatch-agent
      </code></pre>

      <h2>Metrics & Monitoring</h2>

      <h3>Enable Metrics</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: "/metrics"
  metrics_collection_interval_seconds: 15
      </code></pre>

      <h3>Available Metrics</h3>

      <table>
        <thead>
          <tr>
            <th>Metric</th>
            <th>Type</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>dso_rotation_total</code></td>
            <td>Counter</td>
            <td>Total rotations completed</td>
          </tr>
          <tr>
            <td><code>dso_rotation_failed_total</code></td>
            <td>Counter</td>
            <td>Total failed rotations</td>
          </tr>
          <tr>
            <td><code>dso_rotation_duration_seconds</code></td>
            <td>Histogram</td>
            <td>Rotation duration in seconds</td>
          </tr>
          <tr>
            <td><code>dso_rotation_health_check_failures</code></td>
            <td>Counter</td>
            <td>Health check failures during rotation</td>
          </tr>
          <tr>
            <td><code>dso_agent_uptime_seconds</code></td>
            <td>Gauge</td>
            <td>Agent uptime in seconds</td>
          </tr>
          <tr>
            <td><code>dso_lock_acquisition_failures</code></td>
            <td>Counter</td>
            <td>Failed lock acquisitions</td>
          </tr>
          <tr>
            <td><code>dso_provider_latency_seconds</code></td>
            <td>Histogram</td>
            <td>Provider API latency</td>
          </tr>
          <tr>
            <td><code>dso_container_count</code></td>
            <td>Gauge</td>
            <td>Currently managed containers</td>
          </tr>
          <tr>
            <td><code>dso_memory_bytes</code></td>
            <td>Gauge</td>
            <td>Agent memory usage</td>
          </tr>
        </tbody>
      </table>

      <h3>Prometheus Setup</h3>

      <h4>Configuration</h4>

      <pre><code className="language-yaml">
# /etc/prometheus/prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'dso-agent'
    static_configs:
      - targets: ['localhost:9090']
    metrics_path: '/metrics'

  # Optional: Monitor multiple DSO agents
  - job_name: 'dso-agents-cluster'
    static_configs:
      - targets:
        - 'dso-host-1:9090'
        - 'dso-host-2:9090'
        - 'dso-host-3:9090'
      </code></pre>

      <h4>Verify Metrics Collection</h4>

      <pre><code className="language-bash">
# View metrics endpoint
curl http://localhost:9090/metrics

# Should show Prometheus-formatted metrics
# HELP dso_rotation_total Total rotations completed
# TYPE dso_rotation_total counter
# dso_rotation_total{service="api"} 42
      </code></pre>

      <h2>Grafana Dashboards</h2>

      <h3>Create Dashboard</h3>

      <p>
        Import or create dashboard with panels:
      </p>

      <h4>Panel 1: Rotation Status</h4>

      <pre><code className="language-text">
Title: "Rotations per Hour"
Query: rate(dso_rotation_total[1h])
Type: Graph
      </code></pre>

      <h4>Panel 2: Failure Rate</h4>

      <pre><code className="language-text">
Title: "Rotation Failure Rate"
Query: rate(dso_rotation_failed_total[1h])
Type: Stat
Threshold: Red if > 0
      </code></pre>

      <h4>Panel 3: Rotation Duration</h4>

      <pre><code className="language-text">
Title: "Avg Rotation Duration"
Query: avg(dso_rotation_duration_seconds)
Type: Stat
Unit: seconds
      </code></pre>

      <h4>Panel 4: Agent Uptime</h4>

      <pre><code className="language-text">
Title: "Agent Uptime"
Query: dso_agent_uptime_seconds / 3600
Type: Stat
Unit: hours
Decimals: 1
      </code></pre>

      <h3>Example Queries</h3>

      <pre><code className="language-text">
# Rotations by service (past 24h)
sum by (service) (increase(dso_rotation_total[24h]))

# Success rate
rate(dso_rotation_total[5m]) / rate(dso_rotation_failed_total[5m])

# P95 rotation duration
histogram_quantile(0.95, rate(dso_rotation_duration_seconds_bucket[5m]))

# Provider latency SLO (target 100ms)
rate(dso_provider_latency_seconds_sum[5m]) / rate(dso_provider_latency_seconds_count[5m])
      </code></pre>

      <h2>Health Checks</h2>

      <h3>Enable Health Endpoint</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  health_check_enabled: true
  health_check_port: 8081
  health_check_path: "/health"
      </code></pre>

      <h3>Health Check Response</h3>

      <pre><code className="language-bash">
curl http://localhost:8081/health
# Output:
# {
#   "status": "healthy",
#   "uptime_seconds": 3600,
#   "rotation_in_progress": false,
#   "last_rotation": "2025-03-15T10:30:00Z",
#   "provider_status": "reachable",
#   "container_count": 3,
#   "error_count": 0
# }
      </code></pre>

      <h3>Kubernetes Readiness Probe (if applicable)</h3>

      <pre><code className="language-yaml">
# Only if running in Kubernetes (not typical for DSO)
readinessProbe:
  httpGet:
    path: /health
    port: 8081
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 5

livenessProbe:
  httpGet:
    path: /health
    port: 8081
  initialDelaySeconds: 30
  periodSeconds: 30
  timeoutSeconds: 5
      </code></pre>

      <h2>Alerting Rules</h2>

      <h3>Prometheus Alerting</h3>

      <pre><code className="language-yaml">
# /etc/prometheus/alert.rules.yml
groups:
  - name: dso_alerts
    interval: 30s
    rules:
      # Critical: Any rotation failure
      - alert: DSO_RotationFailed
        expr: increase(dso_rotation_failed_total[5m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "DSO rotation failed"
          description: "Rotation failed for service {{ $labels.service }}"

      # Warning: High failure rate
      - alert: DSO_HighFailureRate
        expr: rate(dso_rotation_failed_total[5m]) / rate(dso_rotation_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "DSO failure rate > 10%"

      # Critical: Agent down
      - alert: DSO_AgentDown
        expr: up{job="dso-agent"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "DSO agent is down"

      # Warning: Provider unreachable
      - alert: DSO_ProviderUnreachable
        expr: dso_provider_errors_total[5m] > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Provider unreachable from DSO agent"

      # Warning: High rotation latency
      - alert: DSO_HighLatency
        expr: histogram_quantile(0.95, rate(dso_rotation_duration_seconds_bucket[5m])) > 120
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Rotation latency > 2 minutes"
      </code></pre>

      <h3>Alertmanager Configuration</h3>

      <pre><code className="language-yaml">
# /etc/alertmanager/config.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'service']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'default'
  routes:
    - match:
        severity: critical
      receiver: 'pagerduty'
      repeat_interval: 1h

receivers:
  - name: 'default'
    slack_configs:
      - api_url: 'https://hooks.slack.com/...'
        channel: '#dso-alerts'

  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: 'pagerduty-service-key'
      </code></pre>

      <h2>Audit Logging</h2>

      <h3>Enable Audit Logging</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  audit_enabled: true
  audit_log_path: "/var/log/dso/audit.log"
  audit_log_format: "json"
  audit_max_size_mb: 50
  audit_max_backups: 10
  audit_max_age_days: 365  # Keep 1 year for compliance
      </code></pre>

      <h3>Audit Log Entry</h3>

      <pre><code className="language-json">
{
  "timestamp": "2025-03-15T10:30:45Z",
  "event": "rotation_completed",
  "user": "system",
  "action": "secret_rotation",
  "service": "api",
  "status": "success",
  "secrets_updated": ["DATABASE_PASSWORD", "API_KEY"],
  "old_container_id": "abc123...",
  "new_container_id": "xyz789...",
  "duration_ms": 45000,
  "health_check_passed": true
}
      </code></pre>

      <h3>Audit Log Analysis</h3>

      <pre><code className="language-bash">
# Find all failed rotations
grep '"status":"failed"' /var/log/dso/audit.log

# Find rotations for specific service
grep '"service":"api"' /var/log/dso/audit.log

# Count rotations per day
jq '.timestamp' /var/log/dso/audit.log | grep "^\"2025-03-15" | wc -l

# Export for compliance review
jq -s '.' /var/log/dso/audit.log > audit-review.json
      </code></pre>

      <h2>Observability Checklist</h2>

      <ul>
        <li>☐ Structured logging enabled (JSON format)</li>
        <li>☐ Log aggregation configured (ELK/Splunk/CloudWatch)</li>
        <li>☐ Metrics collection enabled (Prometheus port 9090)</li>
        <li>☐ Grafana dashboard created (key metrics visible)</li>
        <li>☐ Prometheus alerts configured (rotation failures, provider issues)</li>
        <li>☐ Alertmanager configured (Slack/PagerDuty/Email)</li>
        <li>☐ Audit logging enabled (compliance retention)</li>
        <li>☐ Health endpoint monitored</li>
        <li>☐ Retention policies set (logs, metrics, audit)</li>
        <li>☐ Team trained on dashboards and alerts</li>
      </ul>

      <h2>Key Metrics to Monitor</h2>

      <p>
        Focus on these metrics:
      </p>

      <ul>
        <li><strong>Rotation Success Rate:</strong> Should be > 99%</li>
        <li><strong>Rotation Duration:</strong> Should be < 2 minutes typically</li>
        <li><strong>Agent Uptime:</strong> Should be > 99.9%</li>
        <li><strong>Provider Latency:</strong> Should be < 500ms</li>
        <li><strong>Health Check Pass Rate:</strong> Should be 100%</li>
        <li><strong>Container Churn:</strong> Should only happen on rotation</li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/production-readiness">Production readiness checklist</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting guide</a></li>
        <li><a href="/docs/guide/best-practices">Best practices</a></li>
      </ul>
    </div>
  );
}
