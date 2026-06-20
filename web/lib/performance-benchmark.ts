/**
 * Performance Benchmark Harness
 *
 * Measures actual performance across all operations with synthetic data.
 * Generates small (100), medium (500), and large (1000) container scenarios.
 */

/* ============================================================================
   SYNTHETIC DATA GENERATION
   ============================================================================ */

export interface BenchmarkScenario {
  name: string
  containers: number
  secrets: number
  events: number
}

export interface BenchmarkResult {
  operation: string
  scenario: string
  durationMs: number
  memoryHeapMb: number
  memoryExternalMb: number
}

export interface BenchmarkContainer {
  id: string
  name: string
  image: string
  status: 'running' | 'stopped'
  environment_variable_names: string[]
  dso_awareness: {
    status: 'managed' | 'unmanaged'
    managed_secrets: string[]
  }
}

export interface BenchmarkSecret {
  id: string
  name: string
  provider: string
  status: 'error' | 'active'
  last_rotated: string
  next_rotation: string
}

export interface BenchmarkEvent {
  id: string
  timestamp: string
  action: string
  severity: 'info' | 'warning' | 'error'
  message: string
}

// Synthetic environment generators
export const SCENARIOS: BenchmarkScenario[] = [
  { name: 'Small', containers: 100, secrets: 50, events: 100 },
  { name: 'Medium', containers: 500, secrets: 200, events: 500 },
  { name: 'Large', containers: 1000, secrets: 500, events: 1000 },
]

export function generateContainers(count: number): BenchmarkContainer[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `container-${i}`,
    name: `app-${i % 10}-${Math.floor(i / 10)}`,
    image: `image:${i}`,
    status: i % 3 === 0 ? 'running' : 'stopped',
    environment_variable_names: Array.from(
      { length: Math.floor(Math.random() * 5) + 2 },
      (_, j) => {
        const names = ['DB_PASSWORD', 'API_KEY', 'SECRET_TOKEN', 'AUTH_KEY', 'ENCRYPTION_KEY']
        return names[j % names.length]
      }
    ),
    dso_awareness: {
      status: i % 2 === 0 ? 'managed' : 'unmanaged',
      managed_secrets: Array.from(
        { length: Math.floor(Math.random() * 3) },
        (_, j) => `secret-${i}-${j}`
      ),
    },
  }))
}

export function generateSecrets(count: number): BenchmarkSecret[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `secret-${i}`,
    name: `secret-${i}`,
    provider: ['vault', 'consul', 'aws-secrets'][i % 3],
    status: i % 5 === 0 ? 'error' : 'active',
    last_rotated: new Date(Date.now() - Math.random() * 30 * 24 * 60 * 60 * 1000).toISOString(),
    next_rotation: new Date(Date.now() + Math.random() * 7 * 24 * 60 * 60 * 1000).toISOString(),
  }))
}

export function generateEvents(count: number): BenchmarkEvent[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `event-${i}`,
    timestamp: new Date(Date.now() - (count - i) * 60 * 1000).toISOString(),
    action: ['rotate', 'create', 'delete', 'update'][i % 4],
    status: i % 20 === 0 ? 'failure' : 'success',
    error: i % 20 === 0 ? 'Rotation timeout' : undefined,
    message: `Event ${i}`,
  }))
}

/* ============================================================================
   TIMING UTILITIES
   ============================================================================ */

interface TimedResult<T> {
  result: T
  durationMs: number
  memoryHeap: number
}

export function measureTime<T>(
  fn: () => T,
  label?: string
): TimedResult<T> {
  const startMem = process.memoryUsage().heapUsed
  const startTime = performance.now()

  const result = fn()

  const endTime = performance.now()
  const endMem = process.memoryUsage().heapUsed
  const durationMs = endTime - startTime
  const memoryHeap = (endMem - startMem) / 1024 / 1024 // Convert to MB

  if (label && process.env.NODE_ENV === 'development') {
    console.log(`${label}: ${durationMs.toFixed(2)}ms (Heap: ${memoryHeap.toFixed(2)}MB)`)
  }

  return { result, durationMs, memoryHeap }
}

/* ============================================================================
   DRIFT DETECTION BENCHMARKS
   ============================================================================ */

import { detectDriftIssues } from './drift-detection'

export function benchmarkDriftDetection(
  containers: BenchmarkContainer[],
  secrets: BenchmarkSecret[],
  mappings: Array<{ container: string; secret: string }>,
  events: BenchmarkEvent[]
) {
  return measureTime(
    () => detectDriftIssues(containers, secrets, mappings, events),
    'Drift Detection'
  )
}

/* ============================================================================
   REMEDIATION PLANNING BENCHMARKS
   ============================================================================ */

