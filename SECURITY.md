# Security Policy

## Overview
DSO (Docker Secret Operator) is designed with a "Security-First" architecture, ensuring that sensitive data is managed with minimal exposure and strictly follows the principle of least privilege.

## Secret Lifecycle
The lifecycle of a secret in DSO is ephemeral and volatile:

1. **Fetch**: The DSO Agent retrieves the secret from a secure provider (e.g., HashiCorp Vault) directly into memory.
2. **In-Memory Cache**: Secrets are stored in a non-persistent, RAM-only cache. They are never written to the host's physical disk.
3. **Transport**:
   - For **`env`** mode: Secrets are metadata-injected into the container's environment configuration before start.
   - For **`file`** mode: Secrets are packed into an in-memory `tar` archive and streamed directly into the container's `tmpfs` mount via the Docker API.
4. **Injection**: Secrets become available to the application process at runtime.
5. **Rotation/Cleanup**:
   - On rotation: The old container is removed, and its associated `tmpfs` is wiped by the kernel.
   - On shutdown: The DSO Agent clears its RAM cache. No forensic traces remain on the host disk.

## Security Guarantees
- **Zero-Disk Leaks**: Secrets never touch the host filesystem in plaintext.
- **Redaction by Default**: All DSO logs are filtered through a centralized redaction utility to prevent sensitive data from reaching observability stacks.
- **Isolated Injection**: File-based secrets are injected into kernel-managed `tmpfs` mounts with `0400` (read-only) permissions.

## Reporting Vulnerabilities
Please report any security vulnerabilities via GitHub Issues with the `security` label, or contact the maintainers directly.
