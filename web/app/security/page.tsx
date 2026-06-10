'use client'

import { useEffect, useState } from 'react'
import { AlertTriangle, Shield, Users, Lock, Zap, TrendingUp, TrendingDown } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface SecurityOverview {
  active_sessions: number
  locked_accounts: number
  disabled_users: number
  failed_logins_24h: number
  successful_logins_24h: number
  password_resets_24h: number
  active_admins: number
  suspicious_activities: number
  trends: Record<string, string>
}

interface StatCard {
  title: string
  value: number
  icon: React.ReactNode
  trend?: string
  color: string
  href: string
}

export default function SecurityDashboard() {
  const [overview, setOverview] = useState<SecurityOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    const fetchOverview = async () => {
      try {
        const response = await fetch('/api/security/overview')
        if (!response.ok) {
          if (response.status === 403) {
            router.push('/login')
            return
          }
          throw new Error('Failed to fetch security overview')
        }
        const data = await response.json()
        setOverview(data)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchOverview()
    const interval = setInterval(fetchOverview, 30000)
    return () => clearInterval(interval)
  }, [router])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="animate-spin">
          <Shield className="w-8 h-8 text-blue-500" />
        </div>
      </div>
    )
  }

  if (error || !overview) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">
        {error || 'Failed to load security overview'}
      </div>
    )
  }

  const getTrendIcon = (trend?: string) => {
    if (trend === '↑') return <TrendingUp className="w-4 h-4 text-red-500" />
    if (trend === '↓') return <TrendingDown className="w-4 h-4 text-green-500" />
    return <TrendingUp className="w-4 h-4 text-gray-300" />
  }

  const stats: StatCard[] = [
    {
      title: 'Failed Logins (24h)',
      value: overview.failed_logins_24h,
      icon: <AlertTriangle className="w-6 h-6" />,
      trend: overview.trends?.failed_logins,
      color: 'bg-red-50 border-red-200',
      href: '/security/events?type=LOGIN_FAILURE',
    },
    {
      title: 'Locked Accounts',
      value: overview.locked_accounts,
      icon: <Lock className="w-6 h-6" />,
      color: 'bg-orange-50 border-orange-200',
      href: '/users',
    },
    {
      title: 'Active Sessions',
      value: overview.active_sessions,
      icon: <Zap className="w-6 h-6" />,
      color: 'bg-blue-50 border-blue-200',
      href: '/security/sessions',
    },
    {
      title: 'Password Resets (24h)',
      value: overview.password_resets_24h,
      icon: <Shield className="w-6 h-6" />,
      color: 'bg-purple-50 border-purple-200',
      href: '/security/events?type=PASSWORD_RESET',
    },
    {
      title: 'Suspicious Activities',
      value: overview.suspicious_activities,
      icon: <AlertTriangle className="w-6 h-6" />,
      trend: overview.trends?.suspicious_activities,
      color: 'bg-yellow-50 border-yellow-200',
      href: '/security/suspicious',
    },
    {
      title: 'Disabled Users',
      value: overview.disabled_users,
      icon: <Users className="w-6 h-6" />,
      color: 'bg-gray-50 border-gray-200',
      href: '/users',
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Security Operations Console</h1>
        <div className="flex gap-2">
          <Link href="/security/events" className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm">
            View Events
          </Link>
          <Link href="/security/alerts" className="px-4 py-2 bg-orange-600 text-white rounded-lg hover:bg-orange-700 text-sm">
            View Alerts
          </Link>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {stats.map((stat) => (
          <Link key={stat.title} href={stat.href}>
            <div className={`border rounded-lg p-6 cursor-pointer hover:shadow-lg transition-shadow ${stat.color}`}>
              <div className="flex items-start justify-between mb-4">
                <div className="text-gray-600">{stat.icon}</div>
                {stat.trend && getTrendIcon(stat.trend)}
              </div>
              <div className="text-2xl font-bold text-gray-900">{stat.value}</div>
              <p className="text-sm text-gray-600 mt-2">{stat.title}</p>
            </div>
          </Link>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <h2 className="text-xl font-bold mb-4">Quick Actions</h2>
          <div className="space-y-2">
            <Link href="/security/events" className="block px-4 py-2 hover:bg-gray-50 rounded text-blue-600 hover:underline">
              → Security Event Timeline
            </Link>
            <Link href="/security/suspicious" className="block px-4 py-2 hover:bg-gray-50 rounded text-blue-600 hover:underline">
              → Suspicious Activity Panel
            </Link>
            <Link href="/security/sessions" className="block px-4 py-2 hover:bg-gray-50 rounded text-blue-600 hover:underline">
              → Session Security Console
            </Link>
          </div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <h2 className="text-xl font-bold mb-4">Status</h2>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-gray-700">Security Events</span>
              <span className="inline-block w-3 h-3 bg-green-500 rounded-full"></span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-gray-700">Detection System</span>
              <span className="inline-block w-3 h-3 bg-green-500 rounded-full"></span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-gray-700">Alert System</span>
              <span className="inline-block w-3 h-3 bg-green-500 rounded-full"></span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
