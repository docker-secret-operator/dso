import { apiFetch } from '../api-fetch'

/** Matches internal/server/rest.go ContainerSummary (GET /api/containers). */
export interface ContainerSummary {
  id: string
  strategy: string
  compose_path?: string
  secrets: string[]
  /** Phase 3: current-state only, from ReloaderController.IsDegraded -- does not survive an agent restart. */
  degraded: boolean
  degraded_reason?: string
  /**
   * Phase 4: safe Docker metadata from a single ContainerList call
   * (watcher.ListDockerContainerInfo). Omitted (undefined) when Docker no
   * longer reports this container -- render "Not available", never a
   * fabricated value.
   */
  name?: string
  image?: string
  /** Docker lifecycle state: running/exited/created/paused/restarting/dead. */
  status?: string
  /** Docker healthcheck state: healthy/unhealthy/starting. Absent (not "healthy") when no healthcheck is configured. */
  health?: string
  /** Published ports only, formatted "hostPort->containerPort/proto". */
  ports?: string[]
}

/** Matches internal/server/rest.go handleContainers response body. */
export interface ContainersResponse {
  containers: ContainerSummary[]
  total_count: number
}

export async function fetchContainers(): Promise<ContainersResponse> {
  const res = await apiFetch('/api/containers')
  if (!res.ok) {
    throw new Error(`Failed to load containers (${res.status})`)
  }
  return res.json()
}
