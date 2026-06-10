'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, UserPlus, Pencil, Trash2, ShieldOff, ShieldCheck, Unlock, KeyRound } from 'lucide-react'
import { apiClient, type User } from '@/lib/api-client'

const ROLES = ['viewer', 'operator', 'reviewer', 'approver', 'admin'] as const
type Role = typeof ROLES[number]

function roleBadgeClass(role: string): string {
  switch (role) {
    case 'admin':    return 'bg-red-100 text-red-700'
    case 'approver': return 'bg-purple-100 text-purple-700'
    case 'reviewer': return 'bg-blue-100 text-blue-700'
    case 'operator': return 'bg-green-100 text-green-700'
    default:         return 'bg-gray-100 text-gray-600'
  }
}

interface UserFormData {
  username: string
  display_name: string
  role: Role
  password: string
}

const emptyForm: UserFormData = { username: '', display_name: '', role: 'viewer', password: '' }

export default function UsersPage() {
  const queryClient = useQueryClient()
  const [search, setSearch]       = useState('')
  const [roleFilter, setRoleFilter] = useState('')
  const [showForm, setShowForm]   = useState(false)
  const [editUser, setEditUser]   = useState<User | null>(null)
  const [form, setForm]           = useState<UserFormData>(emptyForm)
  const [formError, setFormError] = useState('')
  const [actionError, setActionError] = useState('')

  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ['users', search, roleFilter],
    queryFn: () => apiClient.listUsers({ search: search || undefined, role: roleFilter || undefined }),
  })

  const users: User[] = data?.users ?? []

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (editUser) {
        return apiClient.updateUser(editUser.id, {
          display_name: form.display_name,
          role: form.role,
        })
      }
      return apiClient.createUser({
        username: form.username,
        password: form.password,
        display_name: form.display_name || undefined,
        role: form.role,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      setShowForm(false)
      setEditUser(null)
      setForm(emptyForm)
      setFormError('')
    },
    onError: (err: unknown) => {
      setFormError(err instanceof Error ? err.message : 'Save failed')
    },
  })

  const toggleMutation = useMutation({
    mutationFn: (u: User) => apiClient.updateUser(u.id, { disabled: !u.disabled }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
    onError: (err: unknown) => setActionError(err instanceof Error ? err.message : 'Failed'),
  })

  const unlockMutation = useMutation({
    mutationFn: (id: string) => apiClient.updateUser(id, { unlock: true } as Parameters<typeof apiClient.updateUser>[1]),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
    onError: (err: unknown) => setActionError(err instanceof Error ? err.message : 'Failed'),
  })

  const forceResetMutation = useMutation({
    mutationFn: (id: string) => apiClient.updateUser(id, { force_password_reset: true } as Parameters<typeof apiClient.updateUser>[1]),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
    onError: (err: unknown) => setActionError(err instanceof Error ? err.message : 'Failed'),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.deleteUser(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
    onError: (err: unknown) => setActionError(err instanceof Error ? err.message : 'Failed'),
  })

  function openCreate() {
    setEditUser(null)
    setForm(emptyForm)
    setFormError('')
    setShowForm(true)
  }

  function openEdit(u: User) {
    setEditUser(u)
    setForm({ username: u.username, display_name: u.display_name, role: u.role as Role, password: '' })
    setFormError('')
    setShowForm(true)
  }

  return (
    <div className="p-6 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Users</h1>
          <p className="text-sm text-muted-foreground mt-1">Manage system users and roles</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={openCreate}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md bg-primary text-primary-foreground hover:bg-primary/90"
          >
            <UserPlus className="h-4 w-4" />
            New User
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex gap-3">
        <input
          type="text"
          placeholder="Search username…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="flex-1 max-w-xs px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
        />
        <select
          value={roleFilter}
          onChange={e => setRoleFilter(e.target.value)}
          className="px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
        >
          <option value="">All roles</option>
          {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
        </select>
      </div>

      {actionError && (
        <div className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
          {actionError}
        </div>
      )}

      {isError && (
        <div className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
          Failed to load users.
        </div>
      )}

      {/* Table */}
      {isLoading ? (
        <div className="space-y-2">
          {[...Array(4)].map((_, i) => <div key={i} className="h-12 rounded-md bg-muted animate-pulse" />)}
        </div>
      ) : users.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center space-y-2">
          <p className="text-muted-foreground">No users found.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-3 text-left font-medium">Username</th>
                <th className="px-4 py-3 text-left font-medium">Display Name</th>
                <th className="px-4 py-3 text-left font-medium">Role</th>
                <th className="px-4 py-3 text-left font-medium">Status</th>
                <th className="px-4 py-3 text-left font-medium">Security</th>
                <th className="px-4 py-3 text-left font-medium">Created</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {users.map(u => (
                <tr key={u.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3 font-mono">{u.username}</td>
                  <td className="px-4 py-3 text-muted-foreground">{u.display_name || '—'}</td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex px-2 py-0.5 rounded text-xs font-medium capitalize ${roleBadgeClass(u.role)}`}>
                      {u.role}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    {u.disabled
                      ? <span className="text-xs text-red-600 font-medium">Disabled</span>
                      : <span className="text-xs text-green-600 font-medium">Active</span>}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {u.locked && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-orange-100 text-orange-700">Locked</span>
                      )}
                      {u.must_change_password && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-700">Pwd Reset</span>
                      )}
                      {!u.locked && !u.must_change_password && (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground text-xs">
                    {new Date(u.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => openEdit(u)}
                        className="p-1.5 rounded hover:bg-muted"
                        title="Edit"
                      >
                        <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                      </button>
                      <button
                        onClick={() => { setActionError(''); toggleMutation.mutate(u) }}
                        className="p-1.5 rounded hover:bg-muted"
                        title={u.disabled ? 'Enable' : 'Disable'}
                      >
                        {u.disabled
                          ? <ShieldCheck className="h-3.5 w-3.5 text-green-600" />
                          : <ShieldOff className="h-3.5 w-3.5 text-yellow-600" />}
                      </button>
                      {u.locked && (
                        <button
                          onClick={() => { setActionError(''); unlockMutation.mutate(u.id) }}
                          className="p-1.5 rounded hover:bg-muted"
                          title="Unlock account"
                        >
                          <Unlock className="h-3.5 w-3.5 text-blue-500" />
                        </button>
                      )}
                      <button
                        onClick={() => { setActionError(''); forceResetMutation.mutate(u.id) }}
                        className="p-1.5 rounded hover:bg-muted"
                        title="Force password reset"
                      >
                        <KeyRound className="h-3.5 w-3.5 text-purple-500" />
                      </button>
                      <button
                        onClick={() => {
                          setActionError('')
                          if (confirm(`Delete user "${u.username}"?`)) deleteMutation.mutate(u.id)
                        }}
                        className="p-1.5 rounded hover:bg-muted"
                        title="Delete"
                      >
                        <Trash2 className="h-3.5 w-3.5 text-red-500" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create / Edit modal */}
      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-background rounded-lg border border-border shadow-xl p-6 w-full max-w-md space-y-4">
            <h2 className="text-lg font-semibold">{editUser ? 'Edit User' : 'Create User'}</h2>

            {formError && (
              <div className="rounded-md bg-red-50 border border-red-200 px-3 py-2 text-sm text-red-700">
                {formError}
              </div>
            )}

            <div className="space-y-3">
              {!editUser && (
                <>
                  <div>
                    <label className="text-sm font-medium">Username</label>
                    <input
                      type="text"
                      value={form.username}
                      onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
                      className="mt-1 w-full px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
                    />
                  </div>
                  <div>
                    <label className="text-sm font-medium">Password</label>
                    <input
                      type="password"
                      value={form.password}
                      onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                      className="mt-1 w-full px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
                      placeholder="Min 12 chars, upper, lower, number"
                    />
                  </div>
                </>
              )}
              <div>
                <label className="text-sm font-medium">Display Name</label>
                <input
                  type="text"
                  value={form.display_name}
                  onChange={e => setForm(f => ({ ...f, display_name: e.target.value }))}
                  className="mt-1 w-full px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
                />
              </div>
              <div>
                <label className="text-sm font-medium">Role</label>
                <select
                  value={form.role}
                  onChange={e => setForm(f => ({ ...f, role: e.target.value as Role }))}
                  className="mt-1 w-full px-3 py-2 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
                >
                  {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
                </select>
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => { setShowForm(false); setEditUser(null) }}
                className="px-4 py-2 text-sm rounded-md border border-border hover:bg-muted"
              >
                Cancel
              </button>
              <button
                onClick={() => saveMutation.mutate()}
                disabled={saveMutation.isPending}
                className="px-4 py-2 text-sm rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              >
                {saveMutation.isPending ? 'Saving…' : editUser ? 'Save' : 'Create'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
