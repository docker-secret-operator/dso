'use client'

import { useEffect, useState, Suspense } from 'react'
import { apiFetch } from "@/lib/api-fetch"
import { Search, Filter, ChevronLeft, ChevronRight } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'

interface SecurityEvent {
  id: string
  type: string
  severity: string
  username: string
  ip_address: string
  message: string
  created_at: string
}

const EVENT_TYPES = [
  'LOGIN_SUCCESS',
  'LOGIN_FAILURE',
  'ACCOUNT_LOCKED',
  'ACCOUNT_UNLOCKED',
  'PASSWORD_CHANGED',
  'PASSWORD_RESET',
  'SESSION_CREATED',
  'SESSION_REVOKED',
  'SESSION_EXPIRED',
  'USER_DISABLED',
  'USER_ENABLED',
]

const SEVERITIES = ['low', 'medium', 'high', 'critical']

function getSeverityColor(severity: string) {
  const colors = {
    low: 'bg-blue-100 text-blue-800',
    medium: 'bg-yellow-100 text-yellow-800',
    high: 'bg-orange-100 text-orange-800',
    critical: 'bg-red-100 text-red-800',
  }
  return colors[severity as keyof typeof colors] || 'bg-gray-100 text-gray-800'
}

function SecurityEventsContent() {
  const [events, setEvents] = useState<SecurityEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const router = useRouter()
  const searchParams = useSearchParams()

  const [filters, setFilters] = useState({
    type: searchParams?.get('type') || '',
    severity: searchParams?.get('severity') || '',
    username: searchParams?.get('username') || '',
    ip_address: searchParams?.get('ip_address') || '',
    search: '',
  })

  useEffect(() => {
    const fetchEvents = async () => {
      setLoading(true)
      try {
        const params = new URLSearchParams()
        if (filters.type) params.append('type', filters.type)
        if (filters.severity) params.append('severity', filters.severity)
        if (filters.username) params.append('username', filters.username)
        if (filters.ip_address) params.append('ip_address', filters.ip_address)
        params.append('limit', pageSize.toString())
        params.append('offset', ((page - 1) * pageSize).toString())

        const response = await apiFetch(`/api/security/events?${params}`)
        if (!response.ok) {
          if (response.status === 403) {
            router.push('/login')
            return
          }
          throw new Error('Failed to fetch events')
        }
        const data = await response.json()
        setEvents(data || [])
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchEvents()
  }, [filters, page, pageSize, router])

  const handleFilterChange = (field: string, value: string) => {
    setFilters({ ...filters, [field]: value })
    setPage(1)
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Security Event Timeline</h1>
        <Link href="/security" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 text-sm">
          ← Back
        </Link>
      </div>

      <div className="bg-white border border-gray-200 rounded-lg p-6 space-y-4">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Filter className="w-5 h-5" />
          Filters
        </h2>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Event Type</label>
            <select
              value={filters.type}
              onChange={(e) => handleFilterChange('type', e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg"
            >
              <option value="">All Types</option>
              {EVENT_TYPES.map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Severity</label>
            <select
              value={filters.severity}
              onChange={(e) => handleFilterChange('severity', e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg"
            >
              <option value="">All Severities</option>
              {SEVERITIES.map((sev) => (
                <option key={sev} value={sev}>
                  {sev.charAt(0).toUpperCase() + sev.slice(1)}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Username</label>
            <input
              type="text"
              placeholder="Search username..."
              value={filters.username}
              onChange={(e) => handleFilterChange('username', e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">IP Address</label>
            <input
              type="text"
              placeholder="Search IP..."
              value={filters.ip_address}
              onChange={(e) => handleFilterChange('ip_address', e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg"
            />
          </div>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading events...</div>
        ) : events.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No events found</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Type</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Severity</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Username</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">IP Address</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Message</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Time</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {events.map((event) => (
                  <tr key={event.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 text-sm font-medium text-gray-900">{event.type}</td>
                    <td className="px-6 py-4 text-sm">
                      <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${getSeverityColor(event.severity)}`}>
                        {event.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700">{event.username}</td>
                    <td className="px-6 py-4 text-sm text-gray-700 font-mono">{event.ip_address}</td>
                    <td className="px-6 py-4 text-sm text-gray-700 max-w-xs truncate">{event.message}</td>
                    <td className="px-6 py-4 text-sm text-gray-700 whitespace-nowrap">{formatDate(event.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex items-center justify-between px-6 py-4 bg-gray-50 border-t border-gray-200">
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
              disabled={events.length < pageSize}
              className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
            >
              <ChevronRight className="w-5 h-5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default function SecurityEventsPage() {
  return (
    <Suspense fallback={<div className="p-8 text-center">Loading...</div>}>
      <SecurityEventsContent />
    </Suspense>
  )
}
