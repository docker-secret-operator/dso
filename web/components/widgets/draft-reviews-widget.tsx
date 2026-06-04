'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ArrowRight, CheckCircle2, AlertCircle, XCircle } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

export function DraftReviewsWidget() {
  // Simulated state for demo purposes
  const [reviews] = useState({
    pending: 2,
    approved: 1,
    rejected: 0,
  })

  const riskDistribution = {
    critical: 0,
    high: 1,
    medium: 1,
    low: 1,
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Draft Reviews</CardTitle>
          <CardDescription>Review and approval simulations</CardDescription>
        </div>
        <Link href="/review">
          <Button variant="outline" size="sm" className="gap-2">
            Open
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2 text-sm">
          <div className="flex justify-between items-center p-2 bg-yellow-50 rounded">
            <span className="text-yellow-700">Pending Review</span>
            <Badge variant="secondary" className="bg-yellow-100 text-yellow-900">
              {reviews.pending}
            </Badge>
          </div>
          <div className="flex justify-between items-center p-2 bg-green-50 rounded">
            <span className="text-green-700">Approved</span>
            <Badge variant="secondary" className="bg-green-100 text-green-900">
              {reviews.approved}
            </Badge>
          </div>
          <div className="flex justify-between items-center p-2 bg-red-50 rounded">
            <span className="text-red-700">Rejected</span>
            <Badge variant="secondary" className="bg-red-100 text-red-900">
              {reviews.rejected}
            </Badge>
          </div>
        </div>

        <div className="border-t pt-4">
          <p className="text-xs font-semibold text-gray-900 mb-3">Risk Distribution</p>
          <div className="space-y-2 text-xs">
            <div className="flex items-center justify-between">
              <span className="text-red-700">Critical</span>
              <Badge variant="outline" className="bg-red-100 text-red-800">
                {riskDistribution.critical}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-orange-700">High</span>
              <Badge variant="outline" className="bg-orange-100 text-orange-800">
                {riskDistribution.high}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-yellow-700">Medium</span>
              <Badge variant="outline" className="bg-yellow-100 text-yellow-800">
                {riskDistribution.medium}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-green-700">Low</span>
              <Badge variant="outline" className="bg-green-100 text-green-800">
                {riskDistribution.low}
              </Badge>
            </div>
          </div>
        </div>

        <p className="text-xs text-gray-600 italic">
          All review workflows exist in browser memory. No data is persisted.
        </p>
      </CardContent>
    </Card>
  )
}
