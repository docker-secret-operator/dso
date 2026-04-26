# Getting Started with Docker Secret Operator (DSO)

Welcome to DSO, the production-grade, local-first secret runtime system for Docker Compose.

## The Problem
Developing locally with `docker-compose` often involves hardcoding secrets in `.env` files. These files frequently leak into version control, end up scattered across developer machines, and expose sensitive information like database credentials or API keys directly in `docker inspect` outputs. 

## The Solution
DSO replaces plain-text `.env` files with a highly secure, encrypted local vault. It intercepts your `docker-compose.yaml` at runtime and intelligently injects secrets securely into your containers without touching your disk or exposing them to `docker inspect` via zero-persistence `tmpfs` mounts.

## Quick Start

### 1. Initialize DSO
Initialize the secure vault in your workspace:
```bash
docker dso init
```

### 2. Set Secrets
Store a secret securely in the vault under your project namespace:
```bash
docker dso secret set myapp/db_password
```
*(You will be prompted to enter the secret invisibly.)*

### 3. Update docker-compose.yaml
Use the `dsofile://` protocol to fetch the secret at runtime:
```yaml
services:
  database:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://myapp/db_password
```

### 4. Run it!
Simply run your stack using DSO:
```bash
docker dso up
```
The DSO Agent will automatically intercept the creation process, build a temporary RAM disk inside the container, and securely inject your `db_password` as a file right before the container starts.
