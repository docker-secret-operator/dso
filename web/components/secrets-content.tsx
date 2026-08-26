'use client'

import { useCallback, useEffect, useState } from 'react'
import { KeyRound } from 'lucide-react'
import { fetchSecrets, type SecretsResponse } from '@/lib/api/dashboard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'

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
            <CardContent>
              {!state.data || state.data.active_secrets.length === 0 ? (
                <EmptyState
                  data-testid="secrets-empty"
                  icon={KeyRound}
                  title="No secrets currently in cache"
                  description="Secrets DSO manages will appear here once they're loaded from a provider."
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
                    {state.data.active_secrets.map((sec) => (
                      <TableRow key={`${sec.provider}:${sec.name}`}>
                        <TableCell>{sec.name}</TableCell>
                        <TableCell className="text-muted-foreground">{sec.provider}</TableCell>
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
