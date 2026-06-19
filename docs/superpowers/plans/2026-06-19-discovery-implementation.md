# Phase 5B: Discovery Page Implementation Plan

> **For agentic workers:** RECOMMENDED: Use superpowers:subagent-driven-development for task-by-task execution, or superpowers:executing-plans for inline execution. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a fully functional Discovery page showing discovered containers, secret mapping suggestions, and cache health metrics with real API integration and 30-second auto-refresh.

**Architecture:** Main page manages search/filters/state with 3 independent React Query hooks. 9 reusable components handle display, filtering, and interaction. Local client-side filtering, isolated error handling, no global state.

**Tech Stack:** React Query (caching/refresh), TypeScript strict mode, Phase 2 API layer (discovery.ts), UI components (Card, Badge, Button, Drawer), Lucide icons.

---

## File Structure

```
web/
├── app/
│   └── discovery/
│       └── page.tsx (NEW - main page with ProtectedRoute)
│
├── components/
│   └── discovery/
│       ├── EmptyState.tsx (NEW - reusable)
│       ├── RefreshButton.tsx (NEW - manual refresh)
│       ├── CoverageMetrics.tsx (NEW - 4 summary cards)
│       ├── ContainerRow.tsx (NEW - table row sub-component)
│       ├── ContainerTable.tsx (NEW - main table)
│       ├── ContainerDetailsDrawer.tsx (NEW - modal with sections)
│       ├── SecretMappingsTable.tsx (NEW - mappings list)
│       ├── DiscoveryMetricsSection.tsx (NEW - collapsible metrics)
│       └── DiscoveryFilters.tsx (EXISTING - enhance for status filter)
│
└── lib/
    └── api/
        └── discovery.ts (EXISTING - already complete)
```

---

## Task Sequence

### Task 1: Create EmptyState.tsx (Reusable Component)

**Files:**
- Create: `web/components/discovery/EmptyState.tsx`

**Rationale:** Foundational component used by multiple other components. Build early for reuse.

- [ ] **Step 1: Create file with component skeleton**

```typescript
'use client'

import { AlertCircle, Search, Database } from 'lucide-react'

export type EmptyStateType = 'no-containers' | 'no-mappings' | 'filter-mismatch'

interface EmptyStateProps {
  type: EmptyStateType
  onRetry?: () => void
}

export function EmptyState({ type, onRetry }: EmptyStateProps) {
  const config = {
    'no-containers': {
      icon: Database,
      title: 'No containers discovered',
      description: 'Try refreshing or check your environment.',
    },
    'no-mappings': {
      icon: AlertCircle,
      title: 'No secret mappings detected',
      description: 'This is perfectly valid — your containers may already be configured.',
    },
    'filter-mismatch': {
      icon: Search,
      title: 'No containers match current filters',
      description: 'Try adjusting your search term or filters.',
    },
  }

  const { icon: Icon, title, description } = config[type]

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <Icon className="w-12 h-12 text-slate-500 mb-3" />
      <h3 className="text-lg font-semibold text-slate-200 mb-1">{title}</h3>
      <p className="text-sm text-slate-400 mb-4 text-center max-w-md">{description}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="text-sm text-indigo-400 hover:text-indigo-300 underline"
        >
          Try again
        </button>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/EmptyState.tsx
git commit -m "feat: create EmptyState component for discovery"
```

---

### Task 2: Create RefreshButton.tsx (Manual Refresh with Timestamp)

**Files:**
- Create: `web/components/discovery/RefreshButton.tsx`

**Rationale:** Used in main page header. Simple, reusable button with timestamp display.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { useCallback, useEffect, useState } from 'react'
import { RotateCw } from 'lucide-react'

interface RefreshButtonProps {
  isRefreshing: boolean
  onRefresh: () => Promise<void>
}

