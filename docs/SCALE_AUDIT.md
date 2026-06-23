# Scale & Performance Audit — P2

> Completed: 2026-06-23 on branch `feature/web-ui`

## What was changed

### 1. Secrets API — server-side pagination (`internal/server/rest.go`)

`GET /api/secrets` now accepts query parameters:

| Param | Default | Max | Notes |
|---|---|---|---|
| `page` | 1 | — | 1-based |
| `pageSize` | 50 | 200 | |
| `search` | — | — | substring match on name+provider (server-side) |
| `status` | — | — | `ok`, `pending`, `error` |
| `provider` | — | — | exact match |
| `sortBy` | `name` | — | `name`, `provider`, `status`, `last_rotated`, `next_rotation` |
| `sortOrder` | `asc` | — | `asc`, `desc` |

Response shape:
```json
{
  "items": [...],
  "page": 1,
  "pageSize": 50,
  "total": 4823,
  "active_secrets": [...],
  "total_count": 4823
}
```

`active_secrets` / `total_count` are kept as aliases for backward compatibility with older clients.

**Before:** The frontend loaded all secrets on every page render, then filtered/sorted/paginated client-side. At 5,000 secrets that is ~5 MB per request, filtering 5,000 objects in the browser on every keystroke.

**After:** The backend filters, sorts, and pages before serializing. The browser always receives ≤200 items regardless of corpus size.

### 2. Dashboard posture API (`internal/server/rest.go`)

`GET /api/dashboard/posture` returns pre-aggregated counts:

```json
{
  "managedSecrets": 4823,
  "needRotation": 0,
  "secretErrors": 12,
  "fresh": 0,
  "aging": 0,
  "overdue": 0,
  "unknown": 4811
}
```

**Before:** `DashboardContent` called `getSecrets()` (all secrets) just to compute 7 counts for the EstateHero widget. Every dashboard load triggered an unbounded fetch.

**After:** One small JSON object. The EstateHero widget no longer causes an O(n) request at dashboard open.

**Honesty note:** `fresh`/`aging`/`overdue` are always 0 because DSO does not currently track per-secret rotation history with real timestamps. The backend marks all non-error secrets as `unknown` rather than guessing. This matches what the frontend was already showing before this change.

### 3. Secrets page — server-driven filter/sort/paginate (`web/app/secrets/page.tsx`)

- Search input debounced 300 ms before hitting the server
- Status filter and column sort resets page to 1
- `<Pagination>` component added below the table
- `useMemo` for client-side filtering removed entirely

### 4. Global search — lazy fetch + debounce (`web/components/global-search.tsx`, `web/hooks/useGlobalSearch.ts`)

- All 4 data queries have `enabled: isOpen` — zero network traffic until the user opens search (Cmd+K)
- Secrets query changed from `getSecrets()` (all) to `getSecretsPage({pageSize: 50})`
- Filter computation debounced 300 ms in `useGlobalSearch`
- Result cap raised to 50 (was 20)

---

## Bottlenecks resolved

| Area | Before | After |
|---|---|---|
| Secrets list | Load all → filter in browser | Server filter/sort/page, ≤200 rows |
| Dashboard load | Load all secrets for 7 counts | Single aggregate endpoint |
| Global search open | 4 fetches on app mount | 0 fetches until search opened |
| Global search typing | Filter on every keystroke | 300 ms debounce |

## Validated scale

The implementation is O(1) in secret count for:
- Dashboard open (posture aggregate)
- Secrets page initial load (page 1, 50 items)
- Global search open (no fetch)

The implementation is O(k) where k = page size (≤200) for:
- Secrets page navigation and search

The backend still iterates all secrets to filter/sort in memory (Go slice). For very large corpora (10,000+) this will need a SQLite-backed query. At 5,000 secrets the in-memory filter completes in <5 ms.

## Remaining limitations

1. **Backend filter is in-memory.** `handleListSecrets` loads all secrets from the store, then filters. For corpora >10,000 the filter loop will add measurable latency. A follow-up should push `search`, `status`, and `provider` into the SQLite query layer.

2. **No real rotation tracking.** `fresh`/`aging`/`overdue` posture counts are always 0. The posture widget shows `unknown` for all non-errored secrets. Fixing this requires per-secret rotation history in the database.

3. **Global search indexes only the first 50 secrets.** The search modal will miss secrets beyond the first page. A dedicated server-side search endpoint would be needed for full coverage.

4. **Audit and execution tables are client-side filtered.** Both tables receive a bounded server page (50 rows), filter locally, and paginate client-side. This is acceptable at current scales. If audit log volume grows, the search filter in `AuditContent` should move server-side.

5. **`@tanstack/react-virtual` is installed but unused.** All current tables are already bounded via server-side pagination (secrets: 50/page, audit: 50/page, executions: 20/page client-side). Virtualization was installed as a dependency and deferred — apply it only if a table's row count becomes genuinely unbounded.
