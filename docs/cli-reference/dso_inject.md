## dso inject

Inject secrets directly into a running container

### Synopsis

Inject a secret into a running container without persisting to vault.

This is a one-time injection useful for:
- Testing injection logic
- Debugging application behavior
- Ad-hoc secret updates
- Emergency secret rotation

The secret is mounted as a file inside the container. This does NOT
persist to the vault or configuration - use configuration for persistent changes.

Examples:
  docker dso inject --container my-app --secret db_password --value "secret123"
  docker dso inject --container abc123 --secret api_key  # Prompts for value
  echo "secret123" | docker dso inject --container my-app --secret pwd --mount /etc/secrets

```
dso inject [flags]
```

### Options

```
      --container string   Target container ID or name (required)
  -h, --help               help for inject
      --mount string       Mount path inside container (default "/run/secrets")
      --secret string      Secret path/name (required)
      --value string       Secret value (will prompt if not provided)
```

### Options inherited from parent commands

```
  -c, --config string   config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml) (default "dso.yaml")
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

