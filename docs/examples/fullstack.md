# Fullstack Application Example

This example demonstrates how to compose a realistic architecture combining a Node.js API, a React frontend (built with Vite), and a PostgreSQL database.

## 1. Store the Secrets
```bash
docker dso secret set fullstack/db_pass
docker dso secret set fullstack/stripe_key
```

## 2. Docker Compose Configuration
```yaml
version: '3.8'

services:
  database:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: admin
      POSTGRES_DB: app_db
      POSTGRES_PASSWORD_FILE: dsofile://fullstack/db_pass
    volumes:
      - db_data:/var/lib/postgresql/data

  api:
    image: node:20-alpine
    working_dir: /app
    command: npm run dev
    volumes:
      - ./api:/app
    ports:
      - "4000:4000"
    environment:
      # File injection for the DB
      DB_PASSWORD_FILE: dsofile://fullstack/db_pass
      DB_HOST: database
      DB_USER: admin
      
      # Environment injection for the Stripe Key
      STRIPE_SECRET_KEY: dso://fullstack/stripe_key
    depends_on:
      - database

  frontend:
    image: node:20-alpine
    working_dir: /app
    command: npm run dev
    volumes:
      - ./frontend:/app
    ports:
      - "5173:5173"
    environment:
      # Standard public variables
      VITE_API_URL: http://localhost:4000
    depends_on:
      - api

volumes:
  db_data:
```

## 3. Start the Stack
Bring the entire architecture up seamlessly. DSO will resolve the secrets in parallel and mount them safely across the isolated containers.
```bash
docker dso up --build
```
