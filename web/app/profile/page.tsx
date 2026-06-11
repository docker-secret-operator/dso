'use client'

import { useState, useEffect } from 'react'
import { Save, Mail, User, Shield, Clock, AlertCircle, CheckCircle } from 'lucide-react'
import { useAuth } from '@/lib/auth-context'

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
        const response = await fetch('/api/user/profile')

        if (!response.ok) {
          throw new Error('Failed to fetch profile')
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
  }, [])

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value } = e.target
    setFormData(prev => ({
      ...prev,
      [name]: value,
    }))
  }

  const handleSaveProfile = async () => {
    try {
      setSaving(true)
      setError(null)

      const response = await fetch('/api/user/profile', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
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
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  if (loading) {
    return <div className="p-8">Loading profile...</div>
  }

  return (
    <div className="space-y-8 p-8">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold text-gray-900">My Profile</h1>
        <p className="mt-2 text-gray-600">Manage your account information and settings</p>
      </div>

      {/* Messages */}
      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800 flex items-center gap-3">
          <AlertCircle className="h-5 w-5" />
          {error}
        </div>
      )}

      {success && (
        <div className="rounded-lg border border-green-200 bg-green-50 p-4 text-green-800 flex items-center gap-3">
          <CheckCircle className="h-5 w-5" />
          Profile updated successfully
        </div>
      )}

      {/* Main Profile Card */}
      {profile && (
        <div className="grid grid-cols-1 gap-8 lg:grid-cols-3">
          {/* Avatar and Status */}
          <div className="lg:col-span-1">
            <div className="rounded-lg border border-gray-200 bg-white p-6 space-y-4">
              {/* Avatar */}
              <div className="flex justify-center">
                <div className="h-32 w-32 rounded-full bg-gradient-to-br from-blue-400 to-purple-500 flex items-center justify-center text-white text-4xl font-bold">
                  {profile.full_name?.charAt(0).toUpperCase() || profile.username?.charAt(0).toUpperCase()}
                </div>
              </div>

              {/* Basic Info */}
              <div className="text-center">
                <h2 className="text-2xl font-bold text-gray-900">{profile.full_name || profile.username}</h2>
                <p className="mt-1 text-sm text-gray-600">{profile.email}</p>
              </div>

              {/* Role Badge */}
              <div className="flex justify-center">
                <span className="inline-flex items-center rounded-full bg-blue-100 px-3 py-1 text-sm font-medium text-blue-800">
                  <Shield className="h-4 w-4 mr-1" />
                  {profile.role.charAt(0).toUpperCase() + profile.role.slice(1)}
                </span>
              </div>

              {/* Status */}
              <div className="pt-4 border-t border-gray-200">
                <div className="flex items-center justify-center gap-2">
                  <div className={`h-3 w-3 rounded-full ${
                    profile.status === 'active' ? 'bg-green-500' : 'bg-gray-400'
                  }`} />
                  <span className="text-sm font-medium text-gray-700">
                    {profile.status === 'active' ? 'Active' : 'Inactive'}
                  </span>
                </div>
              </div>

              {/* Account Links */}
              <div className="pt-4 border-t border-gray-200 space-y-2">
                <a href="/settings/password" className="block w-full rounded-lg bg-gray-100 px-4 py-2 text-center text-sm font-medium text-gray-700 hover:bg-gray-200">
                  Change Password
                </a>
                <a href="/settings/sessions" className="block w-full rounded-lg bg-gray-100 px-4 py-2 text-center text-sm font-medium text-gray-700 hover:bg-gray-200">
                  Manage Sessions
                </a>
              </div>
            </div>
          </div>

          {/* Profile Information */}
          <div className="lg:col-span-2 space-y-6">
            {/* Edit Profile Section */}
            <div className="rounded-lg border border-gray-200 bg-white p-6">
              <div className="flex items-center justify-between mb-6">
                <h3 className="text-lg font-semibold text-gray-900">Profile Information</h3>
                <button
                  onClick={() => setEditMode(!editMode)}
                  className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
                >
                  {editMode ? 'Cancel' : 'Edit'}
                </button>
              </div>

              <div className="space-y-4">
                {/* Full Name */}
                <div>
                  <label htmlFor="full_name" className="block text-sm font-medium text-gray-700 mb-2">
                    Full Name
                  </label>
                  <input
                    type="text"
                    id="full_name"
                    name="full_name"
                    value={formData.full_name}
                    onChange={handleInputChange}
                    disabled={!editMode}
                    className="w-full rounded-lg border border-gray-300 px-4 py-2 text-gray-900 disabled:bg-gray-50 disabled:text-gray-500"
                    placeholder="Enter your full name"
                  />
                </div>

                {/* Email */}
                <div>
                  <label htmlFor="email" className="block text-sm font-medium text-gray-700 mb-2">
                    Email Address
                  </label>
                  <input
                    type="email"
                    id="email"
                    name="email"
                    value={formData.email}
                    onChange={handleInputChange}
                    disabled={!editMode}
                    className="w-full rounded-lg border border-gray-300 px-4 py-2 text-gray-900 disabled:bg-gray-50 disabled:text-gray-500"
                    placeholder="Enter your email"
                  />
                </div>

                {/* Avatar URL */}
                <div>
                  <label htmlFor="avatar_url" className="block text-sm font-medium text-gray-700 mb-2">
                    Avatar URL
                  </label>
                  <input
                    type="url"
                    id="avatar_url"
                    name="avatar_url"
                    value={formData.avatar_url}
                    onChange={handleInputChange}
                    disabled={!editMode}
                    className="w-full rounded-lg border border-gray-300 px-4 py-2 text-gray-900 disabled:bg-gray-50 disabled:text-gray-500"
                    placeholder="https://example.com/avatar.jpg"
                  />
                </div>

                {/* Save Button */}
                {editMode && (
                  <button
                    onClick={handleSaveProfile}
                    disabled={saving}
                    className="w-full rounded-lg bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 disabled:bg-gray-400 flex items-center justify-center gap-2"
                  >
                    <Save className="h-4 w-4" />
                    {saving ? 'Saving...' : 'Save Changes'}
                  </button>
                )}
              </div>
            </div>

            {/* Security & Activity */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* Security Status */}
              <div className="rounded-lg border border-gray-200 bg-white p-6">
                <h3 className="text-lg font-semibold text-gray-900 mb-4 flex items-center gap-2">
                  <Shield className="h-5 w-5" />
                  Security
                </h3>
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-sm text-gray-600">Two-Factor Auth</span>
                    <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                      profile.mfa_enabled
                        ? 'bg-green-100 text-green-800'
                        : 'bg-gray-100 text-gray-800'
                    }`}>
                      {profile.mfa_enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  <button className="w-full rounded-lg bg-gray-100 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200">
                    Manage 2FA
                  </button>
                </div>
              </div>

              {/* Account Activity */}
              <div className="rounded-lg border border-gray-200 bg-white p-6">
                <h3 className="text-lg font-semibold text-gray-900 mb-4 flex items-center gap-2">
                  <Clock className="h-5 w-5" />
                  Activity
                </h3>
                <div className="space-y-3 text-sm">
                  <div>
                    <p className="text-gray-600">Last Login</p>
                    <p className="text-gray-900 font-medium">{formatDate(profile.last_login)}</p>
                  </div>
                  <div>
                    <p className="text-gray-600">Account Created</p>
                    <p className="text-gray-900 font-medium">{formatDate(profile.created_at)}</p>
                  </div>
                </div>
              </div>
            </div>

            {/* Account Metadata */}
            <div className="rounded-lg border border-gray-200 bg-white p-6">
              <h3 className="text-lg font-semibold text-gray-900 mb-4">Account Information</h3>
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-gray-600">User ID</p>
                  <p className="font-mono text-gray-900 break-all">{profile.id}</p>
                </div>
                <div>
                  <p className="text-gray-600">Username</p>
                  <p className="text-gray-900">{profile.username}</p>
                </div>
                <div>
                  <p className="text-gray-600">Role</p>
                  <p className="text-gray-900 capitalize">{profile.role}</p>
                </div>
                <div>
                  <p className="text-gray-600">Status</p>
                  <p className="text-gray-900 capitalize">{profile.status}</p>
                </div>
                <div>
                  <p className="text-gray-600">Password Changed</p>
                  <p className="text-gray-900">{formatDate(profile.password_changed_at)}</p>
                </div>
                <div>
                  <p className="text-gray-600">Account Age</p>
                  <p className="text-gray-900">
                    {Math.floor((Date.now() - new Date(profile.created_at).getTime()) / (1000 * 60 * 60 * 24))} days
                  </p>
                </div>
              </div>
            </div>

            {/* Account Actions */}
            <div className="rounded-lg border border-red-200 bg-red-50 p-6">
              <h3 className="text-lg font-semibold text-red-900 mb-4">Account Actions</h3>
              <p className="text-sm text-red-800 mb-4">
                Manage your account security and deletion.
              </p>
              <div className="flex gap-3">
                <button className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
                  Sign Out All Devices
                </button>
                <button className="rounded-lg border border-red-300 px-4 py-2 text-sm font-medium text-red-700 hover:bg-red-50">
                  Deactivate Account
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
