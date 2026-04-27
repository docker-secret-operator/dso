# Django Application Example

Django handles configuration via `settings.py`. Using DSO, you can inject your `SECRET_KEY` and database credentials seamlessly.

## 1. Store the Secrets
```bash
docker dso secret set django/secret_key
docker dso secret set django/db_url
```

## 2. Docker Compose Configuration
```yaml
version: '3.8'

services:
  web:
    build: .
    command: python manage.py runserver 0.0.0.0:8000
    volumes:
      - .:/code
    ports:
      - "8000:8000"
    environment:
      # Inject directly into os.environ
      SECRET_KEY: dso://django/secret_key
      DATABASE_URL: dso://django/db_url
      DEBUG: "True"
```

## 3. Application Code (`settings.py`)
Because you used `dso://`, DSO resolves the variables prior to boot. Your Python code remains beautifully simple:

```python
import os
import dj_database_url

SECRET_KEY = os.environ.get('SECRET_KEY')
DEBUG = os.environ.get('DEBUG', 'False') == 'True'

DATABASES = {
    'default': dj_database_url.config(
        default=os.environ.get('DATABASE_URL')
    )
}
```
