# P5 Bulk Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-select checkboxes and batch action toolbars to the Secrets, Drift, and Policies pages so operators can rotate/ack/resolve/enable/disable/delete many items at once.

**Architecture:** Reusable `useSelection` hook owns Set<id> state; a `BulkToolbar` component renders inline when selection.size > 0; each page adds a checkbox column and wires mutations to new backend batch endpoints. Progress and partial-failure results are shown inline in the toolbar after the mutation settles.

**Tech Stack:** Go (net/http), React 18, React Query v5 (@tanstack/react-query), TypeScript, Tailwind CSS, lucide-react icons.

**Constraints (must not violate):** No AI, no recommendations, no autonomy, no forecasting, no new pages.

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `web/components/common/useSelection.ts` | Generic Set-based selection hook |
| Create | `web/components/common/BulkToolbar.tsx` | Inline toolbar shown when count > 0 |
| Create | `web/components/common/ConfirmModal.tsx` | Destructive-action confirmation dialog |
| Create | `web/lib/api/bulk.ts` | Frontend API client for all bulk endpoints |
| Modify | `web/lib/api/index.ts` | Re-export bulk module |
| Modify | `internal/server/rest.go` | Add AuditService field, rotateSingle helper, handleBulkRotate, route |
| Modify | `internal/api/drift_handler.go` | Add BulkAck + BulkResolve handlers and routes |
| Modify | `internal/api/policy_handler.go` | Add BulkEnable + BulkDisable + BulkDelete handlers and routes |
| Modify | `web/app/secrets/page.tsx` | Checkbox column, BulkToolbar, bulk-rotate mutation |
| Modify | `web/app/drift/_client-wrapper.tsx` | Checkbox column, BulkToolbar, bulk-ack/resolve mutations |
| Modify | `web/app/policies/page.tsx` | Checkbox column, BulkToolbar, ConfirmModal, bulk mutations |
| Create | `docs/BULK_OPERATIONS.md` | Operator-facing reference |

---

## Task 1 — `useSelection` hook

**Files:**
- Create: `web/components/common/useSelection.ts`

- [ ] **Step 1: Create the file**

```typescript
// web/components/common/useSelection.ts
import { useState, useCallback } from 'react'

export interface UseSelection {
  selected: Set<string>
  toggle: (id: string) => void
  togglePage: (ids: string[]) => void
  clear: () => void
  isSelected: (id: string) => boolean
  size: number
}

export function useSelection(): UseSelection {
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const toggle = useCallback((id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  // togglePage: if every id is already selected → deselect all; otherwise select all.
  const togglePage = useCallback((ids: string[]) => {
    setSelected(prev => {
      const allSelected = ids.length > 0 && ids.every(id => prev.has(id))
      const next = new Set(prev)
      if (allSelected) {
        ids.forEach(id => next.delete(id))
      } else {
        ids.forEach(id => next.add(id))
      }
      return next
    })
  }, [])

  const clear = useCallback(() => setSelected(new Set()), [])

  const isSelected = useCallback((id: string) => selected.has(id), [selected])

  return { selected, toggle, togglePage, clear, isSelected, size: selected.size }
}
```

- [ ] **Step 2: Verify TypeScript parses (no build step needed at this stage — just ensure no syntax errors by inspection)**

---

## Task 2 — `BulkToolbar` and `ConfirmModal` components

**Files:**
- Create: `web/components/common/BulkToolbar.tsx`
- Create: `web/components/common/ConfirmModal.tsx`

- [ ] **Step 1: Create BulkToolbar**

```tsx
// web/components/common/BulkToolbar.tsx
'use client'

import type { ReactNode } from 'react'
import { X } from 'lucide-react'

export interface BulkAction {
  label: string
  onClick: () => void
  variant?: 'default' | 'danger'
  disabled?: boolean
}

interface BulkToolbarProps {
  count: number
  actions: BulkAction[]
  onClear: () => void
  /** Optional inline status message shown between count and buttons (e.g. "Rotating 14 secrets…") */
  status?: ReactNode
}

/**
 * Inline toolbar that appears when count > 0.
 * Returns null when count === 0 so callers can render it unconditionally.
 */
export function BulkToolbar({ count, actions, onClear, status }: BulkToolbarProps) {
  if (count === 0) return null
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-indigo-500/10 border border-indigo-500/20 rounded-lg">
      <span className="text-sm font-semibold text-indigo-300 tabular-nums whitespace-nowrap">
        {count} selected
      </span>
      {status && (
        <span className="text-xs text-slate-400 truncate">{status}</span>
      )}
      <div className="flex items-center gap-2">
        {actions.map(action => (
          <button
            key={action.label}
            onClick={action.onClick}
            disabled={action.disabled}
            className={[
              'px-3 py-1.5 text-xs rounded-md border transition-all disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap',
              action.variant === 'danger'
                ? 'border-red-500/30 text-red-300 hover:bg-red-500/10 hover:border-red-500/40'
                : 'border-white/[0.09] text-slate-300 hover:bg-white/5 hover:border-white/20',
            ].join(' ')}
          >
            {action.label}
          </button>
        ))}
      </div>
      <button
        onClick={onClear}
        className="ml-auto p-1 text-slate-600 hover:text-slate-300 transition-colors flex-shrink-0"
        title="Clear selection"
        aria-label="Clear selection"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  )
}
```

