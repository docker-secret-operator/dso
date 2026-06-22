'use client'

import { useCallback, useEffect, useState } from 'react'
import dynamic from 'next/dynamic'
import { apiFetch } from '@/lib/api-fetch'
import {
  AlertTriangle, CheckCircle2, RotateCcw, Save, ShieldCheck, X, History, Loader2,
} from 'lucide-react'

const YamlEditor = dynamic(() => import('./yaml-editor'), {
  ssr: false,
  loading: () => <div className="h-[420px] rounded-lg border border-white/[0.08] bg-[#0B1020] animate-pulse" />,
})

interface PlanChange { op: string; kind: string; name: string; impact?: string }
interface ApplyPlan {
  total_secrets: number
  containers_affected: number
  secrets_to_update: number
  changes: PlanChange[]
}
interface ApplyResponse {
  success: boolean
  restart_required: boolean
  backup_path?: string
  plan?: ApplyPlan
  result?: { success: boolean; error?: string }
}
interface BackupInfo { timestamp: string; path: string; size: number }

const OP_SYMBOL: Record<string, string> = { create: '+', update: '~', remove: '-' }
const OP_COLOR: Record<string, string> = {
  create: 'text-emerald-400', update: 'text-amber-400', remove: 'text-red-400',
}

export function ConfigEditorPanel() {
  const [yaml, setYaml] = useState('')
  const [baseHash, setBaseHash] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [restartRequired, setRestartRequired] = useState(false)

  const [validationErrors, setValidationErrors] = useState<string[] | null>(null)
  const [validated, setValidated] = useState(false)
  const [message, setMessage] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null)

  const [preview, setPreview] = useState<ApplyPlan | null>(null)
  const [backups, setBackups] = useState<BackupInfo[]>([])
  const [showBackups, setShowBackups] = useState(false)

  const loadRaw = useCallback(async () => {
    try {
      const res = await apiFetch('/api/config/raw')
      if (!res.ok) throw new Error(`Failed to load config (${res.status})`)
      const data = await res.json()
      setYaml(data.yaml ?? '')
      setBaseHash(data.sha256 ?? '')
      setRestartRequired(Boolean(data.restart_required))
      setLoaded(true)
    } catch (e) {
      setMessage({ kind: 'err', text: e instanceof Error ? e.message : 'Failed to load configuration' })
    }
  }, [])

  const loadBackups = useCallback(async () => {
    try {
      const res = await apiFetch('/api/config/backups')
      if (res.ok) setBackups(await res.json())
    } catch {
      /* non-fatal */
    }
  }, [])

  useEffect(() => {
    loadRaw()
    loadBackups()
  }, [loadRaw, loadBackups])

  const onEdit = (v: string) => {
    setYaml(v)
    setValidated(false)
    setValidationErrors(null)
    setMessage(null)
  }

  const validate = async () => {
    setBusy(true)
    setMessage(null)
    try {
      const res = await apiFetch('/api/config/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml }),
      })
      if (res.status === 400) {
        const d = await res.json().catch(() => ({}))
        setValidationErrors([d.error || 'Malformed YAML'])
        setValidated(false)
        return
      }
      const data = await res.json()
      setValidated(data.valid)
      setValidationErrors(data.valid ? [] : data.errors || ['Invalid configuration'])
    } catch (e) {
      setValidationErrors([e instanceof Error ? e.message : 'Validation failed'])
    } finally {
      setBusy(false)
    }
  }

  // Step 1 of Save & Apply: dry-run to fetch the plan for confirmation.
  const requestPlan = async () => {
    setBusy(true)
    setMessage(null)
    try {
      const res = await apiFetch('/api/config/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml, dry_run: true }),
      })
      if (!res.ok) {
        const d = await res.json().catch(() => ({}))
        setValidationErrors([d.error || `Validation failed (${res.status})`])
        return
      }
      const data: ApplyResponse = await res.json()
      setPreview(data.plan ?? { total_secrets: 0, containers_affected: 0, secrets_to_update: 0, changes: [] })
    } catch (e) {
      setMessage({ kind: 'err', text: e instanceof Error ? e.message : 'Failed to compute plan' })
    } finally {
      setBusy(false)
    }
  }

  // Step 2: confirmed apply.
  const applyChanges = async () => {
    setBusy(true)
    setPreview(null)
    setMessage(null)
    try {
      const res = await apiFetch('/api/config/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml, base_hash: baseHash, dry_run: false }),
      })
      if (res.status === 409) {
        setMessage({ kind: 'err', text: 'Configuration changed since you loaded it. Reload and re-apply.' })
        return
      }
      if (!res.ok) {
        const d = await res.json().catch(() => ({}))
        setMessage({ kind: 'err', text: d.error || `Apply failed (${res.status})` })
        return
      }
      const data: ApplyResponse = await res.json()
      setRestartRequired(data.restart_required)
      if (data.result && !data.result.success) {
        setMessage({ kind: 'err', text: `Configuration saved, but reconcile reported: ${data.result.error}` })
      } else {
        setMessage({ kind: 'ok', text: 'Configuration saved successfully.' })
      }
      await loadRaw()
      await loadBackups()
    } catch (e) {
      setMessage({ kind: 'err', text: e instanceof Error ? e.message : 'Apply failed' })
    } finally {
      setBusy(false)
    }
  }

  const rollback = async (backupPath: string) => {
    setBusy(true)
    setMessage(null)
    try {
      const res = await apiFetch('/api/config/rollback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ backup_path: backupPath }),
      })
      if (!res.ok) {
        const d = await res.json().catch(() => ({}))
        setMessage({ kind: 'err', text: d.error || `Rollback failed (${res.status})` })
        return
      }
      const data: ApplyResponse = await res.json()
      setRestartRequired(data.restart_required)
      setMessage({ kind: 'ok', text: 'Rolled back successfully.' })
      setShowBackups(false)
      await loadRaw()
      await loadBackups()
    } catch (e) {
      setMessage({ kind: 'err', text: e instanceof Error ? e.message : 'Rollback failed' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="rounded-xl border border-white/[0.07] bg-[#111827] p-5 mt-6">
      <div className="flex items-center justify-between gap-3 mb-4">
        <div>
          <h2 className="text-[15px] font-semibold text-slate-100">Edit configuration</h2>
          <p className="text-xs text-slate-500">Validate, preview, and apply changes to dso.yaml. Admin only.</p>
        </div>
        <button
          onClick={() => { setShowBackups((s) => !s); loadBackups() }}
          className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-slate-200 px-2.5 py-1.5 rounded-md border border-white/[0.08] hover:bg-white/[0.03] transition-colors"
        >
          <History className="w-3.5 h-3.5" /> Backups
        </button>
      </div>

      {restartRequired && (
        <div className="flex items-center gap-2 mb-4 px-3 py-2 rounded-lg bg-amber-500/10 border border-amber-500/20 text-amber-300 text-sm">
          <AlertTriangle className="w-4 h-4 flex-shrink-0" />
          Some changes require an agent restart to take effect.
        </div>
      )}

      {showBackups && (
        <div className="mb-4 rounded-lg border border-white/[0.08] bg-[#0B1020] p-3">
          <p className="text-xs font-semibold text-slate-300 mb-2">Restore a previous version</p>
          {backups.length === 0 ? (
            <p className="text-xs text-slate-600">No backups yet.</p>
          ) : (
            <ul className="space-y-1">
              {backups.map((b) => (
                <li key={b.path} className="flex items-center justify-between gap-3 text-xs">
                  <span className="font-mono text-slate-400">{b.timestamp}</span>
                  <span className="text-slate-600">{b.size} B</span>
                  <button
                    onClick={() => rollback(b.path)}
                    disabled={busy}
                    className="flex items-center gap-1 text-amber-400 hover:text-amber-300 disabled:opacity-50"
                  >
                    <RotateCcw className="w-3 h-3" /> Restore
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {loaded ? (
        <YamlEditor value={yaml} onChange={onEdit} readOnly={busy} />
      ) : (
        <div className="h-[420px] rounded-lg border border-white/[0.08] bg-[#0B1020] flex items-center justify-center text-slate-600">
          <Loader2 className="w-5 h-5 animate-spin" />
        </div>
      )}

      {validationErrors && (
        <div className="mt-3">
          {validationErrors.length === 0 ? (
            <p className="flex items-center gap-1.5 text-sm text-emerald-400">
              <CheckCircle2 className="w-4 h-4" /> Configuration is valid.
            </p>
          ) : (
            <div className="rounded-lg bg-red-500/5 border border-red-500/20 px-3 py-2">
              {validationErrors.map((err, i) => (
                <p key={i} className="text-sm text-red-400 font-mono">{err}</p>
              ))}
            </div>
          )}
        </div>
      )}

      {message && (
        <p className={`mt-3 text-sm ${message.kind === 'ok' ? 'text-emerald-400' : 'text-red-400'}`}>{message.text}</p>
      )}

      <div className="flex items-center gap-2 mt-4">
        <button
          onClick={validate}
          disabled={busy || !loaded}
          className="flex items-center gap-1.5 px-3.5 py-2 rounded-lg text-sm font-medium border border-white/[0.12] text-slate-200 hover:bg-white/[0.05] disabled:opacity-50 transition-colors"
        >
          <ShieldCheck className="w-4 h-4" /> Validate
        </button>
        <button
          onClick={requestPlan}
          disabled={busy || !loaded}
          className="flex items-center gap-1.5 px-3.5 py-2 rounded-lg text-sm font-medium bg-[#22D3EE] text-[#0B1020] hover:bg-[#0EA5E9] disabled:opacity-50 transition-colors"
        >
          {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />} Save &amp; Apply
        </button>
        <button
          onClick={() => { loadRaw(); setValidationErrors(null); setMessage(null) }}
          disabled={busy}
          className="flex items-center gap-1.5 px-3.5 py-2 rounded-lg text-sm font-medium text-slate-400 hover:text-slate-200 disabled:opacity-50 transition-colors"
        >
          <X className="w-4 h-4" /> Cancel
        </button>
      </div>

      {/* Dry-run plan preview dialog */}
      {preview && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-lg rounded-xl border border-white/[0.1] bg-[#111827] p-5 shadow-2xl">
            <h3 className="text-[15px] font-semibold text-slate-100 mb-3">Plan preview</h3>
            {preview.changes.length === 0 ? (
              <p className="text-sm text-slate-400">No changes detected.</p>
            ) : (
              <div className="font-mono text-sm space-y-1 max-h-64 overflow-y-auto">
                {preview.changes.map((c, i) => (
                  <div key={i} className={OP_COLOR[c.op] ?? 'text-slate-300'}>
                    {OP_SYMBOL[c.op] ?? '?'} {c.kind} {c.name}
                    {c.impact ? <span className="text-slate-600"> — {c.impact}</span> : null}
                  </div>
                ))}
              </div>
            )}
            <p className="text-xs text-slate-500 mt-3">
              Estimated impact: {preview.containers_affected} container{preview.containers_affected === 1 ? '' : 's'} affected
            </p>
            <div className="flex items-center justify-end gap-2 mt-5">
              <button
                onClick={() => setPreview(null)}
                className="px-3.5 py-2 rounded-lg text-sm text-slate-400 hover:text-slate-200"
              >
                Cancel
              </button>
              <button
                onClick={applyChanges}
                className="px-3.5 py-2 rounded-lg text-sm font-medium bg-[#22D3EE] text-[#0B1020] hover:bg-[#0EA5E9]"
              >
                Apply changes
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
