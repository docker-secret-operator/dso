# MySQL + phpMyAdmin with DSO - Quick Start Card

**Time to Deploy**: 5 minutes | **Difficulty**: Beginner | **Services**: 2

---

## What You'll Get

✅ MySQL database with secure credentials  
✅ phpMyAdmin web interface for database management  
✅ Zero-downtime secret rotation  
✅ No hardcoded passwords in files  

---

## Step 1: Setup DSO (1 minute)

```bash
# Install DSO as Docker plugin
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# Verify installation
docker dso version

# Setup local mode
docker dso setup --mode local

# Initialize vault (run as your user, NOT sudo)
docker dso init
```

✅ You now have an encrypted vault at `~/.dso/vault.enc`

---

## Step 2: Set Secrets (1 minute)

```bash
# Set MySQL root password
docker dso secret set prod-ms/mysql-root-password
# Enter secret for 'prod-ms/mysql-root-password': [type your password, then Enter]

# Set application user
docker dso secret set prod-ms/mysql-user
# Enter secret for 'prod-ms/mysql-user': [e.g., appuser]

# Set application user password
docker dso secret set prod-ms/mysql-user-password
# Enter secret for 'prod-ms/mysql-user-password': [type password, then Enter]
```

✅ Verify secrets were saved:
```bash
docker dso secret list prod-ms
```

Expected output:
```
Secrets in project 'prod-ms':
  - prod-ms/mysql-root-password
  - prod-ms/mysql-user
  - prod-ms/mysql-user-password
```

---

## Step 3: Create docker-compose.yml (1 minute)

**Option A: Use the provided example** (Recommended)
```bash
cp examples/mysql-phpmyadmin-local.yml docker-compose.yml
```

**Option B: Create from scratch**
```bash
cat > docker-compose.yml << 'EOF'
version: '3.8'

services:
  mysql_db:
    container_name: prod-hms-mysql-container
    image: mysql:latest
    labels:
      dso.reloader: "true"
      dso.secrets: "prod-ms"
      dso.update.strategy: "rolling"
    environment:
      MYSQL_ROOT_PASSWORD: dso://prod-ms/mysql-root-password
      MYSQL_USER: dso://prod-ms/mysql-user
      MYSQL_PASSWORD: dso://prod-ms/mysql-user-password
      MYSQL_DATABASE: "myapp_db"
    ports:
      - "3506:3306"
    restart: always
    volumes:
      - mysql_data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 10s
      timeout: 5s
      retries: 5

  phpmyadmin:
    container_name: prod-hms-phpmyadmin-container
    image: phpmyadmin/phpmyadmin:latest
    labels:
      dso.reloader: "true"
      dso.secrets: "prod-ms"
      dso.update.strategy: "rolling"
    ports:
      - "28080:80"
    restart: always
    environment:
      PMA_HOST: mysql_db
      PMA_PORT: 3306
      PMA_USER: dso://prod-ms/mysql-user
      PMA_PASSWORD: dso://prod-ms/mysql-user-password
    depends_on:
      mysql_db:
        condition: service_healthy
    volumes:
      - phpmyadmin_data:/sessions

volumes:
  mysql_data:
  phpmyadmin_data:
EOF
```

✅ Verify syntax:
```bash
docker compose config
```

---

## Step 4: Deploy (1 minute)

```bash
# Deploy services
docker dso up -d

# This will:
# 1. Read docker-compose.yml
# 2. Fetch secrets from vault
# 3. Inject secrets into containers
# 4. Start MySQL and phpMyAdmin
```

Example output:
```
[DSO] Running in LOCAL mode (auto-detected (~/.dso/vault.enc))
[DSO] Resolving secrets...
🚀 Starting DSO agent...
2026/05/20 13:32:20 ✅ [DSO Agent] Docker event stream connected
🐳 Running docker compose...
[+] up 2/2
 ✔ Container prod-hms-mysql-container Created
 ✔ Container prod-hms-phpmyadmin-container Created
```

