# Discovery Page Enhancements Plan

**Goal:** Add 4 high-impact features to make Discovery page production-ready: export, demo mode, quick stats, bulk selection.

**Scope:** 4 tasks, minimal breaking changes, builds on Phase 5B.

---

## Task 1: Export Containers (CSV/JSON)

**Files:**
- Create: `web/lib/utils/discovery-export.ts` (export utility)
- Modify: `web/app/discovery/page.tsx` (add export button)

**Code - Create discovery-export.ts:**

```typescript
export function exportContainersToCSV(containers: any[]): string {
  if (containers.length === 0) return ''

  const headers = [
    'Container Name',
    'Image',
    'Status',
    'Classification',
    'Managed Secrets',
    'Missing Mappings',
  ]

  const rows = containers.map(c => [
    c.container_name,
    c.image,
    c.status,
    c.dso_awareness?.classification ?? 'unmanaged',
    c.dso_awareness?.managed_secrets ?? 0,
    c.dso_awareness?.missing_mappings ?? 0,
  ])

  const csv = [
    headers.join(','),
    ...rows.map(row => row.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(',')),
  ].join('\n')

  return csv
}

export function exportContainersToJSON(containers: any[]): string {
  return JSON.stringify(containers, null, 2)
}

export function downloadExport(
  data: string,
  filename: string,
  mimeType: string
): void {
  const blob = new Blob([data], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}
```

**Modify discovery/page.tsx:**
Add to header actions:
```typescript
<div className="flex gap-2">
  <button
    onClick={() => {
      const csv = exportContainersToCSV(filteredContainers)
      downloadExport(csv, 'discovery-containers.csv', 'text/csv')
    }}
    className="text-xs px-3 py-1.5 rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 transition-colors"
  >
    CSV
  </button>
  <button
    onClick={() => {
      const json = exportContainersToJSON(filteredContainers)
      downloadExport(json, 'discovery-containers.json', 'application/json')
    }}
    className="text-xs px-3 py-1.5 rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 transition-colors"
  >
    JSON
  </button>
  <RefreshButton isRefreshing={isRefreshing} onRefresh={handleRefresh} />
</div>
```

**Commit:** "feat: add CSV/JSON export for discovered containers"

---

## Task 2: Demo Mode with Mock Data

**Files:**
- Create: `web/lib/data/discovery-mock.ts` (mock data)
- Modify: `web/app/discovery/page.tsx` (toggle demo mode)

**Code - Create discovery-mock.ts:**

```typescript
import { ContainerMetadata, SecretMappingSuggestion, DiscoveryMetrics } from '@/lib/api/types'

export const mockContainers: ContainerMetadata[] = [
  {
    container_id: 'abc123def456',
    container_name: 'api-server-prod',
    image: 'registry.example.com/api-server:v1.2.3',
    status: 'running',
    networks: { bridge: { ip_address: '172.17.0.2' }, overlay: { ip_address: '10.0.9.2' } },
    env_vars: {
      DB_HOST: 'postgres.prod.internal',
      DB_PORT: '5432',
      REDIS_URL: 'redis://cache.prod.internal:6379',
      SECRET_KEY: 'should-be-in-vault',
    },
    dso_awareness: {
      classification: 'partial',
      managed_secrets: 1,
      config_references: 2,
      missing_mappings: 1,
    },
    labels: { env: 'production', team: 'platform' },
    restart_policy: { name: 'always', max_retry_count: 0 },
  } as any,
  {
    container_id: 'xyz789uvw012',
    container_name: 'database-primary',
    image: 'postgres:15-alpine',
    status: 'running',
    networks: { bridge: { ip_address: '172.17.0.3' } },
    env_vars: { POSTGRES_PASSWORD: 'hardcoded-password', PGDATA: '/var/lib/postgresql/data' },
    dso_awareness: {
      classification: 'unmanaged',
      managed_secrets: 0,
      config_references: 0,
      missing_mappings: 2,
    },
    labels: { env: 'production', component: 'database' },
    restart_policy: { name: 'unless-stopped', max_retry_count: 0 },
  } as any,
  {
    container_id: 'pqr345stu678',
    container_name: 'cache-redis',
    image: 'redis:7-alpine',
    status: 'running',
    networks: { bridge: { ip_address: '172.17.0.4' } },
    env_vars: { REDIS_PASSWORD: 'secure-password' },
    dso_awareness: {
      classification: 'managed',
      managed_secrets: 2,
      config_references: 1,
      missing_mappings: 0,
    },
    labels: { env: 'production', component: 'cache' },
    restart_policy: { name: 'always', max_retry_count: 0 },
  } as any,
  {
    container_id: 'jkl901mno234',
    container_name: 'worker-background',
    image: 'registry.example.com/worker:latest',
    status: 'stopped',
    networks: { bridge: { ip_address: '172.17.0.5' } },
    env_vars: { QUEUE_URL: 'sqs://queue.aws.internal', API_KEY: 'demo-key' },
    dso_awareness: {
      classification: 'partial',
      managed_secrets: 1,
      config_references: 1,
      missing_mappings: 1,
    },
    labels: { env: 'staging', component: 'worker' },
    restart_policy: { name: 'no', max_retry_count: 0 },
  } as any,
]

export const mockMappings: SecretMappingSuggestion[] = [
  {
    env_var_name: 'DB_PASSWORD',
    suggested_secret_name: 'postgres-password',
    confidence: 'high',
    reason: 'Environment variable contains "password" keyword',
    is_configured: false,
  },
  {
    env_var_name: 'API_KEY',
    suggested_secret_name: 'api-key-production',
    confidence: 'high',
    reason: 'Matches naming pattern for API credentials',
    is_configured: true,
  },
  {
    env_var_name: 'SECRET_KEY',
    suggested_secret_name: 'django-secret-key',
    confidence: 'medium',
    reason: 'Likely Django application secret',
    is_configured: false,
  },
  {
    env_var_name: 'REDIS_PASSWORD',
    suggested_secret_name: 'redis-auth-password',
    confidence: 'high',
    reason: 'Redis authentication credential',
    is_configured: true,
  },
]

export const mockMetrics: DiscoveryMetrics = {
  cache_hits: 1247,
  cache_misses: 89,
  refresh_count: 23,
  avg_latency_ms: 145,
  cache_age_seconds: 42,
}
```

