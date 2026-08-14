'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'

// Static export has no server-side redirect mechanism (no middleware, no
// getServerSideProps), so `/` bounces to `/dashboard` client-side; the
// dashboard route itself (via AuthGuard, once ported) sends unauthenticated
// visitors on to `/login`.
export default function RootPage() {
  const router = useRouter()

  useEffect(() => {
    router.replace('/dashboard')
  }, [router])

  return <div className="min-h-screen bg-[#0B1020]" />
}
