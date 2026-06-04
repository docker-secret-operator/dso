# Persistence Technology Evaluation

**Phase:** 4.0A (Architecture Design)  
**Status:** Evaluation Phase  
**Purpose:** Evaluate persistence technologies for DSO

---

## Evaluation Criteria

### Must-Have Requirements

1. **Single Binary Deployment**
   - No external dependencies
   - Embeddable in Go binary
   - No separate database installation
   - Works offline

2. **Operational Simplicity**
   - Minimal configuration
   - Auto-migration on startup
   - Built-in backup/restore
   - Clear data format

3. **Performance**
   - <50ms query latency
   - <100ms write latency
   - Support for 100K+ records
   - Minimal memory overhead

4. **Security**
   - Encryption support
   - Access control capability
   - Audit log integration
   - Data integrity checking

5. **Embedded Dashboard**
   - Accessible from Go API
   - No network protocol overhead
   - Same process, no IPC
   - Direct memory access

### Nice-to-Have Requirements

- Transaction support
- Query optimization
- Replication capability
- Full-text search
- Time-series support

---

## Option 1: SQLite

### Overview
File-based relational database, single file, serverless.

### Architecture

```
DSO Binary
  ├─ Web UI (static)
  ├─ Go API
  │  └─ sqlite3 driver
  │     └─ dso-data.db (single file)
  └─ Embedded dashboard
```

### Pros ✅

- **Single file**: `dso-data.db` in config directory
- **No setup**: Works out of the box
- **ACID transactions**: Guaranteed data integrity
- **Standard SQL**: Easy migrations, clear queries
- **Mature**: Well-tested, production-ready
- **Embeddable**: sqlite3-go driver available
- **Backup**: Simple file copy
- **Encryption**: SQLCipher extension available
- **Size**: 400 KB driver binary

### Cons ❌

- **No concurrency**: Writer blocks all readers (mitigated by WAL mode)
- **File locking**: Network filesystems problematic
- **Scaling**: Not ideal for >1M records
- **Replication**: Not built-in
- **No authentication**: File-level access only
- **One-replica limit**: No clustering

### Configuration

```
DSO Configuration:
persistence:
  type: "sqlite"
  path: "/var/lib/dso/dso.db"
  journalMode: "WAL"  # Write-Ahead Logging
  cacheSize: 10000
  encryptionKey: "..." # Optional, SQLCipher
```

### Migration Strategy

```
On startup:
1. Check if dso.db exists
2. If not, create schema
3. If yes, run pending migrations
4. Verify data integrity
5. Start DSO

Backup:
  cp /var/lib/dso/dso.db /backups/dso-$(date).db

Restore:
  cp /backups/dso-2026-06-05.db /var/lib/dso/dso.db
  systemctl restart dso
```

### Recommended For

✅ Small-to-medium deployments (1-5 operators)  
✅ Single-server installations  
✅ Development/testing  
✅ Air-gapped environments  
✅ High security requirements (encrypt file)

---

## Option 2: PostgreSQL

### Overview
Powerful relational database, network-based, feature-rich.

### Architecture

```
DSO Binary
  ├─ Web UI (static)
  ├─ Go API
  │  └─ pq driver
  │     └─ (network)
  │        └─ PostgreSQL Server
  │           └─ dso_database
  └─ Embedded dashboard
```

### Pros ✅

- **Unlimited scaling**: Multi-billion records
- **Concurrency**: True parallel readers/writers
- **Advanced features**: JSON, full-text search, window functions
- **Replication**: Built-in streaming replication
- **High availability**: Failover, PITR (Point-In-Time Recovery)
- **Network access**: Remote operations possible
- **Proven**: Used in enterprise deployments
- **Performance**: Excellent query optimizer
- **Security**: Row-level security, roles, encryption

### Cons ❌

- **External dependency**: Separate server installation
- **Setup complexity**: Initial configuration required
- **Network overhead**: Communication latency
- **Operational burden**: Admin, backup, recovery
- **Resource requirements**: Larger footprint
- **License**: Open source (Apache 2.0)
- **Networking**: Requires PostgreSQL accessible from DSO
- **Single binary violation**: Requires separate database server

### Configuration

```
DSO Configuration:
persistence:
  type: "postgresql"
  host: "localhost"
  port: 5432
  database: "dso"
  user: "dso"
  password: "${DSO_DB_PASSWORD}"
  sslMode: "require"
  maxConnections: 20
```

### Migration Strategy

```
On startup:
1. Connect to PostgreSQL
2. Create dso database if not exists
3. Run pending migrations (Flyway/golang-migrate)
4. Verify schema
5. Start DSO

Backup:
  pg_dump -U dso dso | gzip > /backups/dso-$(date).sql.gz

Restore:
  gunzip /backups/dso-2026-06-05.sql.gz
  psql -U postgres < /backups/dso-2026-06-05.sql
  systemctl restart dso
```

### Recommended For

❌ Single binary requirement (violation)  
✅ Large deployments (10+ operators)  
✅ High-availability requirements  
✅ Multi-server infrastructure  
✅ Enterprise deployments  
✅ Future clustering/replication

---

## Option 3: BoltDB (Embedded)

### Overview
Simple key-value store, embedded, single file, no external dependencies.

### Architecture

```
DSO Binary
  ├─ Web UI (static)
  ├─ Go API
  │  └─ bolt driver
  │     └─ dso-data.db (single file)
  └─ Embedded dashboard
```

### Pros ✅

- **Single file**: Like SQLite but simpler
- **No dependencies**: Pure Go, embedded
- **ACID transactions**: Guaranteed
- **Simple API**: Key-value design
- **Fast**: Memory-mapped access
- **Small footprint**: <1 MB driver
- **Backups**: File copy
- **No setup**: Works immediately
- **Good for typed data**: Go structs → bytes

