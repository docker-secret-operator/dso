/**
 * Rotation posture derivation
 *
 * Buckets the secret estate into rotation-health categories from real fields on
 * the Secret model (`next_rotation`, `status`). Used by the dashboard's
 * "Secret estate" hero.
 *
 * HONESTY RULES (do not weaken):
 * - A secret with no `next_rotation` (or rotation disabled) is **Unknown**, never
 *   "Fresh". We must not present unmeasured rotation as healthy.
 * - The "Secret errors" bucket is `status === 'error'`. This is NOT drift
 *   detection. Do not label it "drift" anywhere — that would imply a capability
 *   (config-vs-provider drift) that the dashboard does not have.
 */

import type { Secret } from '@/lib/api-client'

export type RotationBucket = 'fresh' | 'aging' | 'overdue' | 'errored' | 'unknown'

/** A secret is "aging" when its next rotation falls within this window. */
export const AGING_WINDOW_DAYS = 7

const DAY_MS = 24 * 60 * 60 * 1000

export interface RotationPosture {
  fresh: number
  aging: number
  overdue: number
  /** Secrets reporting an error status. NOT drift. */
  errored: number
  /** Secrets with no rotation schedule — rotation health is unmeasured. */
  unknown: number
  /** Total secrets classified. */
  total: number
  /** Actionable rotation work: overdue + aging. */
  needRotation: number
}

/** Secret error state (status === 'error'). Not drift. */
function isErrored(secret: Secret): boolean {
  return secret.status === 'error'
}

/**
 * Classify a single secret into a rotation bucket.
 *
 * @param secret - the secret to classify
 * @param now - reference time in epoch ms (injectable for testing)
 */
export function classifySecret(secret: Secret, now: number = Date.now()): RotationBucket {
  if (isErrored(secret)) return 'errored'

  // No rotation schedule (or rotation disabled) → rotation health is UNKNOWN.
  // Never treat unmeasured rotation as "fresh".
  if (!secret.next_rotation || secret.rotation_strategy === 'none') {
    return 'unknown'
  }

  const next = new Date(secret.next_rotation).getTime()
  if (Number.isNaN(next)) return 'unknown'

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
    errored: 0,
    unknown: 0,
    total: secrets.length,
    needRotation: 0,
  }

  for (const secret of secrets) {
    posture[classifySecret(secret, now)]++
  }

  posture.needRotation = posture.overdue + posture.aging
  return posture
}

/**
 * True when rotation is effectively unmeasured for the whole estate (every
 * secret is Unknown). In this case the UI must say "Rotation data unavailable"
 * rather than show a green/secured posture.
 */
export function isRotationUnavailable(posture: RotationPosture): boolean {
  return posture.total > 0 && posture.unknown >= posture.total
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
  { key: 'errored', label: 'Secret errors', fill: 'bg-red-500', text: 'text-red-400' },
  { key: 'overdue', label: 'Overdue', fill: 'bg-orange-500', text: 'text-orange-400' },
  { key: 'aging', label: 'Aging', fill: 'bg-amber-500', text: 'text-amber-400' },
  { key: 'fresh', label: 'Fresh', fill: 'bg-emerald-500', text: 'text-emerald-400' },
  { key: 'unknown', label: 'Unknown', fill: 'bg-slate-500', text: 'text-slate-400' },
]
