package scheduler

import "context"

// SchedulerStore handles job persistence
type SchedulerStore interface {
	CreateJob(ctx context.Context, job *Job) error
	UpdateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	ListJobs(ctx context.Context) ([]*Job, error)
	DeleteJob(ctx context.Context, id string) error
	ListJobsByStatus(ctx context.Context, status JobStatus) ([]*Job, error)
}

// ExecutionStore handles execution history
type ExecutionStore interface {
	LogExecution(ctx context.Context, exec *JobExecution) error
	GetExecutions(ctx context.Context, jobID string, limit int) ([]*JobExecution, error)
	CleanupOldExecutions(ctx context.Context, olderThan int) error
}
