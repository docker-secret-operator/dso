export function exportContainersToCSV(containers: any[]): string {
  if (containers.length === 0) return ''

  const headers = [
    'Container Name',
    'Image',
    'Status',
    'Classification',
    'Managed Secrets',
    'Missing Mappings',
  ]

  const rows = containers.map(c => [
    c.container_name,
    c.image,
    c.status,
    c.dso_awareness?.status ?? 'unmanaged',
    c.dso_awareness?.managed_secrets?.length ?? 0,
    c.dso_awareness?.missing_mappings?.length ?? 0,
  ])

  const csv = [
    headers.join(','),
    ...rows.map(row => row.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(',')),
  ].join('\n')

  return csv
}

export function exportContainersToJSON(containers: any[]): string {
  return JSON.stringify(containers, null, 2)
}

export function downloadExport(
  data: string,
  filename: string,
  mimeType: string
): void {
  const blob = new Blob([data], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}
