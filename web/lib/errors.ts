/**
 * Typed error classes for API error handling
 * Allows proper error distinction and recovery strategies
 */

export class ApiError extends Error {
  constructor(
    public statusCode: number,
    message: string,
    public code?: string
  ) {
    super(message)
    this.name = 'ApiError'
    Object.setPrototypeOf(this, ApiError.prototype)
  }
}

export class AuthenticationError extends ApiError {
  constructor(message = 'Authentication failed') {
    super(401, message, 'AUTHENTICATION_FAILED')
    this.name = 'AuthenticationError'
    Object.setPrototypeOf(this, AuthenticationError.prototype)
  }
}

export class ForbiddenError extends ApiError {
  constructor(message = 'Access denied') {
    super(403, message, 'FORBIDDEN')
    this.name = 'ForbiddenError'
    Object.setPrototypeOf(this, ForbiddenError.prototype)
  }
}

export class NotFoundError extends ApiError {
  constructor(message = 'Resource not found') {
    super(404, message, 'NOT_FOUND')
    this.name = 'NotFoundError'
    Object.setPrototypeOf(this, NotFoundError.prototype)
  }
}

export class ConflictError extends ApiError {
  constructor(message = 'Conflict occurred') {
    super(409, message, 'CONFLICT')
    this.name = 'ConflictError'
    Object.setPrototypeOf(this, ConflictError.prototype)
  }
}

export class ValidationError extends ApiError {
  constructor(
    message = 'Validation failed',
    public details?: Record<string, string>
  ) {
    super(400, message, 'VALIDATION_ERROR')
    this.name = 'ValidationError'
    Object.setPrototypeOf(this, ValidationError.prototype)
  }
}

export class TimeoutError extends Error {
  constructor(message = 'Request timed out') {
    super(message)
    this.name = 'TimeoutError'
    Object.setPrototypeOf(this, TimeoutError.prototype)
  }
}

export class NetworkError extends Error {
  constructor(message = 'Network error occurred') {
    super(message)
    this.name = 'NetworkError'
    Object.setPrototypeOf(this, NetworkError.prototype)
  }
}

export class UnknownError extends Error {
  constructor(message = 'An unexpected error occurred') {
    super(message)
    this.name = 'UnknownError'
    Object.setPrototypeOf(this, UnknownError.prototype)
  }
}

/**
 * Type guard to check if error is an ApiError
 */
export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

/**
 * Type guard to check if error is an AuthenticationError
 */
export function isAuthenticationError(error: unknown): error is AuthenticationError {
  return error instanceof AuthenticationError
}

/**
 * Type guard to check if error is a ValidationError
 */
export function isValidationError(error: unknown): error is ValidationError {
  return error instanceof ValidationError
}

/**
 * Type guard to check if error is a TimeoutError
 */
export function isTimeoutError(error: unknown): error is TimeoutError {
  return error instanceof TimeoutError
}

/**
 * Type guard to check if error is a NetworkError
 */
export function isNetworkError(error: unknown): error is NetworkError {
  return error instanceof NetworkError
}

/**
 * Extract user-friendly error message from any error
 */
export function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  if (typeof error === 'string') {
    return error
  }
  return 'An unexpected error occurred'
}

/**
 * Extract error code from any error
 */
export function getErrorCode(error: unknown): string {
  if (error instanceof ApiError && error.code) {
    return error.code
  }
  if (error instanceof Error) {
    return error.name
  }
  return 'UNKNOWN_ERROR'
}
