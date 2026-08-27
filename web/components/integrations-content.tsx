'use client'

import { useEffect, useState } from 'react'
import { Plug, Webhook, Database } from 'lucide-react'

import { fetchIntegrations, type IntegrationInfo } from '@/lib/api/integrations'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

const ICONS: Record<string, typeof Webhook> = {
  webhook: Webhook,
  provider: Database,
}

/**
 * Read-only integrations summary: GET /api/integrations. DSO's current
 * branch has no plugin/integration registry (that lives only on
 * advanced-platform behind internal/plugins + SQLite, out of scope for this
 * phase) -- this surfaces the two real integration points the loaded config
 * already knows about: the inbound secret-update webhook and configured
 * secret providers. No credentials, tokens, or endpoints are ever shown.
 */
export function IntegrationsContent() {
  const [integrations, setIntegrations] = useState<IntegrationInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const data = await fetchIntegrations()
        if (!cancelled) setIntegrations(data.integrations)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load integrations')
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
        <header className="mb-6">
          <h1 className="text-xl font-semibold text-foreground">Integrations</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Read-only summary of the integration points DSO has configured
          </p>
        </header>

        {loading && (
          <Card data-testid="integrations-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 2 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-4 w-16" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div
            data-testid="integrations-error"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            {error}
          </div>
        )}

        {!loading && !error && integrations.length === 0 && (
          <Card>
            <EmptyState
              data-testid="integrations-empty"
              icon={Plug}
              title="No integrations configured"
              description="Configure a secret-update webhook or providers in dso.yaml to see them here."
            />
          </Card>
        )}

        {!loading && !error && integrations.length > 0 && (
          <Card data-testid="integrations-table" className="overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="pr-5">Detail</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {integrations.map((it) => {
                  const Icon = ICONS[it.type] ?? Plug
                  return (
                    <TableRow key={it.name}>
                      <TableCell className="pl-5">
                        <div className="flex items-center gap-2 text-foreground">
                          <Icon className="h-4 w-4 text-muted-foreground" />
                          {it.name}
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{it.type}</TableCell>
                      <TableCell>
                        <Badge variant={it.enabled ? 'success' : 'outline'}>
                          {it.enabled ? 'Enabled' : 'Disabled'}
                        </Badge>
                      </TableCell>
                      <TableCell className="pr-5 text-xs text-muted-foreground">
                        {it.type === 'webhook' && (it.auth_configured ? 'Auth token configured' : 'No auth token set')}
                        {it.type === 'provider' && `${it.provider_count ?? 0} provider(s) configured`}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </Card>
        )}
      </div>
    </div>
  )
}
