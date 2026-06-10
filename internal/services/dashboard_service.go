package services

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// DashboardService provides aggregated workflow data for dashboards
type DashboardService struct {
	draftService    *DraftService
	reviewService   *ReviewService
	approvalService *ApprovalService
	auditService    *AuditService
}

// NewDashboardService creates a new dashboard service
func NewDashboardService(
	draftService *DraftService,
	reviewService *ReviewService,
	approvalService *ApprovalService,
	auditService *AuditService,
) *DashboardService {
	return &DashboardService{
		draftService:    draftService,
		reviewService:   reviewService,
		approvalService: approvalService,
		auditService:    auditService,
	}
}

// WorkflowOverview provides high-level workflow metrics
type WorkflowOverview struct {
	DraftStats      DraftStats    `json:"draft_stats"`
	ReviewStats     ReviewStats   `json:"review_stats"`
	ApprovalStats   ApprovalStats `json:"approval_stats"`
	AuditEventCount int           `json:"audit_event_count"`
	LastActivity    time.Time     `json:"last_activity"`
	HealthStatus    string        `json:"health_status"` // healthy, warning, critical
}

// DraftStats provides draft metrics
type DraftStats struct {
	Total       int `json:"total"`
	Draft       int `json:"draft"`
	UnderReview int `json:"under_review"`
	Approved    int `json:"approved"`
	Rejected    int `json:"rejected"`
	Archived    int `json:"archived"`
}

// ReviewStats provides review metrics
type ReviewStats struct {
	Total  int `json:"total"`
	Draft  int `json:"draft"`
	Active int `json:"under_review"`
	Closed int `json:"closed"`
}

// ApprovalStats provides approval metrics
type ApprovalStats struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
	Expired  int `json:"expired"`
	Closed   int `json:"closed"`
}

// GetWorkflowOverview returns high-level workflow metrics
func (ds *DashboardService) GetWorkflowOverview(ctx context.Context) (*WorkflowOverview, error) {
	overview := &WorkflowOverview{
		DraftStats:    DraftStats{},
		ReviewStats:   ReviewStats{},
		ApprovalStats: ApprovalStats{},
		HealthStatus:  "healthy",
	}

	// Get draft stats
	drafts, _ := ds.draftService.ListDrafts(ctx, "")
	if drafts != nil {
		overview.DraftStats.Total = len(drafts)
		for _, draft := range drafts {
			switch draft.Status {
			case "draft":
				overview.DraftStats.Draft++
			case "under_review":
				overview.DraftStats.UnderReview++
			case "approved":
				overview.DraftStats.Approved++
			case "rejected":
				overview.DraftStats.Rejected++
			case "archived":
				overview.DraftStats.Archived++
			}
		}
	}

	// Get review stats
	reviews, _ := ds.reviewService.ListReviews(ctx)
	if reviews != nil {
		overview.ReviewStats.Total = len(reviews)
		for _, review := range reviews {
			switch review.Status {
			case "draft":
				overview.ReviewStats.Draft++
			case "under_review":
				overview.ReviewStats.Active++
			case "closed":
				overview.ReviewStats.Closed++
			}
		}
	}

	// Get approval stats (not available via service, skip for now)
	// Note: Full approval list not exposed via service - would need direct store access
	overview.ApprovalStats.Total = 0
	if false { // Placeholder for future implementation
		var approvals []*storage.Approval
		_ = approvals
		for _, approval := range approvals {
			switch approval.Decision {
			case "pending":
				overview.ApprovalStats.Pending++
			case "approved":
				overview.ApprovalStats.Approved++
			case "rejected":
				overview.ApprovalStats.Rejected++
			case "expired":
				overview.ApprovalStats.Expired++
			case "closed":
				overview.ApprovalStats.Closed++
			}
		}
	}

	// Set health status
	if overview.ReviewStats.Active > 100 {
		overview.HealthStatus = "warning"
	}
	if overview.ApprovalStats.Pending > 200 {
		overview.HealthStatus = "critical"
	}

	overview.LastActivity = time.Now()

	return overview, nil
}

// WorkflowMetrics provides workflow performance metrics
type WorkflowMetrics struct {
	AverageDraftDuration    int64   `json:"avg_draft_duration_minutes"`
	AverageReviewDuration   int64   `json:"avg_review_duration_minutes"`
	AverageApprovalDuration int64   `json:"avg_approval_duration_minutes"`
	OpenReviews             int     `json:"open_reviews"`
	PendingApprovals        int     `json:"pending_approvals"`
	ApprovalSuccessRate     float64 `json:"approval_success_rate"`    // 0-100%
	WorkflowCompletionRate  float64 `json:"workflow_completion_rate"` // 0-100%
	TotalCompletedWorkflows int     `json:"total_completed_workflows"`
	TotalActiveWorkflows    int     `json:"total_active_workflows"`
}

