package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookPlugin sends events to a generic HTTP endpoint
type WebhookPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewWebhookPlugin() *WebhookPlugin {
	return &WebhookPlugin{
		BasePlugin: NewBasePlugin(
			"webhook-plugin",
			"Webhook Integration",
			"1.0.0",
			"Send events to generic HTTP endpoints",
			TypeNotification,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (wp *WebhookPlugin) Capabilities() []Capability {
	return []Capability{CapabilityNotification}
}

func (wp *WebhookPlugin) SetConfig(config *IntegrationConfig) {
	wp.config = config
}

func (wp *WebhookPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (wp *WebhookPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (wp *WebhookPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (wp *WebhookPlugin) Heartbeat() error {
	return nil
}

func (wp *WebhookPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if wp.config == nil || !wp.config.Enabled {
		return fmt.Errorf("webhook not configured or disabled")
	}

	payload, _ := json.Marshal(event)

	req, err := http.NewRequestWithContext(ctx, "POST", wp.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DSO/1.0")

	resp, err := wp.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// SlackPlugin sends events to Slack
type SlackPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewSlackPlugin() *SlackPlugin {
	return &SlackPlugin{
		BasePlugin: NewBasePlugin(
			"slack-plugin",
			"Slack Integration",
			"1.0.0",
			"Send events to Slack channels",
			TypeNotification,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (sp *SlackPlugin) Capabilities() []Capability {
	return []Capability{CapabilityNotification}
}

func (sp *SlackPlugin) SetConfig(config *IntegrationConfig) {
	sp.config = config
}

func (sp *SlackPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (sp *SlackPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (sp *SlackPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (sp *SlackPlugin) Heartbeat() error {
	return nil
}

func (sp *SlackPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if sp.config == nil || !sp.config.Enabled {
		return fmt.Errorf("slack not configured or disabled")
	}

	// Format event for Slack
	slackMsg := map[string]interface{}{
		"text": fmt.Sprintf("*%s*: %v", event.Type, event.Payload),
	}

	payload, _ := json.Marshal(slackMsg)

	req, err := http.NewRequestWithContext(ctx, "POST", sp.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create slack request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := sp.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}

// TeamsPlugin sends events to Microsoft Teams
type TeamsPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewTeamsPlugin() *TeamsPlugin {
	return &TeamsPlugin{
		BasePlugin: NewBasePlugin(
			"teams-plugin",
			"Teams Integration",
			"1.0.0",
			"Send events to Microsoft Teams channels",
			TypeNotification,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (tp *TeamsPlugin) Capabilities() []Capability {
	return []Capability{CapabilityNotification}
}

func (tp *TeamsPlugin) SetConfig(config *IntegrationConfig) {
	tp.config = config
}

func (tp *TeamsPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (tp *TeamsPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (tp *TeamsPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (tp *TeamsPlugin) Heartbeat() error {
	return nil
}

func (tp *TeamsPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if tp.config == nil || !tp.config.Enabled {
		return fmt.Errorf("teams not configured or disabled")
	}

	// Format event for Teams
	teamsMsg := map[string]interface{}{
		"@type":       "MessageCard",
		"@context":    "https://schema.org/extensions",
		"summary":     event.Type,
		"themeColor":  "0078D4",
		"title":       fmt.Sprintf("DSO Event: %s", event.Type),
		"text":        fmt.Sprintf("%v", event.Payload),
	}

	payload, _ := json.Marshal(teamsMsg)

	req, err := http.NewRequestWithContext(ctx, "POST", tp.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create teams request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := tp.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send teams message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("teams returned status %d", resp.StatusCode)
	}

	return nil
}

// EmailPlugin sends events via email (stub for SMTP)
type EmailPlugin struct {
	BasePlugin
	config *IntegrationConfig
}

func NewEmailPlugin() *EmailPlugin {
	return &EmailPlugin{
		BasePlugin: NewBasePlugin(
			"email-plugin",
			"Email Integration",
			"1.0.0",
			"Send events via email notifications",
			TypeNotification,
		),
	}
}

func (ep *EmailPlugin) Capabilities() []Capability {
	return []Capability{CapabilityNotification}
}

func (ep *EmailPlugin) SetConfig(config *IntegrationConfig) {
	ep.config = config
}

func (ep *EmailPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (ep *EmailPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (ep *EmailPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (ep *EmailPlugin) Heartbeat() error {
	return nil
}

func (ep *EmailPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if ep.config == nil || !ep.config.Enabled {
		return fmt.Errorf("email not configured or disabled")
	}
	// SMTP integration would be implemented here
	return nil
}

// PagerDutyPlugin creates incidents in PagerDuty
type PagerDutyPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewPagerDutyPlugin() *PagerDutyPlugin {
	return &PagerDutyPlugin{
		BasePlugin: NewBasePlugin(
			"pagerduty-plugin",
			"PagerDuty Integration",
			"1.0.0",
			"Create incidents in PagerDuty",
			TypeAction,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (pp *PagerDutyPlugin) Capabilities() []Capability {
	return []Capability{CapabilityAction, CapabilityNotification}
}

func (pp *PagerDutyPlugin) SetConfig(config *IntegrationConfig) {
	pp.config = config
}

func (pp *PagerDutyPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (pp *PagerDutyPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (pp *PagerDutyPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (pp *PagerDutyPlugin) Heartbeat() error {
	return nil
}

func (pp *PagerDutyPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if pp.config == nil || !pp.config.Enabled {
		return fmt.Errorf("pagerduty not configured or disabled")
	}
	// PagerDuty API integration would be implemented here
	return nil
}

// JiraPlugin creates issues in Jira
type JiraPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewJiraPlugin() *JiraPlugin {
	return &JiraPlugin{
		BasePlugin: NewBasePlugin(
			"jira-plugin",
			"Jira Integration",
			"1.0.0",
			"Create issues in Jira",
			TypeAction,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (jp *JiraPlugin) Capabilities() []Capability {
	return []Capability{CapabilityAction}
}

func (jp *JiraPlugin) SetConfig(config *IntegrationConfig) {
	jp.config = config
}

func (jp *JiraPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (jp *JiraPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (jp *JiraPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (jp *JiraPlugin) Heartbeat() error {
	return nil
}

func (jp *JiraPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if jp.config == nil || !jp.config.Enabled {
		return fmt.Errorf("jira not configured or disabled")
	}
	// Jira API integration would be implemented here
	return nil
}

// ServiceNowPlugin creates incidents in ServiceNow
type ServiceNowPlugin struct {
	BasePlugin
	config *IntegrationConfig
	client *http.Client
}

func NewServiceNowPlugin() *ServiceNowPlugin {
	return &ServiceNowPlugin{
		BasePlugin: NewBasePlugin(
			"servicenow-plugin",
			"ServiceNow Integration",
			"1.0.0",
			"Create incidents in ServiceNow",
			TypeAction,
		),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (snp *ServiceNowPlugin) Capabilities() []Capability {
	return []Capability{CapabilityAction, CapabilityNotification}
}

func (snp *ServiceNowPlugin) SetConfig(config *IntegrationConfig) {
	snp.config = config
}

func (snp *ServiceNowPlugin) Initialize(ctx context.Context) error {
	return nil
}

func (snp *ServiceNowPlugin) Shutdown(ctx context.Context) error {
	return nil
}

func (snp *ServiceNowPlugin) Health() PluginHealth {
	return HealthHealthy
}

func (snp *ServiceNowPlugin) Heartbeat() error {
	return nil
}

func (snp *ServiceNowPlugin) DeliverEvent(ctx context.Context, event Event) error {
	if snp.config == nil || !snp.config.Enabled {
		return fmt.Errorf("servicenow not configured or disabled")
	}
	// ServiceNow API integration would be implemented here
	return nil
}
