import { NextRequest, NextResponse } from 'next/server'

const API_BASE_URL = process.env.DSO_API_URL || 'http://localhost:8471'

export async function POST(request: NextRequest) {
  try {
    const body = await request.json()

    const response = await fetch(`${API_BASE_URL}/api/auth/login`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
    })

    const data = await response.json()

    if (!response.ok) {
      return NextResponse.json(
        { error: data.error || 'Authentication failed' },
        { status: response.status }
      )
    }

    return NextResponse.json(data)
  } catch (error) {
    if (process.env.NODE_ENV === 'development') {
      console.error('Login error:', error)
    }
    return NextResponse.json(
      { error: 'Unable to reach authentication service' },
      { status: 503 }
    )
  }
}
