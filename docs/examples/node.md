# Node.js Application Example

Node.js applications often use libraries like `dotenv` in development. With DSO, you can skip `.env` files entirely and securely pass your secrets directly to the Node process.

## 1. Store the Secrets
```bash
docker dso secret set backend/jwt_secret
docker dso secret set backend/api_key
```

## 2. Docker Compose Configuration
If your Node.js application is capable of reading files (using `fs.readFileSync`), use `dsofile://`. Otherwise, use `dso://` to map variables directly into `process.env`.

```yaml
version: '3.8'

services:
  api:
    image: node:20-alpine
    working_dir: /app
    command: npm start
    volumes:
      - ./:/app
    ports:
      - "3000:3000"
    environment:
      # Injected safely as a file for code that supports reading _FILE vars
      JWT_SECRET_FILE: dsofile://backend/jwt_secret
      
      # Injected strictly into the environment for legacy libraries
      API_KEY: dso://backend/api_key
      
      NODE_ENV: development
```

## 3. Application Code Example
```javascript
const fs = require('fs');

// Read from the secure DSO tmpfs mount if available
const jwtSecret = process.env.JWT_SECRET_FILE 
  ? fs.readFileSync(process.env.JWT_SECRET_FILE, 'utf8').trim() 
  : 'fallback-dev-secret';

// Standard environment variable
const apiKey = process.env.API_KEY;

console.log("Server initialized securely!");
```
