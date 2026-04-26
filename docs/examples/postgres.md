# Postgres Example

PostgreSQL natively supports Docker secrets via the `POSTGRES_PASSWORD_FILE` environment variable. This makes it a perfect candidate for DSO's zero-persistence file injection (`dsofile://`).

## 1. Store the Secret
Store your database password in the vault:
```bash
docker dso secret set postgres/db_password
```

## 2. Docker Compose Configuration
Configure your `docker-compose.yaml` to use the `dsofile://` protocol.

```yaml
version: '3.8'

services:
  db:
    image: postgres:15-alpine
    restart: always
    environment:
      POSTGRES_USER: admin
      POSTGRES_PASSWORD_FILE: dsofile://postgres/db_password
      POSTGRES_DB: app_db
    ports:
      - "5432:5432"
    volumes:
      - db_data:/var/lib/postgresql/data

volumes:
  db_data:
```

## 3. Run the Database
Start the database securely:
```bash
docker dso up -d
```
The DSO Agent will inject the `db_password` secret as an isolated file in a RAM disk immediately before the `postgres` entrypoint runs.
