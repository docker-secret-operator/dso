import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { apiFetch } from '@/lib/api-fetch'
import { AuthenticationError } from '@/lib/errors'

describe('apiFetch', () => {
  const originalFetch = global.fetch

  afterEach(() => {
    global.fetch = originalFetch
  })

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('returns the response as-is on a normal successful fetch', async () => {
    const okResponse = new Response(JSON.stringify({ ok: true }), { status: 200 })
    global.fetch = vi.fn().mockResolvedValue(okResponse)

    const res = await apiFetch('/api/secrets')
    expect(res.status).toBe(200)
  })

  it('throws AuthenticationError on a 401 instead of returning the response', async () => {
    const unauthorized = new Response(null, { status: 401 })
    global.fetch = vi.fn().mockResolvedValue(unauthorized)

    await expect(apiFetch('/api/secrets')).rejects.toBeInstanceOf(AuthenticationError)
  })

  it('does not throw for other non-OK statuses (left to the caller)', async () => {
    const serverError = new Response(null, { status: 500 })
    global.fetch = vi.fn().mockResolvedValue(serverError)

    const res = await apiFetch('/api/secrets')
    expect(res.status).toBe(500)
  })

  it('always sends credentials: include', async () => {
    const okResponse = new Response(JSON.stringify({}), { status: 200 })
    const fetchMock = vi.fn().mockResolvedValue(okResponse)
    global.fetch = fetchMock

    await apiFetch('/api/secrets')
    expect(fetchMock).toHaveBeenCalledWith('/api/secrets', expect.objectContaining({ credentials: 'include' }))
  })
})