import { generateRemediationPlans } from './remediation-planner'

export function benchmarkRemediationPlanning(
  driftIssues: Array<Record<string, unknown>>,
  containers: BenchmarkContainer[],
  secrets: BenchmarkSecret[],
  mappings: Array<{ container: string; secret: string }>
) {
  return measureTime(
    () => generateRemediationPlans(driftIssues, containers, secrets, mappings),
    'Remediation Planning'
  )
}

/* ============================================================================
   CHANGE SETS BENCHMARKS
   ============================================================================ */

import { generateChangeSets } from './change-set'

export function benchmarkChangeSetGeneration(
  remediationPlans: Array<Record<string, unknown>>,
  containers: BenchmarkContainer[],
  secrets: BenchmarkSecret[],
  mappings: Array<{ container: string; secret: string }>
) {
  return measureTime(
    () => generateChangeSets(remediationPlans, containers, secrets, mappings),
    'Change Set Generation'
  )
}

/* ============================================================================
   WORKSPACE VALIDATION BENCHMARKS
   ============================================================================ */

import {
  validateDraftConfiguration,
  generateValidationSummary,
} from './workspace-validation'
import { createWorkspace } from './workspace'

export function benchmarkWorkspaceValidation(
  containers: BenchmarkContainer[],
  secrets: BenchmarkSecret[],
  mappings: Array<{ container: string; secret: string }>
) {
  const workspace = createWorkspace()

  const validationResult = measureTime(
    () => validateDraftConfiguration(workspace, containers, secrets, mappings),
    'Workspace Validation'
  )

  const summaryResult = measureTime(
    () => generateValidationSummary(validationResult.result),
    'Validation Summary'
  )

  return {
    validation: validationResult,
    summary: summaryResult,
  }
}

/* ============================================================================
   REVIEW WORKFLOW BENCHMARKS
   ============================================================================ */

import {
  createDraftReview,
  calculateRiskAssessment,
  generateChecklist,
} from './review-workflow'

export function benchmarkReviewWorkflow(
  workspace: Record<string, unknown>,
  validationResults: Array<Record<string, unknown>>,
  containers: BenchmarkContainer[],
  secrets: BenchmarkSecret[],
  mappings: Array<{ container: string; secret: string }>
) {
  const checklistResult = measureTime(
    () => generateChecklist(validationResults),
    'Review Checklist'
  )

  const riskResult = measureTime(
    () => calculateRiskAssessment(workspace, validationResults),
    'Risk Assessment'
  )

  const reviewResult = measureTime(
    () => createDraftReview(workspace, validationResults),
    'Review Creation'
  )

  return {
    checklist: checklistResult,
    risk: riskResult,
    review: reviewResult,
  }
}

/* ============================================================================
   COMPREHENSIVE BENCHMARK SUITE
   ============================================================================ */

export interface ComprehensiveBenchmarkResults {
  scenario: string
  timestamp: string
  measurements: {
    driftDetection: TimedResult<any>
    remediationPlanning: TimedResult<any>
    changeSetGeneration: TimedResult<any>
    workspaceValidation: {
      validation: TimedResult<any>
      summary: TimedResult<any>
    }
    reviewWorkflow: {
      checklist: TimedResult<any>
      risk: TimedResult<any>
      review: TimedResult<any>
    }
  }
  memoryPeak: number
}

export function runComprehensiveBenchmark(scenario: BenchmarkScenario): ComprehensiveBenchmarkResults {
  if (process.env.NODE_ENV === 'development') {
    console.log(`\n${'='.repeat(80)}`)
    console.log(`BENCHMARK: ${scenario.name} Environment`)
    console.log(`Containers: ${scenario.containers}, Secrets: ${scenario.secrets}, Events: ${scenario.events}`)
    console.log(`${'='.repeat(80)}\n`)
  }

  // Generate synthetic data
  const containers = generateContainers(scenario.containers)
  const secrets = generateSecrets(scenario.secrets)
  const events = generateEvents(scenario.events)

  // Build mappings from container DSO awareness
  const mappings: Array<{ container: string; secret: string }> = []
  containers.forEach((c) => {
    c.dso_awareness?.managed_secrets?.forEach((s: string) => {
      mappings.push({ container: c.name, secret: s })
    })
  })

  // Run benchmarks
  const startMem = process.memoryUsage().heapUsed

  const driftResult = benchmarkDriftDetection(containers, secrets, mappings, events)

  const remediationResult = benchmarkRemediationPlanning(
    driftResult.result,
    containers,
    secrets,
    mappings
  )

  const changeSetResult = benchmarkChangeSetGeneration(
    remediationResult.result,
    containers,
    secrets,
    mappings
  )

  const workspace = createWorkspace()
  const workspaceResult = benchmarkWorkspaceValidation(containers, secrets, mappings)

  const reviewResult = benchmarkReviewWorkflow(
    workspace,
    workspaceResult.validation.result,
    containers,
    secrets,
    mappings
  )

  const endMem = process.memoryUsage().heapUsed
  const memoryPeak = (endMem - startMem) / 1024 / 1024

  if (process.env.NODE_ENV === 'development') {
    console.log(`\nPeak Memory Delta: ${memoryPeak.toFixed(2)}MB`)
    console.log(`${'='.repeat(80)}\n`)
  }

  return {
    scenario: scenario.name,
    timestamp: new Date().toISOString(),
    measurements: {
      driftDetection: driftResult,
      remediationPlanning: remediationResult,
      changeSetGeneration: changeSetResult,
      workspaceValidation: workspaceResult,
      reviewWorkflow: reviewResult,
    },
    memoryPeak,
  }
}

