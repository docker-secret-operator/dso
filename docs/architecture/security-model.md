# Security Architecture

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define security requirements for persistent storage

---

## Overview

Security model addresses:
- Data at rest encryption
- Access control
- Audit integrity
- Sensitive data protection
- Compliance requirements

---

## Encryption at Rest

### Requirements

**Data to encrypt:**
- Draft configurations (contains structure but no secret values)
- Review records (contains no sensitive values)
- Approval records (contains no sensitive values)
- Audit logs (for privacy, not security)

**Data NOT to encrypt:**
- Secrets themselves (never stored, only referenced)
- Passwords/credentials (never persisted)
- API keys (never persisted)

### Encryption Model

```
SQLite encryption (SQLCipher):

Configuration:
persistence:
  type: "sqlite"
  path: "/var/lib/dso/dso.db"
  encryption:
    enabled: true
    keyDerivation: "PBKDF2"
    keyIterations: 64000
    cipherAlgorithm: "AES-256"

Key management:
- Derived from master key
- Master key from environment variable
- Optional hardware security module (HSM) integration
- Key rotation capability
```

### Key Management

**Phase 4.0 (Simple):**
```
Master key from:
1. Environment variable: DSO_ENCRYPTION_KEY
2. Config file: /etc/dso/encryption-key.txt (600 perms)
3. Fallback: Generated on first startup (stored in config)

Benefits:
- No external dependency
- Operator controls key
- Can rotate manually

Risks:
- Key in environment variables (standard practice)
- Key in config file (protected by OS permissions)
```

**Phase 4.1+ (HSM/KMS):**
```
Hardware Security Module integration:
- AWS KMS
- HashiCorp Vault
- TPM 2.0
- Hardware token

Benefits:
- Key never leaves secure hardware
- Audit trail of key access
- Key rotation by provider
- Compliance-ready (FIPS, etc.)
```

### Encryption Overhead

```
Performance impact:
- Encryption/decryption: ~5% overhead
- Acceptable for query latencies

Storage impact:
- Slight increase (cipher adds metadata)
- Acceptable trade-off
```

---

## Access Control

### Authentication

```
DSO doesn't implement user authentication.
External system handles auth (reverse proxy, SSO, etc.)

Trust model:
- Authentication happens at HTTP/reverse proxy layer
- DSO receives authenticated user ID in header
- DSO implements authorization based on user ID
```

### Authorization Model

**Resource-based access control:**

```
Draft
  - Owner: creator of draft
  - Permissions:
    • Owner: read, write, delete, initiate review
    • Admins: read, write, delete, any draft
    • Other operators: read-only (if shared)

Review
  - Initiator: creator of review
  - Approvers: assigned by initiator or system rule
  - Permissions:
    • Initiator: read, modify status
    • Assigned approver: read, approve/reject
    • Admins: read, modify, override

Approval
  - Approver: only assigned approver can decide
  - Permissions:
    • Assigned approver: create decision once
    • Admin: override, change decision (audit logged)

Audit Log
  - Any authenticated user: read own events
  - Admins: read all events
  - Compliance officer: export for compliance
  - No user: modify or delete events
```

### Implementation

```
On every operation:
1. Extract user ID from context
2. Load resource (draft, review, etc.)
3. Check permissions:
   - Is user owner/approver/admin?
   - Is user explicitly shared with resource?
4. Allow or deny operation
5. Log authorization check

Example:
GET /api/drafts/{id}
  ↓ Load draft
  ↓ Check: user == draft.owner OR user == admin OR in shared_with
  ↓ Yes → return draft
  ↓ No → return 403 Forbidden
```

---

## Sensitive Data Protection

### What We Store

✅ **Safe to store:**
- Draft structure (which containers, which secrets, relationships)
- Review records (timestamps, decisions, who approved)
- Audit logs (what happened, when, who)
- Mapping/secret names (references only)

❌ **NEVER store:**
- Secret values (passwords, tokens, keys)
- API keys
- Database credentials
- Encryption keys (derive from master key)
- Environment variables

### Validation

```
On draft creation:
1. Extract configuration
2. Check no secret values in config
3. Check no environment variable values
4. Check no credentials in mappings
5. Checksum configuration (for tampering detection)
6. Store only structure/names

Serialization check:
  mapping = {
    container: "postgres",
    secret: "db-password"
  }
  ↓ Store only names
  ✅ SAFE to store

  secret = {
    name: "db-password",
    provider: "vault",
    value: "super-secret-1234"  ← NEVER store this
  }
  ↓ Drop value before storage
  ✅ SAFE to store names only
```

---

## Audit Integrity

### Immutable Audit Trail

```
Audit events cannot be:
- Modified after creation
- Deleted (only expired after retention)
- Unsigned
- Out of sequence

Enforcement:
- Write-only access to audit table
- No UPDATE or DELETE on audit records
- Cryptographic signing per event
- Sequence numbering to detect gaps
```

### Audit Signing

```
Event signing:
1. Create event object
2. Serialize to canonical JSON
3. Calculate HMAC-SHA256(event, master_key)
4. Store event + signature

Verification:
1. Load event + signature
2. Recalculate HMAC-SHA256(event, master_key)
3. Compare signatures
4. Reject if mismatch

Benefits:
- Detect tampering
- Prove authenticity
- Compliance requirement
```

### Audit Export Security

```
Export requires:
- Admin authentication
- Reason/justification logged
- Digital signature on export file
- Checksum included
- Timestamp of export

Export format includes:
{
  "exportedAt": "...",
  "exportedBy": "...",
  "reason": "compliance audit",
  "integrity": {
    "signature": "...",
    "checksum": "..."
  },
  "events": [...]
}
```

