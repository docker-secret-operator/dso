import { NextRequest, NextResponse } from 'next/server'

export const runtime = 'nodejs'

export async function GET(request: NextRequest) {
  try {
    // Fetch all required data from the backend
    const [containersRes, secretsRes, eventsRes] = await Promise.all([
      fetch('http://localhost:8471/api/discovery/docker', { cache: 'no-store' }),
      fetch('http://localhost:8471/api/secrets', { cache: 'no-store' }),
      fetch('http://localhost:8471/api/events', { cache: 'no-store' }),
    ])

    if (!containersRes.ok || !secretsRes.ok || !eventsRes.ok) {
      return NextResponse.json(
        { error: 'Failed to fetch required data' },
        { status: 503 }
      )
    }

    const containersData = await containersRes.json()
    const secretsData = await secretsRes.json()
    const eventsData = await eventsRes.json()

    const containers = containersData.containers || []
    const secrets = secretsData.secrets || []
    const events = eventsData.events || []

    // Build mappings from containers' DSO awareness
    const mappings: Array<{ container: string; secret: string }> = []
    containers.forEach((container: any) => {
      const managedSecrets = container.dso_awareness?.managed_secrets || []
      managedSecrets.forEach((secret: string) => {
        mappings.push({ container: container.name, secret })
      })
    })

    // Dynamically import and run drift & remediation functions
    const { detectDriftIssues } = await import('@/lib/drift-detection')
    const {
      generateRemediationPlans,
      generateRemediationSummary,
      estimateCumulativeImpact,
    } = await import('@/lib/remediation-planner')

    const issues = detectDriftIssues(containers, secrets, mappings, events)
    const plans = generateRemediationPlans(issues, containers, secrets, mappings)
    const summary = generateRemediationSummary(plans)
    const cumulativeImpact = estimateCumulativeImpact(plans)

    // Generate the report
    const report = {
      timestamp: new Date().toISOString(),
      summary,
      plans,
      cumulativeImpact,
      resources: {
        totalContainers: containers.length,
        managedContainers: containers.filter((c: any) => c.dso_awareness?.status === 'managed')
          .length,
        totalSecrets: secrets.length,
        totalMappings: mappings.length,
        totalEvents: events.length,
      },
    }

    return NextResponse.json(report, {
      headers: {
        'Content-Disposition': `attachment; filename="remediation-report-${new Date().toISOString().split('T')[0]}.json"`,
        'Content-Type': 'application/json',
      },
    })
  } catch (error) {
    console.error('Failed to generate remediation report:', error)
    return NextResponse.json(
      { error: 'Failed to generate remediation report' },
      { status: 500 }
    )
  }
}
