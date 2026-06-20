'use client'

import { useState } from 'react'
import { apiClient } from '@/lib/api-client'

function validatePolicy(password: string): string | null {
  if (password.length < 8) return 'Password must be at least 8 characters.'
  if (!/[A-Z]/.test(password)) return 'Password must contain at least one uppercase letter.'
  if (!/[a-z]/.test(password)) return 'Password must contain at least one lowercase letter.'
  if (!/[0-9]/.test(password)) return 'Password must contain at least one digit.'
  return null
}

export default function ChangePasswordPage() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSuccess(false)

    const policyErr = validatePolicy(next)
    if (policyErr) { setError(policyErr); return }
    if (next !== confirm) { setError('New password and confirmation do not match.'); return }
    if (next === current) { setError('New password must differ from the current password.'); return }

    setLoading(true)
    try {
      await apiClient.changePassword(current, next)
      setSuccess(true)
      setCurrent('')
      setNext('')
      setConfirm('')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to change password.'
      setError(msg)
    } finally {
      setLoading(false)
    }
  }

  const policyViolation = next ? validatePolicy(next) : null
  const mismatch = confirm && next !== confirm

  return (
    <div className="p-6 max-w-md">
      <div className="mb-6">
        <h1 className="text-2xl font-semibold text-slate-100">Change Password</h1>
        <p className="text-sm text-slate-400 mt-1">Update your account password</p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        {success && (
          <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-300">
            Password changed successfully.
          </div>
        )}

        {error && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">
            {error}
          </div>
        )}

        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-slate-300">Current Password</label>
          <input
            type="password"
            value={current}
            onChange={e => setCurrent(e.target.value)}
            required
            disabled={loading}
            autoComplete="current-password"
            className="w-full rounded-lg border border-white/[0.09] bg-[#1a1d24] px-3 py-2.5 text-sm text-slate-200 placeholder:text-slate-700 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30 disabled:opacity-50 transition-all"
          />
        </div>

        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-slate-300">New Password</label>
          <input
            type="password"
            value={next}
            onChange={e => setNext(e.target.value)}
            required
            disabled={loading}
            autoComplete="new-password"
            placeholder="Min 8 chars, upper, lower, digit"
            className="w-full rounded-lg border border-white/[0.09] bg-[#1a1d24] px-3 py-2.5 text-sm text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30 disabled:opacity-50 transition-all"
          />
          {policyViolation && (
            <p className="text-xs text-red-400">{policyViolation}</p>
          )}
        </div>

        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-slate-300">Confirm New Password</label>
          <input
            type="password"
            value={confirm}
            onChange={e => setConfirm(e.target.value)}
            required
            disabled={loading}
            autoComplete="new-password"
            className="w-full rounded-lg border border-white/[0.09] bg-[#1a1d24] px-3 py-2.5 text-sm text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30 disabled:opacity-50 transition-all"
          />
          {mismatch && (
            <p className="text-xs text-red-400">Passwords do not match.</p>
          )}
        </div>

        <div className="pt-2">
          <p className="text-xs text-slate-500 mb-3">
            Requirements: at least 8 characters, one uppercase letter, one lowercase letter, one digit.
          </p>
          <button
            type="submit"
            disabled={loading || !current || !next || !confirm || !!policyViolation || !!mismatch}
            className="w-full rounded-lg bg-indigo-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:ring-offset-[#0B1020] disabled:opacity-50 disabled:cursor-not-allowed transition-all"
          >
            {loading ? 'Saving…' : 'Change Password'}
          </button>
        </div>
      </form>
    </div>
  )
}
