'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Settings, Lock } from 'lucide-react'
import { fetchConfigRaw, ConfigFetchError, type ConfigRawResponse } from '@/lib/api/config'

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
    <div className="min-h-screen bg-[#0B1020] px-6 py-8">
      <div className="max-w-5xl mx-auto">
        <header className="flex items-center justify-between mb-8">
          <div>
            <div className="flex items-center gap-2">
              <Link href="/dashboard" className="text-slate-500 hover:text-slate-300 transition-colors">
                <ArrowLeft className="w-4 h-4" />
              </Link>
              <h1 className="text-xl font-semibold text-slate-100">Configuration</h1>
            </div>
            <p className="text-sm text-slate-500 mt-1">Loaded configuration — read-only, secret-redacted</p>
          </div>
        </header>

        {state.status === 'loading' && (
          <div data-testid="configuration-loading" className="text-sm text-slate-500">
            Loading configuration…
          </div>
        )}

        {state.status === 'unauthorized' && (
          <div data-testid="configuration-unauthorized" className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 text-sm text-amber-400 flex items-center gap-2">
            <Lock className="w-4 h-4" />
            <span>You are not authorized to view configuration.</span>
          </div>
        )}

        {state.status === 'error' && (
          <div data-testid="configuration-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400 flex items-center justify-between">
            <span>{state.error}</span>
            <button onClick={load} className="text-red-300 underline hover:text-red-200">
              Retry
            </button>
          </div>
        )}

        {state.status === 'ok' && (
          <div data-testid="configuration-content" className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <Settings className="w-5 h-5 text-slate-400" />
                <span className="text-sm font-medium text-slate-300">Providers</span>
              </div>
              {(!state.data || state.data.providers.length === 0) ? (
                <p data-testid="configuration-providers-empty" className="text-sm text-slate-600">
                  No providers configured.
                </p>
              ) : (
                <ul className="space-y-1.5">
                  {state.data.providers.map((p) => (
                    <li key={p} className="text-sm text-slate-300">{p}</li>
                  ))}
                </ul>
              )}
            </div>

            <div className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <Lock className="w-5 h-5 text-indigo-400" />
                <span className="text-sm font-medium text-slate-300">Configured secrets</span>
              </div>
              {(!state.data || state.data.secrets.length === 0) ? (
                <p data-testid="configuration-secrets-empty" className="text-sm text-slate-600">
                  No secrets configured.
                </p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="text-left text-slate-500 border-b border-white/[0.06]">
                        <th className="py-2 pr-4 font-medium">Name</th>
                        <th className="py-2 pr-4 font-medium">Provider</th>
                      </tr>
                    </thead>
                    <tbody>
                      {state.data.secrets.map((sec) => (
                        <tr key={`${sec.provider}:${sec.name}`} className="border-b border-white/[0.03] text-slate-300">
                          <td className="py-2 pr-4">{sec.name}</td>
                          <td className="py-2 pr-4 text-slate-500">{sec.provider}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
