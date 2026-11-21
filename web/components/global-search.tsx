'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { Command } from 'cmdk'
import {
  Search,
  BarChart3,
  Lock,
  Bell,
  ScrollText,
  Boxes,
  Settings,
  FileClock,
  Users,
  Plug,
  GitCompare,
  LineChart,
  AlertTriangle,
  KeyRound,
} from 'lucide-react'

import { cn } from '@/lib/utils'
import { fetchSecrets } from '@/lib/api/dashboard'

/**
 * Client-side search over DSO's static route list, plus secret NAMES
 * (metadata already fetched for the Secrets page -- GET /api/secrets never
 * returns values, see lib/api/dashboard.ts's SecretSummary, so there is
 * nothing sensitive to leak here beyond what the Secrets page already
 * displays). This is deliberately NOT full-text search over event/audit
 * content or secret values -- advanced-platform's global-search.tsx
 * depended on /api/discovery/docker (doesn't exist on this branch) and
 * queried broader content, which risked surfacing sensitive metadata in a
 * quick-open palette. Nav + secret names is the honest scope here; see
 * docs/webui-phase1-notes.md.
 */
const ROUTES = [
  { name: 'Dashboard', href: '/dashboard', icon: BarChart3 },
  { name: 'Events', href: '/events', icon: Bell },
  { name: 'Audit', href: '/audit', icon: FileClock },
  { name: 'Analytics', href: '/analytics', icon: LineChart },
  { name: 'Alerts', href: '/alerts', icon: AlertTriangle },
  { name: 'Drift', href: '/drift', icon: GitCompare },
  { name: 'Secrets', href: '/secrets', icon: Lock },
  { name: 'Configuration', href: '/configuration', icon: Settings },
  { name: 'Containers', href: '/containers', icon: Boxes },
  { name: 'Integrations', href: '/integrations', icon: Plug },
  { name: 'Logs', href: '/logs', icon: ScrollText },
  { name: 'Users / Access', href: '/users', icon: Users },
  { name: 'Settings', href: '/settings', icon: Settings },
]

export function GlobalSearch() {
  const [open, setOpen] = useState(false)
  const [secretNames, setSecretNames] = useState<string[]>([])
  const router = useRouter()

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((prev) => !prev)
      }
      if (e.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])

  // Loaded lazily on open rather than eagerly on every page -- the palette
  // is the only place besides the Secrets page itself that needs secret
  // names, so there's no reason to fetch them before the operator actually
  // opens the search.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    fetchSecrets()
      .then((res) => {
        if (!cancelled) setSecretNames(res.active_secrets.map((s) => s.name))
      })
      .catch(() => {
        // Best-effort -- the palette still works for navigation if this fails.
      })
    return () => {
      cancelled = true
    }
  }, [open])

  function go(href: string) {
    setOpen(false)
    router.push(href)
  }

  return (
    <>
      <button
        type="button"
        data-testid="global-search-trigger"
        onClick={() => setOpen(true)}
        className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
      >
        <Search className="h-4 w-4" />
        <span>Search</span>
        <kbd className="ml-2 rounded border border-border bg-muted/40 px-1.5 py-0.5 text-[10px] font-medium">
          ⌘K
        </kbd>
      </button>

      {open && (
        <div
          className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-[15vh]"
          onClick={() => setOpen(false)}
        >
          <Command
            data-testid="global-search-dialog"
            className="w-full max-w-lg overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
            onClick={(e) => e.stopPropagation()}
            loop
          >
            <div className="flex items-center gap-2 border-b border-border px-4">
              <Search className="h-4 w-4 text-muted-foreground" />
              <Command.Input
                autoFocus
                placeholder="Jump to a page..."
                className="w-full bg-transparent py-3 text-sm text-foreground outline-none placeholder:text-muted-foreground"
              />
            </div>
            <Command.List className="max-h-80 overflow-y-auto p-2">
              <Command.Empty className="px-3 py-6 text-center text-sm text-muted-foreground">
                No matching page.
              </Command.Empty>
              <Command.Group heading="Navigate" className="px-2 pb-1 pt-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground [&_[cmdk-group-heading]]:px-1 [&_[cmdk-group-heading]]:pb-1">
                {ROUTES.map((route) => {
                  const Icon = route.icon
                  return (
                    <Command.Item
                      key={route.href}
                      value={route.name}
                      onSelect={() => go(route.href)}
                      className={cn(
                        'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm text-foreground',
                        'data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground'
                      )}
                    >
                      <Icon className="h-4 w-4 text-muted-foreground" />
                      {route.name}
                    </Command.Item>
                  )
                })}
              </Command.Group>
              {secretNames.length > 0 && (
                <Command.Group heading="Secrets" className="px-2 pb-1 pt-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground [&_[cmdk-group-heading]]:px-1 [&_[cmdk-group-heading]]:pb-1">
                  {secretNames.map((name) => (
                    <Command.Item
                      key={`secret-${name}`}
                      value={name}
                      onSelect={() => go('/secrets')}
                      className={cn(
                        'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm text-foreground',
                        'data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground'
                      )}
                    >
                      <KeyRound className="h-4 w-4 text-muted-foreground" />
                      {name}
                    </Command.Item>
                  ))}
                </Command.Group>
              )}
            </Command.List>
          </Command>
        </div>
      )}
    </>
  )
}