### Cons ❌

- **No SQL**: Must code all logic
- **No querying**: Must iterate to search
- **No indexing**: All queries full-table scans
- **Manual migration**: No schema evolution helpers
- **Concurrency limitations**: One writer at a time (vs SQLite WAL)
- **Learning curve**: Different mental model
- **No encryption**: File-level access only
- **Complex queries**: Cannot express ad-hoc queries

### Configuration

```
DSO Configuration:
persistence:
  type: "boltdb"
  path: "/var/lib/dso/dso.db"
  bucketPrefix: "dso:"
```

### Migration Strategy

```
On startup:
1. Open dso.db
2. Create required buckets if not exist
3. Verify bucket structure
4. Start DSO

Backup:
  cp /var/lib/dso/dso.db /backups/dso-$(date).db

Restore:
  cp /backups/dso-2026-06-05.db /var/lib/dso/dso.db
  systemctl restart dso
```

### Recommended For

✅ Minimal deployments (single operator)  
⚠️ Small deployments (2-3 operators)  
❌ Any deployment needing complex queries  
❌ Any deployment needing multiple indexes

---

## Option 4: File-Based Storage

### Overview
Structured JSON/YAML files, human-readable, git-compatible.

### Architecture

```
DSO Binary
  ├─ Web UI (static)
  ├─ Go API
  │  └─ JSON/YAML marshaler
  │     └─ /var/lib/dso/
  │        ├─ drafts/
  │        ├─ reviews/
  │        ├─ approvals/
  │        ├─ snapshots/
  │        └─ audit-logs/
  └─ Embedded dashboard
```

### Pros ✅

- **Human-readable**: Direct file inspection
- **Version control**: Compatible with git
- **No dependencies**: Pure file I/O
- **Simple model**: One file per entity
- **Backup**: Standard file copy or git
- **Accessible**: Edit manually if needed
- **Portable**: Easy to migrate
- **Transparent**: Know exactly what's stored

### Cons ❌

- **No transactions**: Partial write risk
- **Concurrency**: File locking problematic
- **No indexing**: Directory scans slow
- **Query difficulty**: Load all, filter in memory
- **Scalability**: 100K+ files = slow filesystem
- **Integrity**: No checksums, easy corruption
- **Performance**: I/O bound, not optimized
- **Recovery**: Manual recovery difficult
- **Duplication**: Same data in multiple places

### Configuration

```
DSO Configuration:
persistence:
  type: "files"
  path: "/var/lib/dso/"
  format: "json"  # or "yaml"
  compress: true
  encryptionKey: "..."
```

### Directory Structure

```
/var/lib/dso/
├─ drafts/
│  ├─ draft-uuid-1.json
│  ├─ draft-uuid-2.json
│  └─ ...
├─ reviews/
│  ├─ review-uuid-1.json
│  └─ ...
├─ approvals/
│  └─ approval-uuid-1.json
├─ snapshots/
│  └─ snapshot-uuid-1.json
├─ audit-logs/
│  ├─ 2026-06-05.jsonl
│  ├─ 2026-06-04.jsonl
│  └─ ...
└─ schema-version.txt
```

### Recommended For

✅ Development/testing  
⚠️ Very small deployments (git compatibility)  
❌ Production with >10 operators  
❌ Any deployment needing concurrency  
❌ Any deployment needing queries

---

## Recommendation Matrix

| Criterion | SQLite | PostgreSQL | BoltDB | Files |
|-----------|--------|-----------|--------|-------|
| Single binary | ✅ | ❌ | ✅ | ✅ |
| No setup | ✅ | ❌ | ✅ | ✅ |
| Scalability (1K-100K) | ✅ | ✅ | ⚠️ | ❌ |
| Query capability | ✅ | ✅ | ❌ | ⚠️ |
| Concurrency | ⚠️ | ✅ | ❌ | ❌ |
| Performance | ✅ | ✅ | ✅ | ⚠️ |
| Security | ✅ | ✅ | ⚠️ | ⚠️ |
| Enterprise features | ⚠️ | ✅ | ❌ | ❌ |
| Backup/restore | ✅ | ✅ | ✅ | ⚠️ |

---

## **RECOMMENDATION: SQLite**

### Why SQLite

**Primary Recommendation: SQLite for Phase 4.0**

**Rationale:**
1. ✅ Meets single-binary requirement
2. ✅ Zero operational setup
3. ✅ ACID guarantees for audit compliance
4. ✅ Excellent query capability (SQL)
5. ✅ Good performance for expected scale
6. ✅ Mature, proven in production
7. ✅ Supports encryption (SQLCipher)
8. ✅ Simple migration path

**Trade-offs:**
- Slight concurrency limitations (mitigated by WAL mode)
- Network filesystems not ideal (acceptable for single-server)

**Migration Path:**
- Phase 4.0: SQLite (core functionality)
- Phase 5.0+: PostgreSQL (if scaling beyond 5-10 operators required)

---

## PostgreSQL as Alternative

If requirements change:
- Multiple geographically distributed DSO instances
- High-availability requirements (>99.99%)
- 10+ operators simultaneously
- Enterprise replication needs

Then PostgreSQL becomes acceptable despite single-binary violation.

---

## Phase 4.0A Consensus

**Technology Choice: SQLite**

**Rationale:**
- Maintains single binary requirement
- Supports all audit/security needs
- Zero operational burden
- Simple migration path to PostgreSQL
- Proven for expected scale

**Revisit Criteria:**
- If >5 concurrent operators: monitor lock contention
- If >1M records: monitor performance
- If multi-site deployment needed: migrate to PostgreSQL

---

## References

- See: `security-model.md` (encryption at rest)
- See: `migration-plan.md` (implementation phases)
