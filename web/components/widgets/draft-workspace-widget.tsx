'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ArrowRight, FileText } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'
import { createWorkspace, getWorkspaceSummary } from '@/lib/workspace'

export function DraftWorkspaceWidget() {
  const [workspace] = useState(() => createWorkspace())
  const summary = getWorkspaceSummary(workspace)

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Draft Workspace</CardTitle>
          <CardDescription>Browser-based configuration simulator</CardDescription>
        </div>
        <Link href="/workspace">
          <Button variant="outline" size="sm" className="gap-2">
            Open
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3 p-3 bg-blue-50 rounded border border-blue-200">
          <FileText className="w-5 h-5 text-blue-600 flex-shrink-0" />
          <div>
            <p className="text-sm font-semibold text-blue-900">
              {summary.isModified ? 'Draft in progress' : 'Ready for changes'}
            </p>
            <p className="text-xs text-blue-700">
              {summary.changeCount > 0
                ? `${summary.changeCount} changes pending`
                : 'No pending changes'}
            </p>
          </div>
        </div>

        {summary.isModified && (
          <div className="space-y-2 text-sm">
            <div className="flex justify-between items-center">
              <span className="text-gray-600">Mappings:</span>
              <span className="font-semibold text-gray-900">{summary.mappingCount}</span>
            </div>
            {summary.addedMappings > 0 && (
              <div className="text-xs text-green-700">
                <Badge variant="secondary" className="text-xs">
                  +{summary.addedMappings}
                </Badge>
              </div>
            )}
            {summary.removedMappings > 0 && (
              <div className="text-xs text-red-700">
                <Badge variant="secondary" className="text-xs">
                  -{summary.removedMappings}
                </Badge>
              </div>
            )}
          </div>
        )}

        <p className="text-xs text-gray-600 italic">
          All changes exist only in browser memory. Export to download draft.
        </p>
      </CardContent>
    </Card>
  )
}
