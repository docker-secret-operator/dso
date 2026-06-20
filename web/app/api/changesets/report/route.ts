import { NextRequest, NextResponse } from 'next/server'
import { cookies } from 'next/headers'

export const runtime = 'nodejs'

export async function GET(request: NextRequest) {
  try {
    const cookieStore = await cookies()
    const token = cookieStore.get('dso_api_token')?.value

    if (!token) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const baseUrl = process.env.DSO_API_URL || 'http://localhost:8471'
    const [containersRes, secretsRes, eventsRes] = await Promise.all([
      fetch(`${baseUrl}/api/discovery/docker`, { cache: 'no-store' }),
      fetch(`${baseUrl}/api/secrets`, { cache: 'no-store' }),
      fetch(`${baseUrl}/api/events`, { cache: 'no-store' }),
    ])

    if (!containersRes.ok || !secretsRes.ok || !eventsRes.ok) {
      return NextResponse.json({ error: 'Failed to fetch data' }, { status: 503 })
    }

    const containersData = await containersRes.json()
    const secretsData = await secretsRes.json()
    const eventsData = await eventsRes.json()

    const containers = containersData.containers || []
    const secrets = secretsData.secrets || []
    const events = eventsData.events || []

    const mappings: Array<{ container: string; secret: string }> = []
    containers.forEach((container: any) => {
      const managedSecrets = container.dso_awareness?.managed_secrets || []
      managedSecrets.forEach((secret: string) => {
        mappings.push({ container: container.name, secret })
      })
    })

    const { detectDriftIssues } = await import('@/lib/drift-detection')
    const { generateRemediationPlans } = await import('@/lib/remediation-planner')
    const { generateChangeSets, generateChangeSetSummary } = await import('@/lib/change-set')

    const issues = detectDriftIssues(containers, secrets, mappings, events)
    const plans = generateRemediationPlans(issues, containers, secrets, mappings)
    const changeSets = generateChangeSets(plans, containers, secrets, mappings)
    const summary = generateChangeSetSummary(changeSets)

    return NextResponse.json(
      { timestamp: new Date().toISOString(), summary, changeSets },
      {
        headers: {
          'Content-Disposition': `attachment; filename="changesets-${new Date().toISOString().split('T')[0]}.json"`,
        },
      }
    )
  } catch (error) {
    if (process.env.NODE_ENV === 'development') {
      console.error('Failed to generate changesets report:', error)
    }
    return NextResponse.json({ error: 'Failed to generate report' }, { status: 500 })
  }
}
