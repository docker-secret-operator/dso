'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, UserPlus, Pencil, Trash2, ShieldOff, ShieldCheck, Unlock, KeyRound } from 'lucide-react'
import { apiClient, type User } from '@/lib/api-client'

const ROLES = ['viewer', 'operator', 'reviewer', 'approver', 'admin'] as const
type Role = typeof ROLES[number]

function roleBadgeClass(role: string): string {
  switch (role) {
    case 'admin':    return 'bg-red-500/15 text-red-400'
    case 'approver': return 'bg-purple-500/15 text-purple-400'
    case 'reviewer': return 'bg-blue-500/15 text-blue-400'
    case 'operator': return 'bg-emerald-500/15 text-emerald-400'
    default:         return 'bg-slate-700/30 text-slate-400'
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
  const [deleteConfirm, setDeleteConfirm] = useState<User | null>(null)

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
          <h1 className="text-2xl font-semibold text-slate-100">Users</h1>
          <p className="text-sm text-slate-400 mt-1">Manage system users and roles</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={openCreate}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-500 transition-colors"
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
          className="flex-1 max-w-xs px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
        />
        <select
          value={roleFilter}
          onChange={e => setRoleFilter(e.target.value)}
          className="px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
        >
          <option value="">All roles</option>
          {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
        </select>
      </div>

      {actionError && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300 flex items-center justify-between">
          <span>{actionError}</span>
          <button onClick={() => setActionError('')} className="text-red-400 hover:text-red-300 ml-4 text-xs">Dismiss</button>
        </div>
      )}

      {isError && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">
          Failed to load users.
        </div>
      )}

      {/* Inline delete confirm */}
      {deleteConfirm && (
        <div className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-red-300">Delete user <span className="font-mono font-semibold">{deleteConfirm.username}</span>? This cannot be undone.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button
              onClick={() => setDeleteConfirm(null)}
              className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={() => { const u = deleteConfirm; setDeleteConfirm(null); setActionError(''); deleteMutation.mutate(u.id) }}
              className="px-3 py-1.5 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors"
            >
              Delete
            </button>
          </div>
        </div>
      )}

      {/* Table */}
      {isLoading ? (
        <div className="space-y-2">
          {[...Array(4)].map((_, i) => <div key={i} className="h-12 rounded-lg bg-white/5 animate-pulse" />)}
        </div>
      ) : users.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center space-y-2">
          <p className="text-slate-500">No users found.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-white/[0.07] overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-[#0f1015] border-b border-white/[0.07]">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Username</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Display Name</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Role</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Status</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Security</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Created</th>
                <th className="px-4 py-3 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {users.map(u => (
                <tr key={u.id} className="hover:bg-white/[0.03] transition-colors">
                  <td className="px-4 py-3 font-mono text-slate-200">{u.username}</td>
                  <td className="px-4 py-3 text-slate-400">{u.display_name || '—'}</td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex px-2 py-0.5 rounded text-xs font-medium capitalize ${roleBadgeClass(u.role)}`}>
                      {u.role}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    {u.disabled
                      ? <span className="text-xs text-red-400 font-medium">Disabled</span>
                      : <span className="text-xs text-emerald-400 font-medium">Active</span>}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {u.locked && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-orange-500/15 text-orange-400">Locked</span>
                      )}
                      {u.must_change_password && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-amber-500/15 text-amber-400">Pwd Reset</span>
                      )}
                      {!u.locked && !u.must_change_password && (
                        <span className="text-xs text-slate-600">—</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-slate-500 text-xs">
                    {new Date(u.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => openEdit(u)}
                        className="p-1.5 rounded-md hover:bg-white/5 text-slate-500 hover:text-slate-300 transition-colors"
                        title="Edit"
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      <button
                        onClick={() => { setActionError(''); toggleMutation.mutate(u) }}
                        className="p-1.5 rounded-md hover:bg-white/5 transition-colors"
                        title={u.disabled ? 'Enable' : 'Disable'}
                      >
                        {u.disabled
                          ? <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
                          : <ShieldOff className="h-3.5 w-3.5 text-amber-500" />}
                      </button>
                      {u.locked && (
                        <button
                          onClick={() => { setActionError(''); unlockMutation.mutate(u.id) }}
                          className="p-1.5 rounded-md hover:bg-white/5 transition-colors"
                          title="Unlock account"
                        >
                          <Unlock className="h-3.5 w-3.5 text-blue-400" />
                        </button>
                      )}
                      <button
                        onClick={() => { setActionError(''); forceResetMutation.mutate(u.id) }}
                        className="p-1.5 rounded-md hover:bg-white/5 transition-colors"
                        title="Force password reset"
                      >
                        <KeyRound className="h-3.5 w-3.5 text-purple-400" />
                      </button>
                      <button
                        onClick={() => { setActionError(''); setDeleteConfirm(u) }}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-slate-500 hover:text-red-400 transition-colors"
                        title="Delete"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
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
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="bg-[#111318] rounded-xl border border-white/[0.09] shadow-2xl p-6 w-full max-w-md space-y-4">
            <h2 className="text-base font-semibold text-slate-100">{editUser ? 'Edit User' : 'Create User'}</h2>

            {formError && (
              <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-300">
                {formError}
              </div>
            )}

            <div className="space-y-3">
              {!editUser && (
                <>
                  <div>
                    <label className="block text-sm font-medium text-slate-300 mb-1.5">Username</label>
                    <input
                      type="text"
                      value={form.username}
                      onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
                      className="w-full px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-slate-300 mb-1.5">Password</label>
                    <input
                      type="password"
                      value={form.password}
                      onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                      className="w-full px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
                      placeholder="Min 12 chars, upper, lower, number"
                    />
                  </div>
                </>
              )}
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-1.5">Display Name</label>
                <input
                  type="text"
                  value={form.display_name}
                  onChange={e => setForm(f => ({ ...f, display_name: e.target.value }))}
                  className="w-full px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-1.5">Role</label>
                <select
                  value={form.role}
                  onChange={e => setForm(f => ({ ...f, role: e.target.value as Role }))}
                  className="w-full px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30"
                >
                  {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
                </select>
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => { setShowForm(false); setEditUser(null) }}
                className="px-4 py-2 text-sm rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => saveMutation.mutate()}
                disabled={saveMutation.isPending}
                className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
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