**Modify discovery/page.tsx - add state and toggle:**
```typescript
const [demoMode, setDemoMode] = useState(false)

// In queries, use mock data if demoMode is enabled:
const { data: discoveryData, isLoading: containersLoading, error: containersError } = useQuery({
  queryKey: ['discovery', 'containers', demoMode],
  queryFn: async () => {
    if (demoMode) {
      return {
        containers: mockContainers,
        total: mockContainers.length,
        managed: mockContainers.filter(c => c.dso_awareness?.classification === 'managed').length,
        partial: mockContainers.filter(c => c.dso_awareness?.classification === 'partial').length,
        unmanaged: mockContainers.filter(c => c.dso_awareness?.classification === 'unmanaged').length,
        timestamp: new Date().toISOString(),
      }
    }
    return discoveryApi.getContainers()
  },
  refetchInterval: demoMode ? false : 30000,
  staleTime: demoMode ? Infinity : 25000,
  retry: demoMode ? false : 2,
})

// Similar for mappings and metrics queries

// Add demo mode toggle button in header:
{demoMode && (
  <span className="text-xs bg-amber-500/20 text-amber-300 px-2 py-1 rounded border border-amber-500/30">
    Demo Mode
  </span>
)}
<button
  onClick={() => setDemoMode(!demoMode)}
  className="text-xs px-3 py-1.5 rounded-lg border transition-colors"
  title="Toggle mock data mode"
>
  {demoMode ? '🎯 Mock' : '🔴 Live'}
</button>
```

**Commit:** "feat: add demo mode with mock container data"

---

## Task 3: Quick Stats Widget

**Files:**
- Create: `web/components/discovery/QuickStats.tsx` (new component)
- Modify: `web/app/discovery/page.tsx` (add component)

**Code - Create QuickStats.tsx:**

```typescript
'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card } from '@/components/ui-modern'
import { AlertCircle, CheckCircle2, TrendingUp } from 'lucide-react'

interface QuickStatsProps {
  containers?: ContainerMetadata[]
  lastRefreshTime?: Date
}

export function QuickStats({ containers, lastRefreshTime }: QuickStatsProps) {
  if (!containers || containers.length === 0) return null

  const managed = containers.filter(c => c.dso_awareness?.classification === 'managed').length
  const partial = containers.filter(c => c.dso_awareness?.classification === 'partial').length
  const unmanaged = containers.filter(c => c.dso_awareness?.classification === 'unmanaged').length
  const needsMapping = containers.filter(c => (c.dso_awareness?.missing_mappings ?? 0) > 0).length
  const coverage = Math.round((managed / containers.length) * 100)

  let statusIcon = <CheckCircle2 className="w-4 h-4 text-emerald-400" />
  let statusLabel = 'Excellent'
  let statusColor = 'text-emerald-400'

  if (coverage < 80) statusLabel = 'Good'
  if (coverage < 60) statusLabel = 'Warning'
  if (coverage < 40) {
    statusLabel = 'Critical'
    statusIcon = <AlertCircle className="w-4 h-4 text-red-400" />
    statusColor = 'text-red-400'
  }

  return (
    <Card className="p-4 border-indigo-500/20 bg-indigo-500/5">
      <div className="flex items-start justify-between">
        <div className="space-y-3 flex-1">
          <div className="flex items-center gap-2">
            {statusIcon}
            <span className={`text-sm font-semibold ${statusColor}`}>{statusLabel} Coverage</span>
            <span className="text-xs text-slate-500">({coverage}%)</span>
          </div>
          <div className="text-xs text-slate-400 space-y-1">
            <p>
              <span className="font-medium">{managed}</span> managed • <span className="font-medium">{partial}</span> partial •{' '}
              <span className="font-medium">{unmanaged}</span> unmanaged
            </p>
            {needsMapping > 0 && (
              <p className="text-amber-400">
                <TrendingUp className="w-3 h-3 inline mr-1" />
                <span className="font-medium">{needsMapping}</span> containers need secret mapping
              </p>
            )}
          </div>
        </div>
        {lastRefreshTime && (
          <div className="text-right">
            <p className="text-xs text-slate-500">Last scan</p>
            <p className="text-xs text-slate-400">
              {new Date(lastRefreshTime).toLocaleTimeString()}
            </p>
          </div>
        )}
      </div>
    </Card>
  )
}
```

