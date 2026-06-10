'use client'

import { useState } from 'react'
import { apiClient } from '@/lib/api-client'

function validatePolicy(password: string): string | null {
  if (password.length < 12) return 'Password must be at least 12 characters.'
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
        <h1 className="text-2xl font-semibold">Change Password</h1>
        <p className="text-sm text-muted-foreground mt-1">Update your account password</p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        {success && (
          <div className="rounded-md bg-green-50 border border-green-200 px-4 py-3 text-sm text-green-700">
            Password changed successfully.
          </div>
        )}

        {error && (
          <div className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        )}

        <div className="space-y-1">
          <label className="text-sm font-medium">Current Password</label>
          <input
            type="password"
            value={current}
            onChange={e => setCurrent(e.target.value)}
            required
            disabled={loading}
            autoComplete="current-password"
            className="w-full px-3 py-2 text-sm rounded-md border border-input bg-background focus:outline-none focus:ring-2 focus:ring-primary disabled:opacity-50"
          />
        </div>

        <div className="space-y-1">
          <label className="text-sm font-medium">New Password</label>
          <input
            type="password"
            value={next}
            onChange={e => setNext(e.target.value)}
            required
            disabled={loading}
            autoComplete="new-password"
            placeholder="Min 12 chars, upper, lower, digit"
            className="w-full px-3 py-2 text-sm rounded-md border border-input bg-background focus:outline-none focus:ring-2 focus:ring-primary disabled:opacity-50"
          />
          {policyViolation && (
            <p className="text-xs text-red-600">{policyViolation}</p>
          )}
        </div>

        <div className="space-y-1">
          <label className="text-sm font-medium">Confirm New Password</label>
          <input
            type="password"
            value={confirm}
            onChange={e => setConfirm(e.target.value)}
            required
            disabled={loading}
            autoComplete="new-password"
            className="w-full px-3 py-2 text-sm rounded-md border border-input bg-background focus:outline-none focus:ring-2 focus:ring-primary disabled:opacity-50"
          />
          {mismatch && (
            <p className="text-xs text-red-600">Passwords do not match.</p>
          )}
        </div>

        <div className="pt-2">
          <p className="text-xs text-muted-foreground mb-3">
            Requirements: at least 12 characters, one uppercase letter, one lowercase letter, one digit.
          </p>
          <button
            type="submit"
            disabled={loading || !current || !next || !confirm || !!policyViolation || !!mismatch}
            className="w-full px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? 'Saving…' : 'Change Password'}
          </button>
        </div>
      </form>
    </div>
  )
}
