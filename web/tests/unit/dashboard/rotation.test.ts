import { describe, it, expect } from 'vitest'
import type { Secret } from '@/lib/api-client'
import {
  classifySecret,
  deriveRotationPosture,
  isRotationUnavailable,
  AGING_WINDOW_DAYS,
} from '@/lib/dashboard/rotation'

const NOW = new Date('2026-06-22T12:00:00Z').getTime()
const DAY_MS = 24 * 60 * 60 * 1000

function secret(overrides: Partial<Secret> = {}): Secret {
  return {
    name: 'svc/token',
    provider: 'vault',
    status: 'ok',
    ...overrides,
  }
}

describe('classifySecret', () => {
  it('classifies a secret past its next_rotation as overdue', () => {
    const s = secret({ next_rotation: new Date(NOW - DAY_MS).toISOString() })
    expect(classifySecret(s, NOW)).toBe('overdue')
  })

  it('classifies a secret due within the aging window as aging', () => {
    const s = secret({ next_rotation: new Date(NOW + 3 * DAY_MS).toISOString() })
    expect(classifySecret(s, NOW)).toBe('aging')
  })

  it('classifies a secret due far in the future as fresh', () => {
    const s = secret({ next_rotation: new Date(NOW + (AGING_WINDOW_DAYS + 10) * DAY_MS).toISOString() })
    expect(classifySecret(s, NOW)).toBe('fresh')
  })

  it('treats an errored secret as errored regardless of rotation date', () => {
    const s = secret({ status: 'error', next_rotation: new Date(NOW + 30 * DAY_MS).toISOString() })
    expect(classifySecret(s, NOW)).toBe('errored')
  })

  it('treats a secret with no rotation schedule as UNKNOWN, never fresh', () => {
    expect(classifySecret(secret({ next_rotation: undefined }), NOW)).toBe('unknown')
  })

  it('treats a secret with rotation disabled as unknown even if a date exists', () => {
    const s = secret({ rotation_strategy: 'none', next_rotation: new Date(NOW - DAY_MS).toISOString() })
    expect(classifySecret(s, NOW)).toBe('unknown')
  })

  it('treats an unparseable next_rotation as unknown', () => {
    expect(classifySecret(secret({ next_rotation: 'not-a-date' }), NOW)).toBe('unknown')
  })
})

describe('deriveRotationPosture', () => {
  it('aggregates buckets and totals across the estate', () => {
    const secrets: Secret[] = [
      secret({ next_rotation: new Date(NOW - DAY_MS).toISOString() }), // overdue
      secret({ next_rotation: new Date(NOW + 2 * DAY_MS).toISOString() }), // aging
      secret({ next_rotation: new Date(NOW + 60 * DAY_MS).toISOString() }), // fresh
      secret({ status: 'error' }), // errored
      secret({ next_rotation: undefined }), // unknown
    ]
    const p = deriveRotationPosture(secrets, NOW)
    expect(p).toMatchObject({ overdue: 1, aging: 1, fresh: 1, errored: 1, unknown: 1, total: 5 })
  })

  it('reports needRotation as overdue + aging', () => {
    const secrets: Secret[] = [
      secret({ next_rotation: new Date(NOW - DAY_MS).toISOString() }), // overdue
      secret({ next_rotation: new Date(NOW - 5 * DAY_MS).toISOString() }), // overdue
      secret({ next_rotation: new Date(NOW + DAY_MS).toISOString() }), // aging
      secret({ next_rotation: new Date(NOW + 90 * DAY_MS).toISOString() }), // fresh
    ]
    expect(deriveRotationPosture(secrets, NOW).needRotation).toBe(3)
  })

  it('handles an empty estate', () => {
    expect(deriveRotationPosture([], NOW)).toMatchObject({
      fresh: 0, aging: 0, overdue: 0, errored: 0, unknown: 0, total: 0, needRotation: 0,
    })
  })
})

describe('isRotationUnavailable', () => {
  it('is true when every secret is unknown', () => {
    const secrets: Secret[] = [secret({ next_rotation: undefined }), secret({ rotation_strategy: 'none' })]
    expect(isRotationUnavailable(deriveRotationPosture(secrets, NOW))).toBe(true)
  })
  it('is false when at least one secret has rotation data', () => {
    const secrets: Secret[] = [
      secret({ next_rotation: undefined }),
      secret({ next_rotation: new Date(NOW + 60 * DAY_MS).toISOString() }),
    ]
    expect(isRotationUnavailable(deriveRotationPosture(secrets, NOW))).toBe(false)
  })
  it('is false for an empty estate', () => {
    expect(isRotationUnavailable(deriveRotationPosture([], NOW))).toBe(false)
  })
})
