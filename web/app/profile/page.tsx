'use client'

import { useState, useEffect } from 'react'
import { Save, User, Shield, Clock, AlertCircle, CheckCircle } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'

interface UserProfile {
  id: string
  username: string
  email: string
  full_name: string
  avatar_url?: string
  role: string
  status: string
  created_at: string
  last_login: string
  mfa_enabled: boolean
  password_changed_at: string
}

interface ProfileFormData {
  full_name: string
  email: string
  avatar_url: string
}

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token
    ? { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }
    : { 'Content-Type': 'application/json' }
}

export default function ProfilePage() {
  const { user: authUser } = useAuth()
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [formData, setFormData] = useState<ProfileFormData>({
    full_name: '',
    email: '',
    avatar_url: '',
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const [editMode, setEditMode] = useState(false)

  useEffect(() => {
    const fetchProfile = async () => {
      try {
        setLoading(true)
        const response = await fetch('/api/user/profile', {
          headers: getAuthHeaders(),
        })

        if (!response.ok) {
          // Fallback to auth context data when profile endpoint unavailable
          const fallback: UserProfile = {
            id: authUser?.id || '',
            username: authUser?.username || '',
            email: '',
            full_name: authUser?.display_name || authUser?.username || '',
            role: authUser?.role || 'user',
            status: 'active',
            created_at: new Date().toISOString(),
            last_login: new Date().toISOString(),
            mfa_enabled: false,
            password_changed_at: new Date().toISOString(),
          }
          setProfile(fallback)
          setFormData({ full_name: fallback.full_name, email: fallback.email, avatar_url: '' })
          return
        }

        const data = await response.json()
        setProfile(data)
        setFormData({
          full_name: data.full_name || '',
          email: data.email || '',
          avatar_url: data.avatar_url || '',
        })
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchProfile()
  }, [authUser])

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value } = e.target
    setFormData(prev => ({ ...prev, [name]: value }))
  }

  const handleSaveProfile = async () => {
    try {
      setSaving(true)
      setError(null)

      const response = await fetch('/api/user/profile', {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(formData),
      })

      if (!response.ok) {
        throw new Error('Failed to save profile')
      }

      const updatedProfile = await response.json()
      setProfile(updatedProfile)
      setSuccess(true)
      setEditMode(false)
      setTimeout(() => setSuccess(false), 3000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save profile')
    } finally {
      setSaving(false)
    }
  }

  const formatDate = (dateString: string) => {
    if (!dateString) return '—'
    try {
      return new Date(dateString).toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return '—'
    }
  }

  if (loading) {
    return <div className="p-8 text-slate-200">Loading profile…</div>
  }

  const initials = (profile?.full_name || profile?.username || 'U')
    .split(' ')
    .map(n => n[0])
    .join('')
    .toUpperCase()
    .slice(0, 2)

  return (
    <div className="space-y-6 p-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-semibold text-slate-100">My Profile</h1>
        <p className="mt-1 text-sm text-slate-400">Manage your account information and settings</p>
      </div>

      {/* Messages */}
      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-red-300 flex items-center gap-3">
          <AlertCircle className="h-5 w-5 flex-shrink-0" />
          {error}
        </div>
      )}

      {success && (
        <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-4 text-emerald-300 flex items-center gap-3">
          <CheckCircle className="h-5 w-5 flex-shrink-0" />
          Profile updated successfully
        </div>
      )}

      {profile && (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* Avatar and Status */}
          <div className="lg:col-span-1">
            <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-6 space-y-4">
              {/* Avatar */}
              <div className="flex justify-center">
                <div className="h-24 w-24 rounded-full bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center text-white text-3xl font-bold shadow-lg">
                  {initials}
                </div>
              </div>

              {/* Basic Info */}
              <div className="text-center">
                <h2 className="text-lg font-semibold text-slate-100">{profile.full_name || profile.username}</h2>
                {profile.email && <p className="mt-0.5 text-sm text-slate-400">{profile.email}</p>}
              </div>

              {/* Role Badge */}
              <div className="flex justify-center">
                <span className="inline-flex items-center rounded-full bg-indigo-500/15 px-3 py-1 text-sm font-medium text-indigo-400">
                  <Shield className="h-3.5 w-3.5 mr-1.5" />
                  {profile.role.charAt(0).toUpperCase() + profile.role.slice(1)}
                </span>
              </div>

              {/* Status */}
              <div className="pt-4 border-t border-white/[0.06]">
                <div className="flex items-center justify-center gap-2">
                  <div className={`h-2 w-2 rounded-full ${profile.status === 'active' ? 'bg-emerald-400' : 'bg-slate-600'}`} />
                  <span className="text-sm text-slate-400">
                    {profile.status === 'active' ? 'Active' : 'Inactive'}
                  </span>
                </div>
              </div>

              {/* Account Links */}
              <div className="pt-4 border-t border-white/[0.06] space-y-2">
                <a
                  href="/settings/password"
                  className="block w-full rounded-lg border border-white/[0.09] px-4 py-2 text-center text-sm text-slate-300 hover:bg-white/5 hover:text-white transition-colors"
                >
                  Change Password
                </a>
                <a
                  href="/settings/sessions"
                  className="block w-full rounded-lg border border-white/[0.09] px-4 py-2 text-center text-sm text-slate-300 hover:bg-white/5 hover:text-white transition-colors"
                >
                  Manage Sessions
                </a>
              </div>
            </div>
          </div>

          {/* Profile Information */}
          <div className="lg:col-span-2 space-y-5">
            {/* Edit Profile Section */}
            <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-6">
              <div className="flex items-center justify-between mb-5">
                <h3 className="text-sm font-semibold text-slate-300">Profile Information</h3>
                <button
                  onClick={() => setEditMode(!editMode)}
                  className="rounded-lg border border-white/[0.09] px-3 py-1.5 text-sm text-slate-300 hover:bg-white/5 hover:text-white transition-colors"
                >
                  {editMode ? 'Cancel' : 'Edit'}
                </button>
              </div>

              <div className="space-y-4">
                <div>
                  <label htmlFor="full_name" className="block text-sm font-medium text-slate-400 mb-1.5">
                    Full Name
                  </label>
                  <input
                    type="text"
                    id="full_name"
                    name="full_name"
                    value={formData.full_name}
                    onChange={handleInputChange}
                    disabled={!editMode}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#1a1d24] px-3 py-2.5 text-sm text-slate-200 disabled:opacity-50 disabled:text-slate-500 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30 transition-all"
                    placeholder="Enter your full name"
                  />
                </div>

                <div>
                  <label htmlFor="email" className="block text-sm font-medium text-slate-400 mb-1.5">
                    Email Address
                  </label>
                  <input
                    type="email"
                    id="email"
                    name="email"
                    value={formData.email}
                    onChange={handleInputChange}
                    disabled={!editMode}
                    className="w-full rounded-lg border border-white/[0.09] bg-[#1a1d24] px-3 py-2.5 text-sm text-slate-200 disabled:opacity-50 disabled:text-slate-500 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30 transition-all"
                    placeholder="Enter your email"
                  />
                </div>

                {editMode && (
                  <button
                    onClick={handleSaveProfile}
                    disabled={saving}
                    className="w-full rounded-lg bg-indigo-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50 flex items-center justify-center gap-2 transition-colors"
                  >
                    <Save className="h-4 w-4" />
                    {saving ? 'Saving…' : 'Save Changes'}
                  </button>
                )}
              </div>
            </div>

            {/* Security & Activity */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-5">
                <h3 className="text-sm font-semibold text-slate-300 mb-4 flex items-center gap-2">
                  <Shield className="h-4 w-4 text-slate-500" />
                  Security
                </h3>
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-sm text-slate-400">Two-Factor Auth</span>
                    <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                      profile.mfa_enabled
                        ? 'bg-emerald-500/15 text-emerald-400'
                        : 'bg-slate-700/30 text-slate-500'
                    }`}>
                      {profile.mfa_enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  <button
                    disabled
                    className="w-full rounded-lg border border-white/[0.09] px-4 py-2 text-sm text-slate-500 cursor-not-allowed opacity-50"
                    title="MFA configuration coming soon"
                  >
                    Manage 2FA
                  </button>
                </div>
              </div>

              <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-5">
                <h3 className="text-sm font-semibold text-slate-300 mb-4 flex items-center gap-2">
                  <Clock className="h-4 w-4 text-slate-500" />
                  Activity
                </h3>
                <div className="space-y-3 text-sm">
                  <div>
                    <p className="text-slate-500 text-xs mb-0.5">Last Login</p>
                    <p className="text-slate-300 font-medium text-xs">{formatDate(profile.last_login)}</p>
                  </div>
                  <div>
                    <p className="text-slate-500 text-xs mb-0.5">Account Created</p>
                    <p className="text-slate-300 font-medium text-xs">{formatDate(profile.created_at)}</p>
                  </div>
                </div>
              </div>
            </div>

            {/* Account Metadata */}
            <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-5">
              <h3 className="text-sm font-semibold text-slate-300 mb-4 flex items-center gap-2">
                <User className="h-4 w-4 text-slate-500" />
                Account Information
              </h3>
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">User ID</p>
                  <p className="font-mono text-slate-300 text-xs break-all">{profile.id || '—'}</p>
                </div>
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">Username</p>
                  <p className="text-slate-300 font-mono text-xs">{profile.username}</p>
                </div>
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">Role</p>
                  <p className="text-slate-300 capitalize text-xs">{profile.role}</p>
                </div>
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">Status</p>
                  <p className="text-slate-300 capitalize text-xs">{profile.status}</p>
                </div>
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">Password Changed</p>
                  <p className="text-slate-300 text-xs">{formatDate(profile.password_changed_at)}</p>
                </div>
                <div>
                  <p className="text-slate-500 text-xs mb-0.5">Account Age</p>
                  <p className="text-slate-300 text-xs">
                    {profile.created_at
                      ? `${Math.floor((Date.now() - new Date(profile.created_at).getTime()) / (1000 * 60 * 60 * 24))} days`
                      : '—'}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
