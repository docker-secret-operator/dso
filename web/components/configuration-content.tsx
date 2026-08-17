'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Settings, Lock } from 'lucide-react'
import { fetchConfigRaw, ConfigFetchError, type ConfigRawResponse } from '@/lib/api/config'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

type LoadStatus = 'loading' | 'ok' | 'unauthorized' | 'error'

interface ConfigState {
  status: LoadStatus
  error: string | null
  data: ConfigRawResponse | null
}

const initialState: ConfigState = { status: 'loading', error: null, data: null }

/**
 * Real, read-only configuration view: GET /api/config/raw.
 *
 * SECURITY (sensitive page): internal/server/rest.go's handleConfigRaw only
 * ever returns secret {name, provider} pairs and a list of provider names --
 * never credentials or secret values. This component treats the response as
 * sensitive regardless: it is never passed to console.log/console.error,
 * never attached to a URL/query param, and never written to
 * localStorage/sessionStorage -- it only flows into React state, held in
 * memory for the lifetime of this component.
 *
 * 401/403 are handled explicitly and distinctly from a generic fetch
 * failure: on 401 (session invalid/expired) we redirect to /login rather
 * than rendering anything; on 403 we show a clear "not authorized" state.
 * We never silently render an empty table for either case, which would
 * look like "no configuration" instead of "you can't see this."
 *
 * Phase 1 scope: read-only. The backend has no write/apply/validate/rollback
 * endpoints for configuration, so no such UI exists here.
 */
export function ConfigurationContent() {
  const router = useRouter()
  // Kept in a ref rather than a useCallback dependency: some router
  // implementations (and this project's own test mocks) return a
  // freshly-identitied object on every render, which would otherwise
  // recreate `load` every render and, combined with the setState below,
  // retrigger the mount effect in an infinite loop.
  const routerRef = useRef(router)
  routerRef.current = router
  const [state, setState] = useState<ConfigState>(initialState)

  const load = useCallback(async () => {
    setState((s) => ({ ...s, status: 'loading', error: null }))
    try {
      const data = await fetchConfigRaw()
      setState({ status: 'ok', error: null, data })
    } catch (err) {
      if (err instanceof ConfigFetchError && err.status === 401) {
        routerRef.current.replace('/login')
        return
      }
      if (err instanceof ConfigFetchError && err.status === 403) {
        setState({ status: 'unauthorized', error: null, data: null })
        return
      }
      setState({
        status: 'error',
        error: err instanceof Error ? err.message : 'Failed to load configuration',
        data: null,
      })
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <h1 className="text-xl font-semibold text-foreground">Configuration</h1>
          <p className="mt-1 text-sm text-muted-foreground">Loaded configuration — read-only, secret-redacted</p>
        </header>

        {state.status === 'loading' && (
          <div data-testid="configuration-loading" className="text-sm text-muted-foreground">
            Loading configuration…
          </div>
        )}

        {state.status === 'unauthorized' && (
          <div
            data-testid="configuration-unauthorized"
            className="flex items-center gap-2 rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 text-sm text-amber-400"
          >
            <Lock className="h-4 w-4" />
            <span>You are not authorized to view configuration.</span>
          </div>
        )}

        {state.status === 'error' && (
          <div
            data-testid="configuration-error"
            className="flex items-center justify-between rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <span>{state.error}</span>
            <Button variant="link" size="sm" onClick={load} className="h-auto p-0 text-red-300 hover:text-red-200">
              Retry
            </Button>
          </div>
        )}

        {state.status === 'ok' && (
          <div data-testid="configuration-content" className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0">
                <Settings className="h-5 w-5 text-muted-foreground" />
                <CardTitle className="text-sm font-medium text-muted-foreground">Providers</CardTitle>
              </CardHeader>
              <CardContent>
                {!state.data || state.data.providers.length === 0 ? (
                  <p data-testid="configuration-providers-empty" className="text-sm text-muted-foreground">
                    No providers configured.
                  </p>
                ) : (
                  <ul className="space-y-1.5">
                    {state.data.providers.map((p) => (
                      <li key={p} className="text-sm text-foreground">
                        {p}
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0">
                <Lock className="h-5 w-5 text-primary" />
                <CardTitle className="text-sm font-medium text-muted-foreground">Configured secrets</CardTitle>
              </CardHeader>
              <CardContent>
                {!state.data || state.data.secrets.length === 0 ? (
                  <p data-testid="configuration-secrets-empty" className="text-sm text-muted-foreground">
                    No secrets configured.
                  </p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Provider</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {state.data.secrets.map((sec) => (
                        <TableRow key={`${sec.provider}:${sec.name}`}>
                          <TableCell>{sec.name}</TableCell>
                          <TableCell className="text-muted-foreground">{sec.provider}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  )
}
