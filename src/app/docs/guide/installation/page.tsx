import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Installation Guide",
  description: "Installation instructions for Docker Secret Operator on Linux, macOS, and Windows."
};

export default function InstallationPage() {
  return (
    <div>
      <h1>Installation Guide</h1>

      <p>
        Complete installation instructions for Docker Secret Operator v3.5.1 across all platforms.
      </p>

      <h2>System Requirements</h2>

      <h3>Minimum Requirements</h3>

      <ul>
        <li><strong>Docker:</strong> v20.10 or newer</li>
        <li><strong>Docker Compose:</strong> v2.0 or newer (included in Docker Desktop)</li>
        <li><strong>CPU:</strong> 1+ core</li>
        <li><strong>RAM:</strong> 256MB free</li>
        <li><strong>Disk:</strong> 100MB free</li>
      </ul>

      <h3>Recommended</h3>

      <ul>
        <li><strong>Docker:</strong> v24.0 or newer</li>
        <li><strong>RAM:</strong> 1GB+ free</li>
        <li><strong>Disk:</strong> SSD with 500MB free</li>
        <li><strong>Network:</strong> 1Mbps+ for cloud provider integration</li>
      </ul>

      <h2>Platform-Specific Installation</h2>

      <h3>Linux (Ubuntu/Debian)</h3>

      <h4>Step 1: Install Docker</h4>

      <pre><code className="language-bash">
# Add Docker repository
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo \
  "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Verify installation
docker --version
docker compose version

# Add current user to docker group (optional, for non-root use)
sudo usermod -aG docker $USER
newgrp docker
      </code></pre>

      <h4>Step 2: Install DSO</h4>

      <pre><code className="language-bash">
# Download and run installation script
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash

# Verify installation
docker dso --version
      </code></pre>

      <h4>Step 3: Setup for Local Mode (Optional)</h4>

      <pre><code className="language-bash">
# Initialize local vault (no sudo required)
docker dso bootstrap local

# Verify vault created
ls -la ~/.dso/vault.enc
      </code></pre>

      <h4>Step 4: Setup for Agent Mode (Optional)</h4>

      <pre><code className="language-bash">
# Install systemd service
sudo docker dso bootstrap agent

# Start agent service
sudo systemctl start dso-agent
sudo systemctl enable dso-agent

# Verify service
sudo systemctl status dso-agent
      </code></pre>

      <h3>macOS</h3>

      <h4>Step 1: Install Docker Desktop</h4>

      <p>
        Download from <a href="https://www.docker.com/products/docker-desktop">Docker Desktop</a> or use Homebrew:
      </p>

      <pre><code className="language-bash">
brew install docker docker-compose
# Or install Docker Desktop: brew install --cask docker
      </code></pre>

      <p>
        Open Docker Desktop and verify:
      </p>

      <pre><code className="language-bash">
docker --version
docker compose version
      </code></pre>

      <h4>Step 2: Install DSO</h4>

      <pre><code className="language-bash">
# Using Homebrew (recommended)
brew tap antiersolutions/dso https://github.com/antiersolutions/docker-secret-operator
brew install docker-secret-operator

# Or manual installation
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash

# Verify installation
docker dso --version
      </code></pre>

      <h4>Step 3: Setup for Local Mode</h4>

      <pre><code className="language-bash">
# Initialize local vault (no sudo required)
docker dso bootstrap local

# Macros will prompt to store passphrase in Keychain
# Verify vault created
ls -la ~/.dso/vault.enc
      </code></pre>

      <h4>Step 4: Setup for Agent Mode</h4>

      <p>
        macOS uses LaunchAgent instead of systemd:
      </p>

      <pre><code className="language-bash">
# Install LaunchAgent (requires user password)
docker dso bootstrap agent

# Verify agent installed
launchctl list | grep com.antiersolutions.dso-agent

# Check logs
log stream --predicate 'process == "dso-agent"' --level debug
      </code></pre>

      <h3>Windows (WSL2)</h3>

      <h4>Step 1: Install WSL2 & Docker Desktop</h4>

      <pre><code className="language-powershell">
# Open PowerShell as Administrator and run:
wsl --install
wsl --set-default-version 2

# Install Docker Desktop
# Download from https://www.docker.com/products/docker-desktop
# Or use Chocolatey: choco install docker-desktop
      </code></pre>

      <p>
        Restart computer and open Docker Desktop. Verify:
      </p>

      <pre><code className="language-bash">
# In WSL2 terminal
docker --version
docker compose version
      </code></pre>

      <h4>Step 2: Install DSO in WSL2</h4>

      <pre><code className="language-bash">
# In WSL2 terminal
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash

# Verify
docker dso --version
      </code></pre>

      <h4>Step 3: Setup Local Mode</h4>

      <pre><code className="language-bash">
# Initialize local vault
docker dso bootstrap local

# Verify vault
ls -la ~/.dso/vault.enc
      </code></pre>

      <h4>Step 4: Agent Mode Not Recommended</h4>

      <p>
        Agent mode requires systemd which is limited in WSL2. Use Local mode for development or run Agent mode on native Linux host.
      </p>

      <h2>Verification</h2>

      <h3>Check Installation</h3>

      <pre><code className="language-bash">
# Verify Docker plugin is registered
docker dso --version
docker dso --help

# Should show version and all available commands
      </code></pre>

      <h3>Test Local Mode</h3>

      <pre><code className="language-bash">
# Add test secret
docker dso secret set TEST_SECRET "test-value"

# List secrets
docker dso secret list

# Should show: TEST_SECRET (encrypted)
      </code></pre>

      <h3>Test with Docker Compose</h3>

      <pre><code className="language-bash">
# Create test compose file
cat > docker-compose.test.yml << 'EOF'
version: '3.8'
services:
  test:
    image: alpine:latest
    command: sh -c 'echo $TEST_SECRET'
    environment:
      TEST_SECRET: ${TEST_SECRET}
EOF

# Run with DSO
docker dso up -f docker-compose.test.yml

# Should inject secret and run container
      </code></pre>

      <h2>Post-Installation Configuration</h2>

      <h3>Configure Shell Aliases (Optional)</h3>

      <p>
        Add to ~/.bashrc or ~/.zshrc:
      </p>

      <pre><code className="language-bash">
alias dso='docker dso'
alias dso-up='docker dso up'
alias dso-down='docker dso down'
alias dso-status='docker dso status'
alias dso-logs='docker dso logs'
      </code></pre>

      <h3>Configure Auto-Completion (Linux/macOS)</h3>

      <pre><code className="language-bash">
# Bash
docker dso completion bash | sudo tee /etc/bash_completion.d/docker-dso

# Zsh
docker dso completion zsh | sudo tee /usr/share/zsh/site-functions/_docker-dso

# Reload shell
exec $SHELL
      </code></pre>

      <h2>Uninstallation</h2>

      <h3>Remove DSO</h3>

      <h4>Linux/macOS</h4>

      <pre><code className="language-bash">
# Stop agent if running
sudo systemctl stop dso-agent

# Uninstall
sudo rm -rf /opt/dso
sudo rm /usr/local/bin/dso

# Remove systemd service
sudo rm /etc/systemd/system/dso-agent.service
sudo systemctl daemon-reload

# Remove local vault (if using local mode)
rm -rf ~/.dso
      </code></pre>

      <h4>macOS (Homebrew)</h4>

      <pre><code className="language-bash">
brew uninstall docker-secret-operator
      </code></pre>

      <h4>Windows (WSL2)</h4>

      <pre><code className="language-bash">
# In WSL2 terminal
sudo rm -rf /opt/dso
sudo rm /usr/local/bin/dso
rm -rf ~/.dso
      </code></pre>

      <h2>Troubleshooting Installation</h2>

      <h3>Docker Not Found</h3>

      <pre><code className="language-bash">
# Verify Docker is installed and running
docker --version

# Start Docker daemon (Linux)
sudo systemctl start docker
      </code></pre>

      <h3>Permission Denied Installing Plugin</h3>

      <pre><code className="language-bash">
# Run installer with sudo if needed
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | sudo bash
      </code></pre>

      <h3>Docker Plugin Not Recognized</h3>

      <pre><code className="language-bash">
# Restart Docker daemon
sudo systemctl restart docker

# Or on macOS
# Restart Docker Desktop from menu bar
      </code></pre>

      <h3>Vault Initialization Fails</h3>

      <pre><code className="language-bash">
# Check if ~/.dso directory exists
ls -la ~/.dso

# If corrupted, remove and reinitialize
rm -rf ~/.dso
docker dso bootstrap local
      </code></pre>

      <h3>Agent Service Won't Start</h3>

      <pre><code className="language-bash">
# Check service status
sudo systemctl status dso-agent

# View logs
sudo journalctl -u dso-agent -n 50 -e

# Check configuration
sudo cat /etc/dso/config.yml
      </code></pre>

      <h2>Upgrading DSO</h2>

      <h3>Check Current Version</h3>

      <pre><code className="language-bash">
docker dso --version
      </code></pre>

      <h3>Upgrade to Latest</h3>

      <pre><code className="language-bash">
# Stop running workloads
docker dso down

# Stop agent (if running)
sudo systemctl stop dso-agent

# Reinstall
curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash

# Or with Homebrew
brew upgrade docker-secret-operator

# Restart agent
sudo systemctl start dso-agent

# Verify new version
docker dso --version
      </code></pre>

      <h2>Getting Help</h2>

      <ul>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting guide</a></li>
        <li><a href="/docs/guide/faq">Frequently asked questions</a></li>
        <li><a href="https://github.com/antiersolutions/docker-secret-operator/issues">GitHub Issues</a></li>
        <li><a href="https://github.com/antiersolutions/docker-secret-operator/discussions">GitHub Discussions</a></li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/getting-started">5-minute quick start</a></li>
        <li><a href="/docs/guide/configuration">Configure secrets provider</a></li>
        <li><a href="/docs/guide/providers/local">Setup local vault</a></li>
        <li><a href="/docs/guide/providers/aws">Setup AWS integration</a></li>
        <li><a href="/docs/guide/production-readiness">Production deployment checklist</a></li>
      </ul>
    </div>
  );
}
