'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { Bell, X, CheckCheck, Trash2, AlertCircle, CheckCircle, Info, AlertTriangle, Shield, Zap } from 'lucide-react'
import { useWebSocketContext } from '@/contexts/websocket-context'
import { Event } from '@/lib/api-client'

const STORAGE_KEY = 'dso_notifications'
const MAX_NOTIFICATIONS = 50

type NotificationCategory = 'execution' | 'security' | 'operations' | 'info'

interface Notification {
  id: string
  category: NotificationCategory
  title: string
  message: string
  timestamp: string
  read: boolean
  severity: 'info' | 'warning' | 'error' | 'success'
}

function eventToNotification(event: Event): Notification | null {
  const action = event.action?.toLowerCase() ?? ''
  const severity = event.severity ?? 'info'

  let category: NotificationCategory = 'info'
  let title = ''

  // Execution events
  if (action.includes('rotation') || action.includes('inject') || action.includes('secret')) {
    category = 'execution'
    if (action.includes('fail') || severity === 'error') title = 'Rotation Failed'
    else if (action.includes('complet') || action.includes('success')) title = 'Rotation Completed'
    else if (action.includes('start')) title = 'Rotation Started'
    else if (action.includes('queue') || action.includes('pending')) title = 'Rotation Queued'
    else if (action.includes('cancel')) title = 'Rotation Cancelled'
    else if (action.includes('recover')) title = 'Rotation Recovered'
    else title = 'Rotation Event'
  }
  // Security events
  else if (action.includes('login') || action.includes('auth') || action.includes('password') || action.includes('lock') || action.includes('session')) {
    category = 'security'
    if (action.includes('fail') || action.includes('invalid')) title = 'Login Failure'
    else if (action.includes('lock')) title = 'Account Locked'
    else if (action.includes('password')) title = 'Password Reset'
    else if (action.includes('logout')) title = 'User Logged Out'
    else title = 'Security Event'
  }
  // Operations events
  else if (action.includes('dlq') || action.includes('worker') || action.includes('stale') || action.includes('unhealthy') || action.includes('drift')) {
    category = 'operations'
    if (action.includes('dlq')) title = 'DLQ Activity'
    else if (action.includes('worker')) title = 'Worker Status'
    else if (action.includes('stale')) title = 'Stale Work Detected'
    else if (action.includes('drift')) title = 'Configuration Drift'
    else title = 'Operations Alert'
  }
  else {
    // Only surface warning/error events as info category
    if (severity === 'info') return null
    category = 'info'
    title = 'System Event'
  }

  return {
    id: `${event.timestamp}-${Math.random().toString(36).slice(2, 7)}`,
    category,
    title,
    message: event.message,
    timestamp: event.timestamp,
    read: false,
    severity: severity === 'warning' ? 'warning' : severity === 'error' ? 'error' : 'info',
  }
}

function loadFromStorage(): Notification[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as Notification[]) : []
  } catch {
    return []
  }
}

function saveToStorage(notifications: Notification[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(notifications.slice(0, MAX_NOTIFICATIONS)))
  } catch {
    // quota exceeded — clear and retry
    localStorage.removeItem(STORAGE_KEY)
  }
}

function categoryIcon(cat: NotificationCategory, severity: Notification['severity']) {
  if (cat === 'security') return <Shield className="w-4 h-4 text-orange-500 flex-shrink-0" />
  if (cat === 'execution') return <Zap className="w-4 h-4 text-blue-500 flex-shrink-0" />
  if (severity === 'error') return <AlertCircle className="w-4 h-4 text-red-500 flex-shrink-0" />
  if (severity === 'warning') return <AlertTriangle className="w-4 h-4 text-yellow-500 flex-shrink-0" />
  if (severity === 'success') return <CheckCircle className="w-4 h-4 text-green-500 flex-shrink-0" />
  return <Info className="w-4 h-4 text-blue-400 flex-shrink-0" />
}