---

## Step 5: Verify (1 minute)

```bash
# Quick health check
docker dso doctor

# Expected: All checks ✓
```

```bash
# Check status
docker dso status

# Expected: Services running
```

```bash
# Check containers
docker ps

# Expected:
# prod-hms-mysql-container      mysql:latest
# prod-hms-phpmyadmin-container phpmyadmin:latest
```

✅ All systems operational!

---

## Access Your Services

### MySQL Database

**Direct Connection:**
```bash
# Find username and password from vault:
docker dso secret get prod-ms/mysql-user
docker dso secret get prod-ms/mysql-user-password

# Connect with mysql client:
mysql -h 127.0.0.1 -P 3506 -u <username> -p

# Or use connection string:
mysql -h 127.0.0.1 -P 3506 -u appuser -p < /path/to/dump.sql
```

**Via Docker:**
```bash
docker exec -it prod-hms-mysql-container mysql -u root -p
# Password: (from dso://prod-ms/mysql-root-password)
```

### phpMyAdmin Web Interface

**URL:**
```
http://localhost:28080
```

**Login:**
- Username: `appuser` (value from dso://prod-ms/mysql-user)
- Password: (value from dso://prod-ms/mysql-user-password)
- Server: `mysql_db`

**Create Database:**
1. Login to phpMyAdmin
2. Click "Databases"
3. Create new database: `myapp_db` (already created, but you can create more)
4. Click "SQL" tab to run queries

---

## Update a Secret

When you need to change a password:

```bash
# Update the secret
docker dso secret set prod-ms/mysql-root-password
# Enter secret for 'prod-ms/mysql-root-password': [new password]

# DSO automatically:
# 1. Starts new MySQL container with new password
# 2. Health checks the new container
# 3. Swaps old ← → new (zero downtime)
# 4. Stops old container
# Result: Password changed, ZERO downtime!

# Watch the rotation:
docker dso status --watch
```

---

## Common Tasks

### View Current Secrets

```bash
# All secrets
docker dso secret list

# Just prod-ms secrets
docker dso secret list prod-ms

# Get specific value (local mode only)
docker dso secret get prod-ms/mysql-user
```

### Backup Your Vault

```bash
# Create backup of master key
cp ~/.dso/master.key ~/.dso/master.key.backup

# Store backup SECURELY:
# - Encrypted cloud storage
# - Team password manager
# - Safe physical location
# 
# ⚠️ If you lose this key, your vault is unrecoverable!
```

### View Container Logs

```bash
# MySQL logs
docker logs prod-hms-mysql-container -f

# phpMyAdmin logs
docker logs prod-hms-phpmyadmin-container -f

# All logs
docker compose logs -f
```

### Check Secret in Container

```bash
# View environment variables
docker exec prod-hms-mysql-container env | grep MYSQL

# Check file-based secrets (if using dsofile://)
docker exec prod-hms-mysql-container ls -la /run/secrets/
```

### Stop All Services

```bash
docker dso down

# Or with vanilla Docker:
docker compose down
```

### Restart Services

```bash
# Complete restart
docker dso down
docker dso up -d

# Or just restart containers
docker compose restart
```

---

## Troubleshooting

### ❌ "Service not healthy" / Connection refused

```bash
# MySQL needs time to initialize (30-60 seconds)
sleep 30
docker dso doctor

# Check logs
docker logs prod-hms-mysql-container

# Check port
netstat -tlnp | grep 3506
```

### ❌ "Secret not found" or blank password

```bash
# Verify secret exists
docker dso secret list prod-ms

# Verify compose file has dso:// prefix
grep "MYSQL_" docker-compose.yml

# ❌ WRONG: MYSQL_PASSWORD: prod-ms/mysql-user-password
# ✅ RIGHT: MYSQL_PASSWORD: dso://prod-ms/mysql-user-password

# Fix and redeploy
docker dso down
docker dso up -d
```

### ❌ Port already in use

```bash
# Find what's using port 3506
lsof -i :3506
netstat -tlnp | grep 3506

# Either:
# 1. Stop the other process
sudo kill <PID>

# 2. Or use different port in docker-compose.yml
# Change: ports: - "3506:3306"
# To:     ports: - "3507:3306"
```

### ❌ "Cannot connect to Docker daemon"

```bash
# Check Docker is running
docker ps

# If not, start Docker
sudo systemctl start docker

# Add your user to docker group
sudo usermod -aG docker $USER
newgrp docker
```

### ❌ Secrets not injected in container

```bash
# Check labels
docker inspect prod-hms-mysql-container | grep -A 10 Labels

# Expected:
# "dso.reloader": "true"
# "dso.secrets": "prod-ms"

# Check compose file
docker compose config

# Verify dso:// prefix is used
grep "dso://" docker-compose.yml
```

---

## Next Steps

### 1. Test Rotation (2 minutes)

```bash
# Watch status in real time
docker dso status --watch

# In another terminal, update a secret:
docker dso secret set prod-ms/mysql-root-password

# Watch as containers rotate with zero downtime!
```

### 2. Add More Secrets

```bash
# Create additional secrets as needed:
docker dso secret set prod-ms/backup-password
docker dso secret set prod-ms/api-token

# Use in compose file:
environment:
  API_TOKEN: dso://prod-ms/api-token
```

### 3. Learn More

- Full guide: `docs/LOCAL_MODE_GUIDE.md`
- Examples: `examples/LOCAL_MODE_EXAMPLES.md`
- CLI reference: `docs/cli.md`

### 4. Move to Production

When ready for production:
```bash
# Switch to agent mode with cloud provider
docker dso setup --mode agent --provider aws
# or azure, vault, huawei
```

---

## Command Cheat Sheet

```bash
# Setup
docker dso setup --mode local
docker dso init

# Secrets
docker dso secret set prod-ms/mysql-root-password
docker dso secret list prod-ms
docker dso secret get prod-ms/mysql-user

# Deployment
docker dso up -d
docker dso down

# Status
docker dso doctor
docker dso status --watch
docker dso watch

# Logs
docker logs prod-hms-mysql-container -f
docker compose logs -f

# Verification
docker ps
docker dso doctor --level full
```

---

## Tips & Tricks

✅ **Always run `docker dso init` as your user, NOT sudo**

✅ **Backup `~/.dso/master.key` — it's critical!**

✅ **Use `dsofile://` for very sensitive data (invisible to `docker inspect`)**

✅ **Test rotation with `docker dso status --watch` — it's cool to watch!**

✅ **Keep `docker-compose.yml` in version control, but add `.dso/` to `.gitignore`**

⚠️ **Never hardcode secrets in compose files**

⚠️ **Don't commit `~/.dso/master.key` to git**

⚠️ **Don't share secrets via Slack, email, or chat — use `docker dso secret set`**

---

## Support

**Something not working?**

1. Check health: `docker dso doctor --level full`
2. View logs: `docker compose logs -f`
3. Verify secrets: `docker dso secret list prod-ms`
4. Read errors carefully and check `.dso` directory

**Still stuck?**

- Full guide: `docs/LOCAL_MODE_GUIDE.md`
- Examples: `examples/LOCAL_MODE_EXAMPLES.md`
- Issues: https://github.com/docker-secret-operator/dso/issues

---

## Success Checklist

- [x] DSO installed (`docker dso version` works)
- [x] Local mode initialized (`~/.dso/vault.enc` exists)
- [x] Secrets created (3 prod-ms secrets)
- [x] docker-compose.yml using `dso://` prefix
- [x] Services deployed (`docker ps` shows 2 running containers)
- [x] Health check passing (`docker dso doctor` all ✓)
- [x] MySQL accessible (can connect on :3506)
- [x] phpMyAdmin accessible (http://localhost:28080)

🎉 **You're done!**

---

**Version**: 1.0  
**Last Updated**: May 2026  
**Status**: Production Ready ✅
