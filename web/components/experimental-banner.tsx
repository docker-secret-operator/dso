import { FlaskConical, Info } from 'lucide-react'

interface ExperimentalBannerProps {
  /** 'experimental' (default): no production data / non-persistent. 'beta': estimates. */
  variant?: 'experimental' | 'beta'
  /** Override the default body copy (e.g. forecasts beta warning). */
  children?: React.ReactNode
}

/**
 * Honesty banner shown at the top of pages whose backend is not production-ready.
 * Makes it impossible to mistake an experimental/estimative page for a trusted one.
 */
export function ExperimentalBanner({ variant = 'experimental', children }: ExperimentalBannerProps) {
  const isBeta = variant === 'beta'
  const Icon = isBeta ? Info : FlaskConical
  const tone = isBeta
    ? 'border-blue-500/20 bg-blue-500/[0.06] text-blue-300'
    : 'border-amber-500/20 bg-amber-500/[0.06] text-amber-300'
  const label = isBeta ? 'Beta' : 'Experimental'
  const body =
    children ??
    'This feature is under development. Production data may not yet exist, and state may not persist across restarts.'

  return (
    <div className={`flex items-start gap-3 rounded-lg border px-4 py-3 ${tone}`} role="note">
      <Icon className="w-4 h-4 mt-0.5 flex-shrink-0" aria-hidden="true" />
      <div className="min-w-0 text-sm">
        <span className="font-semibold">{label}</span>
        <span className="opacity-80"> — {body}</span>
      </div>
    </div>
  )
}