/* ============================================================================
   REGRESSION DETECTION
   ============================================================================ */

export interface RegressionAnalysis {
  operation: string
  durationMs: number
  isRegression: boolean
  threshold: number
  message: string
}

const THRESHOLDS: Record<string, number> = {
  'Drift Detection': 100,
  'Workspace Validation': 100,
  'Risk Assessment': 50,
  'Change Set Generation': 100,
  'Remediation Planning': 200,
  'Review Creation': 50,
  'Validation Summary': 50,
  'Review Checklist': 20,
}

export function analyzeRegressions(results: ComprehensiveBenchmarkResults): RegressionAnalysis[] {
  const analyses: RegressionAnalysis[] = []

  const measurements = results.measurements

  // Check drift detection
  analyses.push({
    operation: 'Drift Detection',
    durationMs: measurements.driftDetection.durationMs,
    isRegression: measurements.driftDetection.durationMs > THRESHOLDS['Drift Detection'],
    threshold: THRESHOLDS['Drift Detection'],
    message: `${measurements.driftDetection.durationMs.toFixed(2)}ms ${
      measurements.driftDetection.durationMs > THRESHOLDS['Drift Detection']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  // Check workspace validation
  analyses.push({
    operation: 'Workspace Validation',
    durationMs: measurements.workspaceValidation.validation.durationMs,
    isRegression:
      measurements.workspaceValidation.validation.durationMs >
      THRESHOLDS['Workspace Validation'],
    threshold: THRESHOLDS['Workspace Validation'],
    message: `${measurements.workspaceValidation.validation.durationMs.toFixed(2)}ms ${
      measurements.workspaceValidation.validation.durationMs > THRESHOLDS['Workspace Validation']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  // Check risk assessment
  analyses.push({
    operation: 'Risk Assessment',
    durationMs: measurements.reviewWorkflow.risk.durationMs,
    isRegression: measurements.reviewWorkflow.risk.durationMs > THRESHOLDS['Risk Assessment'],
    threshold: THRESHOLDS['Risk Assessment'],
    message: `${measurements.reviewWorkflow.risk.durationMs.toFixed(2)}ms ${
      measurements.reviewWorkflow.risk.durationMs > THRESHOLDS['Risk Assessment']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  // Check change set generation
  analyses.push({
    operation: 'Change Set Generation',
    durationMs: measurements.changeSetGeneration.durationMs,
    isRegression:
      measurements.changeSetGeneration.durationMs > THRESHOLDS['Change Set Generation'],
    threshold: THRESHOLDS['Change Set Generation'],
    message: `${measurements.changeSetGeneration.durationMs.toFixed(2)}ms ${
      measurements.changeSetGeneration.durationMs > THRESHOLDS['Change Set Generation']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  // Check remediation planning
  analyses.push({
    operation: 'Remediation Planning',
    durationMs: measurements.remediationPlanning.durationMs,
    isRegression: measurements.remediationPlanning.durationMs > THRESHOLDS['Remediation Planning'],
    threshold: THRESHOLDS['Remediation Planning'],
    message: `${measurements.remediationPlanning.durationMs.toFixed(2)}ms ${
      measurements.remediationPlanning.durationMs > THRESHOLDS['Remediation Planning']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  // Check review creation
  analyses.push({
    operation: 'Review Creation',
    durationMs: measurements.reviewWorkflow.review.durationMs,
    isRegression: measurements.reviewWorkflow.review.durationMs > THRESHOLDS['Review Creation'],
    threshold: THRESHOLDS['Review Creation'],
    message: `${measurements.reviewWorkflow.review.durationMs.toFixed(2)}ms ${
      measurements.reviewWorkflow.review.durationMs > THRESHOLDS['Review Creation']
        ? '❌ REGRESSION'
        : '✅ PASS'
    }`,
  })

  return analyses
}
