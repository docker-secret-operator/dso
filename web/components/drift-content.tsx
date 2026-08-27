'use client'

import { useEffect, useState } from 'react'
import { GitCompare, CheckCircle2, Info } from 'lucide-react'

import { fetchDrift, type DriftFinding } from '@/lib/api/drift'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

const DRIFT_LABEL: Record<DriftFinding['drift_type'], string> = {
  MISSING_CONTAINER: 'Missing container',
  MISSING_MAPPING: 'Missing mapping',
  MAPPING_MISMATCH: 'Mapping mismatch',
}

/**
 * Configuration Drift: GET /api/drift. Compares DSO's declared secret/
 * container mappings (Config.Secrets[].Targets.Containers) against
 * Docker's currently running containers -- computed live on every load,
 * no persistence, no history. Never shows secret values, only names/
 * labels/container identifiers.
 */
export function DriftContent() {
  const [summary, setSummary] = useState<{ total: number; in_sync: number; drifted: number } | null>(null)
  const [findings, setFindings] = useState<DriftFinding[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const data = await fetchDrift()
        if (!cancelled) {
          setSummary(data.summary)
          setFindings(data.findings)
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load drift status')
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-4">
          <h1 className="text-xl font-semibold text-foreground">Configuration Drift</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Compares DSO&apos;s declared secret/container mappings with the currently running Docker containers.
          </p>
        </header>

        <div className="mb-6 flex items-start gap-2 rounded-lg border border-border bg-muted/20 px-4 py-2.5 text-xs text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
          <p>
            Computed live from the loaded configuration and a fresh Docker container list on every request -- nothing
            is stored. Only secrets with explicit <code className="rounded bg-muted/40 px-1 py-0.5">targets.containers</code>{' '}
            declared in <code className="rounded bg-muted/40 px-1 py-0.5">dso.yaml</code> are checked.
          </p>
        </div>

        {loading && (
          <Card data-testid="drift-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <Skeleton className="h-4 w-64" />
                <Skeleton className="h-4 w-24" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div
            data-testid="drift-error"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            {error}
          </div>
        )}

        {!loading && !error && summary && (
          <div className="mb-6 grid grid-cols-3 gap-4">
            <Card className="px-5 py-4">
              <p className="text-xs uppercase tracking-wide text-muted-foreground">Checked</p>
              <p className="mt-1 text-2xl font-semibold text-foreground">{summary.total}</p>
            </Card>
            <Card className="px-5 py-4">
              <p className="text-xs uppercase tracking-wide text-muted-foreground">In sync</p>
              <p className="mt-1 text-2xl font-semibold text-emerald-400">{summary.in_sync}</p>
            </Card>
            <Card className="px-5 py-4">
              <p className="text-xs uppercase tracking-wide text-muted-foreground">Drifted</p>
              <p className="mt-1 text-2xl font-semibold text-red-400">{summary.drifted}</p>
            </Card>
          </div>
        )}

        {!loading && !error && findings.length === 0 && (
          <Card>
            <EmptyState
              data-testid="drift-empty"
              icon={summary && summary.total > 0 ? CheckCircle2 : GitCompare}
              title={summary && summary.total > 0 ? 'Everything is in sync' : 'No declared container targets to check'}
              description={
                summary && summary.total > 0
                  ? 'Every declared secret/container mapping matches what Docker is currently running.'
                  : 'No secret in dso.yaml declares an explicit targets.containers list, so there is nothing to compare yet.'
              }
            />
          </Card>
        )}

        {!loading && !error && findings.length > 0 && (
          <Card data-testid="drift-table" className="overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">Secret</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>Container</TableHead>
                  <TableHead>Drift</TableHead>
                  <TableHead>Expected</TableHead>
                  <TableHead className="pr-5">Actual</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {findings.map((f, idx) => (
                  <TableRow key={`${f.secret}-${f.target}-${idx}`}>
                    <TableCell className="pl-5 text-foreground">{f.secret}</TableCell>
                    <TableCell className="text-muted-foreground">{f.target}</TableCell>
                    <TableCell className="text-muted-foreground">{f.container ?? '—'}</TableCell>
                    <TableCell>
                      <Badge variant="destructive">{DRIFT_LABEL[f.drift_type] ?? f.drift_type}</Badge>
                    </TableCell>
                    <TableCell className="max-w-xs text-xs text-muted-foreground">{f.expected}</TableCell>
                    <TableCell className="max-w-xs pr-5 text-xs text-muted-foreground">{f.actual}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        )}
      </div>
    </div>
  )
}
