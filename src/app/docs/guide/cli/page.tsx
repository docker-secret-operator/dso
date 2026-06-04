import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO CLI Reference",
  description: "Complete Docker Secret Operator command-line interface (CLI) reference and examples."
};

export default function CLIReferencePage() {
  return (
    <div>
      <h1>CLI Reference</h1>

      <p>
        Complete reference for Docker Secret Operator command-line tools and operations.
      </p>

      <h2>Installation Verification</h2>

      <pre><code className="language-bash">
# Verify DSO is installed
docker dso --version
# Output: Docker Secret Operator v3.5.1

# Check all available commands
docker dso --help

# Get help for specific command
docker dso [command] --help
      </code></pre>

      <h2>Local Mode Commands</h2>

      <h3>Bootstrap / Initialization</h3>

      <pre><code className="language-bash">
# Initialize local encrypted vault
docker dso bootstrap local

# For non-interactive (CI/CD):
echo "my-passphrase" | docker dso bootstrap local --passphrase-stdin
      </code></pre>

      <h3>Secret Management</h3>

      <pre><code className="language-bash">
# Add a secret
docker dso secret set DATABASE_PASSWORD "my-password"
docker dso secret set API_KEY "api-key-value"

# Add multiple secrets
docker dso secret set DB_USER postgres
docker dso secret set DB_HOST localhost
docker dso secret set DB_PORT 5432

# Get secret value
docker dso secret get API_KEY

# List all secrets
docker dso secret list
docker dso secret list -v  # Verbose (shows timestamps)

# Delete a secret
docker dso secret delete API_KEY
docker dso secret delete --force API_KEY  # Skip confirmation

# Import secrets from .env file
docker dso secret import secrets.env
docker dso secret import --force secrets.env  # Overwrite existing

# Export secrets to .env file
docker dso secret export > secrets.env
docker dso secret export --output backup.env
      </code></pre>

      <h3>Vault Management</h3>

      <pre><code className="language-bash">
# Unlock vault manually
docker dso vault unlock
# Prompts for passphrase

# Change vault passphrase
docker dso vault change-passphrase
# Prompts for old and new passphrase

# Check vault status
docker dso vault status

# Show vault location
docker dso vault info
      </code></pre>

      <h3>Deployment</h3>

      <pre><code className="language-bash">
# Deploy with secret injection
docker dso up
docker dso up -d  # Detached mode
docker dso up -f docker-compose.custom.yml  # Custom compose file

# Stop deployment
docker dso down

# View logs
docker dso logs
docker dso logs -f  # Follow logs

# Check deployment status
docker dso status

# List running containers
docker ps  # Standard Docker command
      </code></pre>

      <h2>Agent Mode Commands</h2>

      <h3>Bootstrap / Installation</h3>

      <pre><code className="language-bash">
# Bootstrap agent (requires sudo, interactive)
sudo docker dso bootstrap agent

# After bootstrap, agent runs as systemd service
sudo systemctl status dso-agent
sudo systemctl start dso-agent
sudo systemctl stop dso-agent
sudo systemctl restart dso-agent

# Enable auto-start
sudo systemctl enable dso-agent
sudo systemctl disable dso-agent
      </code></pre>

      <h3>Agent Status & Info</h3>

      <pre><code className="language-bash">
# Check agent status
sudo dso status

# View agent configuration
sudo dso config show
sudo dso config validate

# Get agent information
sudo dso info
sudo dso version

# Check health
sudo dso health
# Or via HTTP
curl http://localhost:8081/health
      </code></pre>

      <h3>Rotation Management</h3>

      <pre><code className="language-bash">
# View rotation status
sudo dso rotation status

# View rotation history
sudo dso rotation history
sudo dso rotation history --limit 10
sudo dso rotation history --service api  # Specific service

# View detailed rotation info
sudo dso rotation info [rotation-id]

# Manually trigger rotation (force)
sudo dso rotation trigger

# Manually trigger specific service
sudo dso rotation trigger --service api
      </code></pre>

      <h3>State Management</h3>

      <pre><code className="language-bash">
# View current state
sudo dso state show

# Export state for backup
sudo dso state export > state-backup.json

# Import/restore state
sudo dso state import state-backup.json

# Reset state (dangerous!)
sudo dso state reset --force

# Validate state integrity
sudo dso state validate
      </code></pre>

      <h3>Logs & Diagnostics</h3>

      <pre><code className="language-bash">
# View agent logs
sudo journalctl -u dso-agent -f  # Follow

# View recent errors
sudo journalctl -u dso-agent | grep ERROR

# Export logs
sudo journalctl -u dso-agent -S -7d > logs.txt

# Collect diagnostic information
sudo dso diagnose

# Show metrics
curl http://localhost:9090/metrics
      </code></pre>

      <h3>Maintenance</h3>

      <pre><code className="language-bash">
# Clean up old state files
sudo dso cleanup

# Rotate audit logs
sudo dso logs rotate

# Check for resource leaks
sudo dso check-health

# Reset lock (if stuck)
sudo dso lock reset --force
      </code></pre>

      <h2>Docker Integration</h2>

      <pre><code className="language-bash">
# Standard Docker/Docker Compose commands still work
docker ps
docker logs [container]
docker inspect [container]

# Check secrets are NOT exposed
docker inspect [container] | grep -i password
# Should return nothing

# View container environment
docker exec [container] env | grep DATABASE
# Shows injected secrets (memory only)
      </code></pre>

      <h2>Advanced Options</h2>

      <h3>Global Flags</h3>

      <pre><code className="language-bash">
# Most commands support these flags:
--config /path/to/config.yml  # Custom config file
--verbose                       # Verbose output
--json                          # JSON output format
--no-color                      # Disable colored output
--timeout 30s                   # Operation timeout
      </code></pre>

      <h3>Examples with Options</h3>

      <pre><code className="language-bash">
# Add secret with expiry
docker dso secret set API_KEY "value" --expires 30d

# Import with override
docker dso secret import secrets.env --force --verbose

# Status with JSON output
sudo dso status --json | jq .

# Logs with color disabled
sudo journalctl -u dso-agent --no-pager | grep ERROR
      </code></pre>

      <h2>Common Workflows</h2>

      <h3>Workflow 1: Local Development Setup</h3>

      <pre><code className="language-bash">
# 1. Bootstrap local vault
docker dso bootstrap local

# 2. Add secrets
docker dso secret set DATABASE_PASSWORD "postgres"
docker dso secret set API_KEY "dev-key"

# 3. Create docker-compose.yml
cat > docker-compose.yml << 'EOF'
version: '3.8'
services:
  api:
    image: myapp:latest
    environment:
      DATABASE_PASSWORD: ${DATABASE_PASSWORD}
      API_KEY: ${API_KEY}
EOF

# 4. Deploy
docker dso up

# 5. Test
docker exec [container] env | grep API_KEY

# 6. Stop
docker dso down
      </code></pre>

      <h3>Workflow 2: Update Secrets</h3>

      <pre><code className="language-bash">
# Local mode:
docker dso secret set DATABASE_PASSWORD "new-password"
# Then redeploy:
docker dso down && docker dso up

# Agent mode:
aws secretsmanager update-secret \
  --secret-id DATABASE_PASSWORD \
  --secret-string "new-password"
# Agent detects and rotates automatically
      </code></pre>

      <h3>Workflow 3: Backup & Restore</h3>

      <pre><code className="language-bash">
# Backup secrets
docker dso secret export > secrets-backup.env

# Backup state (agent mode)
sudo dso state export > state-backup.json

# Restore secrets
docker dso secret import secrets-backup.env

# Restore state (agent mode)
sudo dso state import state-backup.json
      </code></pre>

      <h3>Workflow 4: Troubleshooting</h3>

      <pre><code className="language-bash">
# 1. Check if agent is running
sudo systemctl status dso-agent

# 2. View recent errors
sudo journalctl -u dso-agent -n 20 | grep ERROR

# 3. Check secret exists
docker dso secret list
aws secretsmanager list-secrets

# 4. Verify provider connectivity
curl -I https://secretsmanager.us-east-1.amazonaws.com

# 5. Check Docker integration
docker ps
docker logs [container]

# 6. Force recovery if needed
sudo systemctl restart dso-agent
      </code></pre>

      <h2>Command Reference Table</h2>

      <table>
        <thead>
          <tr>
            <th>Command</th>
            <th>Mode</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>docker dso bootstrap local</code></td>
            <td>Local</td>
            <td>Initialize local vault</td>
          </tr>
          <tr>
            <td><code>sudo docker dso bootstrap agent</code></td>
            <td>Agent</td>
            <td>Initialize agent service</td>
          </tr>
          <tr>
            <td><code>docker dso secret set NAME value</code></td>
            <td>Local</td>
            <td>Add/update secret</td>
          </tr>
          <tr>
            <td><code>docker dso secret get NAME</code></td>
            <td>Local</td>
            <td>Retrieve secret value</td>
          </tr>
          <tr>
            <td><code>docker dso secret list</code></td>
            <td>Local</td>
            <td>List all secrets</td>
          </tr>
          <tr>
            <td><code>docker dso secret delete NAME</code></td>
            <td>Local</td>
            <td>Delete secret</td>
          </tr>
          <tr>
            <td><code>docker dso secret import FILE</code></td>
            <td>Local</td>
            <td>Import from .env file</td>
          </tr>
          <tr>
            <td><code>docker dso secret export</code></td>
            <td>Local</td>
            <td>Export to .env file</td>
          </tr>
          <tr>
            <td><code>docker dso vault unlock</code></td>
            <td>Local</td>
            <td>Unlock vault manually</td>
          </tr>
          <tr>
            <td><code>docker dso up</code></td>
            <td>Local</td>
            <td>Deploy with secrets</td>
          </tr>
          <tr>
            <td><code>docker dso down</code></td>
            <td>Local</td>
            <td>Stop deployment</td>
          </tr>
          <tr>
            <td><code>sudo dso status</code></td>
            <td>Agent</td>
            <td>Check agent status</td>
          </tr>
          <tr>
            <td><code>sudo dso rotation status</code></td>
            <td>Agent</td>
            <td>View rotation status</td>
          </tr>
          <tr>
            <td><code>sudo dso rotation history</code></td>
            <td>Agent</td>
            <td>View rotation history</td>
          </tr>
          <tr>
            <td><code>sudo dso state show</code></td>
            <td>Agent</td>
            <td>View current state</td>
          </tr>
          <tr>
            <td><code>sudo journalctl -u dso-agent</code></td>
            <td>Agent</td>
            <td>View service logs</td>
          </tr>
        </tbody>
      </table>

      <h2>Exit Codes</h2>

      <table>
        <thead>
          <tr>
            <th>Code</th>
            <th>Meaning</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>0</code></td>
            <td>Success</td>
          </tr>
          <tr>
            <td><code>1</code></td>
            <td>General error</td>
          </tr>
          <tr>
            <td><code>2</code></td>
            <td>Invalid arguments</td>
          </tr>
          <tr>
            <td><code>3</code></td>
            <td>Permission denied</td>
          </tr>
          <tr>
            <td><code>4</code></td>
            <td>Provider error</td>
          </tr>
          <tr>
            <td><code>5</code></td>
            <td>Rotation failed</td>
          </tr>
          <tr>
            <td><code>127</code></td>
            <td>Command not found</td>
          </tr>
        </tbody>
      </table>

      <h2>Shell Completion</h2>

      <h3>Bash</h3>

      <pre><code className="language-bash">
# Install completion
docker dso completion bash | sudo tee /etc/bash_completion.d/docker-dso

# Or manually source in ~/.bashrc:
eval "$(docker dso completion bash)"
      </code></pre>

      <h3>Zsh</h3>

      <pre><code className="language-bash">
# Install completion
docker dso completion zsh | sudo tee /usr/share/zsh/site-functions/_docker-dso

# Or manually in ~/.zshrc:
eval "$(docker dso completion zsh)"
      </code></pre>

      <h2>Tips & Tricks</h2>

      <ul>
        <li><strong>Copy secret to clipboard:</strong> <code>docker dso secret get NAME | pbcopy</code> (macOS) or <code>xclip</code> (Linux)</li>
        <li><strong>View all secrets at once:</strong> <code>docker dso secret list -v</code></li>
        <li><strong>Follow logs in real-time:</strong> <code>sudo journalctl -u dso-agent -f</code></li>
        <li><strong>Export to JSON:</strong> <code>sudo dso status --json | jq .</code></li>
        <li><strong>Create alias for common commands:</strong> <code>alias dso='sudo docker dso'</code></li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/getting-started">Getting started guide</a></li>
        <li><a href="/docs/guide/configuration">Configuration reference</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting guide</a></li>
      </ul>
    </div>
  );
}
