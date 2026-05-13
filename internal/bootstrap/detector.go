package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CloudDetector detects cloud provider metadata
type CloudDetector struct {
	timeout time.Duration
	logger  Logger
}

// NewCloudDetector creates a new cloud detector
func NewCloudDetector(timeout time.Duration, logger Logger) *CloudDetector {
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &CloudDetector{
		timeout: timeout,
		logger:  logger,
	}
}

// DetectCloudProvider attempts to detect the cloud provider
func (cd *CloudDetector) DetectCloudProvider(ctx context.Context) (*CloudProviderInfo, error) {
	// Try AWS first (most common)
	if result := cd.detectAWS(ctx); result.Detected {
		cd.logger.Info("Cloud provider detected", "provider", ProviderAWS)
		return result, nil
	}

	// Try Azure
	if result := cd.detectAzure(ctx); result.Detected {
		cd.logger.Info("Cloud provider detected", "provider", ProviderAzure)
		return result, nil
	}

	// Try Huawei
	if result := cd.detectHuawei(ctx); result.Detected {
		cd.logger.Info("Cloud provider detected", "provider", ProviderHuawei)
		return result, nil
	}

	// Not detected - return local environment
	cd.logger.Info("No cloud provider detected, running in local mode")
	return &CloudProviderInfo{
		Provider:  "local",
		Detected:  false,
		Metadata:  make(map[string]string),
		Timestamp: time.Now(),
	}, nil
}

// detectAWS detects AWS using IMDSv2 (token-based approach)
func (cd *CloudDetector) detectAWS(ctx context.Context) *CloudProviderInfo {
	const (
		tokenURL    = "http://169.254.169.254/latest/api/token"
		metadataURL = "http://169.254.169.254/latest/meta-data/instance-id"
	)

	// Create context with timeout
	detectionCtx, cancel := context.WithTimeout(ctx, cd.timeout)
	defer cancel()

	// Step 1: Request IMDSv2 token
	token, err := cd.getIMDSv2Token(detectionCtx, tokenURL)
	if err != nil {
		cd.logger.Debug("AWS IMDSv2 token request failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	// Step 2: Use token to fetch metadata
	instanceID, err := cd.fetchAWSMetadata(detectionCtx, metadataURL, token)
	if err != nil {
		cd.logger.Debug("AWS metadata fetch failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	// Successfully detected AWS
	return &CloudProviderInfo{
		Provider:  ProviderAWS,
		Detected:  true,
		Metadata:  map[string]string{"instance_id": instanceID},
		Timestamp: time.Now(),
	}
}

// getIMDSv2Token requests a token from AWS IMDSv2
func (cd *CloudDetector) getIMDSv2Token(ctx context.Context, tokenURL string) (string, error) {
	client := &http.Client{
		Timeout: cd.timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	// IMDSv2 requires X-aws-ec2-metadata-token-ttl-seconds header
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600") // 6 hours

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	token, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token: %w", err)
	}

	return string(token), nil
}

// fetchAWSMetadata fetches AWS metadata using IMDSv2 token
func (cd *CloudDetector) fetchAWSMetadata(ctx context.Context, metadataURL, token string) (string, error) {
	client := &http.Client{
		Timeout: cd.timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create metadata request: %w", err)
	}

	// Use token in header
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read metadata: %w", err)
	}

	return string(body), nil
}

// detectAzure detects Azure using IMDS
func (cd *CloudDetector) detectAzure(ctx context.Context) *CloudProviderInfo {
	const (
		metadataURL = "http://169.254.169.254/metadata/instance?api-version=2021-02-01"
	)

	detectionCtx, cancel := context.WithTimeout(ctx, cd.timeout)
	defer cancel()

	client := &http.Client{
		Timeout: cd.timeout,
	}

	req, err := http.NewRequestWithContext(detectionCtx, "GET", metadataURL, nil)
	if err != nil {
		cd.logger.Debug("Azure metadata request creation failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	// Azure IMDS requires Metadata:true header
	req.Header.Set("Metadata", "true")

	resp, err := client.Do(req)
	if err != nil {
		cd.logger.Debug("Azure metadata request failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cd.logger.Debug("Azure metadata returned non-200 status", "status", resp.StatusCode)
		return &CloudProviderInfo{Detected: false}
	}

	// Read response to confirm validity
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cd.logger.Debug("Failed to read Azure metadata", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	// Extract basic info from response
	metadata := make(map[string]string)
	if len(body) > 0 {
		metadata["response_size"] = fmt.Sprintf("%d bytes", len(body))
	}

	return &CloudProviderInfo{
		Provider:  ProviderAzure,
		Detected:  true,
		Metadata:  metadata,
		Timestamp: time.Now(),
	}
}

// detectHuawei detects Huawei Cloud using metadata service
func (cd *CloudDetector) detectHuawei(ctx context.Context) *CloudProviderInfo {
	const (
		// Huawei metadata service endpoints
		metadataURL = "http://169.254.169.254/openstack/latest/meta_data.json"
	)

	detectionCtx, cancel := context.WithTimeout(ctx, cd.timeout)
	defer cancel()

	client := &http.Client{
		Timeout: cd.timeout,
	}

	req, err := http.NewRequestWithContext(detectionCtx, "GET", metadataURL, nil)
	if err != nil {
		cd.logger.Debug("Huawei metadata request creation failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	resp, err := client.Do(req)
	if err != nil {
		cd.logger.Debug("Huawei metadata request failed", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cd.logger.Debug("Huawei metadata returned non-200 status", "status", resp.StatusCode)
		return &CloudProviderInfo{Detected: false}
	}

	// Read response to confirm validity
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cd.logger.Debug("Failed to read Huawei metadata", "error", err.Error())
		return &CloudProviderInfo{Detected: false}
	}

	// Extract basic info from response
	metadata := make(map[string]string)
	if len(body) > 0 {
		metadata["response_size"] = fmt.Sprintf("%d bytes", len(body))
	}

	return &CloudProviderInfo{
		Provider:  ProviderHuawei,
		Detected:  true,
		Metadata:  metadata,
		Timestamp: time.Now(),
	}
}
