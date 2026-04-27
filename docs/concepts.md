# Concepts & Architecture

Docker Secret Operator (DSO) relies on three core concepts: URIs, the Agent Runtime, and the Secret Lifecycle.

## 1. Secret Protocols (`dso://` vs `dsofile://`)

DSO introduces two URI protocols to natively resolve secrets within your `docker-compose.yaml`:

### `dsofile://` (Recommended)
**Behavior:** Injects the secret as a physical file inside a `tmpfs` RAM disk (`/run/secrets/dso/`).
**Why use it:** This is the most secure method. The secret is never written to disk, and it does not show up in `docker inspect`. Applications designed for production often read from files (e.g., `POSTGRES_PASSWORD_FILE`).
```yaml
environment:
  API_KEY_FILE: dsofile://global/api_key
```

### `dso://` (Legacy/Compatibility)
**Behavior:** Injects the plain-text secret directly into the environment variable.
**Why use it:** Use this *only* if the application absolutely does not support reading secrets from a file. 
**Warning:** Secrets injected this way are visible if someone runs `docker inspect <container>`.
```yaml
environment:
  API_KEY: dso://global/api_key
```

## 2. The DSO Agent

The **DSO Agent** is a lightweight, background daemon that listens to Docker socket events. 
When you run `docker dso up`:
1. The CLI decrypts the vault and parses your compose file.
2. It sends an in-memory `AgentSeed` (the required secrets) to the Agent.
3. As Docker emits container `create` events, the Agent catches them.
4. The Agent streams a `.tar` archive of your secrets directly into the container's isolated `tmpfs` mount via the Docker API. 

## 3. Secret Lifecycle
1. **Creation:** Secrets are encrypted securely in `~/.dso/vault.enc` using AES-256-GCM.
2. **Runtime Memory:** Secrets exist in RAM only for the split-second `docker dso up` is executing.
3. **Container Restarts:** The Agent tracks container state. If a container natively restarts and its `tmpfs` is wiped, the Agent instantly detects the `start` event and re-injects the secret securely.