- [ ] **Step 2: Create ConfirmModal**

```tsx
// web/components/common/ConfirmModal.tsx
'use client'

interface ConfirmModalProps {
  title: string
  message: string
  confirmLabel?: string
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Centered blocking modal for destructive confirmations.
 * Clicking the backdrop calls onCancel.
 */
export function ConfirmModal({
  title,
  message,
  confirmLabel = 'Confirm',
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm z-50"
        onClick={onCancel}
      />
      {/* Dialog */}
      <div className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-full max-w-sm px-4">
        <div className="bg-[#111827] border border-white/[0.09] rounded-xl p-6 shadow-2xl">
          <h3 className="text-sm font-semibold text-slate-100 mb-2">{title}</h3>
          <p className="text-sm text-slate-400 mb-6 leading-relaxed">{message}</p>
          <div className="flex gap-3 justify-end">
            <button
              onClick={onCancel}
              className="px-4 py-2 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={() => { onConfirm(); }}
              className="px-4 py-2 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors"
            >
              {confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}
```

---

## Task 3 — Backend: `POST /api/secrets/bulk-rotate`

**Files:**
- Modify: `internal/server/rest.go`

The RESTServer struct needs an `AuditService` field. The bulk-rotate handler loops over names, calls a new private `rotateSingle` helper for each, collects failures, audits, then triggers one drift rescan at the end.

- [ ] **Step 1: Add `AuditService` field to RESTServer struct**

Find the block near line 160 that reads:
```go
	// P4 real drift detection
	DriftEngine    *drift.Engine
	InjectionStore drift.InjectionStore
	// startup time for uptime reporting
	startTime time.Time
```
Replace with:
```go
	// P4 real drift detection
	DriftEngine    *drift.Engine
	InjectionStore drift.InjectionStore
	// P5 bulk operations — audit logging
	AuditService *services.AuditService
	// startup time for uptime reporting
	startTime time.Time
```

- [ ] **Step 2: Wire AuditService in the RESTServer assembly**

Find the assembly block (around line 1490) that ends with:
```go
		GraphHandler:            graphHandler,
	}
```
Add before the closing `}`:
```go
		AuditService:            auditService,
```
So it reads:
```go
		GraphHandler:            graphHandler,
		AuditService:            auditService,
	}
```

- [ ] **Step 3: Add `rotateSingle` method**

Add this method right before `handleRotateSecret` (around line 1072 in the original file):

```go
// rotateSingle rotates one secret: triggers provider webhook and records the injection hash.
// It does NOT trigger a drift rescan — callers decide when to rescan.
// Returns (providerName, error).
func (s *RESTServer) rotateSingle(ctx context.Context, name string) (string, error) {
	var targetSecret *config.SecretMapping
	for _, sec := range s.Config.Secrets {
		if sec.Name == name {
			cp := sec
			targetSecret = &cp
			break
		}
	}
	if targetSecret == nil {
		return "", fmt.Errorf("not configured")
	}
	pName := targetSecret.Provider
	if pName == "" {
		for k := range s.Config.Providers {
			pName = k
			break
		}
	}
	pCfg, ok := s.Config.Providers[pName]
	if !ok {
		return pName, fmt.Errorf("provider not found")
	}
	if err := s.TriggerEngine.HandleWebhook(pName, pCfg, *targetSecret, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return pName, err
	}
	// Record the injection hash so drift detection has baseline state.
	if s.InjectionStore != nil && s.Cache != nil {
		cacheKey := fmt.Sprintf("%s:%s", pName, name)
		if h, ok := s.Cache.GetHash(cacheKey); ok {
			_ = s.InjectionStore.RecordInjection(ctx, name, h)
		}
	}
	return pName, nil
}
```

- [ ] **Step 4: Refactor `handleRotateSecret` to use `rotateSingle`**

Replace the body of `handleRotateSecret` (keep the func signature):

```go
func (s *RESTServer) handleRotateSecret(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "application/json")
	if s.Config == nil || s.TriggerEngine == nil {
		http.Error(w, "rotation not available", http.StatusServiceUnavailable)
		return
	}
	pName, err := s.rotateSingle(r.Context(), name)
	if err != nil {
		s.Logger.Error("rotation failed", zap.String("secret", name), zap.Error(err))
		code := http.StatusInternalServerError
		switch err.Error() {
		case "not configured":
			code = http.StatusNotFound
		case "provider not found":
			code = http.StatusBadRequest
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if s.DriftEngine != nil {
		go func() {
			if err := s.DriftEngine.RunScan(context.Background()); err != nil {
				s.Logger.Warn("post-rotation drift scan failed", zap.Error(err))
			}
		}()
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "rotated",
		"secret":     name,
		"provider":   pName,
		"rotated_at": time.Now().UTC().Format(time.RFC3339),
	})
}
```

