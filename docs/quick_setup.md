# 🚀 DSO Quick Setup Guide (V3.1)

Get DSO running in your production or local development environment in under 3 minutes.

---

## 1. Quick Installation (Docker CLI Plugin)
DSO is a native plugin. Run the following command as the user who runs Docker.

```bash
# Production Installer (Linux/macOS)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

*Verification:*
```bash
docker dso version
# Expected: DSO Version v3.1.0 (Plugin Native)
```

---

## 2. Secure Your First Stack (The "First Run" Flow)
DSO follows a safe **Validate → Up** flow to ensure absolute deployment confidence.

### A. Define your Secrets (`dso.yaml`)
Create a minimal configuration to fetch a secret from a local file (for dev) or AWS (for prod).

```yaml
providers:
  local:
    type: file
    config:
      path: "/tmp/dso-secrets"

secrets:
  - name: my-app-key
    provider: local
    mappings:
      KEY: APP_SECRET
```

### B. Validate the Configuration
Ensure your secret providers are reachable and the YAML schema is correct before any traffic hits the stack.

```bash
docker dso validate -c dso.yaml
```

### C. Deploy with Injection
Deploy your standard Docker Compose file. DSO will automatically start the background agent and securely inject the secrets into RAM.

```bash
docker dso up -c dso.yaml -f docker-compose.yml -d
```

---

## 3. DevOps Production Checklist
*   [ ] **Zero Persistence**: Use `inject: {type: file}` in production for highest security (RAM-only).
*   [ ] **IAM Integration**: Use `auth: {method: iam_role}` when running on EC2 or ECS.
*   [ ] **Observability**: Monitor rotation events in real-time with `docker dso watch`.
*   [ ] **Agent Health**: Check the background agent with `docker dso agent --status`.

---

## 🛠️ Production Commands Sheet
| Goal | Command |
| :--- | :--- |
| **Full Deploy** | `docker dso up -c dso.yaml -d` |
| **Clean Stop** | `docker dso down -f docker-compose.yml` |
| **Inspect Cache** | `docker dso fetch [secret_name]` |
| **Live Stream** | `docker dso watch` |
| **Manual Sync** | `docker dso sync` |
