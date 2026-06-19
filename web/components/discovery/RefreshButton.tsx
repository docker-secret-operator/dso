'use client'

import { useCallback, useEffect, useState } from 'react'
import { RotateCw } from 'lucide-react'

interface RefreshButtonProps {
  isRefreshing: boolean
  onRefresh: () => Promise<void>
}

export function RefreshButton({ isRefreshing, onRefresh }: RefreshButtonProps) {
  const [lastRefreshTimestamp, setLastRefreshTimestamp] = useState<number | null>(null)
  const [relativeTime, setRelativeTime] = useState<string>('')

  // Update relative time every second
  useEffect(() => {
    if (!lastRefreshTimestamp) return

    const interval = setInterval(() => {
      const now = Date.now()
      const secondsAgo = Math.floor((now - lastRefreshTimestamp) / 1000)

      if (secondsAgo < 60) {
        setRelativeTime(`${secondsAgo}s ago`)
      } else if (secondsAgo < 3600) {
        const minutesAgo = Math.floor(secondsAgo / 60)
        setRelativeTime(`${minutesAgo}m ago`)
      } else {
        const hoursAgo = Math.floor(secondsAgo / 3600)
        setRelativeTime(`${hoursAgo}h ago`)
      }
    }, 1000)

    return () => clearInterval(interval)
  }, [lastRefreshTimestamp])

  const handleRefresh = useCallback(async () => {
    await onRefresh()
    setLastRefreshTimestamp(Date.now())
  }, [onRefresh])

  return (
    <div className="flex flex-col items-center gap-1">
      <button
        onClick={handleRefresh}
        disabled={isRefreshing}
        className="inline-flex items-center gap-2 px-3 py-2 text-sm rounded-lg border border-white/10 text-slate-300 hover:text-slate-100 hover:bg-white/5 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        <RotateCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
        {isRefreshing ? 'Refreshing…' : 'Refresh'}
      </button>
      {lastRefreshTimestamp && !isRefreshing && (
        <span className="text-xs text-slate-500">Last refreshed: {relativeTime}</span>
      )}
    </div>
  )
}