export function RefreshButton({ isRefreshing, onRefresh }: RefreshButtonProps) {
  const [lastRefreshTimestamp, setLastRefreshTimestamp] = useState<number | null>(null)
  const [relativeTime, setRelativeTime] = useState<string>('')

  // Update relative time every second
  useEffect(() => {
    if (!lastRefreshTimestamp) return

    const interval = setInterval(() => {
      const now = Date.now()
      const secondsAgo = Math.floor((now - lastRefreshTimestamp) / 1000)

      if (secondsAgo < 60) {
        setRelativeTime(`${secondsAgo}s ago`)
      } else if (secondsAgo < 3600) {
        const minutesAgo = Math.floor(secondsAgo / 60)
        setRelativeTime(`${minutesAgo}m ago`)
      } else {
        const hoursAgo = Math.floor(secondsAgo / 3600)
        setRelativeTime(`${hoursAgo}h ago`)
      }
    }, 1000)

    return () => clearInterval(interval)
  }, [lastRefreshTimestamp])

  const handleRefresh = useCallback(async () => {
    await onRefresh()
    setLastRefreshTimestamp(Date.now())
  }, [onRefresh])

  return (
    <div className="flex flex-col items-center gap-1">
      <button
        onClick={handleRefresh}
        disabled={isRefreshing}
        className="inline-flex items-center gap-2 px-3 py-2 text-sm rounded-lg border border-white/10 text-slate-300 hover:text-slate-100 hover:bg-white/5 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        <RotateCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
        {isRefreshing ? 'Refreshing…' : 'Refresh'}
      </button>
      {lastRefreshTimestamp && !isRefreshing && (
        <span className="text-xs text-slate-500">Last refreshed: {relativeTime}</span>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/RefreshButton.tsx
git commit -m "feat: create RefreshButton with timestamp tracking"
```

---

### Task 3: Create CoverageMetrics.tsx (Summary Cards)

**Files:**
- Create: `web/components/discovery/CoverageMetrics.tsx`

**Rationale:** Display summary of container counts. Mirror Dashboard KPI pattern.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { TrendingUp } from 'lucide-react'

interface CoverageMetricsProps {
  containers?: ContainerMetadata[]
  isLoading: boolean
}

interface MetricCard {
  label: string
  value: number
  percentage: number
  color: string
}

export function CoverageMetrics({ containers, isLoading }: CoverageMetricsProps) {
  if (isLoading) {
    return (
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[...Array(4)].map((_, i) => (
          <Skeleton key={i} className="h-24 rounded-lg" />
        ))}
      </div>
    )
  }

  const total = containers?.length ?? 0
  const managed = containers?.filter(c => c.dso_awareness?.classification === 'managed').length ?? 0
  const partial = containers?.filter(c => c.dso_awareness?.classification === 'partial').length ?? 0
  const unmanaged = containers?.filter(c => c.dso_awareness?.classification === 'unmanaged').length ?? 0

  const metrics: MetricCard[] = [
    { label: 'Total', value: total, percentage: 100, color: 'blue' },
    {
      label: 'Managed',
      value: managed,
      percentage: total > 0 ? Math.round((managed / total) * 100) : 0,
      color: 'emerald',
    },
    {
      label: 'Partial',
      value: partial,
      percentage: total > 0 ? Math.round((partial / total) * 100) : 0,
      color: 'amber',
    },
    {
      label: 'Unmanaged',
      value: unmanaged,
      percentage: total > 0 ? Math.round((unmanaged / total) * 100) : 0,
      color: 'slate',
    },
  ]

  const colorClasses: Record<string, string> = {
    blue: 'text-blue-400',
    emerald: 'text-emerald-400',
    amber: 'text-amber-400',
    slate: 'text-slate-400',
  }

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      {metrics.map(metric => (
        <Card key={metric.label} className="p-4">
          <div className="space-y-2">
            <p className="text-xs text-slate-500 font-medium">{metric.label}</p>
            <div className="flex items-baseline justify-between">
              <p className={`text-2xl font-bold ${colorClasses[metric.color]}`}>{metric.value}</p>
              <p className="text-xs text-slate-400">{metric.percentage}%</p>
            </div>
          </div>
        </Card>
      ))}
    </div>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/CoverageMetrics.tsx
git commit -m "feat: create CoverageMetrics summary card component"
```

---

### Task 4: Create ContainerRow.tsx (Table Row Sub-component)

**Files:**
- Create: `web/components/discovery/ContainerRow.tsx`

**Rationale:** Row sub-component for ContainerTable. Keeps table logic separate from row rendering.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Badge } from '@/components/ui-modern'
import { ChevronRight } from 'lucide-react'

interface ContainerRowProps {
  container: ContainerMetadata
  onSelect: (container: ContainerMetadata) => void
}

export function ContainerRow({ container, onSelect }: ContainerRowProps) {
  const classificationColor: Record<string, string> = {
    managed: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    partial: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    unmanaged: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
  }

  const statusColor: Record<string, string> = {
    running: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    stopped: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
    paused: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
  }

  const classification = container.dso_awareness?.classification ?? 'unmanaged'
  const status = container.status ?? 'unknown'

  return (
    <button
      onClick={() => onSelect(container)}
      className="w-full px-4 py-3 border-b border-white/[0.06] hover:bg-white/[0.02] transition-colors text-left"
    >
      <div className="grid grid-cols-6 gap-3 items-center">
        <div className="col-span-1 truncate">
          <p className="text-sm font-medium text-slate-200 truncate">{container.container_name}</p>
        </div>
        <div className="col-span-1 truncate">
          <p className="text-xs text-slate-400 truncate">{container.image}</p>
        </div>
        <div className="col-span-1">
          <Badge variant="outline" size="sm" className={statusColor[status]}>
            {status}
          </Badge>
        </div>
        <div className="col-span-1">
          <Badge variant="outline" size="sm" className={classificationColor[classification]}>
            {classification}
          </Badge>
        </div>
        <div className="col-span-1">
          <p className="text-sm text-slate-400 text-center">
            {container.dso_awareness?.managed_secrets ?? 0}
          </p>
        </div>
        <div className="col-span-1 flex items-center justify-between">
          <p className="text-sm text-slate-400 text-center">
            {container.dso_awareness?.missing_mappings ?? 0}
          </p>
          <ChevronRight className="w-4 h-4 text-slate-600" />
        </div>
      </div>
    </button>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/ContainerRow.tsx
git commit -m "feat: create ContainerRow sub-component for table"
```

---

### Task 5: Create ContainerTable.tsx (Main Table Component)

**Files:**
- Create: `web/components/discovery/ContainerTable.tsx`

**Rationale:** Main table for displaying filtered containers. Uses ContainerRow sub-component.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { ContainerRow } from './ContainerRow'
import { EmptyState } from './EmptyState'

interface ContainerTableProps {
  containers: ContainerMetadata[]
  isLoading: boolean
  onSelectContainer: (container: ContainerMetadata) => void
}

export function ContainerTable({
  containers,
  isLoading,
  onSelectContainer,
}: ContainerTableProps) {
  if (isLoading) {
    return (
      <Card className="overflow-hidden">
        <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
          <div className="grid grid-cols-6 gap-3 text-xs font-semibold text-slate-500">
            <span>Name</span>
            <span>Image</span>
            <span>Status</span>
            <span>Classification</span>
            <span>Secrets</span>
            <span>Missing</span>
          </div>
        </div>
        <div>
          {[...Array(5)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-none border-b border-white/[0.06]" />
          ))}
        </div>
      </Card>
    )
  }

  if (containers.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="filter-mismatch" />
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
        <div className="grid grid-cols-6 gap-3 text-xs font-semibold text-slate-500">
          <span>Name</span>
          <span>Image</span>
          <span>Status</span>
          <span>Classification</span>
          <span>Secrets</span>
          <span>Missing</span>
        </div>
      </div>
      <div>
        {containers.map(container => (
          <ContainerRow
            key={container.container_id}
            container={container}
            onSelect={onSelectContainer}
          />
        ))}
      </div>
    </Card>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/ContainerTable.tsx
git commit -m "feat: create ContainerTable component"
```

---

### Task 6: Create SecretMappingsTable.tsx (Mappings Display)

**Files:**
- Create: `web/components/discovery/SecretMappingsTable.tsx`

**Rationale:** Display secret mapping suggestions with confidence levels and highlighting.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { SecretMappingSuggestion } from '@/lib/api/types'
import { Card, Badge, Skeleton } from '@/components/ui-modern'
import { CheckCircle2, AlertCircle } from 'lucide-react'
import { EmptyState } from './EmptyState'

interface SecretMappingsTableProps {
  mappings?: SecretMappingSuggestion[]
  searchTerm: string
  isLoading: boolean
}

export function SecretMappingsTable({
  mappings,
  searchTerm,
  isLoading,
}: SecretMappingsTableProps) {
  if (isLoading) {
    return (
      <Card className="overflow-hidden">
        <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
          <div className="grid grid-cols-5 gap-3 text-xs font-semibold text-slate-500">
            <span>Environment Variable</span>
            <span>Suggested Secret</span>
            <span>Confidence</span>
            <span>Reason</span>
            <span>Status</span>
          </div>
        </div>
        <div>
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-none border-b border-white/[0.06]" />
          ))}
        </div>
      </Card>
    )
  }

  if (!mappings || mappings.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="no-mappings" />
      </Card>
    )
  }

  const normalizedSearch = searchTerm.trim().toLowerCase()

  const filtered = normalizedSearch === ''
    ? mappings
    : mappings.filter(
        m =>
          m.env_var_name.toLowerCase().includes(normalizedSearch) ||
          m.suggested_secret_name.toLowerCase().includes(normalizedSearch)
      )

  if (filtered.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="filter-mismatch" />
      </Card>
    )
  }

  const confidenceColors: Record<string, string> = {
    high: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    medium: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    low: 'bg-red-500/20 text-red-300 border-red-500/30',
  }

  return (
    <Card className="overflow-hidden">
      <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
        <div className="grid grid-cols-5 gap-3 text-xs font-semibold text-slate-500">
          <span>Environment Variable</span>
          <span>Suggested Secret</span>
          <span>Confidence</span>
          <span>Reason</span>
          <span>Status</span>
        </div>
      </div>
      <div>
        {filtered.map(mapping => {
          const isHighlighted =
            mapping.env_var_name.toLowerCase().includes(normalizedSearch) ||
            mapping.suggested_secret_name.toLowerCase().includes(normalizedSearch)

          return (
            <div
              key={mapping.env_var_name}
              className={`px-4 py-3 border-b border-white/[0.06] ${
                isHighlighted ? 'bg-indigo-500/10' : 'hover:bg-white/[0.02]'
              } transition-colors`}
            >
              <div className="grid grid-cols-5 gap-3 items-center">
                <p className="text-sm font-mono text-slate-300">{mapping.env_var_name}</p>
                <p className="text-sm font-mono text-slate-400">{mapping.suggested_secret_name}</p>
                <Badge
                  variant="outline"
                  size="sm"
                  className={confidenceColors[mapping.confidence]}
                >
                  {mapping.confidence}
                </Badge>
                <p className="text-xs text-slate-500" title={mapping.reason}>
                  {mapping.reason}
                </p>
                <div className="flex items-center justify-center">
                  {mapping.is_configured ? (
                    <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                  ) : (
                    <AlertCircle className="w-4 h-4 text-amber-400" />
                  )}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </Card>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/SecretMappingsTable.tsx
git commit -m "feat: create SecretMappingsTable component with highlighting"
```

---

### Task 7: Create DiscoveryMetricsSection.tsx (Collapsible Metrics)

**Files:**
- Create: `web/components/discovery/DiscoveryMetricsSection.tsx`

**Rationale:** Display cache metrics in collapsible section.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { useState } from 'react'
import { DiscoveryMetrics } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { ChevronDown } from 'lucide-react'

interface DiscoveryMetricsSectionProps {
  metrics?: DiscoveryMetrics
  isLoading: boolean
}

export function DiscoveryMetricsSection({
  metrics,
  isLoading,
}: DiscoveryMetricsSectionProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  if (isLoading) {
    return (
      <Card className="p-4">
        <Skeleton className="h-12 w-full rounded" />
      </Card>
    )
  }

  if (!metrics) {
    return (
      <Card className="p-4">
        <p className="text-sm text-slate-500">Unable to load discovery metrics</p>
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full px-4 py-3 flex items-center justify-between hover:bg-white/[0.02] transition-colors"
      >
        <h3 className="text-sm font-semibold text-slate-300">Discovery Metrics</h3>
        <ChevronDown
          className={`w-4 h-4 text-slate-500 transition-transform ${
            isExpanded ? 'rotate-180' : ''
          }`}
        />
      </button>

      {isExpanded && (
        <div className="border-t border-white/[0.06] px-4 py-3 space-y-3">
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Hits</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_hits}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Misses</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_misses}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Refresh Count</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.refresh_count}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Age</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_age_seconds}s</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Latency</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.avg_latency_ms}ms</p>
            </div>
          </div>
        </div>
      )}
    </Card>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/DiscoveryMetricsSection.tsx
git commit -m "feat: create DiscoveryMetricsSection collapsible component"
```

---

### Task 8: Create ContainerDetailsDrawer.tsx (Modal with Sections)

**Files:**
- Create: `web/components/discovery/ContainerDetailsDrawer.tsx`

**Rationale:** Display full container details with collapsible env vars section.

- [ ] **Step 1: Create component**

```typescript
'use client'

import { useState } from 'react'
import { ContainerMetadata } from '@/lib/api/types'
import { Card, Badge } from '@/components/ui-modern'
import { X, Copy, ChevronDown } from 'lucide-react'

interface ContainerDetailsDrawerProps {
  container: ContainerMetadata | null
  onClose: () => void
}

export function ContainerDetailsDrawer({
  container,
  onClose,
}: ContainerDetailsDrawerProps) {
  const [showEnvVars, setShowEnvVars] = useState(false)
  const [copiedId, setCopiedId] = useState(false)

  if (!container) return null

  const handleCopyId = async () => {
    await navigator.clipboard.writeText(container.container_id)
    setCopiedId(true)
    setTimeout(() => setCopiedId(false), 2000)
  }

  const classificationColor: Record<string, string> = {
    managed: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    partial: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    unmanaged: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
  }

  const classification = container.dso_awareness?.classification ?? 'unmanaged'

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-end md:items-center justify-end md:justify-center p-4">
      <Card className="w-full md:max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 className="text-lg font-semibold text-slate-200">Container Details</h2>
          <button
            onClick={onClose}
            className="text-slate-500 hover:text-slate-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto space-y-4 p-6">
          {/* General */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">General</h3>
            <div className="space-y-3">
              <div>
                <p className="text-xs text-slate-500 mb-1">Container ID</p>
                <div className="flex items-center gap-2">
                  <p className="text-sm font-mono text-slate-300 truncate">
                    {container.container_id.slice(0, 12)}
                  </p>
                  <button
                    onClick={handleCopyId}
                    className="text-slate-500 hover:text-slate-300 transition-colors"
                    title="Copy container ID"
                  >
                    <Copy className="w-4 h-4" />
                  </button>
                  {copiedId && <span className="text-xs text-emerald-400">Copied!</span>}
                </div>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Name</p>
                <p className="text-sm text-slate-200">{container.container_name}</p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Image</p>
                <p className="text-sm font-mono text-slate-300">{container.image}</p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Status</p>
                <Badge variant="outline" size="sm">
                  {container.status}
                </Badge>
              </div>
            </div>
          </div>

          {/* Networks */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">Networks</h3>
            <div className="space-y-2">
              {Object.entries(container.networks || {}).map(([name, info]) => (
                <div key={name} className="text-sm">
                  <p className="text-slate-300 font-medium">{name}</p>
                  <p className="text-xs text-slate-500">{(info as any)?.ip_address || 'N/A'}</p>
                </div>
              ))}
            </div>
          </div>

          {/* Restart Policy */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">Restart Policy</h3>
            <div className="text-sm">
              <p className="text-slate-300 font-medium">
                {(container.restart_policy as any)?.name || 'Unknown'}
              </p>
              {(container.restart_policy as any)?.max_retry_count && (
                <p className="text-xs text-slate-500">
                  Max retries: {(container.restart_policy as any).max_retry_count}
                </p>
              )}
            </div>
          </div>

          {/* Environment Variables */}
          <div>
            <button
              onClick={() => setShowEnvVars(!showEnvVars)}
              className="flex items-center gap-2 w-full mb-3"
            >
              <ChevronDown
                className={`w-4 h-4 text-slate-500 transition-transform ${
                  showEnvVars ? 'rotate-180' : ''
                }`}
              />
              <h3 className="text-sm font-semibold text-slate-300">
                Environment Variables ({Object.keys(container.env_vars || {}).length})
              </h3>
            </button>

            {showEnvVars && (
              <div className="bg-white/[0.01] border border-white/[0.06] rounded-lg p-3 max-h-64 overflow-y-auto">
                <div className="space-y-2">
                  {Object.entries(container.env_vars || {}).map(([key, value]) => (
                    <div key={key} className="text-xs">
                      <p className="font-mono text-slate-400">
                        {key}=<span className="text-slate-300">{value}</span>
                      </p>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* DSO Awareness */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">DSO Awareness</h3>
            <div className="space-y-3">
              <div>
                <p className="text-xs text-slate-500 mb-1">Classification</p>
                <Badge
                  variant="outline"
                  size="sm"
                  className={classificationColor[classification]}
                >
                  {classification}
                </Badge>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Managed Secrets</p>
                <p className="text-sm text-slate-200">
                  {container.dso_awareness?.managed_secrets ?? 0}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Config References</p>
                <p className="text-sm text-slate-200">
                  {container.dso_awareness?.config_references ?? 0}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Missing Mappings</p>
                <p className="text-sm text-slate-200">
                  {container.dso_awareness?.missing_mappings ?? 0}
                </p>
              </div>
            </div>
          </div>
        </div>
      </Card>
    </div>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/components/discovery/ContainerDetailsDrawer.tsx
git commit -m "feat: create ContainerDetailsDrawer modal component"
```

---

### Task 9: Enhance DiscoveryFilters.tsx (Add Status Filter)

**Files:**
- Modify: `web/components/discovery/DiscoveryFilters.tsx`

**Rationale:** Extend existing filter component to support runtime status (running/stopped).

- [ ] **Step 1: Read current file to understand structure**

```bash
cat web/components/discovery-filters.tsx | head -50
```

- [ ] **Step 2: Create enhanced version with status filter**

Replace file content with:

```typescript
'use client'

import React, { useMemo } from 'react'
import { Badge, Button } from '@/components/ui/badge'
import { X } from 'lucide-react'

export type FilterType = 'managed' | 'partial' | 'unmanaged' | 'running' | 'stopped'

export interface DiscoveryFiltersProps {
  filters: { classification: FilterType[]; status: FilterType[] }
  onFilterChange: (filters: { classification: FilterType[]; status: FilterType[] }) => void
  containerCount: {
    managed: number
    partial: number
    unmanaged: number
  }
}

export function DiscoveryFilters({
  filters,
  onFilterChange,
  containerCount,
}: DiscoveryFiltersProps) {
  const toggleFilter = (filter: FilterType, type: 'classification' | 'status') => {
    const current = filters[type]
    const updated = current.includes(filter)
      ? current.filter(f => f !== filter)
      : [...current, filter]
    onFilterChange({
      ...filters,
      [type]: updated,
    })
  }

  const clearAllFilters = () => {
    onFilterChange({ classification: [], status: [] })
  }

  const hasActiveFilters = filters.classification.length > 0 || filters.status.length > 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-slate-300">Filters</h3>
        {hasActiveFilters && (
          <Button
            onClick={clearAllFilters}
            variant="ghost"
            size="sm"
            className="text-xs text-slate-500 hover:text-slate-300"
          >
            Clear all
          </Button>
        )}
      </div>

      {/* Classification Filters */}
      <div>
        <p className="text-xs text-slate-500 font-medium mb-2">Classification</p>
        <div className="space-y-2">
          {['managed', 'partial', 'unmanaged'].map(type => (
            <button
              key={type}
              onClick={() => toggleFilter(type as FilterType, 'classification')}
              className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
                filters.classification.includes(type as FilterType)
                  ? type === 'managed'
                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                    : type === 'partial'
                      ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
                      : 'border-slate-500/40 bg-slate-500/10 text-slate-400'
                  : 'border-white/[0.09] bg-transparent text-slate-400 hover:bg-white/[0.05]'
              }`}
            >
              <span className="text-sm font-medium capitalize">{type}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Status Filters */}
      <div>
        <p className="text-xs text-slate-500 font-medium mb-2">Status</p>
        <div className="space-y-2">
          {['running', 'stopped'].map(status => (
            <button
              key={status}
              onClick={() => toggleFilter(status as FilterType, 'status')}
              className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
                filters.status.includes(status as FilterType)
                  ? status === 'running'
                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                    : 'border-slate-500/40 bg-slate-500/10 text-slate-400'
                  : 'border-white/[0.09] bg-transparent text-slate-400 hover:bg-white/[0.05]'
              }`}
            >
              <span className="text-sm font-medium capitalize">{status}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Active Chips */}
      {hasActiveFilters && (
        <div className="pt-2 border-t border-white/[0.06]">
          <div className="flex flex-wrap gap-2">
            {[...filters.classification, ...filters.status].map(filter => (
              <Badge
                key={filter}
                variant="secondary"
                className="gap-1 text-xs"
              >
                {filter.charAt(0).toUpperCase() + filter.slice(1)}
                <button
                  onClick={() => {
                    if (filters.classification.includes(filter as FilterType)) {
                      toggleFilter(filter as FilterType, 'classification')
                    } else {
                      toggleFilter(filter as FilterType, 'status')
                    }
                  }}
                  className="ml-1 hover:opacity-75"
                >
                  <X className="w-3 h-3" />
                </button>
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/components/discovery/DiscoveryFilters.tsx
git commit -m "feat: enhance DiscoveryFilters to support status filtering"
```

---

### Task 10: Create Main Discovery Page (app/discovery/page.tsx)

**Files:**
- Create: `web/app/discovery/page.tsx`

**Rationale:** Main page with state management, queries, and layout. The heart of Phase 5B.

- [ ] **Step 1: Create page component with ProtectedRoute and state**

```typescript
'use client'

import { useState, useMemo, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { PageHeader, Card } from '@/components/ui-modern'
import { Search, X } from 'lucide-react'
import * as discoveryApi from '@/lib/api/discovery'
import { ContainerMetadata } from '@/lib/api/types'

// Import components
import { CoverageMetrics } from '@/components/discovery/CoverageMetrics'
import { ContainerTable } from '@/components/discovery/ContainerTable'
import { ContainerDetailsDrawer } from '@/components/discovery/ContainerDetailsDrawer'
import { DiscoveryFilters } from '@/components/discovery/DiscoveryFilters'
import { SecretMappingsTable } from '@/components/discovery/SecretMappingsTable'
import { DiscoveryMetricsSection } from '@/components/discovery/DiscoveryMetricsSection'
import { RefreshButton } from '@/components/discovery/RefreshButton'
import { EmptyState } from '@/components/discovery/EmptyState'

type FilterType = 'managed' | 'partial' | 'unmanaged' | 'running' | 'stopped'

function DiscoveryContent() {
  const queryClient = useQueryClient()

  // State
  const [searchTerm, setSearchTerm] = useState('')
  const [filters, setFilters] = useState<{ classification: FilterType[]; status: FilterType[] }>({
    classification: [],
    status: [],
  })
  const [selectedContainer, setSelectedContainer] = useState<ContainerMetadata | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  // Queries
  const { data: discoveryData, isLoading: containersLoading, error: containersError } = useQuery({
    queryKey: ['discovery', 'containers'],
    queryFn: discoveryApi.getContainers,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
    refetchOnWindowFocus: false,
  })

  const { data: mappingsData, isLoading: mappingsLoading, error: mappingsError } = useQuery({
    queryKey: ['discovery', 'mappings'],
    queryFn: discoveryApi.getMappings,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
    refetchOnWindowFocus: false,
  })

  const { data: metricsData, isLoading: metricsLoading, error: metricsError } = useQuery({
    queryKey: ['discovery', 'metrics'],
    queryFn: discoveryApi.getDiscoveryMetrics,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
    refetchOnWindowFocus: false,
  })

  // Normalization and filtering
  const normalizedSearch = searchTerm.trim().toLowerCase()

  const filteredContainers = useMemo(() => {
    const containers = discoveryData?.containers || []

    return containers
      .filter(c => {
        if (filters.classification.length === 0) return true
        const classification = c.dso_awareness?.classification ?? 'unmanaged'
        return filters.classification.includes(classification as FilterType)
      })
      .filter(c => {
        if (filters.status.length === 0) return true
        return filters.status.includes(c.status as FilterType)
      })
      .filter(c => {
        if (normalizedSearch === '') return true
        return (
          c.container_name.toLowerCase().includes(normalizedSearch) ||
          c.image.toLowerCase().includes(normalizedSearch) ||
          c.status.toLowerCase().includes(normalizedSearch)
        )
      })
  }, [discoveryData?.containers, filters, normalizedSearch])

  // Manual refresh
  const handleRefresh = useCallback(async () => {
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
  }, [queryClient])

  // Container counts for filter display
  const containerCounts = {
    managed: discoveryData?.managed ?? 0,
    partial: discoveryData?.partial ?? 0,
    unmanaged: discoveryData?.unmanaged ?? 0,
  }

  return (
    <div className="p-6 space-y-5">
      {/* Header */}
      <PageHeader
        title="Discovery"
        description="Container discovery and secret mapping suggestions"
        actions={<RefreshButton isRefreshing={isRefreshing} onRefresh={handleRefresh} />}
      />

      {/* Search & Filters */}
      <div className="flex flex-col md:flex-row gap-3">
        <div className="relative flex-1 max-w-lg">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-600" />
          <input
            className="w-full pl-9 pr-4 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-300 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20"
            placeholder="Search by container name, image, or status…"
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
          />
          {searchTerm && (
            <button
              className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-600 hover:text-slate-400"
              onClick={() => setSearchTerm('')}
            >
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>

        <Card className="p-3">
          <DiscoveryFilters
            filters={filters}
            onFilterChange={setFilters}
            containerCount={containerCounts}
          />
        </Card>
      </div>

      {/* Coverage Metrics */}
      <CoverageMetrics containers={discoveryData?.containers} isLoading={containersLoading} />

      {/* Container Error */}
      {containersError && (
        <Card className="p-4 border-red-500/30 bg-red-500/10">
          <div className="flex items-center justify-between">
            <p className="text-sm text-red-400">Unable to load discovered containers</p>
            <button
              onClick={() =>
                queryClient.invalidateQueries({ queryKey: ['discovery', 'containers'] })
              }
              className="text-sm text-red-400 hover:text-red-300 underline"
            >
              Retry
            </button>
          </div>
        </Card>
      )}

      {/* Container Table */}
      {!containersError && (
        <ContainerTable
          containers={filteredContainers}
          isLoading={containersLoading}
          onSelectContainer={setSelectedContainer}
        />
      )}

      {/* Secret Mappings */}
      <div>
        <h2 className="text-lg font-semibold text-slate-200 mb-3">Secret Mapping Suggestions</h2>
        {mappingsError ? (
          <Card className="p-4 border-amber-500/30 bg-amber-500/10">
            <p className="text-sm text-amber-400">Unable to load secret suggestions</p>
          </Card>
        ) : (
          <SecretMappingsTable
            mappings={mappingsData?.suggestions}
            searchTerm={searchTerm}
            isLoading={mappingsLoading}
          />
        )}
      </div>

      {/* Discovery Metrics */}
      <div>
        <h2 className="text-lg font-semibold text-slate-200 mb-3">Cache Health</h2>
        <DiscoveryMetricsSection metrics={metricsData} isLoading={metricsLoading} />
      </div>

      {/* Container Details Drawer */}
      <ContainerDetailsDrawer
        container={selectedContainer}
        onClose={() => setSelectedContainer(null)}
      />
    </div>
  )
}

export default function DiscoveryPage() {
  return (
    <ProtectedRoute>
      <DiscoveryContent />
    </ProtectedRoute>
  )
}
```

- [ ] **Step 2: Verify TypeScript**

```bash
cd web && npx tsc --noEmit --skipLibCheck
```

Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/app/discovery/page.tsx
git commit -m "feat: create main Discovery page with state management and queries"
```

---

### Task 11: Verify All Components Compile & Integration Test

**Files:**
- All components created
- Main page created

**Rationale:** Final verification before testing in browser.

- [ ] **Step 1: Full TypeScript check**

```bash
cd web && npx tsc --noEmit --skipLibCheck 2>&1 | head -50
```

Expected: No errors (clean output)

- [ ] **Step 2: Verify all components import correctly**

Run the dev server:

```bash
cd web && npm run dev
```

Expected: Server starts without errors

- [ ] **Step 3: Navigate to discovery page**

Open browser to: `http://localhost:3000/discovery`

Expected: Page loads, shows loading skeletons initially

- [ ] **Step 4: Verify features**

Check the following work:
- [ ] Page loads and shows containers
- [ ] Search box filters by name/image/status
- [ ] Filter badges work (managed/partial/unmanaged)
- [ ] Status filters work (running/stopped)
- [ ] Container click opens details drawer
- [ ] Details drawer shows all sections (general, networks, env vars collapsed, DSO)
- [ ] Env vars section is collapsible and scrollable
- [ ] Container ID has copy button
- [ ] Secret mappings table displays
- [ ] Confidence badges show colors (high=green, medium=yellow, low=red)
- [ ] Metrics section collapsible and shows cache stats
- [ ] Refresh button works and updates "Last refreshed" timestamp
- [ ] Empty states show correct messages and icons
- [ ] Auto-refresh fires every 30 seconds (check React Query devtools)

- [ ] **Step 5: Commit final verification**

```bash
git add -A && git commit -m "feat: complete Phase 5B Discovery integration with all components"
```

---

## Validation Checklist (Pre-Shipping)

Before marking Phase 5B complete:

- [ ] Discovery page loads without errors
- [ ] Containers display with correct classification badges (green/yellow/gray)
- [ ] Search works across container name, image, status
- [ ] Classification filters work (managed/partial/unmanaged)
- [ ] Status filters work (running/stopped)
- [ ] Active filter chips display and remove correctly
- [ ] Container click opens details drawer
- [ ] Details drawer shows all sections
- [ ] Environment variables section collapsible and scrollable
- [ ] Container ID copy button functional
- [ ] Secret mappings table visible with confidence colors
- [ ] Search highlights matching mappings
- [ ] Metrics section collapsible with all stats
- [ ] Manual refresh button works and updates timestamp
- [ ] "Last refreshed: Xs ago" updates in real-time
- [ ] Auto-refresh fires every 30 seconds
- [ ] Error states handled gracefully and non-blocking
- [ ] Empty states show distinct icons and messages
- [ ] TypeScript strict mode passes (no 'any' types)
- [ ] All components reusable (no hardcoded values)
- [ ] React Query query keys frozen: ['discovery', 'containers|mappings|metrics']
- [ ] No memory leaks on unmount (intervals cleaned up)

---

## Success Criteria

✅ **Phase 5B Complete When:**
1. All 9 + 1 (main page) files created
2. Real backend API integration (no mock data)
3. Search + filters working correctly
4. 30-second auto-refresh + manual refresh
5. All error states handled gracefully
6. TypeScript strict mode compliance
7. Follows Phase 5A audit page patterns
8. All validations passing
9. Ready for Phase 5C Operations Console

