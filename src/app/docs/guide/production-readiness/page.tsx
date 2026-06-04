import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Production Readiness Checklist",
  description: "Complete checklist for deploying Docker Secret Operator safely to production."
};

export default function ProductionReadinessPage() {
  return (
    <div>
      <h1>Production Readiness Checklist</h1>

      <p>
        Use this checklist to verify DSO is properly configured for production before deploying to your infrastructure. This covers security, reliability, observability, and operational best practices.
      </p>

      <h2>Pre-Deployment Checklist</h2>

      <h3>1. Infrastructure & Prerequisites</h3>

      <ul>
        <li>
          <strong>☐ Docker Version</strong>
          <ul>
            <li>Verify: <code>docker --version</code> → v20.10 or newer</li>
            <li>Verify: <code>docker compose version</code> → v2.0 or newer</li>
          </ul>
        </li>
        <li>
          <strong>☐ System Resources</strong>
          <ul>
            <li>CPU: Minimum 1 core, recommended 2+</li>
            <li>RAM: Minimum 512MB, recommended 2GB</li>
            <li>Disk: Minimum 500MB free, recommended SSD</li>
          </ul>
        </li>
        <li>
          <strong>☐ Network Connectivity</strong>
          <ul>
            <li>DSO host can reach secret provider (AWS, Azure, Vault, etc.)</li>
            <li>If webhook mode: Secret provider can reach DSO webhook endpoint</li>
            <li>Firewall rules allow outbound HTTPS (443) to provider</li>
          </ul>
        </li>
        <li>
          <strong>☐ User & Permissions</strong>
          <ul>
            <li>DSO agent runs as dedicated non-root user (dso)</li>
            <li>DSO user has docker group membership</li>
            <li>Verify: <code>groups dso | grep docker</code></li>
          </ul>
        </li>
      </ul>

      <h3>2. Secret Provider Setup</h3>

      <ul>
        <li>
          <strong>☐ Provider Account Created</strong>
          <ul>
            <li>AWS: Secrets Manager service enabled</li>
            <li>Azure: Key Vault created and accessible</li>
            <li>Vault: Vault cluster running and unsealed</li>
          </ul>
        </li>
        <li>
          <strong>☐ Credentials Configured</strong>
          <ul>
            <li>AWS: IAM role or access keys created</li>
            <li>Azure: Service principal or managed identity configured</li>
            <li>Vault: AppRole or auth method configured</li>
            <li>Credentials stored securely (NOT hardcoded)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Secrets Created</strong>
          <ul>
            <li>All required secrets exist in provider</li>
            <li>Secret names follow consistent naming convention (e.g., prod/service/secret)</li>
            <li>Verify access: <code>dso secret list</code> returns all secrets</li>
          </ul>
        </li>
        <li>
          <strong>☐ Event Detection Configured</strong>
          <ul>
            <li>Polling: <code>polling_interval_seconds</code> set (60s recommended)</li>
            <li>Webhook: EventBridge rule created (AWS) or equivalent</li>
            <li>Webhook: DSO webhook endpoint accessible from provider</li>
          </ul>
        </li>
      </ul>

      <h3>3. Agent Installation & Configuration</h3>

      <ul>
        <li>
          <strong>☐ DSO Agent Installed</strong>
          <ul>
            <li>Verify: <code>docker dso --version</code> → v3.5.1</li>
            <li>Verify: <code>which docker dso</code> → /usr/local/bin/dso</li>
          </ul>
        </li>
        <li>
          <strong>☐ Agent Bootstrapped</strong>
          <ul>
            <li>Verify: <code>sudo systemctl status dso-agent</code> → active</li>
            <li>Verify: <code>/etc/dso/config.yml</code> exists and is valid YAML</li>
            <li>Verify: <code>/var/lib/dso/</code> directory exists with permissions 700</li>
          </ul>
        </li>
        <li>
          <strong>☐ Configuration Validated</strong>
          <ul>
            <li><code>provider.type</code> correct (aws, azure, vault, etc.)</li>
            <li><code>provider.region</code> or <code>provider.url</code> correct</li>
            <li><code>discovery.mode</code> set (polling or webhook)</li>
            <li><code>observability.structured_logging</code> enabled</li>
            <li><code>log_level</code> set to info (not debug in production)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Service Auto-Start Configured</strong>
          <ul>
            <li>Verify: <code>sudo systemctl is-enabled dso-agent</code> → enabled</li>
            <li>Agent starts automatically on reboot</li>
          </ul>
        </li>
      </ul>

      <h3>4. Docker Compose Configuration</h3>

      <ul>
        <li>
          <strong>☐ Version & Format</strong>
          <ul>
            <li>Version: <code>version: '3.8'</code> or newer</li>
            <li>Valid YAML (check: <code>docker compose config</code>)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Services Configured</strong>
          <ul>
            <li>All services using up-to-date images</li>
            <li>Images available (not private repos with missing creds)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Health Checks Defined</strong>
          <ul>
            <li>All critical services have healthcheck</li>
            <li>Healthcheck timeout: 5-10 seconds</li>
            <li>Healthcheck interval: 5-15 seconds</li>
            <li>Healthcheck start_period: 30-60 seconds (for slow apps)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Secret Injection</strong>
          <ul>
            <li>All secrets referenced with <code>${secret-name}</code> syntax</li>
            <li>Secret names match what exists in provider</li>
            <li>No hardcoded secrets in compose file</li>
            <li>No secrets in image tags or image names</li>
          </ul>
        </li>
        <li>
          <strong>☐ Environment Variables</strong>
          <ul>
            <li>Service dependencies use environment variables</li>
            <li>Example: <code>DATABASE_URL: postgres://user:${DB_PASSWORD}@db:5432/app</code></li>
          </ul>
        </li>
      </ul>

      <h3>5. Security & Access Control</h3>

      <ul>
        <li>
          <strong>☐ File Permissions</strong>
          <ul>
            <li><code>/etc/dso/</code> → 755</li>
            <li><code>/etc/dso/config.yml</code> → 640</li>
            <li><code>/var/lib/dso/</code> → 700</li>
            <li>Verify: <code>ls -la /var/lib/dso/</code></li>
          </ul>
        </li>
        <li>
          <strong>☐ Firewall Rules</strong>
          <ul>
            <li>Deny all inbound by default</li>
            <li>Allow outbound HTTPS (443) to provider</li>
            <li>If webhook: Allow inbound HTTPS (8443) from provider only</li>
            <li>Verify: <code>sudo ufw status</code> (Linux)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Network Segmentation</strong>
          <ul>
            <li>DSO host not exposed to internet</li>
            <li>Only internal networks can access DSO</li>
            <li>Secret provider credentials not stored in version control</li>
          </ul>
        </li>
        <li>
          <strong>☐ Credential Storage</strong>
          <ul>
            <li>AWS: IAM role (best) or encrypted credentials</li>
            <li>Azure: Managed identity (best) or service principal</li>
            <li>Vault: AppRole with encrypted Secret ID</li>
            <li>No credentials in docker-compose.yml</li>
          </ul>
        </li>
        <li>
          <strong>☐ TLS Certificate Validation</strong>
          <ul>
            <li><code>tls_verify: true</code> in config</li>
            <li><code>tls_skip_verify: false</code> (never set to true)</li>
            <li>Custom CA: <code>tls_ca_file</code> specified if needed</li>
          </ul>
        </li>
      </ul>

      <h3>6. Observability & Monitoring</h3>

      <ul>
        <li>
          <strong>☐ Logging Enabled</strong>
          <ul>
            <li><code>structured_logging: true</code> in config</li>
            <li>Logs go to systemd journal: <code>journalctl -u dso-agent</code></li>
            <li>Logs are JSON-formatted for parsing</li>
          </ul>
        </li>
        <li>
          <strong>☐ Log Aggregation (Optional but Recommended)</strong>
          <ul>
            <li>Logs forwarded to centralized logging (ELK, Splunk, CloudWatch, etc.)</li>
            <li>Systemd journal forwarding configured if needed</li>
          </ul>
        </li>
        <li>
          <strong>☐ Metrics Exposed</strong>
          <ul>
            <li><code>metrics_port: 9090</code> in config</li>
            <li>Prometheus scrape configured: <code>/metrics</code> endpoint</li>
            <li>Verify: <code>curl http://localhost:9090/metrics | head</code></li>
          </ul>
        </li>
        <li>
          <strong>☐ Health Endpoint</strong>
          <ul>
            <li><code>health_check_port: 8081</code> in config</li>
            <li>Verify: <code>curl http://localhost:8081/health</code></li>
          </ul>
        </li>
        <li>
          <strong>☐ Alerting Rules</strong>
          <ul>
            <li>Alert on agent crash (systemd watcher or monitoring tool)</li>
            <li>Alert on rotation failure (from logs)</li>
            <li>Alert on health check failures (metrics)</li>
            <li>Alert on lock timeout (metrics)</li>
          </ul>
        </li>
      </ul>

      <h3>7. Testing & Validation</h3>

      <ul>
        <li>
          <strong>☐ Dry-Run Test</strong>
          <ul>
            <li>Run compose without production data first</li>
            <li>Verify containers start and become healthy</li>
            <li>Verify secrets injected correctly</li>
          </ul>
        </li>
        <li>
          <strong>☐ Manual Rotation Test</strong>
          <ul>
            <li>Update secret in provider manually</li>
            <li>Verify agent detects change (within polling interval)</li>
            <li>Verify containers rotate automatically</li>
            <li>Verify no downtime during rotation</li>
            <li>Verify application continues functioning</li>
          </ul>
        </li>
        <li>
          <strong>☐ Health Check Test</strong>
          <ul>
            <li>Verify health checks pass before rotation completes</li>
            <li>Test health check failure (intentionally break)</li>
            <li>Verify automatic rollback occurs</li>
          </ul>
        </li>
        <li>
          <strong>☐ Crash Recovery Test</strong>
          <ul>
            <li>Kill dso-agent process mid-rotation: <code>sudo systemctl stop dso-agent</code></li>
            <li>Verify containers remain stable</li>
            <li>Restart agent: <code>sudo systemctl start dso-agent</code></li>
            <li>Verify automatic recovery (containers restored, no manual cleanup)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Concurrency Test</strong>
          <ul>
            <li>Trigger rotation while another is in-progress</li>
            <li>Verify second rotation waits (lock prevents concurrent access)</li>
            <li>Verify both rotations eventually complete</li>
          </ul>
        </li>
      </ul>

      <h3>8. Operational Documentation</h3>

      <ul>
        <li>
          <strong>☐ Runbooks Created</strong>
          <ul>
            <li>How to add/update secrets in provider</li>
            <li>How to check rotation status</li>
            <li>How to view logs: <code>journalctl -u dso-agent -f</code></li>
            <li>How to restart agent: <code>sudo systemctl restart dso-agent</code></li>
          </ul>
        </li>
        <li>
          <strong>☐ Incident Response Plan</strong>
          <ul>
            <li>What to do if rotation fails</li>
            <li>What to do if agent crashes</li>
            <li>What to do if provider is unreachable</li>
            <li>Contact info for on-call engineer</li>
          </ul>
        </li>
        <li>
          <strong>☐ Documentation Comments</strong>
          <ul>
            <li>Comments in docker-compose.yml explaining secret mapping</li>
            <li>Comments in config.yml explaining settings</li>
            <li>Link to this checklist and procedures</li>
          </ul>
        </li>
      </ul>

      <h2>Post-Deployment Checklist</h2>

      <h3>First Week Operations</h3>

      <ul>
        <li>
          <strong>☐ Monitor Logs Daily</strong>
          <ul>
            <li>Check for errors: <code>sudo journalctl -u dso-agent | grep ERROR</code></li>
            <li>Check rotation frequency</li>
            <li>Verify no unexpected alerts</li>
          </ul>
        </li>
        <li>
          <strong>☐ Verify Metrics Collection</strong>
          <ul>
            <li>Check Prometheus scraping DSO metrics</li>
            <li>Verify dashboards showing rotation events</li>
            <li>Verify alerts firing correctly (test by breaking something)</li>
          </ul>
        </li>
        <li>
          <strong>☐ Test Secret Rotation (Manual)</strong>
          <ul>
            <li>At least once during first week</li>
            <li>Update a test secret in provider</li>
            <li>Verify rotation happens automatically</li>
            <li>Verify no downtime</li>
            <li>Verify application continues working</li>
          </ul>
        </li>
        <li>
          <strong>☐ Team Training</strong>
          <ul>
            <li>All ops team members know how to check status</li>
            <li>All ops team members know how to add new secrets</li>
            <li>All ops team members know incident procedures</li>
          </ul>
        </li>
      </ul>

      <h3>Ongoing Maintenance</h3>

      <ul>
        <li>
          <strong>☐ Weekly Health Checks</strong>
          <ul>
            <li>Agent service running: <code>sudo systemctl status dso-agent</code></li>
            <li>No ERROR logs: <code>sudo journalctl -u dso-agent -S -7d | grep ERROR</code></li>
            <li>Metrics being collected</li>
          </ul>
        </li>
        <li>
          <strong>☐ Monthly Security Review</strong>
          <ul>
            <li>Credentials haven't leaked (check logs for suspicious access)</li>
            <li>File permissions still correct</li>
            <li>Firewall rules still in place</li>
          </ul>
        </li>
        <li>
          <strong>☐ Quarterly Disaster Recovery Drill</strong>
          <ul>
            <li>Simulate agent crash: <code>sudo systemctl stop dso-agent</code></li>
            <li>Verify recovery behavior</li>
            <li>Document any issues</li>
          </ul>
        </li>
        <li>
          <strong>☐ Annual Security Audit</strong>
          <ul>
            <li>Review all credentials and access controls</li>
            <li>Verify no hardcoded secrets anywhere</li>
            <li>Review logs for unauthorized access attempts</li>
          </ul>
        </li>
      </ul>

      <h2>Common Issues & Fixes</h2>

      <h3>Issue: Agent won't start</h3>

      <pre><code className="language-bash">
# Check status
sudo systemctl status dso-agent

# View error logs
sudo journalctl -u dso-agent -n 50

# Common cause: config.yml invalid YAML
sudo cat /etc/dso/config.yml
# Fix: Correct YAML syntax

# Common cause: permission error
ls -la /var/lib/dso/
# Fix: sudo chown -R dso:dso /var/lib/dso/
      </code></pre>

      <h3>Issue: Secrets not injecting</h3>

      <pre><code className="language-bash">
# Check agent can reach provider
curl -v https://secretsmanager.us-east-1.amazonaws.com

# Check credentials are valid
aws secretsmanager list-secrets --region us-east-1

# Check secret exists
docker dso secret list

# Check docker-compose.yml syntax
docker compose config | grep -A 5 environment
      </code></pre>

      <h3>Issue: Rotation failed</h3>

      <pre><code className="language-bash">
# Check logs for details
sudo journalctl -u dso-agent | grep -A 10 "rotation failed"

# Check container health
docker ps | grep -E "unhealthy|exited"

# Check lock file isn't stuck
ls -la /var/lib/dso/rotation.lock

# Manual recovery
docker dso rotation reset  # If available
sudo systemctl restart dso-agent
      </code></pre>

      <h2>Success Criteria</h2>

      <p>
        Production deployment is successful when:
      </p>

      <ul>
        <li>✓ Agent service starts automatically on boot</li>
        <li>✓ Secrets are injected into containers without manual intervention</li>
        <li>✓ Secret updates in provider trigger automatic rotation within 60 seconds (polling) or 5 seconds (webhook)</li>
        <li>✓ Rotations complete with zero downtime</li>
        <li>✓ Health checks pass for all services</li>
        <li>✓ No secrets appear in logs, docker inspect, or host filesystem</li>
        <li>✓ Monitoring alerts work correctly</li>
        <li>✓ Team understands operational procedures</li>
        <li>✓ Incident response plan documented and tested</li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/observability">Monitoring and observability setup</a></li>
        <li><a href="/docs/guide/recovery-procedures">Recovery procedures</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting guide</a></li>
        <li><a href="/docs/guide/best-practices">Operational best practices</a></li>
      </ul>
    </div>
  );
}
