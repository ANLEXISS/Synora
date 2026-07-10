package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"synora/pkg/contract"
)

type SnapshotClient struct {
	URL        string
	HealthURL  string
	Token      string
	HTTPClient *http.Client
}

func (c SnapshotClient) Fetch() (*contract.PublicSnapshot, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("API unauthorized: set SYNORA_API_TOKEN or pass --token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api returned %s", resp.Status)
	}

	var snapshot contract.PublicSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (c SnapshotClient) FetchHealth() (*contract.RuntimeHealth, error) {
	if strings.TrimSpace(c.HealthURL) == "" {
		return nil, fmt.Errorf("health URL is not configured")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, c.HealthURL, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("API unauthorized: set SYNORA_API_TOKEN or pass --token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api returned %s", resp.Status)
	}
	var health contract.RuntimeHealth
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, err
	}
	return &health, nil
}

func snapshotSummary(snapshot *contract.PublicSnapshot) string {
	if snapshot == nil {
		return "snapshot unavailable"
	}
	state := valueString(snapshot.System["last_state"])
	if state == "" {
		state = valueString(snapshot.System["state"])
	}
	if state == "" {
		state = "unknown"
	}
	return fmt.Sprintf(
		"system=%s devices=%d events=%d clips=%d validations=%d action_results=%d presence=%d identities=%d",
		state,
		len(snapshot.Devices),
		len(snapshot.Events),
		len(snapshot.Clips),
		len(snapshot.Validations),
		len(snapshot.ActionResults),
		len(snapshot.Presence),
		len(snapshot.Identities),
	)
}

func deviceExists(snapshot *contract.PublicSnapshot, deviceID string) bool {
	if snapshot == nil || deviceID == "" {
		return false
	}
	for _, item := range snapshot.Devices {
		if valueString(item["id"]) == deviceID {
			return true
		}
	}
	for _, item := range snapshot.Cameras {
		if valueString(item["id"]) == deviceID {
			return true
		}
	}
	return false
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}
