import { apiClient } from '../api-client'
import {
  User,
  CreateUserRequest,
  UpdateUserRequest,
  ListUsersResponse,
  ListSessionsResponse,
  Session,
  NotFoundError,
  ValidationError,
} from './types'

/**
 * User Management API service
 * Handles user and session management operations
 */

const USERS_API_BASE = '/api/users'
const SESSIONS_API_BASE = '/api/sessions'
const ADMIN_SESSIONS_API_BASE = '/api/admin/sessions'

/**
 * List all users with optional filtering and pagination
 * GET /api/users
 */
export async function listUsers(params?: {
  search?: string
  role?: string
  page?: number
  page_size?: number
}): Promise<ListUsersResponse> {
  const response = await apiClient.client.get<ListUsersResponse>(USERS_API_BASE, { params })
  return response.data
}

/**
 * Get a specific user by ID
 * GET /api/users/{id}
 */
export async function getUser(id: string): Promise<User> {
  try {
    const response = await apiClient.client.get<User>(`${USERS_API_BASE}/${encodeURIComponent(id)}`)
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`User not found: ${id}`)
    }
    throw error
  }
}

/**
 * Create a new user
 * POST /api/users
 */
export async function createUser(request: CreateUserRequest): Promise<User> {
  try {
    const response = await apiClient.client.post<User>(USERS_API_BASE, request)
    return response.data
  } catch (error: any) {
    if (error.response?.status === 409) {
      throw new ValidationError('Username already exists')
    }
    if (error.response?.status === 400) {
      throw new ValidationError(error.response?.data?.error || 'Invalid user data')
    }
    throw error
  }
}

/**
 * Update a user
 * PUT /api/users/{id}
 */
export async function updateUser(id: string, request: UpdateUserRequest): Promise<User> {
  try {
    const response = await apiClient.client.put<User>(
      `${USERS_API_BASE}/${encodeURIComponent(id)}`,
      request
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`User not found: ${id}`)
    }
    throw error
  }
}

/**
 * Delete a user
 * DELETE /api/users/{id}
 */
export async function deleteUser(id: string): Promise<void> {
  try {
    await apiClient.client.delete(`${USERS_API_BASE}/${encodeURIComponent(id)}`)
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`User not found: ${id}`)
    }
    throw error
  }
}

// ============================================================================
// Session Management
// ============================================================================

/**
 * List current user's sessions
 * GET /api/sessions
 */
export async function listSessions(): Promise<ListSessionsResponse> {
  const response = await apiClient.client.get<ListSessionsResponse>(SESSIONS_API_BASE)
  return response.data
}

/**
 * Revoke a specific session
 * DELETE /api/sessions/{id}
 */
export async function revokeSession(sessionId: string): Promise<void> {
  try {
    await apiClient.client.delete(`${SESSIONS_API_BASE}/${encodeURIComponent(sessionId)}`)
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Session not found: ${sessionId}`)
    }
    throw error
  }
}

/**
 * Revoke all current user's sessions
 * POST /api/sessions/revoke-all
 */
export async function revokeAllSessions(): Promise<void> {
  await apiClient.client.post(`${SESSIONS_API_BASE}/revoke-all`, {})
}

// ============================================================================
// Admin Session Management
// ============================================================================

/**
 * List all sessions (admin only)
 * GET /api/admin/sessions
 */
export async function listAdminSessions(): Promise<ListSessionsResponse & { sessions: (Session & { username: string })[] }> {
  const response = await apiClient.client.get<ListSessionsResponse & { sessions: (Session & { username: string })[] }>(
    ADMIN_SESSIONS_API_BASE
  )
  return response.data
}

/**
 * Revoke a session as admin
 * DELETE /api/admin/sessions/{id}
 */
export async function adminRevokeSession(sessionId: string): Promise<void> {
  try {
    await apiClient.client.delete(`${ADMIN_SESSIONS_API_BASE}/${encodeURIComponent(sessionId)}`)
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Session not found: ${sessionId}`)
    }
    throw error
  }
}
