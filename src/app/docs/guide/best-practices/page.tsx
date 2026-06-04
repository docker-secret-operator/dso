import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Best Practices",
  description: "Operational best practices, patterns, and recommendations for Docker Secret Operator."
};

export default function BestPracticesPage() {
  return (
    <div>
      <h1>Best Practices</h1>

      <p>
        Operational best practices for running DSO safely and efficiently in production.
      </p>

      <h2>Secret Management Practices</h2>

      <h3>1. Use Consistent Naming Conventions</h3>

      <p>
        Establish and enforce a naming convention for secrets:
      </p>

      <pre><code className="language-text">
Recommended pattern: {environment}/{service}/{secret-type}

Examples:
prod/api/database-password
prod/api/jwt-secret
prod/worker/kafka-password
staging/api/database-password
dev/api/api-key
      </code></pre>

      <p>
        Benefits:
      </p>

      <ul>
        <li>Easy to identify secret ownership and scope</li>
        <li>Simple to audit and rotate by service</li>
        <li>Clear environment separation (prod vs staging)</li>
        <li>Facilitates IAM policy scoping (e.g., <code>prod/*</code>)</li>
      </ul>

      <h3>2. Rotate Secrets Regularly</h3>

      <p>
        Establish a rotation schedule:
      </p>

      <ul>
        <li><strong>Database passwords:</strong> Every 90 days</li>
        <li><strong>API keys:</strong> Every 180 days</li>
        <li><strong>OAuth tokens:</strong> Every 30 days (if long-lived)</li>
        <li><strong>TLS certificates:</strong> Before expiration (auto-rotated recommended)</li>
        <li><strong>After personnel changes:</strong> Immediately rotate credentials</li>
      </ul>

      <p>
        DSO makes rotation painless. When you update a secret in the provider:
      </p>

      <pre><code className="language-bash">
# Update secret
aws secretsmanager update-secret \
  --secret-id prod/api/database-password \
  --secret-string "new-password-$(date +%s)"

# DSO automatically detects and rotates
# No manual intervention needed
      </code></pre>

      <h3>3. Never Hardcode Secrets</h3>

      <pre><code className="language-yaml">
# ❌ Bad: Secret in source code or docker-compose
environment:
  API_KEY: "sk_live_abc123def456xyz"
  DB_PASSWORD: "postgres123"

# ✓ Good: Use variable substitution
environment:
  API_KEY: ${API_KEY}
  DB_PASSWORD: ${DB_PASSWORD}
      </code></pre>

      <h3>4. Use Secret Prefixes for Access Control</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
provider:
  type: aws-secrets-manager
  secret_prefix: "prod/"  # Only load prod/* secrets
      </code></pre>

      <p>
        This prevents accidental access to non-prod secrets and enforces environment separation.
      </p>

      <h3>5. Implement Secret Version Control</h3>

      <p>
        AWS Secrets Manager and Vault support versioning:
      </p>

      <pre><code className="language-bash">
# AWS: Secrets Manager keeps rotation history
aws secretsmanager list-secret-version-ids --secret-id prod/api/key

# Allows quick rollback if needed
aws secretsmanager get-secret-value \
  --secret-id prod/api/key \
  --version-id xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
      </code></pre>

      <h2>Security Best Practices</h2>

      <h3>1. Use IAM Roles, Not Access Keys</h3>

      <p>
        On AWS, prefer IAM roles:
      </p>

      <pre><code className="language-yaml">
# ✓ Best: IAM role on EC2
provider:
  type: aws-secrets-manager
  auth_method: iam-role
  # No credentials needed (AWS manages automatically)

# ✓ Good: Service principal on Azure
# Managed identity with RBAC

# ⚠️ Acceptable: AppRole on Vault
# Better than plaintext credentials, but needs Secret ID rotation

# ❌ Avoid: Long-lived access keys
# Hard to rotate, easy to leak
      </code></pre>

      <h3>2. Principle of Least Privilege</h3>

      <p>
        Restrict IAM/RBAC policies to minimum needed:
      </p>

      <pre><code className="language-json">
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:DescribeSecret"
      ],
      "Resource": "arn:aws:secretsmanager:*:ACCOUNT_ID:secret:prod/api/*"
    }
  ]
}
      </code></pre>

      <p>
        Key points:
      </p>

      <ul>
        <li>Restrict to specific secret names (prefixes)</li>
        <li>Only allow GetSecretValue, not PutSecretValue</li>
        <li>Limit to specific regions if applicable</li>
        <li>Don't grant access to other services' secrets</li>
      </ul>

      <h3>3. Enable Audit Logging</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  audit_enabled: true
  audit_log_path: /var/log/dso/audit.log
      </code></pre>

      <p>
        Also enable provider-side logging:
      </p>

      <ul>
        <li><strong>AWS:</strong> CloudTrail (all Secrets Manager API calls)</li>
        <li><strong>Azure:</strong> Activity Log (all Key Vault access)</li>
        <li><strong>Vault:</strong> Audit log (all operations)</li>
      </ul>

      <h3>4. Encryption in Transit & at Rest</h3>

      <ul>
        <li><strong>In Transit:</strong> TLS 1.2+ enforced (DSO always uses TLS)</li>
        <li><strong>At Rest:</strong> Use provider encryption (AWS KMS, Azure, Vault auto-encrypt)</li>
        <li><strong>Local Mode:</strong> Use encrypted disk volume if possible</li>
      </ul>

      <h3>5. Network Segmentation</h3>

      <pre><code className="language-bash">
# Firewall rules
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow from 10.0.0.0/8  # Internal network only
sudo ufw allow from 52.89.123.45 to any port 8443 comment "AWS EventBridge webhook"
      </code></pre>

      <h2>Operational Best Practices</h2>

      <h3>1. Plan for Failure, Not Success</h3>

      <p>
        Design your system assuming rotations will sometimes fail:
      </p>

      <ul>
        <li><strong>Health Checks:</strong> Make them meaningful (not just port 3000 is open)</li>
        <li><strong>Graceful Degradation:</strong> Services should work with old secrets briefly</li>
        <li><strong>Monitoring:</strong> Alert on rotation failures immediately</li>
        <li><strong>Playbooks:</strong> Document what to do when rotation fails</li>
      </ul>

      <h3>2. Health Checks: Be Specific</h3>

      <pre><code className="language-yaml">
# ❌ Bad: Too simple
healthcheck:
  test: ["CMD", "curl", "http://localhost:3000"]

# ✓ Good: Actually tests database connectivity
healthcheck:
  test: ["CMD", "curl", "http://localhost:3000/health/db"]
  # Endpoint verifies: DB connection, query execution, etc.

# ✓ Better: Multiple checks
healthcheck:
  test: ["CMD-SHELL", "curl http://localhost:3000/health/db && curl http://localhost:3000/health/cache"]
      </code></pre>

      <h3>3. Observability: Instrument Everything</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  structured_logging: true
  metrics_enabled: true
  metrics_port: 9090
  audit_enabled: true
      </code></pre>

      <p>
        Then collect metrics:
      </p>

      <pre><code className="language-yaml">
# Prometheus scrape config
scrape_configs:
  - job_name: 'dso-agent'
    static_configs:
      - targets: ['localhost:9090']
      </code></pre>

      <p>
        Create alerts:
      </p>

      <ul>
        <li>Rotation failed</li>
        <li>Provider unreachable</li>
        <li>Agent crashed</li>
        <li>Health check timeouts</li>
        <li>Lock contention</li>
      </ul>

      <h3>4. Scheduled Maintenance Windows</h3>

      <p>
        Plan for major rotations during low-traffic periods:
      </p>

      <pre><code className="language-text">
Schedule:
• Tuesday 2-4 AM UTC: DB password rotation
• First Friday of month 1-2 AM UTC: API key rotation
• During maintenance windows: Major version upgrades

Benefits:
• Lower impact if something goes wrong
• Easier to debug in low-traffic state
• Team is available (vs 3 AM on-call)
• Services can be restarted if needed
      </code></pre>

      <h3>5. Document Everything</h3>

      <p>
        Create and maintain:
      </p>

      <ul>
        <li><strong>Runbook:</strong> How to add/update/rotate secrets</li>
        <li><strong>Architecture Diagram:</strong> DSO + your system</li>
        <li><strong>Secret Inventory:</strong> What secrets exist, who owns them</li>
        <li><strong>Incident Response:</strong> What to do when things break</li>
        <li><strong>Testing Procedures:</strong> How to test rotation before production</li>
      </ul>

      <h2>Performance Best Practices</h2>

      <h3>1. Polling Interval Tuning</h3>

      <pre><code className="language-yaml">
# Too frequent: Wastes API calls, increases latency
discovery:
  polling_interval_seconds: 10  # ❌ 8,640 calls/day

# Reasonable: Balances latency and cost
  polling_interval_seconds: 60  # ✓ 1,440 calls/day

# Slow: Long wait for rotation
  polling_interval_seconds: 600  # ❌ 144 calls/day (but 10min latency)

# Recommended for production:
  polling_interval_seconds: 300  # ✓ 288 calls/day, 5min max latency
      </code></pre>

      <p>
        Or use webhooks (best for production):
      </p>

      <pre><code className="language-yaml">
discovery:
  mode: webhook  # Immediate notification, minimal API calls
      </code></pre>

      <h3>2. Health Check Timeouts</h3>

      <pre><code className="language-yaml">
# ❌ Too aggressive: App needs 45s to start
healthcheck:
  start_period: 10
  timeout: 5

# ✓ Realistic: App needs 45s to startup
healthcheck:
  start_period: 60  # Wait 60s before first check
  timeout: 10       # 10s timeout for each check
  retries: 3        # Allow 3 failures
      </code></pre>

      <h3>3. Resource Management</h3>

      <pre><code className="language-bash">
# Monitor DSO resource usage
ps aux | grep dso-agent | grep -v grep
# Check RSS column for memory

# Set systemd resource limits
sudo systemctl edit dso-agent
# Add:
# [Service]
# MemoryLimit=512M
# TasksMax=100
      </code></pre>

      <h2>Testing Best Practices</h2>

      <h3>1. Test Rotation Process</h3>

      <p>
        Before production:
      </p>

      <pre><code className="language-bash">
# 1. Setup staging environment with same config
# 2. Add test secrets
docker dso secret set TEST_SECRET "value1"

# 3. Deploy
docker dso up -f docker-compose.test.yml

# 4. Verify deployment works
docker ps
docker exec [container] env | grep TEST_SECRET

# 5. Trigger rotation
docker dso secret set TEST_SECRET "value2"

# 6. Verify rotation succeeded
docker ps  # Should still show 1 container running
docker exec [container] env | grep TEST_SECRET  # Should show value2

# 7. Verify zero downtime
# Run load test during rotation, verify no failed requests
      </code></pre>

      <h3>2. Chaos Testing</h3>

      <p>
        Test failure scenarios:
      </p>

      <pre><code className="language-bash">
# Test 1: Kill agent during rotation
# Start rotation, kill agent mid-way
# Restart agent
# Verify automatic recovery

# Test 2: Health check failure
# Update secret with invalid value
# Verify rotation fails and rolls back

# Test 3: Provider unreachable
# Block network to provider
# Verify agent doesn't crash
# Unblock and verify recovery

# Test 4: Lock timeout
# Simulate long rotation
# Trigger second rotation
# Verify second waits for lock
      </code></pre>

      <h3>3. Load Testing</h3>

      <pre><code className="language-bash">
# Run synthetic load test while rotating secrets
# Measure: latency, error rate, connection drops

# Tools: wrk, Apache Bench, k6, etc.
ab -n 10000 -c 100 http://localhost:3000/api

# Should see: No errors, no latency spikes during rotation
      </code></pre>

      <h2>Compliance & Audit Best Practices</h2>

      <h3>1. Audit Trail</h3>

      <pre><code className="language-bash">
# Collect audit logs
sudo journalctl -u dso-agent --no-pager > audit.log

# Archive periodically
tar czf audit-logs-$(date +%Y-%m-%d).tar.gz /var/log/dso/

# Keep long-term (e.g., 1 year)
# For compliance: PCI-DSS, HIPAA, SOC 2, etc.
      </code></pre>

      <h3>2. Compliance Checklist</h3>

      <ul>
        <li>☐ Secrets never on disk (verified via file scanning)</li>
        <li>☐ Audit logging enabled (90+ days retention)</li>
        <li>☐ Encryption in transit (TLS 1.2+ enforced)</li>
        <li>☐ Encryption at rest (via provider)</li>
        <li>☐ Access control (IAM/RBAC configured)</li>
        <li>☐ Secret rotation (scheduled and audited)</li>
        <li>☐ Health monitoring (alerts on failures)</li>
      </ul>

      <h3>3. Documentation for Auditors</h3>

      <p>
        Maintain:
      </p>

      <ul>
        <li>Architecture diagram showing secret flow</li>
        <li>Policy document explaining secret lifecycle</li>
        <li>Access control matrix (who can access what)</li>
        <li>Rotation schedule and history</li>
        <li>Incident reports and remediation</li>
      </ul>

      <h2>Troubleshooting Best Practices</h2>

      <h3>1. Keep Detailed Logs</h3>

      <pre><code className="language-bash">
# Always use structured logging
observability:
  structured_logging: true

# Makes debugging much easier
sudo journalctl -u dso-agent -o json | jq '.MESSAGE' | grep error
      </code></pre>

      <h3>2. Reproduce Locally First</h3>

      <p>
        Before deploying changes to production:
      </p>

      <ul>
        <li>Test in local dev environment</li>
        <li>Test in staging environment</li>
        <li>Test with same config as production</li>
        <li>Document test results</li>
      </ul>

      <h3>3. Have a Rollback Plan</h3>

      <pre><code className="language-bash">
# For configuration changes
# Before updating /etc/dso/config.yml:
sudo cp /etc/dso/config.yml /etc/dso/config.yml.backup

# For secret changes
# Export backup before rotation
docker dso secret export > backup.env

# Can quickly restore if needed
docker dso secret import backup.env
      </code></pre>

      <h2>Key Principles Summary</h2>

      <ol>
        <li><strong>Assume failure:</strong> Design for recovery, not just happy paths</li>
        <li><strong>Be explicit:</strong> Clear naming, clear policies, clear documentation</li>
        <li><strong>Monitor everything:</strong> Metrics, logs, alerts on critical paths</li>
        <li><strong>Test thoroughly:</strong> Both happy paths and failure scenarios</li>
        <li><strong>Secure by default:</strong> Use TLS, IAM roles, least privilege</li>
        <li><strong>Keep it simple:</strong> Don't over-engineer, solve the actual problem</li>
        <li><strong>Automate repetitive tasks:</strong> Rotation, backups, monitoring</li>
        <li><strong>Document everything:</strong> For operators and auditors</li>
      </ol>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
        <li><a href="/docs/guide/observability">Monitoring and observability</a></li>
        <li><a href="/docs/guide/recovery-procedures">Recovery procedures</a></li>
      </ul>
    </div>
  );
}
