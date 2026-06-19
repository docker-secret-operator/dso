import { apiClient } from '../api-client'
import {
  LoginRequest,
  LoginResponse,
  LogoutResponse,
  UserInfo,
  SessionInfo,
  ChangePasswordRequest,
  RefreshResponse,
  UnauthorizedError,
} from './types'

/**
 * Authentication API service
 * Handles login, logout, session management, and password operations
 */

const API_BASE = '/api/auth'

export async function login(credentials: LoginRequest): Promise<LoginResponse> {
  try {
    const response = await apiClient.client.post<LoginResponse>(`${API_BASE}/login`, credentials)

    // Store token in localStorage
    if (response.data.token) {
      localStorage.setItem('dso_api_token', response.data.token)
    }

    return response.data
  } catch (error: any) {
    if (error.response?.status === 401) {
      throw new UnauthorizedError('Invalid username or password')
    }
    throw error
  }
}

export async function logout(): Promise<LogoutResponse> {
  try {
    const response = await apiClient.client.post<LogoutResponse>(`${API_BASE}/logout`, {})

    // Clear token from localStorage
    localStorage.removeItem('dso_api_token')

    return response.data
  } catch (error) {
    // Clear token even on error
    localStorage.removeItem('dso_api_token')
    throw error
  }
}

export async function currentUser(): Promise<UserInfo> {
  const response = await apiClient.client.get<UserInfo>(`${API_BASE}/me`)
  return response.data
}

export async function sessionInfo(): Promise<SessionInfo> {
  const response = await apiClient.client.get<SessionInfo>(`${API_BASE}/session`)
  return response.data
}

export async function changePassword(request: ChangePasswordRequest): Promise<{ status: string }> {
  const response = await apiClient.client.post<{ status: string }>(
    `${API_BASE}/change-password`,
    request
  )
  return response.data
}

export async function resetPassword(userId: string, newPassword: string): Promise<{ status: string }> {
  const response = await apiClient.client.post<{ status: string }>(
    `${API_BASE}/reset-password`,
    { user_id: userId, new_password: newPassword }
  )
  return response.data
}

export async function refreshToken(): Promise<RefreshResponse> {
  const response = await apiClient.client.post<RefreshResponse>(
    `${API_BASE}/refresh`,
    {}
  )
  return response.data
}

/**
 * Get stored token from localStorage
 */
export function getStoredToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem('dso_api_token')
}

/**
 * Clear stored token
 */
export function clearStoredToken(): void {
  if (typeof window === 'undefined') return
  localStorage.removeItem('dso_api_token')
}

/**
 * Check if user is authenticated (has valid token)
 */
export function isAuthenticated(): boolean {
  return !!getStoredToken()
}
