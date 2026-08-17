'use client'

import { useEffect, useState, useCallback } from 'react'
import Link from 'next/link'
import { Activity, ShieldCheck, ShieldAlert, KeyRound, Radio, LogOut } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { fetchHealth, fetchSecrets, fetchDiscovery, type SecretsResponse, type DiscoveryResponse } from '@/lib/api/dashboard'

interface DashboardState {
  loading: boolean
  error: string | null
  healthUp: boolean | null
  secrets: SecretsResponse | null
  discovery: DiscoveryResponse | null
}

const initialState: DashboardState = {
  loading: true,
  error: null,
  healthUp: null,
  secrets: null,
  discovery: null,
}

/**
 * Real, honest dashboard content. Only wired to data the backend actually
 * provides today (GET /health, GET /api/secrets, GET /api/discovery) --
 * no forecast/correlation/autonomy/scheduler/graph/plugins/policy/insights
 * data exists server-side, so no cards for any of that are included here.
 */
export function DashboardContent() {
  const { logout } = useAuth()
  const [state, setState] = useState<DashboardState>(initialState)

  const load = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const [health, secrets, discovery] = await Promise.all([
        fetchHealth(),
        fetchSecrets(),
        fetchDiscovery(),
      ])
      setState({
        loading: false,
        error: null,
        healthUp: health.status === 'up',
        secrets,
        discovery,
      })
    } catch (err) {
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load dashboard data',
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
            <h1 className="text-xl font-semibold text-slate-100">DSO Dashboard</h1>
            <p className="text-sm text-slate-500 mt-1">Docker Secret Operator — operator overview</p>
          </div>
          <div className="flex items-center gap-3">
            <Link href="/events" className="text-sm text-slate-400 hover:text-slate-200 transition-colors">
              Events
            </Link>
            <Link href="/secrets" className="text-sm text-slate-400 hover:text-slate-200 transition-colors">
              Secrets
            </Link>
            <Link href="/configuration" className="text-sm text-slate-400 hover:text-slate-200 transition-colors">
              Configuration
            </Link>
            <button
              onClick={() => logout()}
              className="flex items-center gap-1.5 text-sm text-slate-400 hover:text-red-400 transition-colors"
            >
              <LogOut className="w-4 h-4" />
              Sign out
            </button>
          </div>
        </header>

        {state.loading && (
          <div data-testid="dashboard-loading" className="text-sm text-slate-500">
            Loading dashboard…
          </div>
        )}

        {!state.loading && state.error && (
          <div data-testid="dashboard-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400 flex items-center justify-between">
            <span>{state.error}</span>
            <button onClick={load} className="text-red-300 underline hover:text-red-200">
              Retry
            </button>
          </div>
        )}

        {!state.loading && !state.error && (
          <div data-testid="dashboard-content" className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {/* Agent status */}
            <div className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-3">
                {state.healthUp ? (
                  <ShieldCheck className="w-5 h-5 text-emerald-400" />
                ) : (
                  <ShieldAlert className="w-5 h-5 text-red-400" />
                )}
                <span className="text-sm font-medium text-slate-300">Agent status</span>
              </div>
              <p className="text-2xl font-semibold text-slate-100">
                {state.healthUp ? 'Up' : 'Down'}
              </p>
            </div>

            {/* Secrets */}
            <div className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-3">
                <KeyRound className="w-5 h-5 text-indigo-400" />
                <span className="text-sm font-medium text-slate-300">Managed secrets</span>
              </div>
              <p className="text-2xl font-semibold text-slate-100">
                {state.secrets?.total_count ?? 0}
              </p>
              {state.secrets && state.secrets.total_count === 0 && (
                <p data-testid="dashboard-secrets-empty" className="text-xs text-slate-600 mt-2">
                  No secrets currently in cache.
                </p>
              )}
            </div>

            {/* Capabilities */}
            <div className="bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-3">
                <Radio className="w-5 h-5 text-amber-400" />
                <span className="text-sm font-medium text-slate-300">Webhook capability</span>
              </div>
              <p className="text-2xl font-semibold text-slate-100">
                {state.discovery?.webhook_enabled ? 'Enabled' : 'Disabled'}
              </p>
            </div>

            {/* Secrets table */}
            <div className="sm:col-span-3 bg-[#111827] border border-white/[0.09] rounded-2xl p-6">
              <div className="flex items-center gap-2 mb-4">
                <Activity className="w-5 h-5 text-slate-400" />
                <span className="text-sm font-medium text-slate-300">Secrets</span>
              </div>
              {(!state.secrets || state.secrets.active_secrets.length === 0) ? (
                <p className="text-sm text-slate-600">No secrets to display.</p>
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
                      {state.secrets.active_secrets.map((sec) => (
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
          </div>
        )}
      </div>
    </div>
  )
}