// GetWorkflowMetrics returns workflow performance metrics
func (ds *DashboardService) GetWorkflowMetrics(ctx context.Context) (*WorkflowMetrics, error) {
	metrics := &WorkflowMetrics{
		AverageDraftDuration:    0,
		AverageReviewDuration:   0,
		AverageApprovalDuration: 0,
	}

	// Get draft stats
	drafts, _ := ds.draftService.ListDrafts(ctx, "")
	if drafts != nil {
		openCount := 0
		completedCount := 0
		for _, draft := range drafts {
			if draft.Status == "draft" || draft.Status == "under_review" {
				openCount++
			} else if draft.Status == "approved" || draft.Status == "rejected" {
				completedCount++
			}
		}
		metrics.TotalActiveWorkflows = openCount
		metrics.TotalCompletedWorkflows = completedCount
	}

	// Get review stats
	reviews, _ := ds.reviewService.ListReviews(ctx)
	if reviews != nil {
		for _, review := range reviews {
			if review.Status == "under_review" {
				metrics.OpenReviews++
			}
		}
	}

	// Get approval stats (not available via service, skip for now)
	// Note: Full approval list not exposed via service
	approvedCount := 0
	rejectedCount := 0
	pendingCount := 0

	if false { // Placeholder
		var approvals []*storage.Approval
		for _, approval := range approvals {
			switch approval.Decision {
			case "approved":
				approvedCount++
			case "rejected":
				rejectedCount++
			case "pending":
				pendingCount++
			}
		}
	}

	metrics.PendingApprovals = pendingCount

	// Calculate success rate
	total := approvedCount + rejectedCount
	if total > 0 {
		metrics.ApprovalSuccessRate = (float64(approvedCount) / float64(total)) * 100
	}

	// Calculate completion rate
	if metrics.TotalCompletedWorkflows+metrics.TotalActiveWorkflows > 0 {
		total := metrics.TotalCompletedWorkflows + metrics.TotalActiveWorkflows
		metrics.WorkflowCompletionRate = (float64(metrics.TotalCompletedWorkflows) / float64(total)) * 100
	}

	return metrics, nil
}

// WorkflowChain represents a complete workflow for a draft
type WorkflowChain struct {
	DraftID     string                         `json:"draft_id"`
	Draft       *storage.Draft                 `json:"draft,omitempty"`
	Reviews     []*storage.Review              `json:"reviews,omitempty"`
	Approvals   map[string][]*storage.Approval `json:"approvals,omitempty"` // keyed by review_id
	Status      string                         `json:"status"`              // draft_workflow, active_workflow, complete
	Progress    float64                        `json:"progress"`            // 0-100%
	LastUpdated time.Time                      `json:"last_updated"`
}

// GetWorkflowChain returns complete workflow for a draft
func (ds *DashboardService) GetWorkflowChain(ctx context.Context, draftID string) (*WorkflowChain, error) {
	chain := &WorkflowChain{
		DraftID:   draftID,
		Approvals: make(map[string][]*storage.Approval),
	}

	// Get draft
	draft, err := ds.draftService.GetDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	chain.Draft = draft

	// Get reviews
	reviews, _ := ds.reviewService.ListReviews(ctx)
	if reviews != nil {
		for _, review := range reviews {
			if review.DraftID == draftID {
				chain.Reviews = append(chain.Reviews, review)

				// Get approvals for this review
				approvals, _ := ds.approvalService.GetApprovalsForReview(ctx, review.ID)
				if approvals != nil {
					chain.Approvals[review.ID] = approvals
				}
			}
		}
	}

	// Calculate status and progress
	chain.Status = "draft_workflow"
	chain.Progress = 0

	if draft.Status == "under_review" {
		chain.Status = "active_workflow"
		chain.Progress = 25
	}

	if len(chain.Reviews) > 0 {
		chain.Progress = 50
		if chain.Reviews[0].Status != "under_review" {
			chain.Progress = 75
			chain.Status = "active_workflow"
		}
	}

	if draft.Status == "approved" {
		chain.Progress = 100
		chain.Status = "complete"
	}

	chain.LastUpdated = time.Now()

	return chain, nil
}

// AuditSummary summarizes audit events
type AuditSummary struct {
	TotalEvents   int                   `json:"total_events"`
	EventsByType  map[string]int        `json:"events_by_type"`
	EventsByActor map[string]int        `json:"events_by_actor"`
	RecentEvents  []*storage.AuditEvent `json:"recent_events"`
}

// GetAuditSummary returns audit event summary
func (ds *DashboardService) GetAuditSummary(ctx context.Context, limit int) (*AuditSummary, error) {
	summary := &AuditSummary{
		EventsByType:  make(map[string]int),
		EventsByActor: make(map[string]int),
		RecentEvents:  make([]*storage.AuditEvent, 0),
	}

	// Query audit events
	events, _ := ds.auditService.QueryEvents(ctx, make(map[string]interface{}))

	if events != nil {
		summary.TotalEvents = len(events)

		// Aggregate by type and actor
		for i, event := range events {
			summary.EventsByType[event.Action]++
			summary.EventsByActor[event.ActorID]++

			// Add recent events (first 'limit' events)
			if i < limit {
				summary.RecentEvents = append(summary.RecentEvents, event)
			}
		}
	}

	return summary, nil
}
