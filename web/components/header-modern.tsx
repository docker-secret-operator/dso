'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useAuth } from '@/contexts/AuthContext'
import { Logo } from '@/components/Logo'
import {
  Search,
  Bell,
  Settings,
  LogOut,
  User,
  MoreVertical,
} from 'lucide-react'
import { cn } from '@/lib/utils'

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
    <header className="sticky top-0 z-40 border-b border-[rgba(255,255,255,0.08)] bg-[#0B1020]/95 backdrop-blur-xl">
      <div className="h-16 px-6 flex items-center justify-between gap-6">
        {/* Left: Logo */}
        <Logo href="/" size="sm" showText={false} className="flex-shrink-0" />

        {/* Center: Search */}
        <div className="flex-1 max-w-md">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[#6B7280]" />
            <input
              type="text"
              placeholder="Search operations..."
              className="w-full pl-10 pr-4 py-2 rounded-lg border border-[rgba(255,255,255,0.08)] bg-[#111827] text-sm text-[#F9FAFB] placeholder-[#6B7280] focus:outline-none focus:border-[#22D3EE] focus:ring-2 focus:ring-[#22D3EE]/20 transition-colors"
            />
          </div>
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-2">
          {/* Notifications */}
          <button className="relative p-2 hover:bg-[#111827] rounded-lg transition-colors">
            <Bell className="w-5 h-5 text-[#9CA3AF]" />
            <span className="absolute top-1 right-1 w-2 h-2 bg-red-500 rounded-full" />
          </button>

          {/* Settings */}
          <Link href="/settings">
            <button className="p-2 hover:bg-[#111827] rounded-lg transition-colors">
              <Settings className="w-5 h-5 text-[#9CA3AF]" />
            </button>
          </Link>

          {/* Profile Menu */}
          <div className="relative">
            <button
              onClick={() => setShowMenu(!showMenu)}
              className="flex items-center gap-2 px-3 py-2 hover:bg-[#111827] rounded-lg transition-colors"
            >
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-gradient-to-br from-[#22D3EE] to-[#0EA5E9] text-[#0B1020] text-xs font-bold">
                {getInitials(user?.display_name)}
              </div>
              <span className="text-sm font-medium text-[#F9FAFB] hidden sm:inline">
                {user?.display_name || 'User'}
              </span>
              <MoreVertical className="w-4 h-4 text-[#6B7280]" />
            </button>

            {/* Dropdown Menu */}
            {showMenu && (
              <div className="absolute right-0 mt-2 w-48 rounded-lg border border-[rgba(255,255,255,0.08)] bg-[#111827] shadow-xl z-50">
                <Link href="/profile">
                  <button className="w-full flex items-center gap-3 px-4 py-3 text-sm text-[#F9FAFB] hover:bg-[#1A2235] transition-colors first:rounded-t-lg">
                    <User className="w-4 h-4" />
                    My Profile
                  </button>
                </Link>
                <button
                  onClick={logout}
                  className="w-full flex items-center gap-3 px-4 py-3 text-sm text-[#EF4444] hover:bg-[#1A2235] transition-colors last:rounded-b-lg border-t border-[rgba(255,255,255,0.08)]"
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
