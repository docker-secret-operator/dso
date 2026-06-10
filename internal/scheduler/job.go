package scheduler

import (
	"time"
)

// JobType defines the type of job
type JobType string

const (
	OneTimeJob   JobType = "one_time"
	RecurringJob JobType = "recurring"
	IntervalJob  JobType = "interval"
	DelayedJob   JobType = "delayed"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	StatusPending  JobStatus = "pending"
	StatusRunning  JobStatus = "running"
	StatusSuccess  JobStatus = "success"
	StatusFailed   JobStatus = "failed"
	StatusPaused   JobStatus = "paused"
	StatusDisabled JobStatus = "disabled"
)

// Job represents a scheduled job
type Job struct {
	ID         string
	Name       string
	Type       JobType
	Enabled    bool
	Schedule   string // Cron expression for recurring jobs
	Interval   time.Duration
	NextRun    time.Time
	LastRun    *time.Time
	RetryCount int
	MaxRetries int
	Timeout    time.Duration
	Status     JobStatus
	Metadata   map[string]string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Handler is the function signature for job execution
type Handler func() error

// JobExecution tracks execution history
type JobExecution struct {
	ID        string
	JobID     string
	Success   bool
	DurationMs int64
	Error     *string
	CreatedAt time.Time
}

// JobMetrics tracks job statistics
type JobMetrics struct {
	JobID              string
	TotalRuns          int
	SuccessfulRuns     int
	FailedRuns         int
	AverageDurationMs  float64
	LastExecutionTime  *time.Time
	LastSuccessTime    *time.Time
	LastFailureTime    *time.Time
	CurrentRetryCount  int
}
