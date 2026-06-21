import { NextRequest, NextResponse } from 'next/server'

const API_BASE_URL = process.env.DSO_API_URL || 'http://localhost:8471'

export async function GET(request: NextRequest) {
  try {
    const token = request.headers.get('authorization')?.split(' ')[1]

    if (!token) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const response = await fetch(`${API_BASE_URL}/api/auth/session`, {
      headers: {
        'Authorization': `Bearer ${token}`,
      },
    })

    if (!response.ok) {
      return NextResponse.json(
        { error: 'Failed to fetch session info' },
        { status: response.status }
      )
    }

    const data = await response.json()
    return NextResponse.json(data)
  } catch (error) {
    if (process.env.NODE_ENV === 'development') {
      console.error('Get session error:', error)
    }
    return NextResponse.json(
      { error: 'Unable to reach authentication service' },
      { status: 503 }
    )
  }
}