- [ ] **Step 5: Add `handleBulkRotate` method**

Add after `handleRotateSecret`:

```go
// handleBulkRotate handles POST /api/secrets/bulk-rotate
// Body: {"names":["a","b",...]}
// Response: {"success":N,"failed":M,"failures":[{"name":"x","error":"msg"}]}
// Never aborts on partial failure — all names are attempted.
func (s *RESTServer) handleBulkRotate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.Config == nil || s.TriggerEngine == nil {
		http.Error(w, "rotation not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Names) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "names required"})
		return
	}

	type rotFailure struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []rotFailure
	)
	for _, name := range req.Names {
		if _, err := s.rotateSingle(r.Context(), name); err != nil {
			failures = append(failures, rotFailure{Name: name, Error: err.Error()})
		} else {
			succeeded++
		}
	}

	// Audit one event for the entire batch.
	if s.AuditService != nil {
		user := auth.CurrentUser(r.Context())
		actorID, actorName := "system", "system"
		if user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		_ = s.AuditService.LogEvent(r.Context(), actorID, actorName,
			"bulk.rotate",
			fmt.Sprintf("%d secrets", len(req.Names)),
			fmt.Sprintf("success=%d failed=%d", succeeded, len(failures)),
			"secret",
		)
	}

	// One drift rescan after all rotations.
	if s.DriftEngine != nil && succeeded > 0 {
		go func() {
			if err := s.DriftEngine.RunScan(context.Background()); err != nil {
				s.Logger.Warn("post-bulk-rotation drift scan failed", zap.Error(err))
			}
		}()
	}

	if failures == nil {
		failures = []rotFailure{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}
```

- [ ] **Step 6: Add route for `bulk-rotate` in `handleSecrets`**

Find the `handleSecrets` function body:
```go
	switch {
	case path == "" || path == "/":
		if r.Method == http.MethodGet {
			s.handleListSecrets(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasSuffix(path, "/rotate"):
```
Add a new case BEFORE the `HasSuffix(path, "/rotate")` case:
```go
	case path == "bulk-rotate":
		if r.Method == http.MethodPost {
			s.handleBulkRotate(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
```

- [ ] **Step 7: Build**

```bash
go build -o dso . 2>&1
```
Expected: no output (clean build).

- [ ] **Step 8: Commit**

```bash
git add internal/server/rest.go
git commit -m "feat(bulk): POST /api/secrets/bulk-rotate + rotateSingle refactor + AuditService field"
```

---

## Task 4 — Backend: drift bulk endpoints

**Files:**
- Modify: `internal/api/drift_handler.go`

Add `POST /api/drift/bulk-ack` and `POST /api/drift/bulk-resolve`. Both accept `{"ids":["..."]}` and return `{"success":N,"failed":M,"failures":[...]}`.

- [ ] **Step 1: Add routes to `ServeHTTP`**

Find the switch block in `ServeHTTP`. Add these two cases BEFORE the existing `strings.HasPrefix(path, "/api/drift/")` catch-all GET:

```go
	case path == "/api/drift/bulk-ack" && r.Method == "POST":
		h.BulkAcknowledge(w, r)
	case path == "/api/drift/bulk-resolve" && r.Method == "POST":
		h.BulkResolve(w, r)
```

- [ ] **Step 2: Add `BulkAcknowledge` handler**

Add after `AcknowledgeFinding`:

```go
// BulkAcknowledge handles POST /api/drift/bulk-ack
// Body: {"ids":["finding-id-1","finding-id-2"]}
func (h *DriftHandler) BulkAcknowledge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
		return
	}

	type idFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []idFailure
	)
	for _, id := range req.IDs {
		if err := h.engine.AcknowledgeFinding(id); err != nil {
			failures = append(failures, idFailure{ID: id, Error: err.Error()})
		} else {
			succeeded++
		}
	}
	if failures == nil {
		failures = []idFailure{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}
```

- [ ] **Step 3: Add `BulkResolve` handler**

Add after `BulkAcknowledge`:

```go
// BulkResolve handles POST /api/drift/bulk-resolve
// Body: {"ids":["finding-id-1","finding-id-2"]}
func (h *DriftHandler) BulkResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
		return
	}

	type idFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []idFailure
	)
	for _, id := range req.IDs {
		if err := h.engine.ResolveFinding(id); err != nil {
			failures = append(failures, idFailure{ID: id, Error: err.Error()})
		} else {
			succeeded++
		}
	}
	if failures == nil {
		failures = []idFailure{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}
```

- [ ] **Step 4: Build**

