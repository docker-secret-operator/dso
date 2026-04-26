# Security Model

DSO is engineered from the ground up for zero-trust local development. 

## The Problem with `.env` Files
1. **Disk Leakage:** Plaintext `.env` files are easily committed to Git by accident.
2. **Process Exposure:** Any local process on your machine can read an unencrypted `.env` file.
3. **Docker Inspect:** Docker maps `.env` variables directly into the container spec. Anyone with docker socket access can run `docker inspect <container>` and see your production passwords in plain text.

## How DSO Hardens Security

### 1. Encrypted Storage (AES-256-GCM)
Secrets are stored in `~/.dso/vault.enc` using authenticated encryption. The encryption key is derived via `Argon2id` (128MB memory, 3 passes), defending aggressively against offline brute-force attacks.

### 2. Zero-Persistence File Injection (`tmpfs`)
When you use `dsofile://`, DSO forces a `tmpfs` (RAM disk) mount onto the container at `/run/secrets/dso`. The secret is injected via an in-memory Tar stream. It never touches the host's SSD or HDD, and disappears instantly if the container stops or the machine reboots.

### 3. Evading `docker inspect`
Because `dsofile://` secrets are injected post-creation as isolated files, they are completely invisible to `docker inspect`. The compose file only shows `POSTGRES_PASSWORD_FILE=/run/secrets/dso/filename`.

### 4. Deterministic Daemon Memory
The DSO Agent limits secret lifecycle memory. Rather than caching the full plaintext vault continuously, the CLI only sends an `AgentSeed` (a deduplicated hash map of strictly the required secrets) to the Agent, meaning inactive secrets stay cold and encrypted.
