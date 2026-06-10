'use client'

import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle, X, ChevronLeft, ChevronRight } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface SuspiciousActivity {
  id: string
  type: string
  severity: string
  ip_address?: string
  usernames?: string
  first_seen: string
  last_seen: string
  occurrence_count: number
  message: string
  acknowledged_by?: string
  acknowledged_at?: string
}

function getSeverityColor(severity: string) {
  const colors = {
    low: 'bg-blue-100 text-blue-800 border-blue-300',
    medium: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    high: 'bg-orange-100 text-orange-800 border-orange-300',
    critical: 'bg-red-100 text-red-800 border-red-300',
  }
  return colors[severity as keyof typeof colors] || 'bg-gray-100 text-gray-800'
}

export default function SuspiciousActivityPage() {
  const [activities, setActivities] = useState<SuspiciousActivity[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [acknowledging, setAcknowledging] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    const fetchActivities = async () => {
      setLoading(true)
      try {
        const offset = (page - 1) * pageSize
        const response = await fetch(`/api/security/suspicious?limit=${pageSize}&offset=${offset}`)
        if (!response.ok) {
          if (response.status === 403) {
            router.push('/login')
            return
          }
          throw new Error('Failed to fetch suspicious activities')
        }
        const data = await response.json()
        setActivities(data || [])
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchActivities()
  }, [page, pageSize, router])

  const handleAcknowledge = async (activityID: string) => {
    setAcknowledging(activityID)
    try {
      const response = await fetch(`/api/security/suspicious/${activityID}/acknowledge`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      })
      if (!response.ok) {
        throw new Error('Failed to acknowledge activity')
      }
      setActivities(activities.filter((a) => a.id !== activityID))
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to acknowledge')
    } finally {
      setAcknowledging(null)
    }
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString()
  }

  const parseUsernames = (usernamesStr?: string) => {
    if (!usernamesStr) return []
    try {
      return JSON.parse(usernamesStr)
    } catch {
      return []
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Suspicious Activity Panel</h1>
        <Link href="/security" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 text-sm">
          ← Back
        </Link>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      <div className="space-y-4">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading suspicious activities...</div>
        ) : activities.length === 0 ? (
          <div className="bg-green-50 border border-green-200 rounded-lg p-6 text-center">
            <CheckCircle className="w-8 h-8 text-green-600 mx-auto mb-2" />
            <p className="text-green-800 font-medium">No unacknowledged suspicious activities detected</p>
          </div>
        ) : (
          activities.map((activity) => (
            <div key={activity.id} className={`border rounded-lg p-6 ${getSeverityColor(activity.severity)}`}>
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-start gap-4 flex-1">
                  <AlertTriangle className="w-6 h-6 flex-shrink-0 mt-1" />
                  <div className="flex-1">
                    <h3 className="font-bold text-lg capitalize">{activity.type.replace(/_/g, ' ')}</h3>
                    <p className="text-sm opacity-75 mt-1">{activity.message}</p>
                  </div>
                </div>
                <button
                  onClick={() => handleAcknowledge(activity.id)}
                  disabled={acknowledging === activity.id}
                  className="px-4 py-2 bg-white bg-opacity-70 hover:bg-opacity-100 rounded text-sm font-medium disabled:opacity-50"
                >
                  {acknowledging === activity.id ? 'Acknowledging...' : 'Acknowledge'}
                </button>
              </div>

              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mt-4 text-sm">
                {activity.ip_address && (
                  <div>
                    <div className="font-medium">IP Address</div>
                    <div className="text-xs font-mono opacity-75">{activity.ip_address}</div>
                  </div>
                )}
                <div>
                  <div className="font-medium">Occurrence Count</div>
                  <div className="text-xs opacity-75">{activity.occurrence_count}</div>
                </div>
                <div>
                  <div className="font-medium">First Seen</div>
                  <div className="text-xs opacity-75">{formatDate(activity.first_seen)}</div>
                </div>
                <div>
                  <div className="font-medium">Last Seen</div>
                  <div className="text-xs opacity-75">{formatDate(activity.last_seen)}</div>
                </div>
              </div>

              {activity.usernames && parseUsernames(activity.usernames).length > 0 && (
                <div className="mt-4 pt-4 border-t border-current border-opacity-20">
                  <div className="font-medium text-sm mb-2">Affected Users:</div>
                  <div className="flex flex-wrap gap-2">
                    {parseUsernames(activity.usernames).map((username: string) => (
                      <span key={username} className="px-2 py-1 bg-white bg-opacity-30 rounded text-xs font-mono">
                        {username}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {activity.acknowledged_by && activity.acknowledged_at && (
                <div className="mt-4 pt-4 border-t border-current border-opacity-20 text-xs opacity-75">
                  <CheckCircle className="w-4 h-4 inline-block mr-1" />
                  Acknowledged by {activity.acknowledged_by} at {formatDate(activity.acknowledged_at)}
                </div>
              )}
            </div>
          ))
        )}
      </div>

      {!loading && activities.length > 0 && (
        <div className="flex items-center justify-between px-6 py-4 bg-gray-50 border border-gray-200 rounded-lg">
          <div className="text-sm text-gray-600">
            Page <span className="font-medium">{page}</span>
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => setPage(Math.max(1, page - 1))}
              disabled={page === 1}
              className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
            >
              <ChevronLeft className="w-5 h-5" />
            </button>
            <button
              onClick={() => setPage(page + 1)}
              disabled={activities.length < pageSize}
              className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
            >
              <ChevronRight className="w-5 h-5" />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