**Modify discovery/page.tsx - add component:**
Add after CoverageMetrics:
```typescript
<QuickStats containers={discoveryData?.containers} lastRefreshTime={new Date()} />
```

**Commit:** "feat: add QuickStats widget showing coverage health"

---

## Task 4: Bulk Selection & Actions

**Files:**
- Modify: `web/components/discovery/ContainerTable.tsx` (add selection)
- Modify: `web/components/discovery/ContainerRow.tsx` (add checkbox)
- Modify: `web/app/discovery/page.tsx` (manage bulk state)

**Modify ContainerRow.tsx:**
Add checkbox parameter:
```typescript
interface ContainerRowProps {
  container: ContainerMetadata
  onSelect: (container: ContainerMetadata) => void
  isSelected?: boolean
  onToggleSelect?: (containerId: string) => void
}

// In render:
<div className="grid grid-cols-7 gap-3 items-center">
  {onToggleSelect && (
    <div className="col-span-1">
      <input
        type="checkbox"
        checked={isSelected}
        onChange={() => onToggleSelect(container.container_id)}
        className="w-4 h-4 rounded border-white/20 text-indigo-500 focus:ring-indigo-500/20"
      />
    </div>
  )}
  {/* ...rest of columns... */}
</div>
```

**Modify ContainerTable.tsx:**
```typescript
interface ContainerTableProps {
  containers: ContainerMetadata[]
  isLoading: boolean
  onSelectContainer: (container: ContainerMetadata) => void
  selectedIds?: Set<string>
  onToggleSelect?: (containerId: string) => void
}

// Add to header:
{onToggleSelect && (
  <span className="col-span-1">
    <input
      type="checkbox"
      checked={selectedIds?.size === containers.length && containers.length > 0}
      onChange={() => {
        if (selectedIds?.size === containers.length) {
          selectedIds.clear()
        } else {
          containers.forEach(c => selectedIds.add(c.container_id))
        }
      }}
      className="w-4 h-4"
    />
  </span>
)}
```

**Modify discovery/page.tsx:**
```typescript
const [selectedContainerIds, setSelectedContainerIds] = useState<Set<string>>(new Set())

const handleToggleSelect = (containerId: string) => {
  const newSelected = new Set(selectedContainerIds)
  if (newSelected.has(containerId)) {
    newSelected.delete(containerId)
  } else {
    newSelected.add(containerId)
  }
  setSelectedContainerIds(newSelected)
}

const selectedContainers = filteredContainers.filter(c =>
  selectedContainerIds.has(c.container_id)
)

// Add bulk export button in header (only show if items selected):
{selectedContainerIds.size > 0 && (
  <button
    onClick={() => {
      const csv = exportContainersToCSV(selectedContainers)
      downloadExport(csv, `discovery-selected-${selectedContainerIds.size}.csv`, 'text/csv')
    }}
    className="text-xs px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-colors"
  >
    Export {selectedContainerIds.size} Selected
  </button>
)}
```

**Commit:** "feat: add bulk selection with bulk export"

---

## Task 5: Verify All Enhancements

Run TypeScript check and test all features:
1. Export CSV/JSON
2. Toggle demo mode
3. View quick stats
4. Select containers and bulk export

**Commit:** "feat: complete Discovery page enhancements"

---

## Success Criteria

- ✅ Export works (CSV and JSON)
- ✅ Demo mode toggles and shows mock data
- ✅ Quick stats displays coverage and insights
- ✅ Bulk selection works
- ✅ TypeScript passes
- ✅ Page remains responsive