```bash
go build -o dso . 2>&1
```
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/api/drift_handler.go
git commit -m "feat(bulk): POST /api/drift/bulk-ack and /api/drift/bulk-resolve"
```

---

## Task 5 — Backend: policy bulk endpoints

**Files:**
- Modify: `internal/api/policy_handler.go`

Add `POST /api/policies/bulk-enable`, `/api/policies/bulk-disable`, `/api/policies/bulk-delete`.

- [ ] **Step 1: Add routes to `ServeHTTP`**

In the `ServeHTTP` switch, add these three cases. They must come BEFORE the existing `strings.HasPrefix(path, "/api/policies/")` catch-alls. Insert after the `path == "/api/policies/metrics"` case:

```go
	case path == "/api/policies/bulk-enable" && r.Method == "POST":
		h.BulkEnable(w, r)
	case path == "/api/policies/bulk-disable" && r.Method == "POST":
		h.BulkDisable(w, r)
	case path == "/api/policies/bulk-delete" && r.Method == "POST":
		h.BulkDelete(w, r)
```

- [ ] **Step 2: Add the three bulk handlers**

Add after `GetMetrics`:

```go
// BulkEnable handles POST /api/policies/bulk-enable
// Body: {"ids":["rule-id-1","rule-id-2"]}
func (h *PolicyHandler) BulkEnable(w http.ResponseWriter, r *http.Request) {
	h.bulkToggle(w, r, func(id string) error { return h.engine.EnableRule(id) })
}

// BulkDisable handles POST /api/policies/bulk-disable
// Body: {"ids":["rule-id-1","rule-id-2"]}
func (h *PolicyHandler) BulkDisable(w http.ResponseWriter, r *http.Request) {
	h.bulkToggle(w, r, func(id string) error { return h.engine.DisableRule(id) })
}

// BulkDelete handles POST /api/policies/bulk-delete
// Body: {"ids":["rule-id-1","rule-id-2"]}
func (h *PolicyHandler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	h.bulkToggle(w, r, func(id string) error { return h.engine.DeleteRule(id) })
}

// bulkToggle is the shared implementation for all three bulk policy mutations.
func (h *PolicyHandler) bulkToggle(w http.ResponseWriter, r *http.Request, fn func(string) error) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
		return
	}

	type idFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []idFailure
	)
	for _, id := range req.IDs {
		if err := fn(id); err != nil {
			failures = append(failures, idFailure{ID: id, Error: err.Error()})
		} else {
			succeeded++
		}
	}
	if failures == nil {
		failures = []idFailure{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}
```

- [ ] **Step 3: Build**

```bash
go build -o dso . 2>&1
```
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/api/policy_handler.go
git commit -m "feat(bulk): POST /api/policies/bulk-enable, bulk-disable, bulk-delete"
```

---

## Task 6 — Frontend API client for bulk endpoints

**Files:**
- Create: `web/lib/api/bulk.ts`
- Modify: `web/lib/api/index.ts`

- [ ] **Step 1: Create `web/lib/api/bulk.ts`**

```typescript
import { apiClient } from '../api-client'

export interface BulkRotateResult {
  success: number
  failed: number
  failures: Array<{ name: string; error: string }>
}

export interface BulkIdResult {
  success: number
  failed: number
  failures: Array<{ id: string; error: string }>
}

export async function bulkRotate(names: string[]): Promise<BulkRotateResult> {
  const res = await apiClient.client.post<BulkRotateResult>('/api/secrets/bulk-rotate', { names })
  return res.data
}

export async function bulkDriftAck(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/drift/bulk-ack', { ids })
  return res.data
}

export async function bulkDriftResolve(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/drift/bulk-resolve', { ids })
  return res.data
}

export async function bulkPolicyEnable(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-enable', { ids })
  return res.data
}

export async function bulkPolicyDisable(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-disable', { ids })
  return res.data
}

export async function bulkPolicyDelete(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-delete', { ids })
  return res.data
}
```

- [ ] **Step 2: Export from `web/lib/api/index.ts`**

Add after the `export * as drift from './drift'` line:
```typescript
export * as bulk from './bulk'
```

- [ ] **Step 3: Commit**

```bash
git add web/lib/api/bulk.ts web/lib/api/index.ts
git commit -m "feat(bulk): frontend API client for all six bulk endpoints"
```

---

## Task 7 — Secrets page: checkbox column + bulk rotate

**Files:**
- Modify: `web/app/secrets/page.tsx`

The secrets table gets a checkbox column (col 0), a header checkbox that selects the current page, and a `BulkToolbar` that appears above the table when any rows are selected. Bulk rotate > 50 requires a `ConfirmModal`.

- [ ] **Step 1: Update imports**

Replace the existing import block at the top of `web/app/secrets/page.tsx`:

```tsx
'use client'

import { useState, useMemo, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient, type Secret } from '@/lib/api-client'
import * as auditApi from '@/lib/api/audit'
import * as bulkApi from '@/lib/api/bulk'
import type { BulkRotateResult } from '@/lib/api/bulk'
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar } from '@/components/common/BulkToolbar'
import { ConfirmModal } from '@/components/common/ConfirmModal'
import { Pagination } from '@/components/common/Pagination'
import { PageHeader, Card, Badge, StatusBadge, Button, Input, EmptyState, Skeleton } from '@/components/ui-modern'
import {
  RefreshCw, RotateCcw, Database, Search, ChevronUp, ChevronDown,
  ChevronsUpDown, Shield, X, Server, Clock, Hash,
} from 'lucide-react'
```

- [ ] **Step 2: Add selection state + bulk mutation inside `SecretsPage`**

After the existing `useState`/`useQuery` declarations and `rotateMutation`, add:

```tsx
  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkRotateResult | null>(null)
  const [confirmBulk, setConfirmBulk] = useState(false)

  const bulkRotateMutation = useMutation({
    mutationFn: (names: string[]) => bulkApi.bulkRotate(names),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: ['secrets'] })
    },
  })

  const handleBulkRotate = () => {
    const names = Array.from(sel.selected)
    if (names.length > 50) {
      setConfirmBulk(true)
    } else {
      bulkRotateMutation.mutate(names)
    }
  }
```

- [ ] **Step 3: Add BulkToolbar above the table**

Find the line:
```tsx
      {/* Table */}
      <Card className="overflow-hidden">
```
Insert before it:

```tsx
      {/* Bulk toolbar — visible when any rows selected */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          bulkRotateMutation.isPending
            ? `Rotating ${sel.size} secrets…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} rotated successfully`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed: ${bulkStatus.failures.map(f => f.name).join(', ')}`
            : undefined
        }
        actions={[
          {
            label: bulkRotateMutation.isPending ? 'Rotating…' : 'Rotate',
            onClick: handleBulkRotate,
            disabled: bulkRotateMutation.isPending || sel.size === 0,
          },
        ]}
      />
