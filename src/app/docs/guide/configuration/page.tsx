import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Configuration Reference",
  description: "Complete configuration options, environment variables, and settings for Docker Secret Operator."
};

export default function ConfigurationPage() {
  return (
    <div>
      <h1>Configuration Reference</h1>

      <p>
        Complete reference for DSO configuration files, environment variables, and all available settings.
      </p>

      <h2>Configuration File Locations</h2>

      <table>
        <thead>
          <tr>
            <th>Mode</th>
            <th>Config Location</th>
            <th>Notes</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Local Mode</strong></td>
            <td><code>~/.dso/vault.enc</code></td>
            <td>Encrypted vault (no config file)</td>
          </tr>
          <tr>
            <td><strong>Agent Mode</strong></td>
            <td><code>/etc/dso/config.yml</code></td>
            <td>Main agent configuration</td>
          </tr>
          <tr>
            <td><strong>Agent Mode</strong></td>
            <td><code>/etc/dso/providers/*.yml</code></td>
            <td>Provider-specific settings</td>
          </tr>
          <tr>
            <td><strong>Agent Mode</strong></td>
            <td><code>.dso/compose.yml</code></td>
            <td>Docker Compose in project (optional override)</td>
          </tr>
        </tbody>
      </table>

      <h2>Main Configuration File</h2>

      <h3>Location: /etc/dso/config.yml</h3>

      <p>
        Complete example with all available options:
      </p>

      <pre><code className="language-yaml">
# === AGENT CONFIGURATION ===
agent:
  # Unique identifier for this agent
  name: "production-host-1"

  # Operating mode: "agent"
  mode: "agent"

  # Log level: debug, info, warn, error
  log_level: "info"

  # Whether to run in cluster mode (experimental)
  cluster_enabled: false

# === PROVIDER CONFIGURATION ===
provider:
  # Provider type: aws, azure, vault, huawei, local
  type: "aws-secrets-manager"

  # Authentication method varies by provider
  # AWS: iam-role, access-key, profile
  # Azure: managed-identity, service-principal
  # Vault: approle, jwt, kubernetes
  auth_method: "iam-role"

  # AWS-specific settings
  aws:
    region: "us-east-1"
    endpoint: "https://secretsmanager.us-east-1.amazonaws.com"
    access_key_id: ""  # Only if using access-key auth
    secret_access_key: ""  # Only if using access-key auth
    profile: ""  # Only if using profile auth

  # Azure-specific settings
  azure:
    vault_url: "https://my-vault.vault.azure.net/"
    tenant_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    client_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    client_secret: ""  # Only if using service principal

  # Vault-specific settings
  vault:
    url: "https://vault.internal.example.com:8200"
    namespace: "admin"
    auth_method: "approle"
    role_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    secret_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    jwt_token: ""  # Only if using JWT auth
    kubernetes_role: ""  # Only if using K8s auth

  # Huawei-specific settings
  huawei:
    url: "https://kms.cn-east-2.myhuaweicloud.com"
    region: "cn-east-2"
    project_id: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
    access_key: "XXXXXXXXXXXXXXXXXXXXXXXX"
    secret_key: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

  # Secret filtering options
  secret_prefix: "prod/"  # Only load secrets matching this prefix
  secret_pattern: ""  # Optional regex pattern for secret names

  # TLS configuration
  tls_verify: true  # Always true in production
  tls_skip_verify: false  # NEVER set to true
  tls_ca_file: ""  # Optional custom CA certificate path
  tls_cert_file: ""  # Optional client certificate
  tls_key_file: ""  # Optional client certificate key

  # Request timeout
  request_timeout_seconds: 30

# === SECRET DISCOVERY CONFIGURATION ===
discovery:
  # Discovery mode: polling or webhook
  mode: "webhook"

  # Polling mode settings
  polling_interval_seconds: 60  # Check for changes every 60 seconds
  polling_timeout_seconds: 10  # Timeout for each poll attempt
  jitter_seconds: 5  # Random jitter to spread requests

  # Webhook mode settings
  webhook_path: "/webhook"
  webhook_port: 8443
  webhook_timeout_seconds: 30
  webhook_verify_signature: true
  webhook_signature_header: "X-Secret-Signature"
  webhook_required_headers:  # Additional headers to validate
    - "X-Custom-Auth"

  # Error handling
  max_consecutive_errors: 3  # Skip polling after 3 consecutive errors
  error_backoff_seconds: 300  # Wait 5 minutes before retrying

# === CONTAINER MANAGEMENT ===
containers:
  # Health check timeout for rotations
  health_check_timeout_seconds: 30

  # Graceful shutdown timeout
  shutdown_grace_period_seconds: 30

  # Container restart policy
  restart_policy: "unless-stopped"

  # Blue-green deployment settings
  blue_green:
    enabled: true
    new_container_suffix: ".new"
    old_container_suffix: ".old"
    cleanup_delay_seconds: 10  # Wait before deleting old container
    cleanup_timeout_seconds: 30  # Timeout for cleanup operations

  # Health check configuration
  health_checks:
    enabled: true
    start_period_seconds: 30
    interval_seconds: 10
    timeout_seconds: 5
    retries: 3
    required_passes: 2  # Number of consecutive passes needed

  # Automatic rollback settings
  rollback:
    enabled: true
    on_health_failure: true
    on_startup_failure: true
    max_retry_attempts: 3

# === STATE MANAGEMENT ===
state:
  # State directory location
  directory: "/var/lib/dso"

  # State file names
  state_file: "state.json"
  checkpoint_file: "checkpoint.json"
  lock_file: "rotation.lock"
  audit_file: "audit.log"

  # Lock configuration
  lock_ttl_seconds: 300  # Lock expires after 5 minutes
  lock_acquire_timeout_seconds: 30  # Max wait to acquire lock
  lock_check_interval_seconds: 5  # Check for stale locks every 5 minutes

  # State persistence
  persist_every_rotation: true
  backup_before_rotation: true
  max_state_file_size_mb: 100  # Archive old states

# === OBSERVABILITY CONFIGURATION ===
observability:
  # Logging
  structured_logging: true  # JSON formatted logs
  log_format: "json"  # json or text
  log_output: "journald"  # journald or file
  log_file: "/var/log/dso/dso-agent.log"
  log_max_size_mb: 100
  log_max_backups: 10
  log_max_age_days: 30

  # Metrics
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: "/metrics"
  metrics_collection_interval_seconds: 15

  # Health checks
  health_check_enabled: true
  health_check_port: 8081
  health_check_path: "/health"

  # Audit logging
  audit_enabled: true
  audit_log_path: "/var/log/dso/audit.log"
  audit_log_format: "json"
  audit_max_size_mb: 50
  audit_max_backups: 10
  audit_max_age_days: 365

  # Tracing (optional)
  tracing_enabled: false
  tracing_endpoint: ""

# === ADVANCED SETTINGS ===
advanced:
  # Concurrency control
  max_concurrent_rotations: 1  # Never > 1 (prevents conflicts)
  worker_pool_size: 4

  # Timeouts
  operation_timeout_seconds: 300  # Max duration for any operation
  provider_request_timeout_seconds: 30

  # Retry configuration
  max_retries: 3
  retry_backoff_base_seconds: 2
  retry_backoff_max_seconds: 60

  # Resource limits
  max_memory_mb: 256
  max_goroutines: 100
  max_open_files: 1024

  # Feature flags
  enable_experimental_features: false
  enable_cluster_mode: false

# === COMPOSE FILE CONFIGURATION ===
compose:
  # Docker Compose file location
  file: "docker-compose.yml"

  # Additional compose files to merge
  override_files:
    - "docker-compose.override.yml"

  # Service filtering
  services:
    enabled: true
    include: []  # If specified, only these services
    exclude: []  # Exclude these services

  # Environment variable substitution
  env_substitution:
    enabled: true
    strict_mode: true  # Fail if secret not found
    prefix: ""  # Required prefix for secrets

      </code></pre>

      <h2>Environment Variables</h2>

      <p>
        Override config file settings with environment variables:
      </p>

      <table>
        <thead>
          <tr>
            <th>Environment Variable</th>
            <th>Config Path</th>
            <th>Example Value</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>DSO_AGENT_NAME</code></td>
            <td><code>agent.name</code></td>
            <td><code>host-1.prod</code></td>
          </tr>
          <tr>
            <td><code>DSO_LOG_LEVEL</code></td>
            <td><code>agent.log_level</code></td>
            <td><code>info</code></td>
          </tr>
          <tr>
            <td><code>DSO_PROVIDER_TYPE</code></td>
            <td><code>provider.type</code></td>
            <td><code>aws</code></td>
          </tr>
          <tr>
            <td><code>DSO_AWS_REGION</code></td>
            <td><code>provider.aws.region</code></td>
            <td><code>us-east-1</code></td>
          </tr>
          <tr>
            <td><code>DSO_DISCOVERY_MODE</code></td>
            <td><code>discovery.mode</code></td>
            <td><code>webhook</code></td>
          </tr>
          <tr>
            <td><code>DSO_DISCOVERY_POLLING_INTERVAL</code></td>
            <td><code>discovery.polling_interval_seconds</code></td>
            <td><code>60</code></td>
          </tr>
          <tr>
            <td><code>DSO_METRICS_PORT</code></td>
            <td><code>observability.metrics_port</code></td>
            <td><code>9090</code></td>
          </tr>
          <tr>
            <td><code>DSO_LOG_OUTPUT</code></td>
            <td><code>observability.log_output</code></td>
            <td><code>journald</code></td>
          </tr>
        </tbody>
      </table>

      <h3>Usage Example</h3>

      <pre><code className="language-bash">
# Set log level via environment variable
export DSO_LOG_LEVEL=debug

# Set provider via environment variable
export DSO_PROVIDER_TYPE=vault
export DSO_VAULT_URL=https://vault.internal:8200

# Start agent (environment variables override config file)
sudo systemctl start dso-agent
      </code></pre>

      <h2>Docker Compose Variable Substitution</h2>

      <h3>Syntax</h3>

      <p>
        DSO recognizes these variable patterns in docker-compose.yml:
      </p>

      <pre><code className="language-yaml">
# Basic substitution
environment:
  DATABASE_PASSWORD: ${DATABASE_PASSWORD}

# With default (if not found)
  API_KEY: ${API_KEY:-default-key}

# Multiple secrets in one value
  DATABASE_URL: postgres://user:${DB_PASSWORD}@postgres:5432/app

# Nested structures
  config.json: |
    {
      "api_key": "${API_KEY}",
      "db_password": "${DB_PASSWORD}"
    }
      </code></pre>

      <h3>Substitution Rules</h3>

      <ul>
        <li><strong>Pattern:</strong> <code>${SECRET_NAME}</code></li>
        <li><strong>Case Sensitive:</strong> <code>PASSWORD</code> ≠ <code>password</code></li>
        <li><strong>Default Value:</strong> <code>${SECRET:-default}</code></li>
        <li><strong>No Nesting:</strong> <code>${${OUTER}}</code> not supported</li>
        <li><strong>Strict Mode:</strong> Error if secret not found (when enabled)</li>
      </ul>

      <h2>Provider-Specific Configuration</h2>

      <h3>AWS Secrets Manager</h3>

      <pre><code className="language-yaml">
provider:
  type: aws-secrets-manager
  auth_method: iam-role  # iam-role, access-key, or profile

aws:
  region: us-east-1
  access_key_id: ""  # Only for access-key method
  secret_access_key: ""  # Only for access-key method
  profile: ""  # Only for profile method
      </code></pre>

      <h3>Azure Key Vault</h3>

      <pre><code className="language-yaml">
provider:
  type: azure-key-vault
  auth_method: managed-identity  # managed-identity or service-principal

azure:
  vault_url: https://my-vault.vault.azure.net/
  tenant_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
  client_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx  # For service-principal
  client_secret: ""  # Only for service-principal
      </code></pre>

      <h3>HashiCorp Vault</h3>

      <pre><code className="language-yaml">
provider:
  type: vault
  auth_method: approle  # approle, jwt, or kubernetes

vault:
  url: https://vault.internal:8200
  namespace: admin
  role_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
  secret_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
      </code></pre>

      <h3>Huawei Cloud</h3>

      <pre><code className="language-yaml">
provider:
  type: huawei-kms

huawei:
  url: https://kms.cn-east-2.myhuaweicloud.com
  region: cn-east-2
  project_id: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
  access_key: XXXXXXXXXXXXXXXXXXXXXXXX
  secret_key: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
      </code></pre>

      <h2>Service Systemd Configuration</h2>

      <h3>Location: /etc/systemd/system/dso-agent.service</h3>

      <pre><code className="language-ini">
[Unit]
Description=Docker Secret Operator Agent
Documentation=https://dso.example.com/docs
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
User=dso
Group=docker
WorkingDirectory=/var/lib/dso

# Environment file for variables
EnvironmentFile=/etc/dso/env

# Main command
ExecStart=/usr/local/bin/dso-agent --config /etc/dso/config.yml

# Restart policy
Restart=always
RestartSec=10

# Resource limits
MemoryLimit=512M
TasksMax=100

# Security
PrivateTmp=true
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=yes

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=dso-agent

[Install]
WantedBy=multi-user.target
      </code></pre>

      <h2>Environment File</h2>

      <h3>Location: /etc/dso/env</h3>

      <pre><code className="language-bash">
# Sensitive credentials can be stored here
# File permissions: 600 (dso:dso)

DSO_AWS_REGION=us-east-1
DSO_VAULT_URL=https://vault.internal:8200
DSO_LOG_LEVEL=info
      </code></pre>

      <h2>Configuration Validation</h2>

      <h3>Validate Config File</h3>

      <pre><code className="language-bash">
# Check YAML syntax
python3 -m yaml < /etc/dso/config.yml

# Or use online validator
yq eval '.' /etc/dso/config.yml
      </code></pre>

      <h3>Dry-Run Test</h3>

      <pre><code className="language-bash">
# Test configuration without starting service
docker dso validate-config --config /etc/dso/config.yml
      </code></pre>

      <h2>Configuration Precedence</h2>

      <p>
        Settings are loaded in this order (later overrides earlier):
      </p>

      <ol>
        <li><code>/etc/dso/config.yml</code> (default values)</li>
        <li><code>/etc/dso/env</code> (environment file)</li>
        <li>Environment variables (<code>DSO_*</code>)</li>
        <li>Command-line flags (if supported)</li>
      </ol>

      <p>
        Example: If <code>DSO_LOG_LEVEL=debug</code> environment variable is set, it overrides <code>log_level: info</code> in config file.
      </p>

      <h2>Common Configuration Mistakes</h2>

      <h3>Mistake 1: Invalid YAML Indentation</h3>

      <pre><code className="language-yaml">
# ❌ Wrong: inconsistent indentation
provider:
type: aws  # Should be indented 2 spaces

# ✓ Correct
provider:
  type: aws
      </code></pre>

      <h3>Mistake 2: Missing Required Fields</h3>

      <pre><code className="language-yaml">
# ❌ Wrong: missing provider.type
provider:
  aws:
    region: us-east-1

# ✓ Correct
provider:
  type: aws-secrets-manager
  aws:
    region: us-east-1
      </code></pre>

      <h3>Mistake 3: Hardcoded Credentials</h3>

      <pre><code className="language-yaml">
# ❌ Wrong: credentials in config file
aws:
  access_key_id: AKIAIOSFODNN7EXAMPLE
  secret_access_key: wJalrXUtnFEMI/...

# ✓ Correct: use environment variables
aws:
  access_key_id: ""  # Read from DSO_AWS_ACCESS_KEY_ID env var
  secret_access_key: ""  # Read from DSO_AWS_SECRET_ACCESS_KEY env var
      </code></pre>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/getting-started">Getting started guide</a></li>
        <li><a href="/docs/guide/cli">CLI reference</a></li>
        <li><a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting configuration issues</a></li>
      </ul>
    </div>
  );
}
