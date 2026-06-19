import { describe, it, expect } from 'vitest'
import {
  hasPermission,
  canApprove,
  canReview,
  canOperate,
  canView,
} from '@/lib/auth/permissions'

describe('Auth Permissions', () => {
  describe('Role Hierarchy', () => {
    it('admin should have all permissions', () => {
      expect(canView('admin')).toBe(true)
      expect(canOperate('admin')).toBe(true)
      expect(canReview('admin')).toBe(true)
      expect(canApprove('admin')).toBe(true)
    })

    it('approver should have approve, review, operate, view', () => {
      expect(canView('approver')).toBe(true)
      expect(canOperate('approver')).toBe(true)
      expect(canReview('approver')).toBe(true)
      expect(canApprove('approver')).toBe(true)
    })

    it('reviewer should have review, operate, view', () => {
      expect(canView('reviewer')).toBe(true)
      expect(canOperate('reviewer')).toBe(true)
      expect(canReview('reviewer')).toBe(true)
      expect(canApprove('reviewer')).toBe(false)
    })

    it('operator should have operate, view', () => {
      expect(canView('operator')).toBe(true)
      expect(canOperate('operator')).toBe(true)
      expect(canReview('operator')).toBe(false)
      expect(canApprove('operator')).toBe(false)
    })

    it('viewer should only have view', () => {
      expect(canView('viewer')).toBe(true)
      expect(canOperate('viewer')).toBe(false)
      expect(canReview('viewer')).toBe(false)
      expect(canApprove('viewer')).toBe(false)
    })
  })

  describe('hasPermission', () => {
    it('should return true when role has permission', () => {
      expect(hasPermission('admin', 'view')).toBe(true)
      expect(hasPermission('operator', 'operate')).toBe(true)
    })

    it('should return false when role lacks permission', () => {
      expect(hasPermission('viewer', 'operate')).toBe(false)
      expect(hasPermission('operator', 'approve')).toBe(false)
    })

    it('should handle invalid roles gracefully', () => {
      expect(hasPermission('invalid' as any, 'view')).toBe(false)
    })
  })
})