```

- [ ] **Step 4: Add checkbox column header**

Find the `<thead>` `<tr>` element that starts with `<ThCell col="name" ...`. Prepend a checkbox header cell:

```tsx
                <tr className="border-b border-white/[0.07] bg-white/[0.02]">
                  <th className="pl-4 pr-2 py-3 w-8">
                    <input
                      type="checkbox"
                      aria-label="Select page"
                      checked={secrets.length > 0 && secrets.every(s => sel.isSelected(s.name))}
                      onChange={() => sel.togglePage(secrets.map(s => s.name))}
                      className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                    />
                  </th>
                  <ThCell col="name"         label="Name" />
                  {/* ... rest of ThCell entries unchanged ... */}
```

- [ ] **Step 5: Add checkbox cell to each data row**

Find the `<tr key={secret.name} ...` row. Prepend a checkbox `<td>` as the first child of each row. Also add `onClick` stopPropagation on the checkbox to prevent row drawer from opening:

```tsx
                  <tr
                    key={secret.name}
                    className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                    onClick={() => setSelected(secret)}
                  >
                    <td className="pl-4 pr-2 py-3" onClick={e => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        aria-label={`Select ${secret.name}`}
                        checked={sel.isSelected(secret.name)}
                        onChange={() => sel.toggle(secret.name)}
                        className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-200">{secret.name}</td>
                    {/* ... rest of tds unchanged ... */}
```

- [ ] **Step 6: Add ConfirmModal for large batches**

Find the closing `{selected && (<SecretDrawer .../>)}` block at the bottom. Add before it:

```tsx
      {confirmBulk && (
        <ConfirmModal
          title={`Rotate ${sel.size} secrets?`}
          message={`You are about to rotate ${sel.size} secrets. This will trigger provider webhooks for each secret simultaneously. Continue?`}
          confirmLabel={`Rotate ${sel.size} secrets`}
          onConfirm={() => {
            setConfirmBulk(false)
            bulkRotateMutation.mutate(Array.from(sel.selected))
          }}
          onCancel={() => setConfirmBulk(false)}
        />
      )}
```

- [ ] **Step 7: TypeScript check**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "app/secrets" | head -10
```
Expected: no errors for `app/secrets/page.tsx`.

- [ ] **Step 8: Commit**

```bash
git add web/app/secrets/page.tsx web/components/common/
git commit -m "feat(bulk): secrets page — checkbox column + bulk rotate with confirm for >50"
```

---

## Task 8 — Drift page: checkbox column + bulk ack/resolve

**Files:**
- Modify: `web/app/drift/_client-wrapper.tsx`

- [ ] **Step 1: Update imports**

At the top of `_client-wrapper.tsx`, add:
```tsx
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar } from '@/components/common/BulkToolbar'
import * as bulkApi from '@/lib/api/bulk'
import type { BulkIdResult } from '@/lib/api/bulk'
```

- [ ] **Step 2: Add selection + bulk mutations in `DriftDashboardClient`**

After the `resolveMutation` declaration, add:

```tsx
  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkIdResult | null>(null)

  const bulkAckMutation = useMutation({
    mutationFn: (ids: string[]) => bulkApi.bulkDriftAck(ids),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: FINDINGS_KEY })
    },
  })

  const bulkResolveMutation = useMutation({
    mutationFn: (ids: string[]) => bulkApi.bulkDriftResolve(ids),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: FINDINGS_KEY })
    },
  })
```

- [ ] **Step 3: Add BulkToolbar above the findings table**

Find `{/* Findings table */}` comment. Insert before it:

```tsx
      {/* Bulk toolbar */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          (bulkAckMutation.isPending || bulkResolveMutation.isPending)
            ? `Processing ${sel.size} findings…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} updated`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed`
            : undefined
        }
        actions={[
          {
            label: 'Acknowledge',
            onClick: () => bulkAckMutation.mutate(Array.from(sel.selected)),
            disabled: bulkAckMutation.isPending || bulkResolveMutation.isPending,
          },
          {
            label: 'Resolve',
            onClick: () => bulkResolveMutation.mutate(Array.from(sel.selected)),
            disabled: bulkAckMutation.isPending || bulkResolveMutation.isPending,
          },
        ]}
      />
