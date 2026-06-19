'use client'

import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/contexts/AuthContext'
import { apiClient } from '@/lib/api-client'
import { PageHeader, Card, StatRow, Badge, StatusIndicator, Skeleton } from '@/components/ui-modern'
import { Shield, Server, User, Clock, Hash, ExternalLink } from 'lucide-react'
import packageJson from '../../package.json'
import Link from 'next/link'

function Section({ title, icon, children }: { title: string; icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card className="p-5">
      <div className="flex items-center gap-2 mb-4 pb-3 border-b border-white/[0.06]">
        <span className="text-slate-500">{icon}</span>
        <h2 className="text-sm font-semibold text-slate-300">{title}</h2>
      </div>
      {children}
    </Card>
  )
}

export default function SettingsPage() {
  const { user } = useAuth()

  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 30000,
  })

  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: () => apiClient.getSecrets(),
  })

  const { data: discovery } = useQuery({
    queryKey: ['discovery'],
    queryFn: () => apiClient.getDiscoverySummary(),
  })

  const uptimeHours  = health?.uptime != null ? Math.floor(health.uptime / 3600) : null
  const uptimeMins   = health?.uptime != null ? Math.floor((health.uptime % 3600) / 60) : null

  const providers = [...new Set(secrets.map((s: any) => s.provider).filter(Boolean))]

  return (
    <div className="p-6 space-y-5">
      <PageHeader
        title="Settings"
        description="System information, account links, and configuration."
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">

        {/* System info */}
        <Section title="System" icon={<Server className="w-4 h-4" />}>
          {healthLoading ? (
            <Skeleton className="h-32 w-full rounded" />
          ) : (
            <div className="space-y-0">
              <StatRow
                label="Status"
                value={<StatusIndicator status={health?.status === 'up' ? 'healthy' : 'critical'} label={health?.status === 'up' ? 'Operational' : 'Degraded'} />}
              />
              <StatRow
                label="Version"
                value={<span className="font-mono text-slate-300">{health?.version ?? '—'}</span>}
                icon={<Hash className="w-3.5 h-3.5" />}
              />
              <StatRow
                label="UI Version"
                value={<span className="font-mono text-slate-300">{packageJson.version}</span>}
                icon={<Hash className="w-3.5 h-3.5" />}
              />
              <StatRow
                label="Uptime"
                value={uptimeHours != null ? `${uptimeHours}h ${uptimeMins}m` : '—'}
                icon={<Clock className="w-3.5 h-3.5" />}
              />
              <StatRow
                label="API Endpoint"
                value={<span className="font-mono text-xs text-slate-400">{typeof window !== 'undefined' ? window.location.origin : '—'}</span>}
              />
            </div>
          )}
        </Section>

        {/* Account */}
        <Section title="Account" icon={<User className="w-4 h-4" />}>
          {user ? (
            <div className="space-y-0">
              <StatRow label="Username"     value={<span className="font-mono text-slate-300">{user.username}</span>} />
              <StatRow label="Display name" value={user.display_name ?? '—'} />
              <StatRow label="Role"         value={<Badge variant="info" size="sm" className="capitalize">{user.role}</Badge>} />
              <StatRow
                label="Password"
                value={
                  <Link href="/settings/password" className="text-indigo-400 hover:text-indigo-300 text-xs flex items-center gap-1 transition-colors">
                    Change password <ExternalLink className="w-3 h-3" />
                  </Link>
                }
              />
              <StatRow
                label="Sessions"
                value={
                  <Link href="/settings/sessions" className="text-indigo-400 hover:text-indigo-300 text-xs flex items-center gap-1 transition-colors">
                    Manage sessions <ExternalLink className="w-3 h-3" />
                  </Link>
                }
              />
            </div>
          ) : (
            <Skeleton className="h-32 w-full rounded" />
          )}
        </Section>

        {/* Secrets providers */}
        <Section title="Active Providers" icon={<Shield className="w-4 h-4" />}>
          {secrets.length === 0 ? (
            <p className="text-sm text-slate-600">No providers detected yet.</p>
          ) : (
            <div className="space-y-0">
              <StatRow label="Total secrets"  value={<span className="font-semibold text-slate-300 tabular-nums">{secrets.length}</span>} />
              <StatRow
                label="Providers"
                value={
                  <div className="flex flex-wrap gap-1">
                    {providers.map(p => (
                      <Badge key={p} variant="default" size="sm" className="capitalize">{p}</Badge>
                    ))}
                  </div>
                }
              />
              <StatRow
                label="Healthy"
                value={<span className="text-emerald-400 font-medium tabular-nums">{secrets.filter((s: any) => s.status === 'ok').length}</span>}
              />
              <StatRow
                label="Errors"
                value={
                  <span className={`font-medium tabular-nums ${secrets.filter((s: any) => s.status === 'error').length > 0 ? 'text-red-400' : 'text-slate-500'}`}>
                    {secrets.filter((s: any) => s.status === 'error').length}
                  </span>
                }
              />
            </div>
          )}
        </Section>

        {/* Infrastructure */}
        <Section title="Infrastructure" icon={<Server className="w-4 h-4" />}>
          {discovery ? (
            <div className="space-y-0">
              <StatRow label="Total containers"    value={<span className="font-semibold text-slate-300 tabular-nums">{discovery.total ?? 0}</span>} />
              <StatRow label="Managed"             value={<span className="text-emerald-400 tabular-nums">{discovery.managed ?? 0}</span>} />
              <StatRow label="Partial coverage"    value={<span className="text-amber-400 tabular-nums">{discovery.partial ?? 0}</span>} />
              <StatRow label="Unmanaged"           value={<span className="text-slate-500 tabular-nums">{discovery.unmanaged ?? 0}</span>} />
            </div>
          ) : (
            <Skeleton className="h-32 w-full rounded" />
          )}
        </Section>

      </div>

      {/* Navigation shortcuts */}
      <Card className="p-5">
        <h2 className="text-sm font-semibold text-slate-300 mb-4">Quick Links</h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          {[
            { label: 'Change Password', href: '/settings/password' },
            { label: 'Manage Sessions', href: '/settings/sessions' },
            { label: 'Security Events', href: '/security/events' },
            { label: 'Audit Logs',      href: '/audit' },
            { label: 'User Management', href: '/users' },
            { label: 'Configuration',   href: '/configuration' },
            { label: 'Integrations',    href: '/integrations' },
            { label: 'Plugins',         href: '/plugins' },
          ].map(link => (
            <Link
              key={link.href}
              href={link.href}
              className="flex items-center gap-1.5 px-3 py-2 rounded-lg text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 border border-transparent hover:border-white/[0.07] transition-all"
            >
              <ExternalLink className="w-3 h-3 flex-shrink-0" />
              {link.label}
            </Link>
          ))}
        </div>
      </Card>
    </div>
  )
}
