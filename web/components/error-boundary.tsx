'use client'

import React from 'react'
import { AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface Props {
  children: React.ReactNode
  fallback?: React.ReactNode
}

interface State {
  hasError: boolean
  error?: Error
}

export class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false }
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Error caught by boundary:', error, errorInfo)
  }

  handleReset = () => {
    this.setState({ hasError: false, error: undefined })
  }

  render() {
    if (this.state.hasError) {
      return (
        this.props.fallback || (
          <div className="flex items-center justify-center rounded-lg border border-red-200 bg-red-50 p-6">
            <div className="text-center">
              <AlertCircle className="w-8 h-8 text-red-600 mx-auto mb-3" />
              <h3 className="text-sm font-semibold text-red-900 mb-1">Something went wrong</h3>
              <p className="text-xs text-red-700 mb-4">{this.state.error?.message || 'An error occurred'}</p>
              <div className="flex gap-2 justify-center">
                <Button onClick={this.handleReset} size="sm" variant="outline">
                  Try again
                </Button>
                <Button onClick={() => window.location.reload()} size="sm" variant="outline">
                  Refresh page
                </Button>
              </div>
            </div>
          </div>
        )
      )
    }

    return this.props.children
  }
}
