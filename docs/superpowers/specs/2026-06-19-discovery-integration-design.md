# Phase 5B: Discovery Page Integration — Design Specification

**Date:** 2026-06-19  
**Phase:** 5B  
**Status:** Design Approved  
**Author:** Claude Code  

---

## Overview

Transform the Discovery page from mock data to a fully operational, production-ready interface for container discovery, secret mapping suggestions, and cache health monitoring. The page will display discovered containers classified by DSO awareness status, suggest secret mappings with confidence scoring, and provide visibility into discovery cache performance.

**Key Constraint:** Use only existing backend APIs (4 endpoints already implemented).

---

## Architecture

### Page Structure

```
DiscoveryPage (app/discovery/page.tsx)
│
├── State Management
│   ├── searchTerm (string, normalized)
│   ├── filters { classification[], status[] }
│   ├── selectedContainer (ContainerMetadata | null)
│   └── isRefreshing (boolean)
│
├── React Query Hooks (independent, isolated)
│   ├── useQuery(['discovery', 'containers']) → 30s auto-refresh
│   ├── useQuery(['discovery', 'mappings']) → 30s auto-refresh
│   └── useQuery(['discovery', 'metrics']) → 30s auto-refresh
│
└── Layout
    ├── Header (search, filters, refresh button)
    ├── CoverageMetrics (4 cards: total, managed, partial, unmanaged)
    ├── ContainerTable (filtered containers with classification badges)
    ├── SecretMappingsTable (suggestions with confidence levels)
    ├── DiscoveryMetricsSection (collapsible, cache performance)
    └── ContainerDetailsDrawer (when selectedContainer is set)
```

### Data Flow

1. **Page Load**
   - Trigger 3 parallel queries (containers, mappings, metrics)
   - Each has independent error handling
   - React Query auto-refresh every 30 seconds

2. **Search & Filter**
   - User enters search term → normalize to lowercase, trim whitespace
   - User toggles filter badges → update filter state
   - Apply filters locally (no server round-trip):
     - Filter by classification (managed/partial/unmanaged)
     - Filter by status (running/stopped)
     - Filter by search term (container name, image, status)
   - Memoize result to prevent unnecessary re-renders

3. **Container Selection**
   - User clicks container row → set selectedContainer state
   - ContainerDetailsDrawer opens with full details
   - Close drawer → clear selectedContainer

4. **Manual Refresh**
   - User clicks refresh button → disable button, show spinner
   - Call `discoveryApi.refreshDiscovery()` API
   - Await completion
   - Invalidate all 3 query caches atomically
   - Re-enable button, show "Last refreshed: Xs ago"

---

## Query Configuration

All queries use consistent configuration for reliability and performance:

```typescript
useQuery({
  queryKey: ['discovery', 'containers'],  // or 'mappings', 'metrics'
  queryFn: discoveryApi.getContainers,    // or getMappings, getMetrics
  refetchInterval: 30000,                  // 30 seconds
  staleTime: 25000,                        // Stale after 25 seconds
  retry: 2,                                // Retry failed requests twice
  refetchOnWindowFocus: false,             // Don't refetch on window focus
})
```

**Rationale:**
- `staleTime: 25000` prevents unnecessary refetch before auto-refresh fires
- `retry: 2` provides resilience without excessive retries
- `refetchOnWindowFocus: false` avoids duplicate API calls

---

## Components

### 1. CoverageMetrics.tsx
**Responsibility:** Display high-level discovery summary

**Inputs:**
```typescript
containers: ContainerMetadata[] | undefined
isLoading: boolean
```

**Outputs:** 4 metric cards in a grid:
- Total: all discovered containers
- Managed: count + percentage
- Partial: count + percentage  
- Unmanaged: count + percentage

**Design Pattern:** Mirror Dashboard KPI cards (consistent UI)

**States:**
- Loading: skeleton card placeholders
- Loaded: cards with numbers and percentages
- Error: handled in parent page

