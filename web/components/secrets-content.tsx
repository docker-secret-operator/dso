'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { KeyRound, Search } from 'lucide-react'
import { fetchSecrets, type SecretsResponse } from '@/lib/api/dashboard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { ProviderIcon } from '@/components/provider-icon'

interface SecretsState {
  loading: boolean
  error: string | null
  data: SecretsResponse | null
}

const initialState: SecretsState = { loading: true, error: null, data: null }

/**
 * Real, read-only secrets metadata view: GET /api/secrets.
 *
 * SECURITY: internal/server/rest.go's handleListSecrets never returns
 * plaintext secret values -- only name/provider/status/injection
 * type/rotation flag are in the response body. This component only ever
 * reads those declared fields off SecretSummary (see lib/api/dashboard.ts);
 * it has no code path that would render a `value`/`plaintext`/`secret`
 * field even if a future or malicious response included one, because no
 * such field is destructured or interpolated anywhere below.
 *
 * There is no rotate/edit/delete UI here: the backend exposes only a read
 * endpoint (GET /api/secrets), so this page is intentionally read-only.
 */
export function SecretsContent() {
  const [state, setState] = useState<SecretsState>(initialState)
  const [search, setSearch] = useState('')
  const [providerFilter, setProviderFilter] = useState<string | null>(null)

  const load = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const data = await fetchSecrets()
      setState({ loading: false, error: null, data })
    } catch (err) {
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load secrets',
      }))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const providers = useMemo(() => {
    const all = state.data?.active_secrets.map((s) => s.provider) ?? []
    return Array.from(new Set(all)).sort()
  }, [state.data])

  const filteredSecrets = useMemo(() => {
    const all = state.data?.active_secrets ?? []
    const query = search.trim().toLowerCase()
    return all.filter((sec) => {
      if (providerFilter && sec.provider !== providerFilter) return false
      if (!query) return true
      return sec.name.toLowerCase().includes(query) || sec.provider.toLowerCase().includes(query)
    })
  }, [state.data, search, providerFilter])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <h1 className="text-xl font-semibold text-foreground">Secrets</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Managed secret metadata — read-only, no values ever exposed
          </p>
        </header>

        {state.loading && (
          <Card data-testid="secrets-loading">
            <CardHeader className="flex-row items-center gap-2 space-y-0">
              <Skeleton className="h-5 w-5 rounded-full" />
              <Skeleton className="h-4 w-32" />
            </CardHeader>
            <CardContent className="space-y-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </CardContent>
          </Card>
        )}

        {!state.loading && state.error && (
          <div
            data-testid="secrets-error"
            className="flex items-center justify-between rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <span>{state.error}</span>
            <Button variant="link" size="sm" onClick={load} className="h-auto p-0 text-red-300 hover:text-red-200">
              Retry
            </Button>
          </div>
        )}

        {!state.loading && !state.error && (
          <Card data-testid="secrets-content">
            <CardHeader className="flex-row items-center gap-2 space-y-0">
              <KeyRound className="h-5 w-5 text-primary" />
              <CardTitle className="text-sm font-medium text-muted-foreground">
                {state.data?.total_count ?? 0} managed secret{state.data?.total_count === 1 ? '' : 's'}
              </CardTitle>
            </CardHeader>
            {state.data && state.data.active_secrets.length > 0 && (
              <div className="flex flex-col gap-3 px-6 pb-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="relative w-full sm:max-w-xs">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    data-testid="secrets-search"
                    placeholder="Search by name or provider..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="pl-9"
                  />
                </div>
                {providers.length > 1 && (
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge
                      variant={providerFilter === null ? 'default' : 'secondary'}
                      className="cursor-pointer select-none"
                      onClick={() => setProviderFilter(null)}
                    >
                      All
                    </Badge>
                    {providers.map((p) => (
                      <Badge
                        key={p}
                        variant={providerFilter === p ? 'default' : 'secondary'}
                        className="cursor-pointer select-none"
                        onClick={() => setProviderFilter(p === providerFilter ? null : p)}
                      >
                        <ProviderIcon provider={p} />
                        {p}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            )}
            <CardContent>
              {!state.data || state.data.active_secrets.length === 0 ? (
                <EmptyState
                  data-testid="secrets-empty"
                  icon={KeyRound}
                  title="No secrets currently in cache"
                  description="Secrets DSO manages will appear here once they're loaded from a provider."
                />
              ) : filteredSecrets.length === 0 ? (
                <EmptyState
                  data-testid="secrets-no-match"
                  icon={Search}
                  title={search.trim() ? `No secrets match "${search.trim()}"` : 'No secrets match the selected provider'}
                  description="Try a different search term or clear the filter."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Provider</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Injection</TableHead>
                      <TableHead>Rotation</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filteredSecrets.map((sec) => (
                      <TableRow key={`${sec.provider}:${sec.name}`}>
                        <TableCell>{sec.name}</TableCell>
                        <TableCell className="text-muted-foreground">
                          <span className="inline-flex items-center gap-1.5">
                            <ProviderIcon provider={sec.provider} />
                            {sec.provider}
                          </span>
                        </TableCell>
                        <TableCell>{sec.status}</TableCell>
                        <TableCell className="text-muted-foreground">{sec.injection_type}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {sec.rotation_enabled ? 'on' : 'off'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
