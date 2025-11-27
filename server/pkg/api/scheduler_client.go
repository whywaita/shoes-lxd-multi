package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SchedulerClient represents a client for the scheduler API
type SchedulerClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSchedulerClient creates a new scheduler client
func NewSchedulerClient(baseURL string) *SchedulerClient {
	return &SchedulerClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ScheduleRequest represents a request to the scheduler
type ScheduleRequest struct {
	CPU         int      `json:"cpu"`
	Memory      int      `json:"memory"`
	TargetHosts []string `json:"target_hosts,omitempty"`
}

// ScheduleResponse represents a response from the scheduler
type ScheduleResponse struct {
	Host string `json:"host"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// Schedule requests the scheduler to select a host
func (c *SchedulerClient) Schedule(ctx context.Context, req ScheduleRequest) (*ScheduleResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("scheduler address not configured")
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/schedule", bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("scheduler returned status %d, but failed to decode error response: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("scheduler returned status %d: %s", resp.StatusCode, errResp.Error)
	}

	var schedResp ScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &schedResp, nil
}