```

- [ ] **Step 4: Add header checkbox to the table `<thead>`**

Find the `<thead>` row. Prepend:
```tsx
              <thead className="border-b border-gray-200 bg-gray-50">
                <tr>
                  <th className="pl-4 pr-2 py-3 w-8">
                    <input
                      type="checkbox"
                      aria-label="Select page"
                      checked={findings.length > 0 && findings.every(f => sel.isSelected(f.id))}
                      onChange={() => sel.togglePage(findings.map(f => f.id))}
                      className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                    />
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Severity</th>
                  {/* ... rest of ths unchanged ... */}
```

- [ ] **Step 5: Add checkbox cell to each `FindingRow`**

Update `FindingRow` component to accept `onSelect` and `isSelected` props:

```tsx
function FindingRow({
  f,
  onAck,
  onResolve,
  isSelected,
  onSelect,
}: {
  f: DriftFinding
  onAck: (id: string) => void
  onResolve: (id: string) => void
  isSelected: boolean
  onSelect: (id: string) => void
}) {
```

Add the checkbox as the first `<td>` inside the row:
```tsx
    <tr className="hover:bg-gray-50">
      <td className="pl-4 pr-2 py-3">
        <input
          type="checkbox"
          aria-label={`Select finding ${f.id}`}
          checked={isSelected}
          onChange={() => onSelect(f.id)}
          className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
        />
      </td>
      {/* ... existing tds unchanged ... */}
```

Update the call site in `DriftDashboardClient` to pass the new props:
```tsx
        findings.map(f => (
          <FindingRow
            key={f.id}
            f={f}
            onAck={(id) => ackMutation.mutate(id)}
            onResolve={(id) => resolveMutation.mutate(id)}
            isSelected={sel.isSelected(f.id)}
            onSelect={sel.toggle}
          />
        ))
```

- [ ] **Step 6: TypeScript check**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "drift" | head -10
```
Expected: no errors for drift files.

- [ ] **Step 7: Commit**

```bash
git add web/app/drift/_client-wrapper.tsx
git commit -m "feat(bulk): drift page — checkbox column + bulk ack/resolve"
```

---

## Task 9 — Policies page: checkbox column + bulk enable/disable/delete

**Files:**
- Modify: `web/app/policies/page.tsx`

The policies page uses raw `fetch` + `useEffect` (not React Query). Add selection state and bulk mutations using the same fetch pattern, plus a ConfirmModal for disable and delete actions.

- [ ] **Step 1: Update imports at the top**

```tsx
'use client'

import { useState, useEffect } from 'react'
import { AlertCircle, Play, RotateCw, Trash2, TrendingUp } from 'lucide-react'
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar, type BulkAction } from '@/components/common/BulkToolbar'
import { ConfirmModal } from '@/components/common/ConfirmModal'
import * as bulkApi from '@/lib/api/bulk'
import type { BulkIdResult } from '@/lib/api/bulk'
```

- [ ] **Step 2: Add state inside `PoliciesPage` component**

After the existing `useState` declarations (`rules`, `metrics`, `loading`, `error`, `confirmDeletePolicy`), add:

```tsx
  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkIdResult | null>(null)
  const [bulkPending, setBulkPending] = useState(false)
  const [confirmBulkAction, setConfirmBulkAction] = useState<null | 'disable' | 'delete'>(null)
```

- [ ] **Step 3: Add bulk action handlers**

Add after `handleDelete`:

```tsx
  const handleBulkEnable = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyEnable(ids)
      setBulkStatus(result)
      sel.clear()
      const res = await fetch('/api/policies', { headers: getAuthHeaders() })
      const data = await res.json()
      setRules(data.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk enable failed')
    } finally {
      setBulkPending(false)
    }
  }

  const handleBulkDisable = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyDisable(ids)
      setBulkStatus(result)
      sel.clear()
      const res = await fetch('/api/policies', { headers: getAuthHeaders() })
      const data = await res.json()
      setRules(data.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk disable failed')
    } finally {
      setBulkPending(false)
    }
  }

  const handleBulkDelete = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyDelete(ids)
      setBulkStatus(result)
      sel.clear()
      setRules(prev => prev.filter(r => !ids.includes(r.id)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk delete failed')
    } finally {
      setBulkPending(false)
    }
  }
```

- [ ] **Step 4: Add BulkToolbar above the policies table**

Find `{/* Policies Table */}` comment. Insert before it:

```tsx
      {/* Bulk toolbar */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          bulkPending
            ? `Processing ${sel.size} policies…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} updated`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed: ${bulkStatus.failures.map((f) => f.id).join(', ')}`
            : undefined
        }
        actions={[
          {
            label: 'Enable',
            onClick: handleBulkEnable,
            disabled: bulkPending,
          },
          {
            label: 'Disable',
            onClick: () => setConfirmBulkAction('disable'),
            variant: 'danger',
            disabled: bulkPending,
          },
          {
            label: 'Delete',
            onClick: () => setConfirmBulkAction('delete'),
            variant: 'danger',
            disabled: bulkPending,
          },
        ] satisfies BulkAction[]}
      />
