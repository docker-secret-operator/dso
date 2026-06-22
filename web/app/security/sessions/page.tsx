'use client'

import { useEffect, useState } from 'react'
import { apiFetch } from "@/lib/api-fetch"
import { AlertTriangle, Shield, Globe, Clock } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface Session {
  id: string
  username: string
  ip_address: string
  user_agent: string
  created_at: string
  last_activity: string
  expires_at: string
}

export default function SessionSecurityPage() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [anomalies, setAnomalies] = useState<Map<string, number>>(new Map())
  const router = useRouter()

  useEffect(() => {
    const fetchSessions = async () => {
      setLoading(true)
      try {
        const response = await apiFetch('/api/security/sessions')
        if (!response.ok) {
          if (response.status === 403) {
            router.push('/login')
            return
          }
          throw new Error('Failed to fetch sessions')
        }
        const data = await response.json()
        setSessions(data || [])

        // Detect session anomalies (same user from multiple IPs)
        const userIPs = new Map<string, Set<string>>()
        const userAnomalies = new Map<string, number>()

        ;(data || []).forEach((session: Session) => {
          if (!userIPs.has(session.username)) {
            userIPs.set(session.username, new Set())
          }
          userIPs.get(session.username)?.add(session.ip_address)
        })

        userIPs.forEach((ips, username) => {
          if (ips.size > 1) {
            userAnomalies.set(username, ips.size)
          }
        })

        setAnomalies(userAnomalies)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchSessions()
    const interval = setInterval(fetchSessions, 30000)
    return () => clearInterval(interval)
  }, [router])

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString()
  }

  const isExpired = (expiresAt: string) => {
    return new Date(expiresAt) < new Date()
  }

  const isStale = (lastActivity: string) => {
    const lastActivityTime = new Date(lastActivity).getTime()
    const now = new Date().getTime()
    const diffMinutes = (now - lastActivityTime) / (1000 * 60)
    return diffMinutes > 60 // stale if no activity for 1 hour
  }

  const hasAnomalies = (username: string) => {
    return anomalies.has(username) && anomalies.get(username)! > 1
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Session Security Console</h1>
        <Link href="/security" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 text-sm">
          ← Back
        </Link>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      {anomalies.size > 0 && (
        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-4 text-yellow-800">
          <div className="flex gap-2 mb-2">
            <AlertTriangle className="w-5 h-5 flex-shrink-0" />
            <div>
              <div className="font-semibold">Session Anomalies Detected</div>
              <div className="text-sm">{anomalies.size} users have active sessions from multiple IP addresses</div>
            </div>
          </div>
        </div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading active sessions...</div>
        ) : sessions.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No active sessions</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Username</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">IP Address</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">User Agent</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Created</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Last Activity</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Expires</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {sessions.map((session) => (
                  <tr
                    key={session.id}
                    className={`hover:bg-gray-50 ${
                      hasAnomalies(session.username) ? 'bg-yellow-50' : ''
                    }`}
                  >
                    <td className="px-6 py-4 text-sm font-medium text-gray-900">{session.username}</td>
                    <td className="px-6 py-4 text-sm text-gray-700 font-mono">{session.ip_address}</td>
                    <td className="px-6 py-4 text-sm text-gray-700 max-w-xs truncate" title={session.user_agent}>
                      {session.user_agent.split('/')[0]}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700 whitespace-nowrap">
                      {formatDate(session.created_at)}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700 whitespace-nowrap">
                      {formatDate(session.last_activity)}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700 whitespace-nowrap">
                      {formatDate(session.expires_at)}
                    </td>
                    <td className="px-6 py-4 text-sm">
                      <div className="flex gap-2 flex-wrap">
                        {isExpired(session.expires_at) && (
                          <span className="inline-block px-2 py-1 bg-gray-200 text-gray-800 text-xs rounded">
                            Expired
                          </span>
                        )}
                        {isStale(session.last_activity) && !isExpired(session.expires_at) && (
                          <span className="inline-block px-2 py-1 bg-yellow-200 text-yellow-800 text-xs rounded">
                            Stale
                          </span>
                        )}
                        {hasAnomalies(session.username) && (
                          <span className="inline-block px-2 py-1 bg-orange-200 text-orange-800 text-xs rounded flex items-center gap-1">
                            <AlertTriangle className="w-3 h-3" /> Multi-IP
                          </span>
                        )}
                        {!isExpired(session.expires_at) && !isStale(session.last_activity) && !hasAnomalies(session.username) && (
                          <span className="inline-block px-2 py-1 bg-green-200 text-green-800 text-xs rounded">
                            Active
                          </span>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold">Total Sessions</h3>
            <Shield className="w-5 h-5 text-blue-600" />
          </div>
          <div className="text-3xl font-bold">{sessions.length}</div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold">Session Anomalies</h3>
            <AlertTriangle className="w-5 h-5 text-orange-600" />
          </div>
          <div className="text-3xl font-bold">{anomalies.size}</div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-semibold">Stale Sessions</h3>
            <Clock className="w-5 h-5 text-gray-600" />
          </div>
          <div className="text-3xl font-bold">
            {sessions.filter((s) => isStale(s.last_activity) && !isExpired(s.expires_at)).length}
          </div>
        </div>
      </div>
    </div>
  )
}
