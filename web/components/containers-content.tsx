'use client'

import { useEffect, useMemo, useState } from 'react'
import { Boxes, Search } from 'lucide-react'
import { fetchContainers, type ContainerSummary } from '@/lib/api/containers'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'

/**
 * Real containers content: GET /api/containers (see
 * internal/server/rest.go handleContainers), which reads live from
 * ReloaderController.Targets. Read-only -- no rotate/reload action is
 * exposed here.
 */
export function ContainersContent() {
  const [containers, setContainers] = useState<ContainerSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const data = await fetchContainers()
        if (!cancelled) setContainers(data.containers)
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load containers')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const query = search.trim().toLowerCase()
  const filteredContainers = useMemo(() => {
    if (!query) return containers
    return containers.filter(
      (c) => c.id.toLowerCase().includes(query) || c.secrets.some((s) => s.toLowerCase().includes(query))
    )
  }, [containers, query])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-4xl">
        <header className="mb-8">
          <h1 className="text-xl font-semibold text-foreground">Containers</h1>
          <p className="mt-1 text-sm text-muted-foreground">Containers currently tracked for secret rotation and reload</p>
        </header>

        {!loading && !error && containers.length > 0 && (
          <div className="relative mb-4 max-w-xs">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              data-testid="containers-search"
              placeholder="Search by container ID or secret..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
        )}

        {loading && (
          <Card data-testid="containers-loading" className="overflow-hidden px-5 py-4">
            <div className="space-y-3">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          </Card>
        )}

        {!loading && error && (
          <div data-testid="containers-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {!loading && !error && containers.length === 0 && (
          <Card>
            <EmptyState
              data-testid="containers-empty"
              icon={Boxes}
              title="No containers are currently tracked"
              description="Containers DSO manages for secret rotation and reload will appear here once discovered."
            />
          </Card>
        )}

        {!loading && !error && containers.length > 0 && filteredContainers.length === 0 && (
          <Card>
            <EmptyState
              data-testid="containers-no-match"
              icon={Search}
              title={`No containers match "${search.trim()}"`}
              description="Try a different search term."
            />
          </Card>
        )}

        {!loading && !error && filteredContainers.length > 0 && (
          <Card data-testid="containers-list" className="overflow-hidden px-5 py-2">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Container ID</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Compose Path</TableHead>
                  <TableHead>Secrets</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredContainers.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-mono text-xs">{c.id}</TableCell>
                    <TableCell>
                      <Badge variant={c.strategy === 'restart' ? 'warning' : 'secondary'}>{c.strategy}</Badge>
                    </TableCell>
                    <TableCell>
                      {c.degraded ? (
                        <Badge variant="destructive" title={c.degraded_reason}>
                          Degraded
                        </Badge>
                      ) : (
                        <Badge variant="success">Healthy</Badge>
                      )}
                    </TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-muted-foreground">
                      {c.compose_path || '—'}
                    </TableCell>
                    <TableCell>
                      {c.secrets.length === 0 ? (
                        <span className="text-xs text-muted-foreground">none</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {c.secrets.map((s) => (
                            <Badge key={s} variant="secondary">
                              {s}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </TableCell>
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
