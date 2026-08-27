'use client'

import { useEffect, useState } from 'react'
import { Activity, ShieldCheck, Info } from 'lucide-react'

import { fetchHealth, fetchDiscovery, type DiscoveryResponse } from '@/lib/api/dashboard'
import { fetchOperatorIdentity } from '@/lib/api/auth'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * Settings: only cards backed by real, currently-supported functionality.
 * No password-change form here -- webuiauth's credential is loaded once
 * from dso.yaml at startup and there is no config-file-rewrite mechanism to
 * persist a new hash yet (see docs/webui-phase1-notes.md "Backend gaps").
 * Session management for the current operator lives on the Users/Access
 * page rather than being duplicated here.
 */
export function SettingsContent() {
  const [healthUp, setHealthUp] = useState<boolean | null>(null)
  const [discovery, setDiscovery] = useState<DiscoveryResponse | null>(null)
  const [username, setUsername] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      const [health, disc, identity] = await Promise.all([
        fetchHealth().catch(() => null),
        fetchDiscovery().catch(() => null),
        fetchOperatorIdentity().catch(() => null),
      ])
      if (cancelled) return
      setHealthUp(health?.status === 'up')
      setDiscovery(disc)
      setUsername(identity?.username ?? null)
      setLoading(false)
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-3xl space-y-6">
        <header>
          <h1 className="text-xl font-semibold text-foreground">Settings</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            General connection status and authentication information.
          </p>
        </header>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Activity className="h-4 w-4 text-primary" />
              Connection
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            {loading ? (
              <Skeleton className="h-5 w-40" />
            ) : (
              <>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Agent status</span>
                  <Badge variant={healthUp ? 'success' : 'destructive'}>{healthUp ? 'Up' : 'Unreachable'}</Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Webui enabled</span>
                  <span className="text-foreground">{discovery?.webui_enabled ? 'Yes' : 'No'}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Webhook enabled</span>
                  <span className="text-foreground">{discovery?.webhook_enabled ? 'Yes' : 'No'}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Configured secrets</span>
                  <span className="text-foreground">{discovery?.secret_count ?? '—'}</span>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldCheck className="h-4 w-4 text-primary" />
              Authentication
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            {loading ? (
              <Skeleton className="h-5 w-40" />
            ) : (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Operator</span>
                <span className="font-medium text-foreground">{username ?? 'unknown'}</span>
              </div>
            )}
            <div className="flex items-start gap-2 pt-1">
              <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
              <p className="text-xs leading-relaxed text-muted-foreground">
                Password changes and multi-device session controls are managed on the{' '}
                <a href="/users" className="text-primary underline-offset-2 hover:underline">
                  Users / Access
                </a>{' '}
                page. In-app password changes aren&apos;t supported yet -- the operator credential is set via{' '}
                <code className="rounded bg-muted/40 px-1 py-0.5">dso.yaml</code>.
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