export function NotificationCenter() {
  const { events } = useWebSocketContext()
  const [notifications, setNotifications] = useState<Notification[]>([])
  const [open, setOpen] = useState(false)
  const seenEventTimestamps = useRef(new Set<string>())
  const panelRef = useRef<HTMLDivElement>(null)

  // Load from localStorage on mount
  useEffect(() => {
    const stored = loadFromStorage()
    setNotifications(stored)
    stored.forEach(n => seenEventTimestamps.current.add(n.timestamp + n.message))
  }, [])

  // Persist on change
  useEffect(() => {
    saveToStorage(notifications)
  }, [notifications])

  // FG8: convert incoming WebSocket events to notifications
  useEffect(() => {
    if (events.length === 0) return
    const latest = events[0]
    const key = latest.timestamp + latest.message
    if (seenEventTimestamps.current.has(key)) return
    seenEventTimestamps.current.add(key)

    const notification = eventToNotification(latest)
    if (!notification) return

    setNotifications(prev => [notification, ...prev].slice(0, MAX_NOTIFICATIONS))
  }, [events])

  // Close on outside click
  useEffect(() => {
    if (!open) return
    function handler(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const unreadCount = notifications.filter(n => !n.read).length

  const markAllRead = useCallback(() => {
    setNotifications(prev => prev.map(n => ({ ...n, read: true })))
  }, [])

  const markRead = useCallback((id: string) => {
    setNotifications(prev => prev.map(n => n.id === id ? { ...n, read: true } : n))
  }, [])

  const clearAll = useCallback(() => {
    setNotifications([])
  }, [])

  function formatTime(ts: string) {
    try {
      const d = new Date(ts)
      const now = new Date()
      const diffMs = now.getTime() - d.getTime()
      const diffMins = Math.floor(diffMs / 60000)
      if (diffMins < 1) return 'just now'
      if (diffMins < 60) return `${diffMins}m ago`
      const diffHrs = Math.floor(diffMins / 60)
      if (diffHrs < 24) return `${diffHrs}h ago`
      return d.toLocaleDateString()
    } catch {
      return ''
    }
  }

  return (
    <div className="relative" ref={panelRef}>
      <button
        onClick={() => setOpen(o => !o)}
        className="relative flex items-center justify-center h-8 w-8 rounded-md hover:bg-muted transition-colors"
        title="Notifications"
        aria-label={`Notifications${unreadCount > 0 ? `, ${unreadCount} unread` : ''}`}
      >
        <Bell className="h-4 w-4 text-muted-foreground" />
        {unreadCount > 0 && (
          <span className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 flex items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-bold px-0.5">
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-10 z-50 w-80 rounded-lg border border-border bg-card shadow-xl">
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border">
            <span className="text-sm font-semibold">Notifications</span>
            <div className="flex items-center gap-1">
              {unreadCount > 0 && (
                <button
                  onClick={markAllRead}
                  className="p-1 rounded hover:bg-muted text-xs text-muted-foreground flex items-center gap-1"
                  title="Mark all as read"
                >
                  <CheckCheck className="w-3.5 h-3.5" />
                </button>
              )}
              <button
                onClick={clearAll}
                className="p-1 rounded hover:bg-muted text-muted-foreground"
                title="Clear all"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
              <button
                onClick={() => setOpen(false)}
                className="p-1 rounded hover:bg-muted text-muted-foreground"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>

          {/* List */}
          <div className="max-h-96 overflow-y-auto">
            {notifications.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-10 text-center">
                <Bell className="w-8 h-8 text-muted-foreground/40 mb-2" />
                <p className="text-sm text-muted-foreground">No notifications</p>
              </div>
            ) : (
              <ul>
                {notifications.map(n => (
                  <li
                    key={n.id}
                    onClick={() => markRead(n.id)}
                    className={`flex items-start gap-3 px-4 py-3 border-b border-border/50 cursor-pointer hover:bg-muted/30 transition-colors ${!n.read ? 'bg-muted/20' : ''}`}
                  >
                    {categoryIcon(n.category, n.severity)}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center justify-between gap-2">
                        <p className={`text-xs font-semibold truncate ${!n.read ? 'text-foreground' : 'text-muted-foreground'}`}>
                          {n.title}
                        </p>
                        <span className="text-[10px] text-muted-foreground flex-shrink-0">{formatTime(n.timestamp)}</span>
                      </div>
                      <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{n.message}</p>
                    </div>
                    {!n.read && <div className="w-1.5 h-1.5 rounded-full bg-blue-500 flex-shrink-0 mt-1.5" />}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
