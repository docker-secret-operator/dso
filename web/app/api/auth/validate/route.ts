import { NextRequest, NextResponse } from 'next/server'

const API_BASE_URL = process.env.DSO_API_URL || 'http://localhost:8471'

export async function GET(request: NextRequest) {
  try {
    const token = request.headers.get('authorization')?.split(' ')[1]

    if (!token) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    // Try to validate by getting user info
    const response = await fetch(`${API_BASE_URL}/api/auth/me`, {
      headers: {
        'Authorization': `Bearer ${token}`,
      },
    })

    if (!response.ok) {
      return NextResponse.json(
        { error: 'Token invalid or expired' },
        { status: 401 }
      )
    }

    return NextResponse.json({ valid: true })
  } catch (error) {
    if (process.env.NODE_ENV === 'development') {
      console.error('Token validation error:', error)
    }
    return NextResponse.json(
      { error: 'Unable to validate token' },
      { status: 503 }
    )
  }
}
