'use client'

import { useEffect, useState } from 'react'
import { ShieldCheck, Monitor, LogOut } from 'lucide-react'

import { fetchOperatorIdentity } from '@/lib/api/auth'
import { fetchSessions, revokeOtherSessions, type SessionInfo } from '@/lib/api/sessions'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/toast-provider'

/**
 * "Access & Authentication" -- deliberately NOT a multi-user admin page.
 * DSO's webui has exactly one operator identity (internal/webuiauth); this
 * page is honest about that rather than faking a user table. What it CAN
 * show authoritatively: who is currently authenticated, and every active
 * session (browser/device) for that one operator, with the ability to sign
 * out every session except the current one. See docs/webui-phase1-notes.md.
 */
export function UsersContent() {
  const [username, setUsername] = useState<string | null>(null)
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [revoking, setRevoking] = useState(false)
  const toast = useToast()

  async function load() {
    setLoading(true)
    setError(null)
    try {
      const [identity, sessionList] = await Promise.all([fetchOperatorIdentity(), fetchSessions()])
      setUsername(identity?.username ?? null)
      setSessions(sessionList)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load access information')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  async function handleRevokeOthers() {
    setRevoking(true)
    try {
      const result = await revokeOtherSessions()
      toast.success(
        result.revoked > 0 ? 'Other sessions signed out' : 'No other sessions to sign out',
        result.revoked > 0 ? `${result.revoked} session(s) revoked.` : undefined
      )
      await load()
    } catch (err) {
      toast.error('Failed to sign out other sessions', err instanceof Error ? err.message : undefined)
    } finally {
      setRevoking(false)
    }
  }

  const otherSessionCount = sessions.filter((s) => !s.current).length

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-3xl space-y-6">
        <header>
          <h1 className="text-xl font-semibold text-foreground">Access &amp; Authentication</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            DSO&apos;s webui uses a single operator identity, not multi-user accounts.
          </p>
        </header>

        {loading && (
          <Card>
            <CardContent className="space-y-3 py-6">
              <Skeleton className="h-5 w-48" />
              <Skeleton className="h-5 w-64" />
            </CardContent>
          </Card>
        )}

        {!loading && error && (
          <div className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {!loading && !error && (
          <>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <ShieldCheck className="h-4 w-4 text-primary" />
                  Current operator
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Username</span>
                  <span className="font-medium text-foreground">{username ?? 'unknown'}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Authentication mode</span>
                  <Badge variant="secondary">Session cookie (webui)</Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Account model</span>
                  <span className="text-foreground">Single operator (no multi-user accounts)</span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Monitor className="h-4 w-4 text-primary" />
                  Active sessions
                </CardTitle>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={revoking || otherSessionCount === 0}
                  onClick={handleRevokeOthers}
                  data-testid="revoke-others-button"
                >
                  <LogOut className="mr-1.5 h-3.5 w-3.5" />
                  Sign out other sessions
                </Button>
              </CardHeader>
              <CardContent className="divide-y divide-border p-0">
                {sessions.length === 0 && (
                  <p className="px-6 py-6 text-center text-sm text-muted-foreground">No active sessions.</p>
                )}
                {sessions.map((sess) => (
                  <div key={sess.id} className="flex items-center justify-between px-6 py-3 text-sm">
                    <div>
                      <p className="text-foreground">
                        {sess.id}
                        {sess.current && (
                          <Badge variant="success" className="ml-2">
                            This session
                          </Badge>
                        )}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        Created {new Date(sess.created_at).toLocaleString()} · Last seen{' '}
                        {new Date(sess.last_seen_at).toLocaleString()}
                      </p>
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </div>
  )
}
