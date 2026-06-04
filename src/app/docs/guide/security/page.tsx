import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Security Model",
  description: "Security guarantees, threat model, and security best practices for Docker Secret Operator."
};

export default function SecurityPage() {
  return (
    <div>
      <h1>Security Model</h1>

      <p>
        Understanding DSO's security model is critical for safe production deployment. This guide explains security guarantees, threat models, attack surface, and security best practices.
      </p>

      <h2>Security Guarantees</h2>

      <h3>Secrets Never on Disk (Zero-Persistence)</h3>

      <ul>
        <li><strong>Guarantee:</strong> Plaintext secrets are never written to host filesystem</li>
        <li><strong>Implementation:</strong> Secrets loaded into process memory, injected to container tmpfs</li>
        <li><strong>Cleanup:</strong> Memory cleared immediately after injection</li>
        <li><strong>Verification:</strong> Run: <code>sudo find / -name "*SECRET*"</code> — should find nothing</li>
      </ul>

      <h3>Secrets Not in Docker Metadata</h3>

      <ul>
        <li><strong>Guarantee:</strong> Secrets never appear in <code>docker inspect</code></li>
        <li><strong>Implementation:</strong> Secrets NOT stored as environment variables; injected at runtime to memory only</li>
        <li><strong>Verification:</strong> Run: <code>docker inspect container-id | grep SECRET</code> — returns nothing</li>
        <li><strong>Implication:</strong> Even if attacker has Docker socket access, cannot extract secrets</li>
      </ul>

      <h3>Encryption at Rest</h3>

      <ul>
        <li><strong>Local Mode Vault:</strong> AES-256 encryption with PBKDF2 key derivation</li>
        <li><strong>Vault Backend:</strong> Vault's encryption at rest (tuning is your responsibility)</li>
        <li><strong>AWS Secrets Manager:</strong> AWS KMS encryption (AWS manages keys)</li>
        <li><strong>Azure Key Vault:</strong> FIPS 140-2 Level 2 encryption (Azure manages keys)</li>
        <li><strong>Huawei KMS:</strong> National encryption standards</li>
      </ul>

      <h3>Access Control</h3>

      <ul>
        <li><strong>Agent Service:</strong> Runs as systemd service (configurable user, default: dso)</li>
        <li><strong>Docker Access:</strong> Requires docker group membership</li>
        <li><strong>Credential Access:</strong> Only agent can read credentials from provider</li>
        <li><strong>State Access:</strong> State files owned by dso user (mode 0600)</li>
      </ul>

      <h3>Network Security</h3>

      <ul>
        <li><strong>Provider Communication:</strong> All provider APIs use TLS 1.2+ (enforced)</li>
        <li><strong>Certificate Validation:</strong> Always validated (no insecure option)</li>
        <li><strong>Webhook Verification:</strong> Webhooks include provider-specific HMAC signature</li>
        <li><strong>No Plaintext Transmission:</strong> Secrets never transmitted unencrypted</li>
      </ul>

      <h2>Threat Model</h2>

      <h3>Attack Vectors We Protect Against</h3>

      <table>
        <thead>
          <tr>
            <th>Threat</th>
            <th>Protection</th>
            <th>Residual Risk</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Host Filesystem Compromise</strong><br/>Attacker reads host disk files for secrets</td>
            <td>Secrets never written to disk as plaintext</td>
            <td>None (guaranteed by design)</td>
          </tr>
          <tr>
            <td><strong>docker inspect Exposure</strong><br/>Attacker uses docker API to read secrets</td>
            <td>Secrets not stored as environment variables</td>
            <td>None (guaranteed by design)</td>
          </tr>
          <tr>
            <td><strong>Container Image Tampering</strong><br/>Attacker modifies image to extract secrets</td>
            <td>Use image signing (not DSO responsibility)</td>
            <td>Mitigated by image policy/signing</td>
          </tr>
          <tr>
            <td><strong>Process Memory Dump</strong><br/>Attacker dumps agent memory to read secrets</td>
            <td>Secrets cleared from agent memory after injection</td>
            <td>Minimal window (milliseconds) of exposure</td>
          </tr>
          <tr>
            <td><strong>Network Eavesdropping</strong><br/>Attacker intercepts provider API calls</td>
            <td>TLS 1.2+ encryption, certificate validation</td>
            <td>None (TLS prevents eavesdropping)</td>
          </tr>
          <tr>
            <td><strong>Provider Credential Exposure</strong><br/>Attacker extracts provider credentials</td>
            <td>Credentials stored by provider's secure mechanism</td>
            <td>Depends on provider's security (AWS, Azure, Vault)</td>
          </tr>
          <tr>
            <td><strong>Webhook Injection</strong><br/>Attacker sends fake rotation webhooks</td>
            <td>Webhook HMAC signature validation</td>
            <td>None (forged webhooks rejected)</td>
          </tr>
          <tr>
            <td><strong>Stale Secret Rotation</strong><br/>Attacker with old secret causes outage</td>
            <td>Automatic rotation, atomic swaps, quick rollback</td>
            <td>Covered by operational safeguards</td>
          </tr>
        </tbody>
      </table>

      <h3>Attack Vectors We DON'T Protect Against</h3>

      <table>
        <thead>
          <tr>
            <th>Threat</th>
            <th>Reason</th>
            <th>Mitigation</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Host Kernel Compromise</strong><br/>Attacker with kernel-level access</td>
            <td>No system can protect against kernel-level compromise</td>
            <td>Host hardening, kernel security updates</td>
          </tr>
          <tr>
            <td><strong>Hypervisor Escape</strong><br/>VM escape on cloud host</td>
            <td>Outside our security boundary</td>
            <td>Cloud provider security, hypervisor updates</td>
          </tr>
          <tr>
            <td><strong>Side-Channel Attacks</strong><br/>Cache/timing attacks on CPU</td>
            <td>Not feasible to protect against at application layer</td>
            <td>CPU microcode updates, hardware security</td>
          </tr>
          <tr>
            <td><strong>Supply Chain Attack</strong><br/>Malicious DSO binary</td>
            <td>Verify binary integrity yourself</td>
            <td>Verify checksums, GPG signatures, build from source</td>
          </tr>
          <tr>
            <td><strong>Provider Account Compromise</strong><br/>Attacker with AWS/Vault credentials</td>
            <td>Outside our scope (provider responsibility)</td>
            <td>Provider security, credential rotation, audit logs</td>
          </tr>
        </tbody>
      </table>

      <h2>Security Best Practices</h2>

      <h3>1. Minimize Docker Socket Exposure</h3>

      <pre><code className="language-bash">
# Bad: Give everyone Docker access
sudo usermod -aG docker $USER

# Better: Use sudo with specific dso commands
sudo docker dso bootstrap agent

# Best: Run agent as non-root dso user
sudo usermod -aG docker dso
sudo systemctl start dso-agent  # runs as dso user
      </code></pre>

      <h3>2. Restrict Access to State Files</h3>

      <pre><code className="language-bash">
# Verify permissions
ls -la /var/lib/dso/
# Should show:
# drwx------ dso dso /var/lib/dso

# Never world-readable
sudo chmod 700 /var/lib/dso/
      </code></pre>

      <h3>3. Use Provider-Managed Credentials</h3>

      <p>
        Don't store credentials on disk. Use:
      </p>

      <ul>
        <li><strong>AWS:</strong> IAM roles (if on EC2), IAM user credentials in ~/.aws/credentials</li>
        <li><strong>Azure:</strong> Managed Identity, Azure CLI authentication</li>
        <li><strong>Vault:</strong> AppRole auth, JWT auth, Kubernetes auth</li>
      </ul>

      <pre><code className="language-bash">
# AWS: Use IAM roles (best)
# On EC2 instance with IAM role:
# No credentials needed, DSO automatically uses role

# Azure: Use managed identity (best)
# On Azure VM with managed identity:
# No credentials needed

# Vault: Use AppRole (good)
docker dso bootstrap agent
# Enter AppRole ID and Secret (from Vault)
# DSO stores encrypted in secure storage
      </code></pre>

      <h3>4. Enable Audit Logging</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
observability:
  structured_logging: true
  audit_enabled: true
  audit_log_path: /var/log/dso/audit.log
  log_level: info
      </code></pre>

      <p>
        Enables recording of all rotation operations for compliance.
      </p>

      <h3>5. Implement Webhook Signature Validation</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
discovery:
  mode: webhook
  webhook_path: /webhook
  webhook_verify_signature: true
  webhook_signature_header: X-Secret-Signature
      </code></pre>

      <h3>6. Protect Local Vault Passphrase</h3>

      <p>
        In Local mode, passphrase is stored in system keyring:
      </p>

      <ul>
        <li><strong>Linux:</strong> gnome-keyring or pass (KDE Wallet)</li>
        <li><strong>macOS:</strong> Keychain (automatically locked when screen locked)</li>
        <li><strong>Windows:</strong> Credential Manager (DPAPI encrypted)</li>
      </ul>

      <pre><code className="language-bash">
# Verify passphrase is in keyring (not accessible)
secret-tool lookup dso vault-passphrase
# This command requires authorization

# Best practice: Lock system on idle
systemctl suspend  # or lock screen
      </code></pre>

      <h3>7. Network Segmentation</h3>

      <p>
        Restrict access to DSO host:
      </p>

      <ul>
        <li><strong>Firewall:</strong> Only expose webhook port to secret provider</li>
        <li><strong>VPC Rules:</strong> Restrict outbound to provider IPs only</li>
        <li><strong>No SSH Public:</strong> Don't expose DSO host to internet</li>
      </ul>

      <pre><code className="language-bash">
# Example firewall rules
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow from 10.0.0.0/8  # Internal network

# For webhook (if enabled)
sudo ufw allow from 52.89.123.45 to any port 8443  # AWS (example)
      </code></pre>

      <h3>8. Credential Rotation</h3>

      <p>
        Rotate provider credentials periodically:
      </p>

      <ul>
        <li><strong>AWS:</strong> Rotate IAM user access keys every 90 days</li>
        <li><strong>Vault:</strong> Rotate AppRole Secret ID monthly</li>
        <li><strong>Azure:</strong> Use managed identities (auto-rotated)</li>
      </ul>

      <h3>9. TLS Certificate Validation</h3>

      <pre><code className="language-yaml">
# /etc/dso/config.yml
provider:
  type: vault
  url: https://vault.internal.example.com:8200
  tls_verify: true              # Always true (default)
  tls_skip_verify: false        # NEVER set to true
  tls_ca_file: /etc/dso/ca.pem  # Optional: custom CA
      </code></pre>

      <p>
        Never disable certificate verification (<code>tls_skip_verify: false</code>).
      </p>

      <h3>10. Health Check Security</h3>

      <p>
        Health checks can expose secrets if not careful:
      </p>

      <pre><code className="language-yaml">
# Bad: Secret in health check URL
healthcheck:
  test: ["CMD", "curl", "http://localhost:3000/health?token=$TOKEN"]
  # TOKEN is visible in docker inspect

# Good: Secret via header or env
healthcheck:
  test: ["CMD", "sh", "-c", "curl -H 'Authorization: Bearer $TOKEN' http://localhost:3000/health"]
  # Better: Use explicit bearer header
      </code></pre>

      <h2>Security Incident Response</h2>

      <h3>If Provider Credentials Compromised</h3>

      <ol>
        <li>Immediately revoke compromised credentials at provider</li>
        <li>Create new credentials</li>
        <li>Update DSO configuration with new credentials</li>
        <li>Restart agent: <code>sudo systemctl restart dso-agent</code></li>
        <li>Rotate all secrets in provider</li>
        <li>Review audit logs for unauthorized secret access</li>
      </ol>

      <h3>If Local Vault Passphrase Exposed</h3>

      <ol>
        <li>Create new vault with new passphrase: <code>rm -rf ~/.dso && docker dso bootstrap local</code></li>
        <li>Re-add all secrets to new vault</li>
        <li>Update Local mode deployments</li>
        <li>The old vault is now untrusted; discard it</li>
      </ol>

      <h3>If Host Filesystem Compromised</h3>

      <ol>
        <li>Secrets should not be on disk (they aren't)</li>
        <li>Credentials may be in provider keystore—revoke them immediately</li>
        <li>Review audit logs</li>
        <li>Reimage host system</li>
        <li>Rotate all secrets in provider</li>
      </ol>

      <h2>Compliance</h2>

      <h3>FIPS Compliance</h3>

      <ul>
        <li><strong>Local Mode:</strong> Uses Go crypto (non-FIPS)</li>
        <li><strong>AWS:</strong> FIPS mode available (configure in AWS)</li>
        <li><strong>Azure:</strong> FIPS-certified Key Vault</li>
        <li><strong>Vault:</strong> FIPS mode available (configure in Vault)</li>
      </ul>

      <p>
        For FIPS compliance, use cloud-managed providers (AWS, Azure, Vault in FIPS mode).
      </p>

      <h3>PCI-DSS Compliance</h3>

      <ul>
        <li><strong>Requirement 3.4:</strong> Secrets not written to disk ✓</li>
        <li><strong>Requirement 6.2:</strong> Secure configuration management (audit logs) ✓</li>
        <li><strong>Requirement 8.1:</strong> Access control (provider-managed) ✓</li>
        <li><strong>Requirement 10.2:</strong> Audit logging ✓</li>
      </ul>

      <h3>HIPAA Compliance</h3>

      <ul>
        <li>Encryption at rest (via provider) ✓</li>
        <li>Encryption in transit (TLS) ✓</li>
        <li>Access logs (via provider and DSO audit) ✓</li>
        <li>No plaintext secrets on disk ✓</li>
      </ul>

      <h3>SOC 2 Compliance</h3>

      <ul>
        <li>Audit logging (all operations logged) ✓</li>
        <li>Access control (provider-based auth) ✓</li>
        <li>Encryption (TLS, at-rest encryption) ✓</li>
        <li>Availability (crash recovery, health checks) ✓</li>
      </ul>

      <h2>Security Scanning & Hardening</h2>

      <h3>Image Scanning</h3>

      <pre><code className="language-bash">
# Scan DSO binary for vulnerabilities
docker run --rm -v /opt/dso:/scan aquasec/trivy file /scan/dso-agent

# Scan container image
docker run --rm -i aquasec/trivy image myapp:latest
      </code></pre>

      <h3>Runtime Security Monitoring</h3>

      <p>
        Consider runtime security tools:
      </p>

      <ul>
        <li><strong>Falco:</strong> Runtime threat detection</li>
        <li><strong>Cilium:</strong> Network policy enforcement</li>
        <li><strong>OPA/Gatekeeper:</strong> Admission control</li>
        <li><strong>Seccomp:</strong> System call filtering</li>
      </ul>

      <h3>Host Hardening</h3>

      <ul>
        <li><strong>Kernel Updates:</strong> Apply security patches immediately</li>
        <li><strong>Firewall:</strong> Deny all, allow only required</li>
        <li><strong>SELinux/AppArmor:</strong> Enable mandatory access control</li>
        <li><strong>SSH Keys:</strong> Use Ed25519, disable password auth</li>
        <li><strong>Audit Daemon:</strong> Log system calls via auditd</li>
      </ul>

      <h2>Key Takeaways</h2>

      <ul>
        <li><strong>Secrets Never on Disk:</strong> Guaranteed by design</li>
        <li><strong>Not in Docker Metadata:</strong> Guaranteed by design</li>
        <li><strong>TLS Enforced:</strong> All network communication encrypted</li>
        <li><strong>Audit Trail:</strong> All operations logged</li>
        <li><strong>Access Control:</strong> Provider-based authentication</li>
        <li><strong>Automatic Recovery:</strong> No manual state cleanup</li>
        <li><strong>Compliant:</strong> PCI-DSS, HIPAA, SOC 2 compatible</li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/compliance">Compliance procedures</a></li>
        <li><a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
        <li><a href="/docs/guide/observability">Audit logging and monitoring</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting security issues</a></li>
      </ul>
    </div>
  );
}
