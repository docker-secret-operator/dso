import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Troubleshooting Guide",
  description: "Common issues, diagnostic procedures, and solutions for Docker Secret Operator."
};

export default function TroubleshootingPage() {
  return (
    <div>
      <h1>Troubleshooting Guide</h1>

      <p>
        This guide covers common problems, how to diagnose them, and how to fix them.
      </p>

      <h2>Diagnostic Checklist</h2>

      <p>
        Start here for any issue. Work through these steps systematically:
      </p>

      <pre><code className="language-bash">
# 1. Is DSO agent running?
sudo systemctl status dso-agent

# 2. Are there recent errors?
sudo journalctl -u dso-agent -n 20 | grep -i error

# 3. Can DSO reach the secret provider?
curl -I https://secretsmanager.us-east-1.amazonaws.com  # AWS
curl -I https://vault.internal:8200/v1/sys/health  # Vault

# 4. Do the secrets exist?
docker dso secret list  # Local mode
aws secretsmanager list-secrets --region us-east-1  # AWS

# 5. Are containers running?
docker ps

# 6. Check DSO status
sudo dso status
docker dso status  # Local mode
      </code></pre>

      <h2>Agent Won't Start</h2>

      <h3>Symptoms</h3>

      <pre><code className="language-bash">
sudo systemctl start dso-agent
# Fails or returns immediately
      </code></pre>

      <h3>Diagnosis</h3>

      <pre><code className="language-bash">
# Check service status
sudo systemctl status dso-agent

# View full error output
sudo journalctl -u dso-agent -n 50

# Try running agent directly (not as service)
sudo /usr/local/bin/dso-agent --config /etc/dso/config.yml
      </code></pre>

      <h3>Common Causes & Solutions</h3>

      <table>
        <thead>
          <tr>
            <th>Error</th>
            <th>Cause</th>
            <th>Solution</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>Failed to open config.yml: No such file</code></td>
            <td>Config file not found</td>
            <td>Bootstrap agent: <code>sudo docker dso bootstrap agent</code></td>
          </tr>
          <tr>
            <td><code>Failed to parse config.yml: YAML error</code></td>
            <td>Invalid YAML syntax</td>
            <td>Validate YAML: <code>python3 -m yaml &lt; /etc/dso/config.yml</code></td>
          </tr>
          <tr>
            <td><code>Failed to connect to Docker: permission denied</code></td>
            <td>dso user not in docker group</td>
            <td>Add to group: <code>sudo usermod -aG docker dso</code></td>
          </tr>
          <tr>
            <td><code>Failed to connect to provider: connection refused</code></td>
            <td>Provider unreachable</td>
            <td>Check network: <code>curl -I https://provider-url</code></td>
          </tr>
          <tr>
            <td><code>Failed to authenticate: access denied</code></td>
            <td>Bad credentials</td>
            <td>Verify credentials: <code>aws sts get-caller-identity</code></td>
          </tr>
        </tbody>
      </table>

      <h2>Agent Crashes During Rotation</h2>

      <h3>Symptoms</h3>

      <pre><code className="language-bash">
# Agent was running, then suddenly stops
sudo systemctl status dso-agent
# Active: inactive (dead)

# Check logs
sudo journalctl -u dso-agent | tail -20
# May see panic or kill signal
      </code></pre>

      <h3>Recovery Steps</h3>

      <pre><code className="language-bash">
# 1. Restart the agent (triggers automatic recovery)
sudo systemctl start dso-agent

# 2. Check if recovery succeeded
sudo dso status
docker ps  # Verify containers are healthy

# 3. Check logs for recovery details
sudo journalctl -u dso-agent | grep -i "recovery\|rollback"

# 4. If containers are in bad state, fix manually
docker stop api.new api.old  # Stop partial containers
docker rm api.new api.old    # Remove them
sudo systemctl restart dso-agent  # Restart agent
      </code></pre>

      <h3>Common Causes</h3>

      <ul>
        <li><strong>Out of Memory:</strong> Check available RAM, increase if needed</li>
        <li><strong>Disk Full:</strong> Check disk space: <code>df -h</code></li>
        <li><strong>File Descriptor Limit:</strong> Check: <code>ulimit -n</code></li>
        <li><strong>Docker Daemon Crash:</strong> Check: <code>systemctl status docker</code></li>
      </ul>

      <h2>Secrets Not Injecting</h2>

      <h3>Symptoms</h3>

      <pre><code className="language-bash">
# Containers starting but secrets not set
docker exec myapp env | grep DATABASE_PASSWORD
# Returns nothing (secret not injected)
      </code></pre>

      <h3>Diagnosis Steps</h3>

      <pre><code className="language-bash">
# Step 1: Check if agent is running
sudo systemctl status dso-agent
# Should be active (running)

# Step 2: Verify secrets exist
docker dso secret list  # Local mode
aws secretsmanager list-secrets  # AWS

# Step 3: Check docker-compose.yml syntax
docker compose config | grep -A 5 "environment:"

# Step 4: Check DSO logs
sudo journalctl -u dso-agent | grep -i "secret\|inject"
      </code></pre>

      <h3>Solutions by Cause</h3>

      <h4>Cause: Agent not running</h4>

      <pre><code className="language-bash">
sudo systemctl start dso-agent
sudo systemctl status dso-agent
      </code></pre>

      <h4>Cause: Secret doesn't exist</h4>

      <pre><code className="language-bash">
# Add missing secret
docker dso secret set DATABASE_PASSWORD "value"  # Local mode
aws secretsmanager create-secret --name DATABASE_PASSWORD --secret-string "value"  # AWS
      </code></pre>

      <h4>Cause: Wrong secret name in compose</h4>

      <pre><code className="language-bash">
# Check compose file
cat docker-compose.yml | grep "DATABASE_PASSWORD"
# Should be: DATABASE_PASSWORD: ${DATABASE_PASSWORD}

# Fix: Make sure variable name matches exactly (case-sensitive)
      </code></pre>

      <h4>Cause: Secret path mismatch (AWS with prefix)</h4>

      <pre><code className="language-bash">
# Check configured prefix
sudo cat /etc/dso/config.yml | grep secret_prefix
# Example: secret_prefix: "prod/"

# Check secret name
aws secretsmanager list-secrets | grep prod/
# Secret must match prefix

# If needed, recreate secret with correct prefix
aws secretsmanager create-secret --name "prod/DATABASE_PASSWORD" --secret-string "value"
      </code></pre>

      <h2>Container Health Check Failure</h2>

      <h3>Symptoms</h3>

      <pre><code className="language-bash">
# Container created but rotation fails
docker ps
# Container status shows "unhealthy" or "exited"

# Check logs
sudo journalctl -u dso-agent | grep -i "health"
# Shows: "Health check failed"
      </code></pre>

      <h3>Diagnosis</h3>

      <pre><code className="language-bash">
# Check what health check is defined
cat docker-compose.yml | grep -A 5 "healthcheck:"

# Run health check manually
docker exec myapp curl http://localhost:3000/health

# Check application logs
docker logs myapp | tail -20

# Check if app started
docker logs myapp | grep -i "started\|ready"
      </code></pre>

      <h3>Common Health Check Issues</h3>

      <table>
        <thead>
          <tr>
            <th>Issue</th>
            <th>Cause</th>
            <th>Solution</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Connection refused</td>
            <td>App not listening yet</td>
            <td>Increase start_period in healthcheck</td>
          </tr>
          <tr>
            <td>Database connection error</td>
            <td>Database not responding</td>
            <td>Check database health, credentials</td>
          </tr>
          <tr>
            <td>HTTP 500 error</td>
            <td>Application error</td>
            <td>Check application logs for details</td>
          </tr>
          <tr>
            <td>Timeout</td>
            <td>Slow health check</td>
            <td>Increase timeout value in healthcheck</td>
          </tr>
        </tbody>
      </table>

      <h3>Fix: Adjust Health Check</h3>

      <pre><code className="language-yaml">
# docker-compose.yml
services:
  app:
    image: myapp:latest
    healthcheck:
      # Increase startup time
      start_period: 60  # Wait 60 seconds before first check
      # Increase check timeout
      timeout: 10  # Timeout for each check
      # More lenient retry
      retries: 5  # Allow more failures before marking unhealthy
      </code></pre>

      <h2>Rotation Timeout/Stuck</h2>

      <h3>Symptoms</h3>

      <pre><code className="language-bash">
# Rotation appears to hang
sudo dso status
# Shows: "rotation_in_progress" for > 5 minutes

# Check logs
sudo journalctl -u dso-agent | grep -i "timeout"
      </code></pre>

      <h3>Diagnosis</h3>

      <pre><code className="language-bash">
# Check lock file (if stuck)
ls -la /var/lib/dso/rotation.lock

# Check lock age
stat /var/lib/dso/rotation.lock | grep Modify

# Check if old rotation in progress
sudo dso status

# Check Docker operations
docker ps | grep -E "\.new|\.old"
# May see hanging containers
      </code></pre>

      <h3>Force Recovery</h3>

      <pre><code className="language-bash">
# Force release stuck lock (if older than 5 minutes)
sudo rm /var/lib/dso/rotation.lock

# Restart agent
sudo systemctl restart dso-agent

# Verify recovery
docker ps
sudo dso status
      </code></pre>

      <h2>Authentication/Authorization Errors</h2>

      <h3>AWS: UnauthorizedException</h3>

      <pre><code className="language-bash">
# Error: "User is not authorized to perform: secretsmanager:GetSecretValue"

# 1. Verify credentials
aws sts get-caller-identity

# 2. Verify IAM permissions
aws iam get-user-policy --user-name dso-agent --policy-name dso-secrets-policy

# 3. Verify policy includes GetSecretValue
# Policy should have:
# "secretsmanager:GetSecretValue"
# Check resource ARN matches secret names
      </code></pre>

      <h3>Vault: 403 Permission Denied</h3>

      <pre><code className="language-bash">
# Error: "permission denied" when accessing secret

# 1. Verify AppRole credentials
vault read auth/approle/role/dso-agent

# 2. Check policy attached
vault policy read dso-policy

# 3. Verify policy grants kv/data/secret/* read access

# 4. Validate token
vault token lookup
      </code></pre>

      <h3>Azure: Unauthorized (401)</h3>

      <pre><code className="language-bash">
# Error: "Unauthorized - Invalid credentials"

# 1. Verify service principal credentials
az ad sp show --id [client-id]

# 2. Check if credentials expired
az account get-access-token

# 3. Verify Key Vault access policy
az keyvault show --resource-group [rg] --name [vault-name] | grep -A 5 "accessPolicies"
      </code></pre>

      <h2>Network Connectivity Issues</h2>

      <h3>Provider Unreachable</h3>

      <pre><code className="language-bash">
# Test connectivity to provider
ping secretsmanager.us-east-1.amazonaws.com
# May fail if ICMP blocked

# Try HTTPS
curl -v https://secretsmanager.us-east-1.amazonaws.com

# Check firewall rules
sudo ufw show added  # Linux
# Verify outbound HTTPS (443) is allowed

# Check DNS
nslookup secretsmanager.us-east-1.amazonaws.com
# Should resolve to IP address

# Check routing
route -n | grep default
# Verify default gateway is set
      </code></pre>

      <h3>Webhook Not Receiving Events</h3>

      <pre><code className="language-bash">
# DSO in webhook mode, but not receiving secret change notifications

# 1. Verify webhook endpoint is reachable
curl -k https://[dso-host]:8443/webhook

# 2. Check firewall allows inbound
sudo ufw status | grep 8443

# 3. Check that EventBridge rule is enabled
aws events list-rules | grep dso

# 4. Test webhook manually
curl -X POST https://[dso-host]:8443/webhook \
  -H "Content-Type: application/json" \
  -d '{"detail": {"eventName": "PutSecretValue"}}'

# 5. Check DSO logs for webhook errors
sudo journalctl -u dso-agent | grep -i "webhook"
      </code></pre>

      <h2>Performance Issues</h2>

      <h3>Slow Rotation</h3>

      <pre><code className="language-bash">
# Rotation taking > 1 minute

# Check where time is spent
sudo journalctl -u dso-agent | grep -E "stage:|duration"

# Check container startup time
docker compose up --no-start  # Create without starting
docker start test-container
docker logs test-container | head -5  # When did it start?

# Slow part is usually health checks
cat docker-compose.yml | grep -A 5 "healthcheck:"
# Increase start_period if app is slow to start
      </code></pre>

      <h3>High Memory Usage</h3>

      <pre><code className="language-bash">
# DSO agent using too much memory

# Check actual usage
ps aux | grep dso-agent
# Check RSS column

# Check for memory leaks
sudo systemctl status dso-agent
# Restart agent if bloated
sudo systemctl restart dso-agent

# Check for goroutine leaks
curl http://localhost:9090/metrics | grep goroutine
      </code></pre>

      <h2>Logging & Debugging</h2>

      <h3>Enable Debug Logging</h3>

      <pre><code className="language-bash">
# Temporarily set debug log level
export DSO_LOG_LEVEL=debug
sudo systemctl restart dso-agent

# View debug logs
sudo journalctl -u dso-agent -f

# Once done, revert to info
export DSO_LOG_LEVEL=info
sudo systemctl restart dso-agent
      </code></pre>

      <h3>Export Logs for Analysis</h3>

      <pre><code className="language-bash">
# Export last 7 days of logs
sudo journalctl -u dso-agent -S -7d > dso-logs-7d.txt

# Export with more details
sudo journalctl -u dso-agent -S -7d --no-pager -o short-iso > dso-logs-detailed.txt
      </code></pre>

      <h2>Common Solutions Summary</h2>

      <table>
        <thead>
          <tr>
            <th>Problem</th>
            <th>Quick Fix</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Agent won't start</td>
            <td><code>sudo docker dso bootstrap agent</code> (reconfigure)</td>
          </tr>
          <tr>
            <td>Agent crashed</td>
            <td><code>sudo systemctl start dso-agent</code> (auto-recovery on restart)</td>
          </tr>
          <tr>
            <td>Secrets not injecting</td>
            <td>Check <code>docker dso secret list</code> and compose file syntax</td>
          </tr>
          <tr>
            <td>Health check fails</td>
            <td>Increase <code>start_period</code> and <code>timeout</code> in healthcheck</td>
          </tr>
          <tr>
            <td>Rotation timeout</td>
            <td><code>sudo rm /var/lib/dso/rotation.lock</code> (force release)</td>
          </tr>
          <tr>
            <td>Provider unreachable</td>
            <td>Check network: <code>curl https://provider-url</code></td>
          </tr>
          <tr>
            <td>Auth failed</td>
            <td>Verify credentials: <code>aws sts get-caller-identity</code></td>
          </tr>
        </tbody>
      </table>

      <h2>Getting Help</h2>

      <p>
        If you can't resolve the issue:
      </p>

      <ol>
        <li>Check logs: <code>sudo journalctl -u dso-agent -n 100</code></li>
        <li>Check status: <code>sudo dso status</code></li>
        <li>Run diagnostics: <code>docker dso diagnose</code> (if available)</li>
        <li>Check docs: <a href="/docs">Documentation</a></li>
        <li>File issue: <a href="https://github.com/antiersolutions/docker-secret-operator/issues">GitHub Issues</a></li>
        <li>Contact support with:
          <ul>
            <li>Error logs (last 50 lines)</li>
            <li>Configuration (sanitized, no credentials)</li>
            <li>Environment details (OS, Docker version)</li>
            <li>Steps to reproduce</li>
          </ul>
        </li>
      </ol>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/recovery-procedures">Recovery procedures</a></li>
        <li><a href="/docs/guide/production-readiness">Production readiness checklist</a></li>
        <li><a href="/docs/guide/best-practices">Best practices</a></li>
      </ul>
    </div>
  );
}
