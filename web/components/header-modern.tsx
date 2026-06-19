'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useAuth } from '@/contexts/AuthContext'
import {
  Search,
  Bell,
  Settings,
  LogOut,
  User,
  MoreVertical,
} from 'lucide-react'

export function Header() {
  const { user, logout } = useAuth()
  const [showMenu, setShowMenu] = useState(false)

  const getInitials = (name?: string) => {
    if (!name) return 'U'
    return name
      .split(' ')
      .map(n => n[0])
      .join('')
      .toUpperCase()
      .slice(0, 2)
  }

  return (
    <header className="sticky top-0 z-40 border-b border-slate-200 bg-white/80 backdrop-blur-xl">
      <div className="h-16 px-8 flex items-center justify-between gap-6">
        {/* Left: Search */}
        <div className="flex-1 max-w-md">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <input
              type="text"
              placeholder="Search operations..."
              className="w-full pl-10 pr-4 py-2 rounded-lg border border-slate-200 bg-white text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:border-coral-600 focus:ring-2 focus:ring-coral-500/20"
            />
          </div>
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-4">
          {/* Notifications */}
          <button className="relative p-2 hover:bg-slate-100 rounded-lg transition-colors">
            <Bell className="w-5 h-5 text-slate-600" />
            <span className="absolute top-1 right-1 w-2 h-2 bg-red-500 rounded-full" />
          </button>

          {/* Settings */}
          <Link href="/settings">
            <button className="p-2 hover:bg-slate-100 rounded-lg transition-colors">
              <Settings className="w-5 h-5 text-slate-600" />
            </button>
          </Link>

          {/* Profile Menu */}
          <div className="relative">
            <button
              onClick={() => setShowMenu(!showMenu)}
              className="flex items-center gap-2 px-3 py-2 hover:bg-slate-100 rounded-lg transition-colors"
            >
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-br from-coral-600 to-coral-500 text-white text-xs font-bold">
                {getInitials(user?.display_name)}
              </div>
              <span className="text-sm font-medium text-slate-700 hidden sm:inline">
                {user?.display_name || 'User'}
              </span>
              <MoreVertical className="w-4 h-4 text-slate-400" />
            </button>

            {/* Dropdown Menu */}
            {showMenu && (
              <div className="absolute right-0 mt-2 w-48 rounded-lg border border-slate-200 bg-white shadow-lg z-50">
                <Link href="/profile">
                  <button className="w-full flex items-center gap-3 px-4 py-3 text-sm text-slate-700 hover:bg-slate-50 transition-colors first:rounded-t-lg">
                    <User className="w-4 h-4" />
                    My Profile
                  </button>
                </Link>
                <button
                  onClick={logout}
                  className="w-full flex items-center gap-3 px-4 py-3 text-sm text-red-600 hover:bg-red-50 transition-colors last:rounded-b-lg border-t border-slate-200"
                >
                  <LogOut className="w-4 h-4" />
                  Sign Out
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </header>
  )
}
