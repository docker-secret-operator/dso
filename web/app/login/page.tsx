'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Eye, EyeOff, AlertCircle } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Logo } from '@/components/logo'

// Illustrative feed shown on the brand panel -- rhythm and voice matching
// the real audit stream (see dashboard "Recent activity"), not a live feed.
// Never phrased as this session's actual data.
const FEED = [
  { action: 'secret rotated', target: 'mysql-prod/db-password', when: '2s ago' },
  { action: 'drift detected', target: 'redis-cache/config', when: '18s ago' },
  { action: 'access granted', target: 'svc-payments', when: '41s ago' },
  { action: 'audit logged', target: 'vault-prod/api-token', when: '1m ago' },
]

const CAPABILITIES = ['Rotation', 'Drift detection', 'Audit trail']

export default function LoginPage() {
  const [username, setUsername]   = useState('')
  const [password, setPassword]   = useState('')
  const [showPass, setShowPass]   = useState(false)
  const [loading, setLoading]     = useState(false)
  const [error, setError]         = useState('')
  const [expired, setExpired]     = useState(false)
  const [feedIndex, setFeedIndex] = useState(0)
  const router = useRouter()

  useEffect(() => {
    if (sessionStorage.getItem('session_expired') === '1') {
      sessionStorage.removeItem('session_expired')
      setExpired(true)
    }
  }, [])

  // Cycle the illustrative feed line-by-line; skipped entirely when the
  // visitor has asked the OS for reduced motion.
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const id = setInterval(() => setFeedIndex((i) => (i + 1) % FEED.length), 2800)
    return () => clearInterval(id)
  }, [])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')

    // Validate inputs
    const trimmedUsername = username.trim()
    if (!trimmedUsername) {
      setError('Username is required')
      return
    }

    if (!password) {
      setError('Password is required')
      return
    }

    if (password.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }

    setLoading(true)
    try {
      // The backend (internal/server/rest.go handleAuthLogin) sets the
      // session as an HttpOnly Set-Cookie header on success and returns only
      // {"status":"ok"} in the body -- there is no token/user/session to
      // read or store. `credentials: 'include'` ensures the browser accepts
      // and later re-sends that cookie.
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: trimmedUsername, password }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError(data.error || 'Invalid credentials. Please try again.')
        return
      }
      router.replace('/dashboard')
    } catch {
      setError('Unable to reach the API server. Is the DSO agent running?')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="grid min-h-screen bg-background lg:grid-cols-2">
      {/* Left: auth */}
      <div className="relative flex items-center justify-center px-4 py-12">
        <div className="pointer-events-none absolute inset-0 overflow-hidden">
          <div className="absolute left-1/2 top-1/3 h-[400px] w-[600px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary/[0.06] blur-3xl" />
        </div>

        <div className="relative w-full max-w-sm">
          <div className="mb-8 flex animate-in fade-in slide-in-from-bottom-2 flex-col items-center duration-500">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-accent-dim shadow-lg shadow-primary/20">
              <Logo className="h-6 w-6 text-primary" />
            </div>
            <h1 className="text-xl font-semibold text-foreground">DSO</h1>
            <p className="mt-1 text-sm text-muted-foreground">Docker Secret Operator</p>
          </div>

          <Card className="animate-in fade-in slide-in-from-bottom-4 p-8 shadow-xl delay-100 duration-500 fill-mode-both">
            <h2 className="text-base font-semibold text-foreground">Sign in to your account</h2>
            <p className="mb-6 mt-1.5 text-sm text-muted-foreground">
              Sessions are HttpOnly and expire automatically. Nothing you enter here is stored in the browser.
            </p>

            {/* Notices */}
            {expired && (
              <div className="mb-4 flex items-start gap-2.5 rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 text-sm text-amber-400">
                <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                Your session expired. Please sign in again.
              </div>
            )}
            {error && (
              <div
                role="alert"
                aria-live="assertive"
                className="mb-4 flex items-start gap-2.5 rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                {error}
              </div>
            )}

            <form onSubmit={handleSubmit} className="space-y-4">
              {/* Username */}
              <div className="space-y-1.5">
                <label htmlFor="username" className="block text-sm font-medium text-muted-foreground">
                  Username
                </label>
                <Input
                  id="username"
                  type="text"
                  value={username}
                  onChange={e => setUsername(e.target.value)}
                  required
                  autoFocus
                  autoComplete="username"
                  disabled={loading}
                  placeholder="admin"
                />
              </div>

              {/* Password */}
              <div className="space-y-1.5">
                <label htmlFor="password" className="block text-sm font-medium text-muted-foreground">
                  Password
                </label>
                <div className="relative">
                  <Input
                    id="password"
                    type={showPass ? 'text' : 'password'}
                    value={password}
                    onChange={e => setPassword(e.target.value)}
                    required
                    autoComplete="current-password"
                    disabled={loading}
                    className="pr-10"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPass(v => !v)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
                    aria-label={showPass ? 'Hide password' : 'Show password'}
                    tabIndex={-1}
                  >
                    {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>

              {/* Submit */}
              <Button type="submit" disabled={loading || !username || !password} className="mt-2 w-full">
                {loading ? (
                  <span className="flex items-center justify-center gap-2">
                    <svg className="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                    Signing in…
                  </span>
                ) : (
                  'Sign in'
                )}
              </Button>
            </form>
          </Card>

          <p className="mt-6 animate-in fade-in text-center text-xs text-muted-foreground/70 delay-300 duration-500 fill-mode-both">
            Need access? Ask your platform admin to add you.
          </p>
        </div>
      </div>

      {/* Right: brand panel */}
      <div className="relative hidden overflow-hidden border-l border-border bg-card lg:flex lg:flex-col lg:justify-between lg:p-12">
        {/* Concentric rings + watermark, echoing the lock mark */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <div className="h-[560px] w-[560px] rounded-full border border-border/60" />
          <div className="absolute h-[400px] w-[400px] rounded-full border border-border/60" />
          <div className="absolute h-[240px] w-[240px] rounded-full border border-border/60" />
          <Logo className="absolute h-24 w-24 animate-breathe text-primary/20" />
        </div>

        <div className="relative">
          <span className="inline-flex items-center gap-1.5 font-mono text-xs uppercase tracking-wider text-accent-secondary">
            <span className="relative flex h-1.5 w-1.5">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-accent-secondary opacity-75" />
              <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-accent-secondary" />
            </span>
            Operational visibility
          </span>
          <h2 className="mt-4 max-w-sm text-3xl font-semibold leading-tight text-foreground">
            Every secret rotation, drift, and access —{' '}
            <span className="rounded bg-accent-dim px-1 text-primary">logged the moment it happens</span>.
          </h2>
        </div>

        <div className="relative space-y-2">
          <div className="rounded-lg border border-border bg-background/60 p-4 font-mono text-xs backdrop-blur-sm">
            {FEED.map((entry, i) => {
              const isActive = i === feedIndex
              return (
                <div
                  key={entry.target}
                  className={`flex items-center justify-between gap-3 py-1 transition-opacity duration-500 ${
                    isActive ? 'opacity-100' : 'opacity-40'
                  }`}
                >
                  <span className="flex items-center gap-2 truncate">
                    <span className="text-muted-foreground">{isActive ? '❯' : ' '}</span>
                    <span className="text-foreground">{entry.action}</span>
                    <span className="truncate text-muted-foreground">{entry.target}</span>
                  </span>
                  <span className="flex-shrink-0 text-muted-foreground/70">{entry.when}</span>
                </div>
              )
            })}
          </div>
          <div className="flex flex-wrap gap-1.5 pt-1">
            {CAPABILITIES.map((c) => (
              <Badge key={c} variant="secondary">
                {c}
              </Badge>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
