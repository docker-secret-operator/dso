## dso inject

Inject secrets directly into a running container

### Synopsis

Inject a secret into a running container without persisting to vault.

This is a one-time injection useful for:
- Testing injection logic
- Debugging application behavior
- Ad-hoc secret updates
- Emergency secret rotation

The secret is written as a file (mode 0400, owner root) into a directory that
must ALREADY be a tmpfs mount of the RUNNING target container; otherwise the
command refuses and writes nothing, so plaintext never reaches the container's
writable layer or a volume. Delivery uses 'docker exec' (the image needs sh,
mktemp, base64 and sha256sum) and an atomic rename. This does NOT persist to the
vault or configuration - use configuration for persistent changes.

This is an administrative command: it acts on whichever container you name
through the Docker endpoint in your environment (DOCKER_HOST, default
/var/run/docker.sock); it does not require the container to be DSO-managed.

Providing the value (safest first):
  - Hidden prompt (default on a terminal).
  - --value-stdin, or piping into the command: the exact bytes are injected;
    a trailing newline is kept unless --trim-newline is given.
  - --value: UNSAFE for real secrets. Command-line arguments are visible in
    shell history, process listings (ps, /proc), CI logs and audit/diagnostic
    tooling. Use it only for throwaway test values.

Examples (synthetic values):
  docker dso inject --container abc123 --secret api_key              # hidden prompt
  printf %s "$NEW_VALUE" | docker dso inject --container my-app --secret pwd --value-stdin
  docker dso inject --container my-app --secret tls_key --value-stdin < key.pem
  docker dso inject --container my-app --secret scratch --value "test-only-value"  # unsafe

```
dso inject [flags]
```

### Options

```
      --container string   Target container ID or name (required)
  -h, --help               help for inject
      --mount string       Directory inside the container; must be an existing tmpfs mount (default "/run/secrets/dso")
      --secret string      Secret path/name (required)
      --trim-newline       Remove one trailing newline from stdin input (nothing else is trimmed)
      --value string       UNSAFE for real secrets: visible in shell history, process lists and CI logs. Prefer --value-stdin or the prompt
      --value-stdin        Read the secret value from stdin (exact bytes, nothing trimmed)
```

### Options inherited from parent commands

```
  -c, --config string   config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path') (default "dso.yaml")
      --no-color        Disable ANSI colour in output (also honours NO_COLOR)
```

### SEE ALSO

* [dso](dso.md)	 - Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose

