/**
 * Rotation posture derivation
 *
 * Buckets the secret estate into rotation-health categories from real fields
 * on the Secret model (`next_rotation`, `status`). Used by the dashboard's
 * Posture Summary and the Rotation Health Strip signature component.
 *
 * Data note: there is no dedicated drift endpoint yet — the backend
 * `/api/drift` handler is currently stubbed (501 Not Implemented). Until it
 * lands, "drifted" is derived from secrets whose `status === 'error'`, which is
 * the nearest real signal. Replace `isDrifted()` with the real drift feed once
 * the endpoint exists.
 */

import type { Secret } from '@/lib/api-client'

export type RotationBucket = 'fresh' | 'aging' | 'overdue' | 'drifted'

/** A secret is "aging" when its next rotation falls within this window. */
export const AGING_WINDOW_DAYS = 7

const DAY_MS = 24 * 60 * 60 * 1000

export interface RotationPosture {
  fresh: number
  aging: number
  overdue: number
  drifted: number
  /** Total secrets classified. */
  total: number
  /** Actionable rotation work: overdue + aging. */
  needRotation: number
}

/** Drift proxy until a real drift endpoint exists (see file header). */
function isDrifted(secret: Secret): boolean {
  return secret.status === 'error'
}

/**
 * Classify a single secret into a rotation bucket.
 *
 * @param secret - the secret to classify
 * @param now - reference time in epoch ms (injectable for testing)
 */
export function classifySecret(secret: Secret, now: number = Date.now()): RotationBucket {
  if (isDrifted(secret)) return 'drifted'

  // No rotation schedule (or rotation disabled) → not due, treat as fresh.
  if (!secret.next_rotation || secret.rotation_strategy === 'none') {
    return 'fresh'
  }

  const next = new Date(secret.next_rotation).getTime()
  if (Number.isNaN(next)) return 'fresh'

  if (next <= now) return 'overdue'
  if (next <= now + AGING_WINDOW_DAYS * DAY_MS) return 'aging'
  return 'fresh'
}

/**
 * Derive the full rotation posture across the secret estate.
 *
 * @param secrets - all managed secrets
 * @param now - reference time in epoch ms (injectable for testing)
 */
export function deriveRotationPosture(secrets: Secret[], now: number = Date.now()): RotationPosture {
  const posture: RotationPosture = {
    fresh: 0,
    aging: 0,
    overdue: 0,
    drifted: 0,
    total: secrets.length,
    needRotation: 0,
  }

  for (const secret of secrets) {
    posture[classifySecret(secret, now)]++
  }

  posture.needRotation = posture.overdue + posture.aging
  return posture
}

/** Visual + label metadata for each bucket, ordered most→least severe. */
export const ROTATION_BUCKET_META: Array<{
  key: RotationBucket
  label: string
  /** Tailwind background class for the strip segment. */
  fill: string
  /** Tailwind text class for legend/labels. */
  text: string
}> = [
  { key: 'drifted', label: 'Drifted', fill: 'bg-red-500', text: 'text-red-400' },
  { key: 'overdue', label: 'Overdue', fill: 'bg-orange-500', text: 'text-orange-400' },
  { key: 'aging', label: 'Aging', fill: 'bg-amber-500', text: 'text-amber-400' },
  { key: 'fresh', label: 'Fresh', fill: 'bg-emerald-500', text: 'text-emerald-400' },
]
