'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { ArrowLeft, KeyRound } from 'lucide-react'
import { fetchSecrets, type SecretsResponse } from '@/lib/api/dashboard'

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
    <div className="min-h-screen bg-[#0B1020] px-6 py-8">
      <div className="max-w-5xl mx-auto">
        <header className="flex items-center justify-between mb-8">
          <div>
            <div className="flex items-center gap-2">
              <Link href="/dashboard" className="text-slate-500 hover:text-slate-300 transition-colors">
                <ArrowLeft className="w-4 h-4" />
              </Link>
              <h1 className="text-xl font-semibold text-slate-100">Secrets</h1>
            </div>
            <p className="text-sm text-slate-500 mt-1">Managed secret metadata — read-only, no values ever exposed</p>
          </div>
        </header>

        {state.loading && (
          <div data-testid="secrets-loading" className="text-sm text-slate-500">
            Loading secrets…
          </div>
        )}

        {!state.loading && state.error && (
          <div data-testid="secrets-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400 flex items-center justify-between">
            <span>{state.error}</span>
            <button onClick={load} className="text-red-300 underline hover:text-red-200">
              Retry
            </button>
          </div>
        )}

        {!state.loading && !state.error && (
          <div data-testid="secrets-content" className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <KeyRound className="w-5 h-5 text-indigo-400" />
              <span className="text-sm font-medium text-slate-300">
                {state.data?.total_count ?? 0} managed secret{state.data?.total_count === 1 ? '' : 's'}
              </span>
            </div>

            {(!state.data || state.data.active_secrets.length === 0) ? (
              <p data-testid="secrets-empty" className="text-sm text-slate-600">
                No secrets currently in cache.
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-slate-500 border-b border-white/[0.06]">
                      <th className="py-2 pr-4 font-medium">Name</th>
                      <th className="py-2 pr-4 font-medium">Provider</th>
                      <th className="py-2 pr-4 font-medium">Status</th>
                      <th className="py-2 pr-4 font-medium">Injection</th>
                      <th className="py-2 pr-4 font-medium">Rotation</th>
                    </tr>
                  </thead>
                  <tbody>
                    {state.data.active_secrets.map((sec) => (
                      <tr key={`${sec.provider}:${sec.name}`} className="border-b border-white/[0.03] text-slate-300">
                        <td className="py-2 pr-4">{sec.name}</td>
                        <td className="py-2 pr-4 text-slate-500">{sec.provider}</td>
                        <td className="py-2 pr-4">{sec.status}</td>
                        <td className="py-2 pr-4 text-slate-500">{sec.injection_type}</td>
                        <td className="py-2 pr-4 text-slate-500">{sec.rotation_enabled ? 'on' : 'off'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
