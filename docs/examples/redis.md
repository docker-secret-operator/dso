# Redis Example

While the official Redis image doesn't read passwords from environment variables by default, you can easily configure it to accept a securely injected secret file using a custom startup command.

## 1. Store the Secret
Store your Redis ACL or authentication password:
```bash
docker dso secret set cache/redis_password
```

## 2. Docker Compose Configuration
Pass the injected file directly to the Redis server command.

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    restart: always
    # We pass the file path to --requirepass. 
    # DSO will populate this exact path at runtime via tmpfs.
    command: /bin/sh -c 'redis-server --requirepass "$$(cat /run/secrets/dso/redis_password)"'
    environment:
      REDIS_PASSWORD_FILE: dsofile://cache/redis_password
    ports:
      - "6379:6379"
```
*Note: Because DSO generates filenames based on a hash of the secret path, you can either hardcode the expected hash or simply use the environment variable reference if you write a custom entrypoint script.*

Alternatively, for older environments lacking file injection support, you can use the environment injector (`dso://`):
```yaml
services:
  redis:
    image: redis:7-alpine
    command: /bin/sh -c 'redis-server --requirepass "$$REDIS_PASSWORD"'
    environment:
      REDIS_PASSWORD: dso://cache/redis_password
```
