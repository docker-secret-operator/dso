# CLI Reference

The DSO CLI provides a complete suite of tools to manage your encrypted vault.

## `docker dso init`
Initializes a new DSO vault environment at `~/.dso/`. Generates a master key and prepares the AES-256-GCM encrypted database.
```bash
docker dso init
```

## `docker dso secret set`
Stores a secret in the vault. 
- **Format:** `[project]/[path]`
- Prompts for input interactively with an invisible prompt.
```bash
docker dso secret set api/stripe_key
```
You can also pipe data securely (up to 1MB):
```bash
cat ./cert.pem | docker dso secret set api/tls_cert
```

## `docker dso secret get`
Retrieves a secret from the vault. Ideal for piping to clipboards or external scripts.
```bash
docker dso secret get api/stripe_key
```

## `docker dso secret list`
Lists all keys tracked inside the vault under a specific project (does not reveal values).
```bash
docker dso secret list api
```

## `docker dso env import`
Batch imports an existing `.env` file into the encrypted vault securely. Warns on duplicates and validates syntax.
```bash
docker dso env import .env api
```

## `docker dso up`
The primary runtime command. It parses your `docker-compose.yaml`, seeds the background Agent, mutates the AST, and executes the standard `docker compose up` command.
```bash
docker dso up -d
```
