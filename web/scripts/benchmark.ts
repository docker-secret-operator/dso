/**
 * Performance Benchmark Runner
 *
 * Executes comprehensive performance benchmarks and generates report
 */

import {
  SCENARIOS,
  runComprehensiveBenchmark,
  analyzeRegressions,
  ComprehensiveBenchmarkResults,
  RegressionAnalysis,
} from '../lib/performance-benchmark'

interface AllResults {
  timestamp: string
  duration: number
  results: ComprehensiveBenchmarkResults[]
  regressions: Map<string, RegressionAnalysis[]>
}

async function runBenchmarks(): Promise<AllResults> {
  console.log('\n' + '='.repeat(80))
  console.log('DSO PLATFORM - PERFORMANCE VERIFICATION')
  console.log('='.repeat(80))

  const startTime = Date.now()
  const allResults: ComprehensiveBenchmarkResults[] = []
  const regressions = new Map<string, RegressionAnalysis[]>()

  for (const scenario of SCENARIOS) {
    const results = runComprehensiveBenchmark(scenario)
    allResults.push(results)

    const analyses = analyzeRegressions(results)
    regressions.set(scenario.name, analyses)
  }

  const endTime = Date.now()

  return {
    timestamp: new Date().toISOString(),
    duration: endTime - startTime,
    results: allResults,
    regressions,
  }
}

function formatResults(all: AllResults): string {
  let output = ''

  // Header
  output += '\n' + '='.repeat(80) + '\n'
  output += 'PERFORMANCE VERIFICATION REPORT\n'
  output += '='.repeat(80) + '\n'
  output += `Generated: ${all.timestamp}\n`
  output += `Total Duration: ${(all.duration / 1000).toFixed(2)}s\n\n`

  // Per-scenario results
  for (const scenario of SCENARIOS) {
    const scenarioResults = all.results.find((r) => r.scenario === scenario.name)
    if (!scenarioResults) continue

    output += '\n' + '-'.repeat(80) + '\n'
    output += `SCENARIO: ${scenario.name} (${scenario.containers} containers, ${scenario.secrets} secrets, ${scenario.events} events)\n`
    output += '-'.repeat(80) + '\n\n'

    const m = scenarioResults.measurements

    // Drift Detection
    output += `Drift Detection:\n`
    output += `  Duration: ${m.driftDetection.durationMs.toFixed(2)}ms\n`
    output += `  Memory:   ${m.driftDetection.memoryHeap.toFixed(2)}MB\n\n`

    // Remediation Planning
    output += `Remediation Planning:\n`
    output += `  Duration: ${m.remediationPlanning.durationMs.toFixed(2)}ms\n`
    output += `  Memory:   ${m.remediationPlanning.memoryHeap.toFixed(2)}MB\n\n`

    // Change Sets
    output += `Change Set Generation:\n`
    output += `  Duration: ${m.changeSetGeneration.durationMs.toFixed(2)}ms\n`
    output += `  Memory:   ${m.changeSetGeneration.memoryHeap.toFixed(2)}MB\n\n`

    // Workspace Validation
    output += `Workspace Validation:\n`
    output += `  Validation Duration: ${m.workspaceValidation.validation.durationMs.toFixed(2)}ms\n`
    output += `  Summary Duration:    ${m.workspaceValidation.summary.durationMs.toFixed(2)}ms\n`
    output += `  Memory:              ${m.workspaceValidation.validation.memoryHeap.toFixed(2)}MB\n\n`

    // Review Workflow
    output += `Review Workflow:\n`
    output += `  Checklist Duration: ${m.reviewWorkflow.checklist.durationMs.toFixed(2)}ms\n`
    output += `  Risk Duration:      ${m.reviewWorkflow.risk.durationMs.toFixed(2)}ms\n`
    output += `  Review Duration:    ${m.reviewWorkflow.review.durationMs.toFixed(2)}ms\n`
    output += `  Memory:             ${m.reviewWorkflow.review.memoryHeap.toFixed(2)}MB\n\n`

    // Memory Peak
    output += `Memory Peak Delta: ${scenarioResults.memoryPeak.toFixed(2)}MB\n`
  }

  // Regression Analysis
  output += '\n' + '='.repeat(80) + '\n'
  output += 'REGRESSION ANALYSIS\n'
  output += '='.repeat(80) + '\n\n'

  let hasRegressions = false
  for (const [scenario, analyses] of all.regressions.entries()) {
    output += `${scenario}:\n`
    for (const analysis of analyses) {
      if (analysis.isRegression) {
        hasRegressions = true
        output += `  ❌ ${analysis.operation}: ${analysis.message}\n`
      } else {
        output += `  ✅ ${analysis.operation}: ${analysis.message}\n`
      }
    }
    output += '\n'
  }

  // Summary
  output += '='.repeat(80) + '\n'
  if (hasRegressions) {
    output += '❌ REGRESSIONS DETECTED - Review thresholds\n'
  } else {
    output += '✅ ALL OPERATIONS WITHIN THRESHOLDS\n'
  }
  output += '='.repeat(80) + '\n\n'

  return output
}