---

## Approval Integrity

### Decision Immutability

```
Once approval decision made:
- Cannot change (only cancel review)
- Cannot delete
- Cannot modify comments
- Can only add comments (with new timestamp)

Implementation:
  UPDATE approvals
  SET decision = 'approved'
  WHERE id = ?
    AND decision = 'pending'  ← Only if pending
  ← All other updates rejected
```

### Approval Signing (Future)

```
Phase 4.1+: Cryptographic signatures on approvals

Each approval signed by:
- Approver's private key
- Timestamp
- Review ID
- Decision

Benefits:
- Non-repudiation (approver can't deny decision)
- Compliance (legal requirement)
- Audit trail (cannot be faked)

Implementation:
- Public key infrastructure
- Certificate management
- Key rotation
```

---

## Compliance Requirements

### HIPAA

```
Requirements:
✅ Encryption at rest
✅ Encryption in transit (HTTPS)
✅ Audit trail
✅ Access logs
✅ Data integrity verification
✅ Key management
✅ Retention policies

Not required:
- External audit (responsibility of operator)
- Penetration testing (responsibility of operator)
```

### SOC 2 Type II

```
Requirements:
✅ Logical access controls
✅ Audit trail completeness
✅ Data integrity
✅ Key management
✅ Incident response capability
✅ Configuration review

Implementation needed:
- Compliance attestation process
- External audit readiness
- Documentation of controls
```

### GDPR

```
Requirements:
✅ Data minimization (only store necessary)
✅ Encryption at rest
✅ Access controls
✅ Audit trail
✅ Right to deletion (after retention)

Not applicable:
- Personal data (operator names/emails are metadata, not PII)
- Third-party data processing (single organization)
```

---

## Threat Model

### Threat 1: Unauthorized Access to Database File

```
Attack: Copy dso.db file offline, crack encryption

Defense:
- Encryption at rest (AES-256)
- Strong key derivation (PBKDF2 with high iteration count)
- Master key in environment variable or HSM
- File permissions (600 on dso.db)

Mitigation: Acceptable risk for single-server deployment
```

### Threat 2: Unauthorized Access via API

```
Attack: Bypass authentication, access drafts/reviews

Defense:
- Authentication at reverse proxy layer
- Authorization checks on every operation
- Per-resource access control
- Audit logging of access

Mitigation: Responsibility of operator (auth provider)
```

### Threat 3: Audit Log Tampering

```
Attack: Modify audit logs to hide actions

Defense:
- Immutable audit table (no UPDATE/DELETE)
- Cryptographic signing of events
- Sequence numbering to detect gaps
- Off-site backup

Mitigation: Cannot be defeated (append-only design)
```

### Threat 4: Compromise of Encryption Key

```
Attack: Obtain encryption key, decrypt database

Defense:
- Key in environment variable (not in code)
- Key rotation capability
- HSM integration (Phase 4.1+)
- Key access audit logs

Mitigation: Operator responsibility to protect key
```

### Threat 5: Privilege Escalation

```
Attack: Non-admin user tries to access admin resources

Defense:
- Role-based authorization
- Per-resource access checks
- Audit logging of access attempts
- Failed authorization logging

Mitigation: Operator configures authentication properly
```

---

## Implementation Phases

### Phase 4.0A (Current)
- ✅ Define security model
- ✅ Design encryption requirements
- ✅ Design access control model
- ✅ Design audit integrity requirements

### Phase 4.0 (Future)
- Implement SQLite encryption (SQLCipher)
- Implement authorization layer
- Implement audit event signing
- Implement access control checks
- Implement audit export

### Phase 4.1+ (Future)
- HSM/KMS integration
- Cryptographic signing of approvals
- Certificate-based authentication
- Advanced key rotation

---

## Operational Security

### Key Rotation

```
Phase 4.0: Manual rotation
1. Create new encryption key
2. Dump existing database (unencrypted)
3. Import into new encrypted database
4. Verify integrity
5. Replace old database

Phase 4.1+: Automated rotation
- HSM handles key rotation
- Zero-downtime rotation
- Automatic re-encryption
```

### Backup Security

```
Backups must be:
- Encrypted at rest
- Stored securely (separate location)
- Tested for restoration regularly
- Deleted after retention period
- Integrity verified

Example:
  sqlite3 /var/lib/dso/dso.db "VACUUM INTO '/backups/backup.db'"
  ↓ Creates encrypted backup
  gpg --encrypt /backups/backup.db  (optional extra layer)
```

### Incident Response

```
If encryption key compromised:
1. Stop DSO immediately
2. Rotate encryption key
3. Re-encrypt database
4. Audit all access logs
5. Notify operators
6. Review for data exfiltration

If audit log tampered:
1. Stop DSO immediately
2. Restore from backup
3. Investigate access logs
4. Report to compliance

If unauthorized access detected:
1. Revoke user credentials
2. Audit all that user's actions
3. Reset affected resources
4. Notify operators
```

---

## Security Checklist

- [ ] Encryption at rest enabled
- [ ] Master key in environment variable or HSM
- [ ] Audit logs immutable
- [ ] Audit events signed
- [ ] Access controls implemented
- [ ] Authorization checks on every operation
- [ ] User authentication via reverse proxy
- [ ] Key rotation procedure documented
- [ ] Backup encryption procedure documented
- [ ] Incident response plan documented
- [ ] Compliance requirements met
- [ ] Regular security audits scheduled

---

## References

- See: `persistence-options.md` (SQLite encryption)
- See: `audit-logging.md` (audit event structure)
- See: `draft-persistence.md` (sensitive data handling)
