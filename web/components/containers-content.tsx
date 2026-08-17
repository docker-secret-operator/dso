'use client'

import { useEffect, useState } from 'react'
import { fetchContainers, type ContainerSummary } from '@/lib/api/containers'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

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

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-4xl">
        <header className="mb-8">
          <h1 className="text-xl font-semibold text-foreground">Containers</h1>
          <p className="mt-1 text-sm text-muted-foreground">Containers currently tracked for secret rotation and reload</p>
        </header>

        {loading && (
          <div data-testid="containers-loading" className="text-sm text-muted-foreground">
            Loading containers…
          </div>
        )}

        {!loading && error && (
          <div data-testid="containers-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {!loading && !error && containers.length === 0 && (
          <div data-testid="containers-empty" className="text-sm text-muted-foreground">
            No containers are currently tracked.
          </div>
        )}

        {!loading && !error && containers.length > 0 && (
          <Card data-testid="containers-list" className="overflow-hidden px-5 py-2">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Container ID</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Compose Path</TableHead>
                  <TableHead>Secrets</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {containers.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-mono text-xs">{c.id}</TableCell>
                    <TableCell>
                      <Badge variant={c.strategy === 'restart' ? 'warning' : 'secondary'}>{c.strategy}</Badge>
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