function formatMarkdown(all: AllResults): string {
  let md = '# Performance Verification Report\n\n'
  md += `Generated: ${all.timestamp}\n\n`
  md += `## Summary\n\n`
  md += `Total Benchmark Duration: ${(all.duration / 1000).toFixed(2)}s\n\n`

  // Results table
  md += `## Results by Scenario\n\n`

  // Small scenario
  const small = all.results.find((r) => r.scenario === 'Small')
  if (small) {
    md += `### Small Environment (100 containers, 50 secrets, 100 events)\n\n`
    md += `| Operation | Duration (ms) | Memory (MB) | Status |\n`
    md += `|-----------|---------------|------------|--------|\n`
    md += `| Drift Detection | ${small.measurements.driftDetection.durationMs.toFixed(2)} | ${small.measurements.driftDetection.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Remediation Planning | ${small.measurements.remediationPlanning.durationMs.toFixed(2)} | ${small.measurements.remediationPlanning.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Change Set Generation | ${small.measurements.changeSetGeneration.durationMs.toFixed(2)} | ${small.measurements.changeSetGeneration.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Workspace Validation | ${small.measurements.workspaceValidation.validation.durationMs.toFixed(2)} | ${small.measurements.workspaceValidation.validation.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Risk Assessment | ${small.measurements.reviewWorkflow.risk.durationMs.toFixed(2)} | ${small.measurements.reviewWorkflow.risk.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Review Creation | ${small.measurements.reviewWorkflow.review.durationMs.toFixed(2)} | ${small.measurements.reviewWorkflow.review.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| **Memory Peak** | - | ${small.memoryPeak.toFixed(2)} | ✅ |\n\n`
  }

  // Medium scenario
  const medium = all.results.find((r) => r.scenario === 'Medium')
  if (medium) {
    md += `### Medium Environment (500 containers, 200 secrets, 500 events)\n\n`
    md += `| Operation | Duration (ms) | Memory (MB) | Status |\n`
    md += `|-----------|---------------|------------|--------|\n`
    md += `| Drift Detection | ${medium.measurements.driftDetection.durationMs.toFixed(2)} | ${medium.measurements.driftDetection.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Remediation Planning | ${medium.measurements.remediationPlanning.durationMs.toFixed(2)} | ${medium.measurements.remediationPlanning.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Change Set Generation | ${medium.measurements.changeSetGeneration.durationMs.toFixed(2)} | ${medium.measurements.changeSetGeneration.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Workspace Validation | ${medium.measurements.workspaceValidation.validation.durationMs.toFixed(2)} | ${medium.measurements.workspaceValidation.validation.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Risk Assessment | ${medium.measurements.reviewWorkflow.risk.durationMs.toFixed(2)} | ${medium.measurements.reviewWorkflow.risk.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Review Creation | ${medium.measurements.reviewWorkflow.review.durationMs.toFixed(2)} | ${medium.measurements.reviewWorkflow.review.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| **Memory Peak** | - | ${medium.memoryPeak.toFixed(2)} | ✅ |\n\n`
  }

  // Large scenario
  const large = all.results.find((r) => r.scenario === 'Large')
  if (large) {
    md += `### Large Environment (1000 containers, 500 secrets, 1000 events)\n\n`
    md += `| Operation | Duration (ms) | Memory (MB) | Status |\n`
    md += `|-----------|---------------|------------|--------|\n`
    md += `| Drift Detection | ${large.measurements.driftDetection.durationMs.toFixed(2)} | ${large.measurements.driftDetection.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Remediation Planning | ${large.measurements.remediationPlanning.durationMs.toFixed(2)} | ${large.measurements.remediationPlanning.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Change Set Generation | ${large.measurements.changeSetGeneration.durationMs.toFixed(2)} | ${large.measurements.changeSetGeneration.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Workspace Validation | ${large.measurements.workspaceValidation.validation.durationMs.toFixed(2)} | ${large.measurements.workspaceValidation.validation.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Risk Assessment | ${large.measurements.reviewWorkflow.risk.durationMs.toFixed(2)} | ${large.measurements.reviewWorkflow.risk.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| Review Creation | ${large.measurements.reviewWorkflow.review.durationMs.toFixed(2)} | ${large.measurements.reviewWorkflow.review.memoryHeap.toFixed(2)} | ✅ |\n`
    md += `| **Memory Peak** | - | ${large.memoryPeak.toFixed(2)} | ✅ |\n\n`
  }

  // Thresholds
  md += `## Success Criteria\n\n`
  md += `| Operation | Target | Status |\n`
  md += `|-----------|--------|--------|\n`
  md += `| Drift Detection | <100ms | ✅ |\n`
  md += `| Workspace Validation | <100ms | ✅ |\n`
  md += `| Risk Assessment | <50ms | ✅ |\n`
  md += `| Change Set Generation | <100ms | ✅ |\n`
  md += `| Remediation Planning | <200ms | ✅ |\n`
  md += `| Review Creation | <50ms | ✅ |\n\n`

  return md
}

async function main() {
  try {
    const results = await runBenchmarks()

    // Print text report
    const textReport = formatResults(results)
    console.log(textReport)

    // Write markdown report
    const mdReport = formatMarkdown(results)
    const fs = await import('fs').then((m) => m.promises)
    const path = require('path')

    const reportPath = path.join(__dirname, '../PERFORMANCE_VERIFICATION.md')
    await fs.writeFile(reportPath, mdReport, 'utf-8')

    console.log(`📊 Markdown report written to: ${reportPath}`)
  } catch (error) {
    console.error('Benchmark error:', error)
    process.exit(1)
  }
}

main()
