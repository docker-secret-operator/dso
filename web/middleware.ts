import { NextRequest, NextResponse } from 'next/server'

/**
 * Protected routes that require authentication
 */
const PROTECTED_ROUTES = [
  '/dashboard',
  '/settings',
  '/users',
  '/audit',
  '/discovery',
  '/operations',
  '/events',
  '/configuration',
  '/execution',
  '/policies',
  '/recommendations',
  '/security',
  '/integrations',
  '/scheduler',
  '/profile',
]

/**
 * Public routes that don't require authentication
 */
const PUBLIC_ROUTES = ['/login', '/public', '/health', '/']

/**
 * Middleware to protect routes and handle authentication
 * Runs on every request to check if route is protected
 */
export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Allow public routes
  if (PUBLIC_ROUTES.some(route => pathname.startsWith(route))) {
    return NextResponse.next()
  }

  // Check if route is protected
  const isProtected = PROTECTED_ROUTES.some(route => pathname.startsWith(route))

  if (!isProtected) {
    return NextResponse.next()
  }

  // Get token from cookies (set by auth layer)
  const token = request.cookies.get('dso_api_token')?.value

  if (!token) {
    // No token, redirect to login
    const loginUrl = new URL('/login', request.url)
    loginUrl.searchParams.set('from', pathname)
    return NextResponse.redirect(loginUrl)
  }

  // Token exists, allow request
  // Token validation happens on client-side to avoid server-side auth dependencies
  return NextResponse.next()
}

/**
 * Configuration for middleware
 * Specify which routes the middleware should run on
 */
export const config = {
  matcher: [
    '/((?!_next/static|_next/image|favicon.ico).*)',
  ],
}
