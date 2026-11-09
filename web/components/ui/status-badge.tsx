import { CheckCircle2, XCircle, AlertTriangle, HelpCircle, type LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'
import { Badge, type BadgeProps } from '@/components/ui/badge'

type StatusTone = 'success' | 'warning' | 'destructive' | 'neutral'

const TONE_ICON: Record<StatusTone, LucideIcon> = {
  success: CheckCircle2,
  warning: AlertTriangle,
  destructive: XCircle,
  neutral: HelpCircle,
}

const TONE_BADGE_VARIANT: Record<StatusTone, BadgeProps['variant']> = {
  success: 'success',
  warning: 'warning',
  destructive: 'destructive',
  neutral: 'secondary',
}

// Wire-format status strings vary across the DSO backend endpoints (secrets,
// integrations, alerts, agent health) that surface this badge. Rather than
// hardcode every literal, classify by common substrings so any real value
// still maps to an honest tone instead of silently falling through.
function classify(status: string): StatusTone {
  const s = status.toLowerCase()
  if (['critical', 'error', 'failed', 'failure', 'down', 'disconnected', 'unhealthy'].some((k) => s.includes(k))) {
    return 'destructive'
  }
  if (['warn', 'degraded', 'pending', 'stale', 'unknown'].some((k) => s.includes(k))) {
    return 'warning'
  }
  if (['active', 'ok', 'up', 'healthy', 'connected', 'synced', 'success', 'enabled', 'on'].some((k) => s.includes(k))) {
    return 'success'
  }
  return 'neutral'
}

interface StatusBadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  status: string
  /** Override the automatic classification when the caller already knows the semantic tone. */
  tone?: StatusTone
}

/**
 * Consistent icon + color + text status indicator, reused across secrets,
 * integrations, alerts, and agent health so status is never conveyed by
 * color alone.
 */
function StatusBadge({ status, tone, className, ...props }: StatusBadgeProps) {
  const resolvedTone = tone ?? classify(status)
  const Icon = TONE_ICON[resolvedTone]
  return (
    <Badge variant={TONE_BADGE_VARIANT[resolvedTone]} className={cn('capitalize', className)} {...props}>
      <Icon className="h-3 w-3" strokeWidth={2} />
      {status}
    </Badge>
  )
}

export { StatusBadge }
export type { StatusTone }
