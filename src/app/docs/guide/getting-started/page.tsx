import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Getting Started with DSO",
  description: "5-minute quick start guide for Docker Secret Operator. Setup Local mode or Agent mode production deployment."
};

export default function GettingStartedPage() {
  return (
    <div>
      <h1>Getting Started with Docker Secret Operator</h1>

      <p>
        Get DSO running in 5 minutes. Choose Local mode for development or Agent mode for production.
      </p>

      <h2>Prerequisites</h2>

      <ul>
        <li><strong>Docker:</strong> v20.10+ (with Compose v2.0+)</li>
        <li><strong>Linux/macOS:</strong> Tested on Ubuntu 20.04+, macOS 10.15+</li>
        <li><strong>Windows:</strong> WSL2 with Docker Desktop</li>
        <li><strong>curl:</strong> For installation script</li>
      </ul>

      <h2>Quick Start: Local Mode (Development)</h2>

      <p>
        Local mode is perfect for development. No daemon, no root required, single host only. Secrets stored in encrypted local vault (~/.dso/vault.enc).
      </p>

      <h3>Step 1: Install DSO</h3>

      <pre><code className="language-bash">
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash
      </code></pre>

      <p>This installs the `docker dso` CLI plugin for Docker.</p>

      <h3>Step 2: Initialize Local Vault</h3>

      <pre><code className="language-bash">
docker dso bootstrap local
      </code></pre>

      <p>
        Creates encrypted local vault at <code>~/.dso/vault.enc</code>. You'll be prompted to create a passphrase (stored securely in system keyring).
      </p>

      <h3>Step 3: Add a Secret</h3>

      <pre><code className="language-bash">
docker dso secret set DATABASE_PASSWORD "my-secure-password"
      </code></pre>

      <p>
        Stores encrypted secret in local vault. To list secrets:
      </p>

      <pre><code className="language-bash">
docker dso secret list
      </code></pre>

      <h3>Step 4: Create docker-compose.yml</h3>

      <pre><code className="language-yaml">
version: '3.8'

services:
  app:
    image: nginx:latest
    environment:
      DATABASE_PASSWORD: ${'$'}{DATABASE_PASSWORD}
    ports:
      - "8080:80"
      </code></pre>

      <h3>Step 5: Start with Secrets</h3>

      <pre><code className="language-bash">
docker dso up
      </code></pre>

      <p>
        DSO injects secrets into containers. Verify:
      </p>

      <pre><code className="language-bash">
docker ps
docker dso status
      </code></pre>

      <h3>Step 6: Stop</h3>

      <pre><code className="language-bash">
docker dso down
      </code></pre>

      <p>Stops containers and cleans up.</p>

      <h2>Quick Start: Agent Mode (Production)</h2>

      <p>
        Agent mode is production-grade with automatic recovery, event-driven rotation, cloud provider support, and systemd integration.
      </p>

      <h3>Step 1: Install DSO</h3>

      <pre><code className="language-bash">
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash
      </code></pre>

      <h3>Step 2: Bootstrap Agent</h3>

      <pre><code className="language-bash">
sudo docker dso bootstrap agent
      </code></pre>

      <p>
        Creates systemd service and validates prerequisites. You'll be prompted to configure:
      </p>

      <ul>
        <li><strong>Secret Provider:</strong> Local, AWS Secrets Manager, Azure Key Vault, Vault, or Huawei</li>
        <li><strong>Provider Credentials:</strong> API keys or credentials (stored securely)</li>
        <li><strong>Event Polling:</strong> Webhook mode or polling interval (5-60 seconds)</li>
      </ul>

      <h3>Step 3: Create Agent Configuration</h3>

      <p>
        Configuration stored at <code>/etc/dso/config.yml</code>. Example for AWS:
      </p>

      <pre><code className="language-yaml">
agent:
  name: production-host
  mode: agent
  log_level: info

provider:
  type: aws-secrets-manager
  region: us-east-1
  secret_prefix: prod/

discovery:
  mode: webhook
  webhook_path: /webhook
  port: 8443

observability:
  structured_logging: true
  metrics_port: 9090
  health_check_port: 8081
      </code></pre>

      <h3>Step 4: Start Agent Service</h3>

      <pre><code className="language-bash">
sudo systemctl start dso-agent
sudo systemctl status dso-agent
      </code></pre>

      <p>
        View logs:
      </p>

      <pre><code className="language-bash">
sudo journalctl -u dso-agent -f
      </code></pre>

      <h3>Step 5: Configure Docker Compose for Agent</h3>

      <p>
        Create <code>docker-compose.yml</code> with secret injection:
      </p>

      <pre><code className="language-yaml">
version: '3.8'

services:
  api:
    image: myapp:latest
    environment:
      DATABASE_PASSWORD: ${'$'}{DATABASE_PASSWORD}
      API_KEY: ${'$'}{API_KEY}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  database:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD: ${'$'}{DB_ROOT_PASSWORD}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 3
      </code></pre>

      <h3>Step 6: Deploy</h3>

      <pre><code className="language-bash">
docker compose up -d
      </code></pre>

      <p>
        Agent monitors secrets and automatically rotates containers when secrets change. Verify rotation:
      </p>

      <pre><code className="language-bash">
sudo dso status
sudo dso rotation history
      </code></pre>

      <h2>Verification: Secrets Are Injected</h2>

      <p>
        Verify secrets are properly injected and not exposed:
      </p>

      <h3>Check Container Environment (Should NOT show secrets)</h3>

      <pre><code className="language-bash">
docker inspect my-container | grep DATABASE_PASSWORD
# Should return nothing - secrets not in docker inspect
      </code></pre>

      <h3>Check Memory-Only Injection</h3>

      <pre><code className="language-bash">
docker exec my-container env | grep DATABASE_PASSWORD
# Shows injected secret (in container memory only)
      </code></pre>

      <h3>Verify No Disk Persistence</h3>

      <pre><code className="language-bash">
sudo find /var/lib/docker -name "*DATABASE*"
# Should return nothing - no secrets on host filesystem
      </code></pre>

      <h2>Common Next Steps</h2>

      <ul>
        <li><strong>Local Mode:</strong> <a href="/docs/guide/providers/local">Configure local vault</a>, learn CLI commands</li>
        <li><strong>AWS:</strong> <a href="/docs/guide/providers/aws">Configure AWS Secrets Manager integration</a></li>
        <li><strong>Azure:</strong> <a href="/docs/guide/providers/azure">Configure Azure Key Vault</a></li>
        <li><strong>Vault:</strong> <a href="/docs/guide/providers/vault">Configure HashiCorp Vault</a></li>
        <li><strong>Production:</strong> <a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
        <li><strong>CLI Reference:</strong> <a href="/docs/guide/cli">Complete CLI command reference</a></li>
        <li><strong>Troubleshooting:</strong> <a href="/docs/guide/troubleshooting">Common issues and solutions</a></li>
      </ul>

      <h2>Troubleshooting Quick Fixes</h2>

      <h3>Docker Plugin Not Found</h3>

      <p>
        If `docker dso` command not found, reinstall:
      </p>

      <pre><code className="language-bash">
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash
docker ps  # Restart Docker daemon
      </code></pre>

      <h3>Vault Locked on Startup</h3>

      <pre><code className="language-bash">
# Unlock vault (will prompt for passphrase)
docker dso vault unlock
      </code></pre>

      <h3>Secrets Not Injecting</h3>

      <pre><code className="language-bash">
# Verify DSO service is running
sudo systemctl status dso-agent

# Check logs for errors
sudo journalctl -u dso-agent -n 50

# Verify secret exists
docker dso secret list
      </code></pre>

      <h3>Container Won't Start</h3>

      <p>
        Check DSO logs and compose output:
      </p>

      <pre><code className="language-bash">
docker compose logs
docker dso logs
      </code></pre>

      <h2>Key Concepts to Remember</h2>

      <ul>
        <li><strong>Zero-Persistence:</strong> Secrets injected to process memory/tmpfs only, never on disk</li>
        <li><strong>Automatic Rotation:</strong> When secret changes in provider, DSO detects and rotates containers</li>
        <li><strong>Atomic Swaps:</strong> Blue-green deployment ensures zero-downtime rotation</li>
        <li><strong>Crash Recovery:</strong> If agent crashes, automatically recovers on restart</li>
        <li><strong>Health-Driven:</strong> New container validated before swap; automatic rollback on failure</li>
      </ul>

      <h2>Next: Learn More</h2>

      <ul>
        <li><a href="/docs/guide/architecture">Deep dive: System architecture</a></li>
        <li><a href="/docs/guide/how-it-works">Complete rotation workflow</a></li>
        <li><a href="/docs/guide/configuration">Configuration reference</a></li>
        <li><a href="/docs/guide/cli">CLI command reference</a></li>
      </ul>
    </div>
  );
}