---

### 2. ContainerTable.tsx + ContainerRow.tsx
**Responsibility:** Display filtered containers in tabular format

**Inputs (ContainerTable):**
```typescript
containers: ContainerMetadata[]
isLoading: boolean
onSelectContainer: (container: ContainerMetadata) => void
```

**Columns (ContainerRow):**
| Column | Content | Notes |
|--------|---------|-------|
| Name | container_name | Clickable → opens drawer |
| Image | image | Full image URI |
| Status | status | "running", "stopped", etc. |
| Classification | badge (managed/partial/unmanaged) | Green/yellow/gray |
| Managed Secrets | dso_awareness.managed_secrets count | Numeric |
| Missing Mappings | dso_awareness.missing_mappings count | Numeric |

**States:**
- Loading: skeleton rows
- Loaded: data rows
- Empty: show EmptyState ("No containers discovered")
- Filter mismatch: show EmptyState ("No containers match current filters")

**Pagination:** Not required for Phase 5B (keep simple)

---

### 3. DiscoveryFilters.tsx (Enhanced)
**Responsibility:** Manage filter state and UI

**Inputs:**
```typescript
filters: { classification: string[], status: string[] }
onFilterChange: (filters) => void
containerCounts: { managed, partial, unmanaged, running, stopped }
```

**Filter Options:**
- **Classification:** Managed, Partial, Unmanaged (with counts)
- **Status:** Running, Stopped (with counts)
- **Active Chips:** Remove individual filters with chip close button
- **Clear All:** Clear all filters at once

**Design Pattern:** Reuse existing DiscoveryFilters logic, extend for status

---

### 4. SecretMappingsTable.tsx
**Responsibility:** Display secret mapping suggestions with confidence

**Inputs:**
```typescript
mappings: SecretMappingSuggestion[] | undefined
searchTerm: string (normalized)
isLoading: boolean
```

**Columns:**
| Column | Content | Interaction |
|--------|---------|-------------|
| Env Var | env_var_name | If searchTerm matches, highlight row |
| Suggested Secret | suggested_secret_name | Informational only, no link |
| Confidence | high/medium/low badge | Green/yellow/red |
| Reason | Short text | Hover tooltip (not expandable) |
| Configured | ✓ or ⚠️ | Checkmark if is_configured = true |

**Highlighting:**
- If searchTerm matches env_var_name or suggested_secret_name, highlight entire row
- Case-insensitive matching

**States:**
- Loading: skeleton rows
- Loaded: data rows
- Empty: show EmptyState ("No secret suggestions yet")
- Error: show "Unable to load secret suggestions" (non-blocking)

---

### 5. DiscoveryMetricsSection.tsx
**Responsibility:** Display cache/refresh performance metrics (collapsible)

**Inputs:**
```typescript
metrics: DiscoveryMetrics | undefined
isLoading: boolean
```

**Collapsed by default.** When expanded, shows:
- Cache Hits: number
- Cache Misses: number
- Refresh Count: total number of manual refreshes
- Cache Age: seconds since last refresh
- Latency: average response time in milliseconds

**States:**
- Loading: placeholder text
- Loaded: metric values
- Error: show "Unable to load discovery metrics" (non-blocking)

**No trend calculations:** Keep it simple for Phase 5B

---

### 6. ContainerDetailsDrawer.tsx
**Responsibility:** Display full container details (drawer/modal)

**Inputs:**
```typescript
container: ContainerMetadata | null
onClose: () => void
```

**Sections (stacked vertically):**

1. **General**
   - Container ID (copy button)
   - Container Name
   - Image (full URI)
   - Status badge

2. **Networks**
   - List of connected networks with IP addresses

3. **Restart Policy**
   - Policy type (always, unless-stopped, etc.)
   - Max retry count (if applicable)

4. **Environment Variables** (collapsible, collapsed by default)
   - Read-only table of all env vars
   - Scrollable if large list (containers can have hundreds)
   - Column: key, value

