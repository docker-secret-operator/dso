package scheduler

import (
	"sync"
	"time"
)

// SystemMetrics tracks overall scheduler metrics
type SystemMetrics struct {
	TotalJobs         int
	RunningJobs       int
	SuccessfulRuns    int
	FailedRuns        int
	AverageDuration   float64
	LastExecution     *time.Time
	WorkerUtilization float64
	ActiveWorkers     int
	QueuedJobs        int
	CompletedJobs     int
}

// MetricsCollector collects scheduler metrics
type MetricsCollector struct {
	scheduler  *Scheduler
	workerPool *WorkerPool
	mu         sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(scheduler *Scheduler, workerPool *WorkerPool) *MetricsCollector {
	return &MetricsCollector{
		scheduler:  scheduler,
		workerPool: workerPool,
	}
}

// GetMetrics returns system metrics
func (mc *MetricsCollector) GetMetrics() *SystemMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	jobs := mc.scheduler.List()
	runningCount := 0
	successCount := 0
	failedCount := 0
	totalDuration := int64(0)
	lastExecTime := (*time.Time)(nil)

	for _, job := range jobs {
		if job.Status == StatusRunning {
			runningCount++
		}

		if metrics := mc.scheduler.GetMetrics(job.ID); metrics != nil {
			successCount += metrics.SuccessfulRuns
			failedCount += metrics.FailedRuns
			if metrics.LastExecutionTime != nil && (lastExecTime == nil || metrics.LastExecutionTime.After(*lastExecTime)) {
				lastExecTime = metrics.LastExecutionTime
			}
		}
	}

	avgDuration := 0.0
	if successCount+failedCount > 0 {
		avgDuration = float64(totalDuration) / float64(successCount+failedCount)
	}

	workerStats := mc.workerPool.GetStats()
	activeWorkers := workerStats["active_workers"].(int)
	totalWorkers := workerStats["total_workers"].(int)
	utilization := 0.0
	if totalWorkers > 0 {
		utilization = float64(activeWorkers) / float64(totalWorkers) * 100
	}

	return &SystemMetrics{
		TotalJobs:         len(jobs),
		RunningJobs:       runningCount,
		SuccessfulRuns:    successCount,
		FailedRuns:        failedCount,
		AverageDuration:   avgDuration,
		LastExecution:     lastExecTime,
		WorkerUtilization: utilization,
		ActiveWorkers:     activeWorkers,
		QueuedJobs:        workerStats["queued_jobs"].(int),
		CompletedJobs:     workerStats["completed_jobs"].(int),
	}
}

// GetJobMetrics returns metrics for a specific job
func (mc *MetricsCollector) GetJobMetrics(jobID string) *JobMetrics {
	return mc.scheduler.GetMetrics(jobID)
}

// GetWorkerMetrics returns worker pool metrics
func (mc *MetricsCollector) GetWorkerMetrics() map[string]interface{} {
	return mc.workerPool.GetStats()
}