```

- [ ] **Step 5: Add ConfirmModal for disable and delete**

Find `{confirmDeletePolicy && (` block. After it (or at the bottom before the closing `</div>`), add:

```tsx
      {confirmBulkAction === 'disable' && (
        <ConfirmModal
          title={`Disable ${sel.size} policies?`}
          message={`You are about to disable ${sel.size} policies. Disabled policies will not evaluate or fire until re-enabled.`}
          confirmLabel={`Disable ${sel.size} policies`}
          onConfirm={() => { setConfirmBulkAction(null); handleBulkDisable() }}
          onCancel={() => setConfirmBulkAction(null)}
        />
      )}
      {confirmBulkAction === 'delete' && (
        <ConfirmModal
          title={`Delete ${sel.size} policies?`}
          message={`You are about to permanently delete ${sel.size} policies. This action cannot be undone.`}
          confirmLabel={`Delete ${sel.size} policies`}
          onConfirm={() => { setConfirmBulkAction(null); handleBulkDelete() }}
          onCancel={() => setConfirmBulkAction(null)}
        />
      )}
```

- [ ] **Step 6: Add checkbox header to the table `<thead>`**

Find the `<thead>` row. Prepend the checkbox `<th>`:
```tsx
            <thead className="border-b border-slate-700/50 bg-[#0B1020]">
              <tr>
                <th className="pl-6 pr-2 py-3 w-8">
                  <input
                    type="checkbox"
                    aria-label="Select page"
                    checked={rules.length > 0 && rules.every(r => sel.isSelected(r.id))}
                    onChange={() => sel.togglePage(rules.map(r => r.id))}
                    className="rounded border-slate-600 bg-transparent accent-indigo-500 cursor-pointer"
                  />
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Name</th>
                {/* ... rest unchanged ... */}
```

- [ ] **Step 7: Add checkbox cell to each rule row**

Find `rules.map(rule => (` and inside the `<tr>` for each rule, prepend:
```tsx
                    <td className="pl-6 pr-2 py-4">
                      <input
                        type="checkbox"
                        aria-label={`Select ${rule.name}`}
                        checked={sel.isSelected(rule.id)}
                        onChange={() => sel.toggle(rule.id)}
                        className="rounded border-slate-600 bg-transparent accent-indigo-500 cursor-pointer"
                      />
                    </td>
```
Also update `colSpan` on the "No policies configured" empty-state row from `6` to `7`.

- [ ] **Step 8: TypeScript check**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "policies" | head -10
```
Expected: no errors for `app/policies/page.tsx`.

- [ ] **Step 9: Commit**

```bash
git add web/app/policies/page.tsx
git commit -m "feat(bulk): policies page — checkbox column + bulk enable/disable/delete with confirm"
```

---

## Task 10 — Final build + TypeScript full check

**Files:** none new

- [ ] **Step 1: Full Go build**

```bash
go build -o dso . 2>&1
```
Expected: `BUILD OK` (no output).

- [ ] **Step 2: TypeScript full check (ignore pre-existing test errors)**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "error TS" | grep -v "tests/" | grep -v "performance-benchmark" | head -20
```
Expected: no lines output (zero new TS errors from files we touched).

- [ ] **Step 3: Smoke test backend bulk endpoints**

Start the server with a config that has secrets and run:

```bash
# Login
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

# Bulk rotate (empty names — expect 400)
curl -s -X POST http://localhost:8080/api/secrets/bulk-rotate \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"names":[]}' | python3 -m json.tool

# Bulk drift ack with nonexistent ID — expect {"success":0,"failed":1,"failures":[...]}
curl -s -X POST http://localhost:8080/api/drift/bulk-ack \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"ids":["nonexistent"]}' | python3 -m json.tool

# Bulk policy enable with nonexistent ID — expect {"success":0,"failed":1,"failures":[...]}
curl -s -X POST http://localhost:8080/api/policies/bulk-enable \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"ids":["nonexistent"]}' | python3 -m json.tool
```

Expected for the drift ack: JSON with `success:0`, `failed:1`, `failures:[{id:"nonexistent", error: "finding not found: nonexistent"}]`.

- [ ] **Step 4: Commit everything uncommitted**

```bash
git status
git add -p   # review any remaining changes
git commit -m "feat(bulk): TypeScript + build verified"
```

---

## Task 11 — `docs/BULK_OPERATIONS.md`

**Files:**
- Create: `docs/BULK_OPERATIONS.md`

- [ ] **Step 1: Create the file**

```markdown
# Bulk Operations

Operators managing large secret estates should never need one click per secret.
P5 adds multi-select checkboxes and batch action toolbars to three pages.

---

## Supported actions

| Page | Action | Endpoint | Safety gate |
|------|--------|----------|-------------|
| Secrets | Bulk rotate | `POST /api/secrets/bulk-rotate` | Confirm if count > 50 |
| Drift | Bulk acknowledge | `POST /api/drift/bulk-ack` | None |
| Drift | Bulk resolve | `POST /api/drift/bulk-resolve` | None |
| Policies | Bulk enable | `POST /api/policies/bulk-enable` | None |
| Policies | Bulk disable | `POST /api/policies/bulk-disable` | Always confirm |
| Policies | Bulk delete | `POST /api/policies/bulk-delete` | Always confirm |

---

## Request / response format

### Secrets

```http
POST /api/secrets/bulk-rotate
Authorization: Bearer <token>
Content-Type: application/json

{"names": ["db-password", "api-key", "tls-cert"]}
```

```json
{
  "success": 2,
  "failed": 1,
  "failures": [
    {"name": "tls-cert", "error": "not configured"}
  ]
}
```

### Drift

```http
POST /api/drift/bulk-ack
POST /api/drift/bulk-resolve
Authorization: Bearer <token>
Content-Type: application/json

{"ids": ["drift_stalesecret_db-password", "drift_missingsecret_api-key"]}
```

```json
{
  "success": 2,
  "failed": 0,
  "failures": []
}
```

### Policies

```http
POST /api/policies/bulk-enable
POST /api/policies/bulk-disable
POST /api/policies/bulk-delete
Authorization: Bearer <token>
Content-Type: application/json

{"ids": ["rule-abc", "rule-def"]}
```

```json
{
  "success": 1,
  "failed": 1,
  "failures": [
    {"id": "rule-def", "error": "rule not found: rule-def"}
  ]
}
```

---

## Failure behavior

- All batch endpoints are **non-aborting**: every item in the list is attempted regardless of prior failures.
- The response always includes the full `failures` array with one entry per failed item.
- The UI surfaces failures inline in the bulk toolbar after the call settles:
  `13 succeeded · 1 failed: database-password`
- A failed item is never silently dropped.

---

## Audit events

Every bulk action generates one audit event for the batch.

| Action | Event name | Details |
|--------|-----------|---------|
| Bulk rotate | `bulk.rotate` | `success=N failed=M` |

> Drift and policy bulk actions do not yet generate audit events. Each individual AcknowledgeFinding/ResolveFinding/EnableRule operation runs through the engine, which may have its own internal logging.

---

## Safety gates

| Condition | Behavior |
|-----------|----------|
| Bulk rotate > 50 secrets | Confirmation modal shown before dispatch |
| Bulk disable policies (any count) | Confirmation modal always shown |
| Bulk delete policies (any count) | Confirmation modal always shown |
| Bulk ack / resolve findings | No confirmation required |
| Bulk enable policies | No confirmation required |

---

## Known limits

- No upper bound on batch size is enforced server-side. The backend processes items sequentially (no parallelism within a batch).
- Very large batches (> 500 items) may cause the HTTP request to time out depending on provider response times. Split into smaller batches if needed.
- Selection is per-page: navigating to a new page does not clear the selection, but the header checkbox reflects the current page only.
```

- [ ] **Step 2: Commit**

```bash
git add docs/BULK_OPERATIONS.md
git commit -m "docs: BULK_OPERATIONS.md — endpoints, failure behavior, audit, limits"
```

---

## Self-review

**Spec coverage check:**

| Spec requirement | Task |
|-----------------|------|
| `useSelection` hook | Task 1 |
| Checkbox column + select page | Tasks 7, 8, 9 |
| Toolbar appears when selection.size > 0 | Tasks 7, 8, 9 |
| Rotate selected secrets | Task 7 |
| Acknowledge selected findings | Task 8 |
| Resolve selected findings | Task 8 |
| Enable selected policies | Task 9 |
| Disable selected policies | Task 9 |
| Delete selected policies | Task 9 |
| `POST /api/secrets/bulk-rotate` | Task 3 |
| `POST /api/drift/bulk-ack` + bulk-resolve | Task 4 |
| Policy bulk endpoints | Task 5 |
| Continue after failures, never abort | Tasks 3, 4, 5 |
| Progress feedback ("Rotating 14 secrets…") | Tasks 7, 8, 9 (toolbar status prop) |
| Partial failure display | Tasks 7, 8, 9 (toolbar status after settle) |
| Audit event for bulk.rotate | Task 3 (handleBulkRotate) |
| Confirm for delete | Task 9 |
| Confirm for disable | Task 9 |
| Confirm for bulk rotate > 50 | Task 7 |
| BULK_OPERATIONS.md | Task 11 |

All spec sections covered. No placeholders remain.