5. **DSO Awareness**
   - Managed Secrets: count + list
   - Config References: count + list
   - Missing Mappings: count + list
   - Classification badge (managed/partial/unmanaged)

**States:**
- Loading: skeleton placeholders
- Loaded: full details
- Error: retry button

---

### 7. RefreshButton.tsx
**Responsibility:** Trigger manual discovery refresh

**Inputs:**
```typescript
isRefreshing: boolean
lastRefreshTime: Date | null
onRefresh: () => Promise<void>
```

**Display:**
- Label: "Refresh" (normal) or "Refreshing…" (during refresh)
- Disabled state while isRefreshing = true
- Loading spinner during refresh
- Underneath: "Last refreshed: Xs ago" (e.g., "Last refreshed: 12s ago")

**Behavior:**
- Button click → disable button, show spinner
- Call `onRefresh()` → awaits completion
- Re-enable button after all queries invalidated

---

### 8. EmptyState.tsx (Reusable)
**Responsibility:** Consistent messaging when no data available

**Inputs:**
```typescript
type: 'no-containers' | 'no-mappings' | 'filter-mismatch'
onRetry?: () => void
```

**Messages:**
- `no-containers`: "No containers discovered. Try refreshing or check your environment."
- `no-mappings`: "No secret mappings detected."
- `filter-mismatch`: "No containers match current filters. Try adjusting your search or filters."

**Design Pattern:** Reuse with Audit and Dashboard for consistency

---

## Search & Filter Strategy

### Normalization
```typescript
const normalizedSearch = searchTerm.trim().toLowerCase()
```
Apply once, reuse across all filter operations.

### Filtering (in order)
```typescript
const filteredContainers = useMemo(() => {
  return containers
    ?.filter(c => 
      filters.classification.length === 0 || 
      filters.classification.includes(c.dso_awareness.classification)
    )
    ?.filter(c => 
      filters.status.length === 0 || 
      filters.status.includes(c.status)
    )
    ?.filter(c => 
      normalizedSearch === '' || 
      c.container_name.toLowerCase().includes(normalizedSearch) ||
      c.image.toLowerCase().includes(normalizedSearch) ||
      c.status.toLowerCase().includes(normalizedSearch)
    ) || []
}, [containers, filters, normalizedSearch])
```

### Rationale
- Memoize to prevent re-render thrashing on large container lists (100+, 500+, thousands)
- Filter in order of selectivity: classification → status → search (reduces work)
- No server round-trips (local filtering only)
- Search term passed to SecretMappingsTable for highlighting

---

## Error Handling

**Isolated failures:**

1. **Containers (Critical)**
   - If query fails: show error banner with "Unable to load containers" + [Retry] button
   - User can still see mappings and metrics
   - Retry invalidates query and refetches

2. **Mappings (Non-Critical)**
   - If query fails: show "Unable to load secret suggestions" in SecretMappingsTable area
   - Page remains fully functional
   - No retry needed (auto-retry in 30s via refetchInterval)

3. **Metrics (Lowest Priority)**
   - If query fails: show "Unable to load discovery metrics" inside collapsible section
   - User can expand/collapse freely
   - No blocking impact

**No global error state:** Each section handles its own failure independently.

---

## Auto-Refresh & Cleanup

**Auto-Refresh:**
- React Query `refetchInterval: 30000` on all 3 queries
- No manual interval setup needed
- Cleanup handled automatically when component unmounts

**Manual Refresh:**
```typescript
async function handleRefresh() {
  setIsRefreshing(true)
  try {
    await discoveryApi.refreshDiscovery()
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['discovery', 'containers'] }),
      queryClient.invalidateQueries({ queryKey: ['discovery', 'mappings'] }),
      queryClient.invalidateQueries({ queryKey: ['discovery', 'metrics'] }),
    ])
  } finally {
    setIsRefreshing(false)
  }
}
```

