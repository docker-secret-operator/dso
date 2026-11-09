import { Cloud, CloudCog, KeyRound, Server, ShieldCheck, type LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * Icon mapping for DSO's supported provider types (see
 * pkg/config/config.go's validProviderTypes: vault, aws, azure, huawei).
 * Uses lucide-react only -- no brand logos, just a consistent,
 * distinguishable icon per provider type.
 */
const PROVIDER_ICONS: Record<string, LucideIcon> = {
  vault: KeyRound,
  aws: Cloud,
  azure: CloudCog,
  huawei: Server,
}

interface ProviderIconProps {
  provider: string
  className?: string
}

export function ProviderIcon({ provider, className }: ProviderIconProps) {
  const Icon = PROVIDER_ICONS[provider.toLowerCase()] ?? ShieldCheck
  return <Icon className={cn('h-3.5 w-3.5 flex-shrink-0', className)} strokeWidth={2} aria-hidden="true" />
}
