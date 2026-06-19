import { NextRequest, NextResponse } from 'next/server'

export const runtime = 'nodejs'

export async function GET(request: NextRequest) {
  try {
    const baseUrl = process.env.DSO_API_URL || 'http://localhost:8471'
    const [containersRes, secretsRes, eventsRes] = await Promise.all([
      fetch(`${baseUrl}/api/discovery/docker`, { cache: 'no-store' }),
      fetch(`${baseUrl}/api/secrets`, { cache: 'no-store' }),
      fetch(`${baseUrl}/api/events`, { cache: 'no-store' }),
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

    // Dynamically import and run drift detection
    // (this is server-side so we can import the library)
    const { detectDriftIssues, generateValidationSummary } = await import('@/lib/drift-detection')

    const issues = detectDriftIssues(containers, secrets, mappings, events)
    const summary = generateValidationSummary(issues)

    // Generate the report
    const report = {
      timestamp: new Date().toISOString(),
      summary,
      issues,
      resources: {
        totalContainers: containers.length,
        managedContainers: containers.filter((c: any) => c.dso_awareness?.status === 'managed').length,
        totalSecrets: secrets.length,
        totalMappings: mappings.length,
        totalEvents: events.length,
      },
    }

    return NextResponse.json(report, {
      headers: {
        'Content-Disposition': `attachment; filename="validation-report-${new Date().toISOString().split('T')[0]}.json"`,
        'Content-Type': 'application/json',
      },
    })
  } catch (error) {
    console.error('Failed to generate validation report:', error)
    return NextResponse.json(
      { error: 'Failed to generate validation report' },
      { status: 500 }
    )
  }
}