**Atomicity:** Invalidate all queries together to ensure consistency.

---

## Authentication & Access Control

- Wrap DiscoveryPage in `<ProtectedRoute>` for authentication
- Uses Phase 3 role-based access control
- Discovery operations require appropriate role permissions

---

## Page Layout (Single Scrollable)

```
┌─ Header ──────────────────────────────────────┐
│ [Search Box] [Filters] [Refresh Button]       │
│ Filter chips (active filters with remove btn) │
└───────────────────────────────────────────────┘

┌─ Coverage Metrics ────────────────────────────┐
│ [Total] [Managed %] [Partial %] [Unmanaged %] │
└───────────────────────────────────────────────┘

┌─ Container Table ──────────────────────────────┐
│ Name | Image | Status | Class | Secrets | Map │
├────────────────────────────────────────────────┤
│ ...rows...                                     │
└────────────────────────────────────────────────┘

┌─ Secret Mappings Table ───────────────────────┐
│ Env Var | Secret | Confidence | Reason | Cfg  │
├────────────────────────────────────────────────┤
│ ...rows...                                     │
└────────────────────────────────────────────────┘

┌─ Discovery Metrics (Collapsible) ─────────────┐
│ ▶ Discovery Metrics                           │
│   (click to expand)                           │
└────────────────────────────────────────────────┘

┌─ Container Details Drawer (Modal) ────────────┐
│ (Opens when container selected)                │
│ [General][Networks][Restart][Env Vars][DSO]  │
└────────────────────────────────────────────────┘
```

---

## File Structure

```
web/
├── app/
│   └── discovery/
│       └── page.tsx (main page, ProtectedRoute wrapper)
│
├── components/
│   └── discovery/
│       ├── CoverageMetrics.tsx
│       ├── ContainerTable.tsx
│       ├── ContainerRow.tsx
│       ├── ContainerDetailsDrawer.tsx
│       ├── DiscoveryFilters.tsx
│       ├── SecretMappingsTable.tsx
│       ├── DiscoveryMetricsSection.tsx
│       ├── RefreshButton.tsx
│       └── EmptyState.tsx (reuse, or create shared)
│
└── lib/
    ├── api/
    │   └── discovery.ts (Phase 2 API layer — already exists)
    │
    └── hooks/
        └── useDiscovery.ts (optional: custom hooks if needed)
```

---

## Validation Checklist

Before marking Phase 5B complete:

- [ ] Discovery page loads without errors
- [ ] Containers display with correct classification badges (green/yellow/gray)
- [ ] Search works across container name, image, status
- [ ] Filters work (classification + status)
- [ ] Active filter chips display and remove correctly
- [ ] Secret mappings table visible with confidence colors
- [ ] Metrics section collapsible and displayable
- [ ] Container click opens details drawer
- [ ] Refresh button disabled during refresh
- [ ] "Last refreshed: Xs ago" timestamp updates
- [ ] Auto-refresh fires every 30 seconds
- [ ] Error states handled gracefully (isolated, non-blocking)
- [ ] Empty states show appropriate messages
- [ ] TypeScript strict mode passes (no 'any' types)
- [ ] All components reusable and composable

---

## Future Considerations (Phase 5C)

- Query keys designed for reuse: `['discovery', ...]` pattern allows Operations Console to extend naturally
- Component structure supports adding:
  - Container action buttons (deploy, monitor, etc.)
  - Integration with Operations Console for orchestration
  - Vault/Secrets Manager links (currently informational only)
- DiscoveryMetrics can be extended to show trends in Phase 5C

---

## Success Criteria

✅ **Phase 5B Complete When:**
1. All 9 components created and functional
2. Real backend API integration (no mock data)
3. Search + filters working correctly
4. 30-second auto-refresh with manual refresh
5. All error states handled gracefully
6. TypeScript strict mode compliance
7. Follows existing Phase 5A audit page patterns
8. Ready for Phase 5C Operations Console integration

