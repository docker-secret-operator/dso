import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Local Mode Setup",
  description: "Configure Docker Secret Operator local encrypted vault for development and testing."
};

export default function LocalProviderPage() {
  return (
    <div>
      <h1>Local Mode: Encrypted Vault Setup</h1>

      <p>
        Local mode is perfect for development, testing, and single-host scenarios. Secrets are stored in an encrypted local vault with no external dependencies or cloud access required.
      </p>

      <h2>When to Use Local Mode</h2>

      <ul>
        <li><strong>Development:</strong> Local development without cloud providers</li>
        <li><strong>Testing:</strong> CI/CD pipelines that need secret rotation</li>
        <li><strong>Single Host:</strong> Not distributed across multiple hosts</li>
        <li><strong>No Cloud:</strong> No AWS, Azure, or Vault available</li>
        <li><strong>Rapid Iteration:</strong> Quick setup without complex authentication</li>
      </ul>

      <h2>When NOT to Use Local Mode</h2>

      <ul>
        <li><strong>Multi-Host:</strong> Secrets need to be shared across hosts</li>
        <li><strong>Production:</strong> For production, use Agent mode with cloud provider</li>
        <li><strong>High Security:</strong> Need encryption key management outside host</li>
        <li><strong>Compliance:</strong> Audit trail required (local mode has limited audit)</li>
      </ul>

      <h2>Quick Start: Local Mode</h2>

      <h3>Step 1: Initialize Local Vault</h3>

      <pre><code className="language-bash">
docker dso bootstrap local
      </code></pre>

      <p>
        Interactive setup:
      </p>

      <pre><code className="language-text">
DSO Local Mode Setup

Vault Location: /home/user/.dso/vault.enc
This directory will be created if it doesn't exist.

Enter vault passphrase (8+ characters):
> my-secure-passphrase

Confirm passphrase:
> my-secure-passphrase

Vault created successfully!
✓ Vault initialized at /home/user/.dso/vault.enc
✓ Passphrase stored in system keyring (gnome-keyring)
✓ Ready to use: docker dso secret set
      </code></pre>

      <h3>Step 2: Add Secrets</h3>

      <pre><code className="language-bash">
# Add a secret
docker dso secret set DATABASE_PASSWORD "my-password-123"
docker dso secret set API_KEY "key-abc-xyz"
docker dso secret set DB_USER "postgres"

# List secrets (without showing values)
docker dso secret list

# Output:
# DATABASE_PASSWORD
# API_KEY
# DB_USER
      </code></pre>

      <h3>Step 3: Create docker-compose.yml</h3>

      <pre><code className="language-yaml">
version: '3.8'

services:
  database:
    image: postgres:15
    environment:
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DATABASE_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 3

  api:
    image: myapp:latest
    depends_on:
      database:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://${DB_USER}:${DATABASE_PASSWORD}@database:5432/app
      API_KEY: ${API_KEY}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  postgres_data:
      </code></pre>

      <h3>Step 4: Deploy with Secrets</h3>

      <pre><code className="language-bash">
# Start with secret injection
docker dso up

# Verify services started
docker ps

# Check logs
docker compose logs -f
      </code></pre>

      <h3>Step 5: Stop Deployment</h3>

      <pre><code className="language-bash">
docker dso down
      </code></pre>

      <h2>Vault Structure</h2>

      <h3>Vault File Location</h3>

      <pre><code className="language-bash">
# Linux/macOS
~/.dso/vault.enc                 # Main encrypted vault file
~/.dso/vault.key                 # Derived key (do not edit)

# Contents
ls -la ~/.dso/
# drwx------ user user .dso
# -rw------- user user vault.enc (2KB-10KB, encrypted)
# -rw------- user user vault.key (metadata, encrypted)
      </code></pre>

      <h3>Vault Structure</h3>

      <pre><code className="language-text">
~/.dso/vault.enc (AES-256-GCM encrypted):

{
  "version": "3.5.1",
  "secrets": {
    "DATABASE_PASSWORD": "my-password-123",
    "API_KEY": "key-abc-xyz",
    "DB_USER": "postgres"
  },
  "metadata": {
    "created_at": "2025-03-15T10:30:00Z",
    "modified_at": "2025-03-15T10:35:00Z",
    "encryption": "AES-256-GCM",
    "key_derivation": "PBKDF2"
  }
}
      </code></pre>

      <h2>Secret Management CLI</h2>

      <h3>Add Secret</h3>

      <pre><code className="language-bash">
docker dso secret set NAME value
docker dso secret set API_KEY "secret-value"

# With --force to overwrite
docker dso secret set --force API_KEY "new-value"
      </code></pre>

      <h3>Get Secret</h3>

      <pre><code className="language-bash">
# Show secret value (only when needed)
docker dso secret get API_KEY
# Output: secret-value

# Copy to clipboard (secure)
docker dso secret get API_KEY | pbcopy  # macOS
docker dso secret get API_KEY | xclip   # Linux
      </code></pre>

      <h3>Delete Secret</h3>

      <pre><code className="language-bash">
docker dso secret delete API_KEY

# Confirm deletion
# Are you sure? (y/N): y
# Secret deleted: API_KEY
      </code></pre>

      <h3>List Secrets</h3>

      <pre><code className="language-bash">
docker dso secret list
# Output:
# DATABASE_PASSWORD
# API_KEY
# DB_USER

# With verbose (shows modification time)
docker dso secret list -v
# DATABASE_PASSWORD   2025-03-15 10:30:00
# API_KEY             2025-03-15 10:30:05
# DB_USER             2025-03-15 10:30:10
      </code></pre>

      <h3>Import Secrets from File</h3>

      <pre><code className="language-bash">
# Create secrets file (.env format)
cat > secrets.env << 'EOF'
DATABASE_PASSWORD=postgres123
API_KEY=mykey456
DB_USER=postgres
EOF

# Import (will prompt for each secret)
docker dso secret import secrets.env

# Or import all without confirmation
docker dso secret import --force secrets.env

# Clean up
rm secrets.env secrets.env.bak  # backup created
      </code></pre>

      <h3>Export Secrets</h3>

      <pre><code className="language-bash">
# Export to .env file (plaintext, secure location!)
docker dso secret export > secrets.env

# Or specify output file
docker dso secret export --output /tmp/secrets.env

# Verify content
cat /tmp/secrets.env

# IMPORTANT: Delete after use
shred -vfz -n 10 /tmp/secrets.env  # Securely overwrite
      </code></pre>

      <h2>Passphrase Management</h2>

      <h3>Stored in System Keyring</h3>

      <p>
        Passphrase is securely stored and retrieved automatically:
      </p>

      <ul>
        <li><strong>Linux (GNOME):</strong> gnome-keyring (auto-locks on session end)</li>
        <li><strong>Linux (KDE):</strong> KDE Wallet (auto-locks on session end)</li>
        <li><strong>macOS:</strong> Keychain (protected by user password, auto-locked when computer sleeps)</li>
        <li><strong>Windows (WSL2):</strong> Credential Manager (DPAPI encrypted)</li>
      </ul>

      <h3>Change Passphrase</h3>

      <pre><code className="language-bash">
# Change passphrase (re-encrypts vault)
docker dso vault change-passphrase

# Prompts:
# Current passphrase:
# > old-passphrase

# New passphrase:
# > new-passphrase

# Confirm new passphrase:
# > new-passphrase

# ✓ Passphrase updated
# ✓ Vault re-encrypted
      </code></pre>

      <h3>Unlock Vault Manually</h3>

      <pre><code className="language-bash">
# If keyring is locked or not responding
docker dso vault unlock

# Prompts:
# Vault passphrase:
# > my-passphrase

# ✓ Vault unlocked (valid for 30 minutes)
      </code></pre>

      <h2>Backup and Recovery</h2>

      <h3>Backup Vault</h3>

      <pre><code className="language-bash">
# Create encrypted backup
cp ~/.dso/vault.enc ~/backups/vault.enc.backup
cp ~/.dso/vault.key ~/backups/vault.key.backup

# Or with date
mkdir -p ~/backups
tar czf ~/backups/dso-vault-$(date +%Y-%m-%d).tar.gz ~/.dso/

# Verify backup
tar tzf ~/backups/dso-vault-2025-03-15.tar.gz
      </code></pre>

      <h3>Restore from Backup</h3>

      <pre><code className="language-bash">
# Restore vault files
cp ~/backups/vault.enc.backup ~/.dso/vault.enc
cp ~/backups/vault.key.backup ~/.dso/vault.key

# Or restore tar backup
tar xzf ~/backups/dso-vault-2025-03-15.tar.gz -C ~/

# Permissions must be 700
chmod 700 ~/.dso
chmod 600 ~/.dso/vault.enc
chmod 600 ~/.dso/vault.key

# Verify restored
docker dso secret list
      </code></pre>

      <h3>Disaster Recovery: Lost Passphrase</h3>

      <p>
        If passphrase is lost and cannot be recovered from keyring:
      </p>

      <pre><code className="language-bash">
# Vault cannot be recovered (encrypted with lost passphrase)
# Must create new vault and re-add secrets

# Step 1: Remove old vault
rm -rf ~/.dso/vault.enc ~/.dso/vault.key

# Step 2: Create new vault
docker dso bootstrap local
# Enter new passphrase

# Step 3: Re-add all secrets
docker dso secret set DATABASE_PASSWORD "value"
docker dso secret set API_KEY "value"
# ... repeat for all secrets
      </code></pre>

      <h2>Local Mode with CI/CD</h2>

      <h3>GitHub Actions Example</h3>

      <pre><code className="language-yaml">
name: Test with Secrets

on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install DSO
        run: |
          curl -sSL https://github.com/antiersolutions/docker-secret-operator/releases/download/v3.5.1/install.sh | bash

      - name: Setup Local Vault
        run: |
          # Non-interactive setup (for CI/CD)
          echo "my-ci-passphrase" | docker dso bootstrap local --passphrase-stdin

      - name: Add Test Secrets
        run: |
          docker dso secret set DATABASE_PASSWORD "test-password"
          docker dso secret set API_KEY "test-key"

      - name: Run Tests with Secrets
        run: |
          docker dso up -f docker-compose.test.yml
          # Run tests
          docker dso down
      </code></pre>

      <h3>Docker Compose Test File</h3>

      <pre><code className="language-yaml">
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_PASSWORD: ${DATABASE_PASSWORD}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 3s
      retries: 5

  test:
    image: myapp:test
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://postgres:${DATABASE_PASSWORD}@postgres:5432/test
      API_KEY: ${API_KEY}
    command: npm test
      </code></pre>

      <h2>Troubleshooting</h2>

      <h3>Vault Locked After Boot</h3>

      <pre><code className="language-bash">
# Unlock manually
docker dso vault unlock
# Enter passphrase when prompted

# Or restart keyring manager (Linux GNOME)
pkill gnome-keyring
# Will restart automatically

# Or unlock screen (macOS)
# Unlock Mac to access Keychain
      </code></pre>

      <h3>Secrets Not Injecting</h3>

      <pre><code className="language-bash">
# Verify secrets exist
docker dso secret list

# Verify vault is unlocked
docker dso vault unlock  # Try to unlock (will fail if already unlocked)

# Check error messages
docker dso up --debug
# Should show substitution details
      </code></pre>

      <h3>Cannot Add Secret (Vault Full?)</h3>

      <pre><code className="language-bash">
# Local vault has no practical size limit
# If experiencing issues:

# Check vault file size
ls -lh ~/.dso/vault.enc
# Should be <10MB even with hundreds of secrets

# If corrupted, export, delete, recreate
docker dso secret export > backup.env
rm -rf ~/.dso
docker dso bootstrap local  # New passphrase
docker dso secret import backup.env
      </code></pre>

      <h2>Security Notes</h2>

      <ul>
        <li><strong>Passphrase:</strong> Choose strong passphrase (12+ characters, mix of types)</li>
        <li><strong>Backup:</strong> Backup vault file, but keep passphrase separate</li>
        <li><strong>Keyring:</strong> Keyring must be unlocked when using secrets (auto-unlock on login)</li>
        <li><strong>Disk:</strong> Encrypt disk if available (LUKS on Linux, FileVault on macOS)</li>
        <li><strong>Never Export:</strong> Don't export plaintext secrets unless absolutely necessary</li>
      </ul>

      <h2>Comparison: Local vs. Cloud Providers</h2>

      <table>
        <thead>
          <tr>
            <th>Feature</th>
            <th>Local</th>
            <th>AWS</th>
            <th>Azure</th>
            <th>Vault</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Setup Time</strong></td>
            <td>1 minute</td>
            <td>10 minutes</td>
            <td>10 minutes</td>
            <td>15 minutes</td>
          </tr>
          <tr>
            <td><strong>External Deps</strong></td>
            <td>None</td>
            <td>AWS account</td>
            <td>Azure account</td>
            <td>Vault server</td>
          </tr>
          <tr>
            <td><strong>Multi-Host</strong></td>
            <td>No</td>
            <td>Yes</td>
            <td>Yes</td>
            <td>Yes</td>
          </tr>
          <tr>
            <td><strong>Audit Trail</strong></td>
            <td>Basic</td>
            <td>Full</td>
            <td>Full</td>
            <td>Full</td>
          </tr>
          <tr>
            <td><strong>For Production</strong></td>
            <td>Not ideal</td>
            <td>Yes</td>
            <td>Yes</td>
            <td>Yes</td>
          </tr>
        </tbody>
      </table>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/getting-started">Getting started guide</a></li>
        <li><a href="/docs/guide/providers/aws">AWS Secrets Manager setup</a></li>
        <li><a href="/docs/guide/providers/vault">HashiCorp Vault setup</a></li>
        <li><a href="/docs/guide/configuration">Configuration reference</a></li>
      </ul>
    </div>
  );
}
