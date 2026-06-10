package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
)

type SecurityService struct {
	store storage.StorageProvider
}

func NewSecurityService(store storage.StorageProvider) *SecurityService {
	return &SecurityService{store: store}
}

// LogSecurityEvent logs a security event
func (s *SecurityService) LogSecurityEvent(ctx context.Context, eventType, severity, username string, userID *string, ipAddress string, userAgent *string, message string, metadata map[string]interface{}) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		metadataJSON = nil
	}

	event := &storage.SecurityEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Severity:  severity,
		Username:  username,
		UserID:    userID,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Message:   message,
		Metadata:  toStringPtr(string(metadataJSON)),
		CreatedAt: time.Now(),
	}

	return s.store.SecurityEvents().Log(ctx, event)
}

// GetSecurityEvents retrieves security events with filters
func (s *SecurityService) GetSecurityEvents(ctx context.Context, filters map[string]interface{}) ([]*storage.SecurityEvent, error) {
	return s.store.SecurityEvents().Query(ctx, filters)
}

// GetSecurityOverview returns a security overview
func (s *SecurityService) GetSecurityOverview(ctx context.Context) (interface{}, error) {
	users, err := s.store.Users().List(ctx)
	if err != nil {
		return nil, err
	}

	sessions, err := s.store.Sessions().ListAll(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	day24hAgo := now.Add(-24 * time.Hour)

	filters := map[string]interface{}{
		"start_time": day24hAgo,
		"end_time":   now,
	}

	events, err := s.store.SecurityEvents().Query(ctx, filters)
	if err != nil {
		return nil, err
	}

	activeSessions := 0
	lockedAccounts := 0
	disabledUsers := 0
	activeAdmins := 0
	failedLogins24h := 0
	successfulLogins24h := 0
	passwordResets24h := 0

	for _, u := range users {
		if u.Disabled {
			disabledUsers++
		}
		if u.LockedUntil != nil && u.LockedUntil.After(now) {
			lockedAccounts++
		}
		if u.Role == "admin" {
			for _, session := range sessions {
				if session.UserID == u.ID && session.ExpiresAt.After(now) {
					activeAdmins++
					break
				}
			}
		}
	}

	for _, session := range sessions {
		if session.ExpiresAt.After(now) {
			activeSessions++
		}
	}

	for _, event := range events {
		if event.Type == "LOGIN_FAILURE" {
			failedLogins24h++
		} else if event.Type == "LOGIN_SUCCESS" {
			successfulLogins24h++
		} else if event.Type == "PASSWORD_RESET" || event.Type == "PASSWORD_CHANGED" {
			passwordResets24h++
		}
	}

	suspiciousActs, err := s.store.SuspiciousActivities().ListUnacknowledged(ctx)
	if err != nil {
		suspiciousActs = nil
	}

	suspiciousActivities := len(suspiciousActs)

	trends := make(map[string]string)
	if failedLogins24h > 0 {
		trends["failed_logins"] = "↑"
	} else {
		trends["failed_logins"] = "→"
	}
	if suspiciousActivities > 0 {
		trends["suspicious_activities"] = "↑"
	} else {
		trends["suspicious_activities"] = "→"
	}

	return map[string]interface{}{
		"active_sessions":       activeSessions,
		"locked_accounts":       lockedAccounts,
		"disabled_users":        disabledUsers,
		"failed_logins_24h":     failedLogins24h,
		"successful_logins_24h": successfulLogins24h,
		"password_resets_24h":   passwordResets24h,
		"active_admins":         activeAdmins,
		"suspicious_activities": suspiciousActivities,
		"trends":                trends,
	}, nil
}

// GetSecurityAlerts retrieves security alerts
func (s *SecurityService) GetSecurityAlerts(ctx context.Context, state string, limit, offset int) ([]*storage.SecurityAlert, error) {
	if state != "" {
		return s.store.SecurityAlerts().ListByState(ctx, state)
	}
	return s.store.SecurityAlerts().List(ctx, limit, offset)
}

// AcknowledgeAlert acknowledges a security alert
func (s *SecurityService) AcknowledgeAlert(ctx context.Context, alertID, state, acknowledgedBy string) error {
	alert, err := s.store.SecurityAlerts().GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert == nil {
		return fmt.Errorf("alert not found")
	}

	now := time.Now()
	alert.State = state
	alert.AcknowledgedBy = &acknowledgedBy
	alert.AcknowledgedAt = &now

	if state == "resolved" {
		alert.ResolvedAt = &now
	}

	return s.store.SecurityAlerts().Update(ctx, alert)
}

// GetSuspiciousActivities retrieves suspicious activities
func (s *SecurityService) GetSuspiciousActivities(ctx context.Context, limit, offset int) ([]*storage.SuspiciousActivity, error) {
	return s.store.SuspiciousActivities().List(ctx, limit, offset)
}

// AcknowledgeSuspiciousActivity acknowledges a suspicious activity
func (s *SecurityService) AcknowledgeSuspiciousActivity(ctx context.Context, activityID, acknowledgedBy string) error {
	activity, err := s.store.SuspiciousActivities().GetByID(ctx, activityID)
	if err != nil {
		return err
	}
	if activity == nil {
		return fmt.Errorf("activity not found")
	}

	now := time.Now()
	activity.AcknowledgedBy = &acknowledgedBy
	activity.AcknowledgedAt = &now

	return s.store.SuspiciousActivities().Update(ctx, activity)
}

// GetActiveSessions retrieves all active sessions with details
func (s *SecurityService) GetActiveSessions(ctx context.Context) ([]map[string]interface{}, error) {
	sessions, err := s.store.Sessions().ListAll(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var activeSessions []map[string]interface{}

	for _, session := range sessions {
		if session.ExpiresAt.After(now) {
			user, err := s.store.Users().GetByID(ctx, session.UserID)
			if err != nil || user == nil {
				continue
			}

			activeSessions = append(activeSessions, map[string]interface{}{
				"id":           session.ID,
				"username":     user.Username,
				"ip_address":   session.IPAddress,
				"user_agent":   session.UserAgent,
				"created_at":   session.CreatedAt,
				"last_activity": session.LastActivity,
				"expires_at":   session.ExpiresAt,
			})
		}
	}

	return activeSessions, nil
}

// DetectBruteForce detects brute force attacks
func (s *SecurityService) DetectBruteForce(ctx context.Context) error {
	now := time.Now()
	period := now.Add(-1 * time.Hour)

	filters := map[string]interface{}{
		"type":       "LOGIN_FAILURE",
		"start_time": period,
		"end_time":   now,
	}

	events, err := s.store.SecurityEvents().Query(ctx, filters)
	if err != nil {
		return err
	}

	ipFailures := make(map[string]int)
	for _, event := range events {
		ipFailures[event.IPAddress]++
	}

	for ip, count := range ipFailures {
		if count >= 5 {
			activity, err := s.findOrCreateSuspiciousActivity(ctx, "brute_force", ip, "high")
			if err == nil && activity != nil {
				activity.OccurrenceCount++
				activity.LastSeen = now
				s.store.SuspiciousActivities().Update(ctx, activity)

				alert := &storage.SecurityAlert{
					ID:        uuid.New().String(),
					Type:      "brute_force",
					Severity:  "high",
					State:     "active",
					Title:     "Brute Force Attack Detected",
					Message:   fmt.Sprintf("Multiple login failures detected from IP %s", ip),
					IPAddress: &ip,
					CreatedAt: now,
				}
				s.store.SecurityAlerts().Create(ctx, alert)
			}
		}
	}

	return nil
}

// DetectCredentialStuffing detects credential stuffing attacks
func (s *SecurityService) DetectCredentialStuffing(ctx context.Context) error {
	now := time.Now()
	period := now.Add(-1 * time.Hour)

	filters := map[string]interface{}{
		"type":       "LOGIN_FAILURE",
		"start_time": period,
		"end_time":   now,
	}

	events, err := s.store.SecurityEvents().Query(ctx, filters)
	if err != nil {
		return err
	}

	usersByIP := make(map[string]map[string]bool)
	for _, event := range events {
		if usersByIP[event.IPAddress] == nil {
			usersByIP[event.IPAddress] = make(map[string]bool)
		}
		usersByIP[event.IPAddress][event.Username] = true
	}

	for ip, users := range usersByIP {
		if len(users) >= 3 {
			activity, err := s.findOrCreateSuspiciousActivity(ctx, "credential_stuffing", ip, "critical")
			if err == nil && activity != nil {
				activity.OccurrenceCount++
				activity.LastSeen = now

				userList := make([]string, 0, len(users))
				for u := range users {
					userList = append(userList, u)
				}
				usersJSON, _ := json.Marshal(userList)
				activity.Usernames = toStringPtr(string(usersJSON))

				s.store.SuspiciousActivities().Update(ctx, activity)

				alert := &storage.SecurityAlert{
					ID:        uuid.New().String(),
					Type:      "credential_stuffing",
					Severity:  "critical",
					State:     "active",
					Title:     "Credential Stuffing Attack Detected",
					Message:   fmt.Sprintf("Multiple user accounts attacked from IP %s", ip),
					IPAddress: &ip,
					CreatedAt: now,
				}
				s.store.SecurityAlerts().Create(ctx, alert)
			}
		}
	}

	return nil
}

// DetectSessionAnomalies detects session anomalies
func (s *SecurityService) DetectSessionAnomalies(ctx context.Context) error {
	now := time.Now()
	sessions, err := s.store.Sessions().ListAll(ctx)
	if err != nil {
		return err
	}

	userIPs := make(map[string]map[string]bool)
	for _, session := range sessions {
		if session.ExpiresAt.After(now) {
			if userIPs[session.UserID] == nil {
				userIPs[session.UserID] = make(map[string]bool)
			}
			userIPs[session.UserID][session.IPAddress] = true
		}
	}

	for userID, ips := range userIPs {
		if len(ips) >= 2 {
			user, err := s.store.Users().GetByID(ctx, userID)
			if err != nil || user == nil {
				continue
			}

			activity, err := s.findOrCreateSuspiciousActivity(ctx, "session_anomaly", "", "medium")
			if err == nil && activity != nil {
				activity.OccurrenceCount++
				activity.LastSeen = now

				ipList := make([]string, 0, len(ips))
				for ip := range ips {
					ipList = append(ipList, ip)
				}
				metadataJSON, _ := json.Marshal(map[string]interface{}{"user": user.Username, "ips": ipList})
				activity.Metadata = toStringPtr(string(metadataJSON))

				s.store.SuspiciousActivities().Update(ctx, activity)

				alert := &storage.SecurityAlert{
					ID:           uuid.New().String(),
					Type:         "session_anomaly",
					Severity:     "medium",
					State:        "active",
					Title:        "Session Anomaly Detected",
					Message:      fmt.Sprintf("User %s has active sessions from multiple IPs", user.Username),
					AffectedUser: &user.Username,
					CreatedAt:    now,
				}
				s.store.SecurityAlerts().Create(ctx, alert)
			}
		}
	}

	return nil
}

// DetectDisabledUserLogins detects login attempts from disabled users
func (s *SecurityService) DetectDisabledUserLogins(ctx context.Context) error {
	now := time.Now()
	period := now.Add(-24 * time.Hour)

	filters := map[string]interface{}{
		"type":       "LOGIN_FAILURE",
		"start_time": period,
		"end_time":   now,
	}

	events, err := s.store.SecurityEvents().Query(ctx, filters)
	if err != nil {
		return err
	}

	disabledLogins := make(map[string]int)
	disabledLoginIPs := make(map[string]string)

	for _, event := range events {
		user, err := s.store.Users().GetByUsername(ctx, event.Username)
		if err == nil && user != nil && user.Disabled {
			disabledLogins[event.Username]++
			disabledLoginIPs[event.Username] = event.IPAddress
		}
	}

	for username, count := range disabledLogins {
		if count >= 1 {
			ip := disabledLoginIPs[username]
			alert := &storage.SecurityAlert{
				ID:           uuid.New().String(),
				Type:         "disabled_user_login",
				Severity:     "high",
				State:        "active",
				Title:        "Disabled User Login Attempt",
				Message:      fmt.Sprintf("Login attempt from disabled user %s", username),
				AffectedUser: &username,
				IPAddress:    &ip,
				CreatedAt:    now,
			}
			s.store.SecurityAlerts().Create(ctx, alert)
		}
	}

	return nil
}

func (s *SecurityService) findOrCreateSuspiciousActivity(ctx context.Context, activityType, ip, severity string) (*storage.SuspiciousActivity, error) {
	activities, err := s.store.SuspiciousActivities().ListUnacknowledged(ctx)
	if err != nil {
		return nil, err
	}

	for _, a := range activities {
		if a.Type == activityType {
			if ip == "" || (a.IPAddress != nil && *a.IPAddress == ip) {
				return a, nil
			}
		}
	}

	now := time.Now()
	activity := &storage.SuspiciousActivity{
		ID:              uuid.New().String(),
		Type:            activityType,
		Severity:        severity,
		IPAddress:       toStringPtr(ip),
		FirstSeen:       now,
		LastSeen:        now,
		OccurrenceCount: 1,
		Message:         fmt.Sprintf("Suspicious activity: %s", activityType),
		CreatedAt:       now,
	}

	err = s.store.SuspiciousActivities().Create(ctx, activity)
	if err != nil {
		return nil, err
	}

	return activity, nil
}

func toStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
