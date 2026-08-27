/**
 * TypeScript interfaces for DSO's webui auth API.
 *
 * PASS 1 SCOPE: only auth-relevant types are defined here. The
 * feature/web-ui branch's types.ts is a large file covering dashboard,
 * secrets, audit, and metrics APIs too -- those are deferred to a later
 * porting pass and are NOT ported here.
 *
 * These types were trimmed to match the REAL backend contract in
 * internal/server/rest.go (handleAuthLogin / handleAuthLogout /
 * handleAuthSession), not the aspirational shape the old frontend assumed:
 *   - Login/logout responses only ever carry {"status":"ok"}. There is no
 *     token, user, or session object in the body -- the session token lives
 *     only in the HttpOnly Set-Cookie header, which JS cannot read.
 *   - There is no /api/auth/me, /api/auth/refresh, or user-profile endpoint.
 *     DSO's webui is a single-operator model; the frontend only needs to
 *     know "am I authenticated", which /api/auth/session answers.
 */

export class ApiError extends Error {
  constructor(
    public message: string,
    public status?: number,
    public details?: Record<string, unknown>
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export class UnauthorizedError extends ApiError {
  constructor(message = 'Unauthorized') {
    super(message, 401)
    this.name = 'UnauthorizedError'
  }
}

export interface LoginRequest {
  username: string
  password: string
}

/** Matches the real body written by handleAuthLogin: `{"status":"ok"}`. */
export interface LoginResponse {
  status: string
}

/** Matches the real body written by handleAuthLogout: `{"status":"ok"}`. */
export interface LogoutResponse {
  status: string
}

/**
 * Matches the real body written by handleAuthSession. `username` was added
 * alongside the Phase 1 Users/Access page -- DSO's session already knows the
 * operator's username server-side, this just exposes that existing fact.
 */
export interface SessionCheckResponse {
  authenticated: boolean
  username?: string
}
