// Package mediamtx contains the small, injectable control boundary used by
// Discovery and runtime health. Unit tests never require a MediaMTX process.
package mediamtx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	DefaultAPIURL  = "http://127.0.0.1:9997"
	maxAPIResponse = 1 << 20
)

type PathController interface {
	ListPaths(context.Context) ([]string, error)
	AddPath(context.Context, string) error
	DeletePath(context.Context, string) error
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(rawURL string, client *http.Client) (*Client, error) {
	if strings.TrimSpace(rawURL) == "" {
		rawURL = DefaultAPIURL
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("MediaMTX API URL must be an HTTP(S) URL without credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if client == nil {
		client = &http.Client{}
	}
	return &Client{baseURL: strings.TrimRight(parsed.String(), "/"), http: client}, nil
}

func (c *Client) ListPaths(ctx context.Context) ([]string, error) {
	var response struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	data, err := c.request(ctx, http.MethodGet, "/v3/paths/list", "", nil)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 && data[0] == '[' {
		var items []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("MediaMTX paths response: %w", err)
		}
		for _, item := range items {
			response.Items = append(response.Items, item)
		}
	} else if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("MediaMTX paths response: %w", err)
	}
	paths := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		if strings.TrimSpace(item.Name) != "" {
			paths = append(paths, strings.TrimSpace(item.Name))
		}
	}
	return normalizePaths(paths), nil
}

func (c *Client) AddPath(ctx context.Context, path string) error {
	path, err := validatePath(path)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, http.MethodPost, "/v3/config/paths/add/"+url.PathEscape(path), `{"source":"publisher","overridePublisher":true}`, map[string]string{"Content-Type": "application/json"})
	return err
}

func (c *Client) DeletePath(ctx context.Context, path string) error {
	path, err := validatePath(path)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, http.MethodDelete, "/v3/config/paths/delete/"+url.PathEscape(path), "", nil)
	return err
}

func (c *Client) request(ctx context.Context, method, suffix, body string, headers map[string]string) ([]byte, error) {
	if c == nil || c.http == nil {
		return nil, errors.New("MediaMTX client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+suffix, strings.NewReader(body))
	if err != nil {
		return nil, errors.New("MediaMTX request could not be created")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MediaMTX request failed: %w", err)
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxAPIResponse+1))
	if readErr != nil {
		return nil, errors.New("MediaMTX response could not be read")
	}
	if len(data) > maxAPIResponse {
		return nil, errors.New("MediaMTX response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("MediaMTX API returned status %d", response.StatusCode)
	}
	return data, nil
}

type ReconcileReport struct {
	Status  string
	Ready   bool
	Current []string
	Desired []string
	Added   []string
	Removed []string
	Error   string
	Checked time.Time
}

func Reconcile(ctx context.Context, controller PathController, desired []string, now time.Time) (ReconcileReport, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	report := ReconcileReport{Status: "degraded", Checked: now, Desired: normalizePaths(desired)}
	if controller == nil {
		return failReport(report, errors.New("MediaMTX path controller is not configured"))
	}
	current, err := controller.ListPaths(ctx)
	if err != nil {
		return failReport(report, err)
	}
	report.Current = normalizePaths(current)
	desiredSet := make(map[string]struct{}, len(report.Desired))
	currentSet := make(map[string]struct{}, len(report.Current))
	for _, path := range report.Desired {
		desiredSet[path] = struct{}{}
	}
	for _, path := range report.Current {
		currentSet[path] = struct{}{}
	}
	for _, path := range report.Desired {
		if _, exists := currentSet[path]; exists {
			continue
		}
		if err := controller.AddPath(ctx, path); err != nil {
			return failReport(report, err)
		}
		report.Added = append(report.Added, path)
	}
	for _, path := range report.Current {
		if _, wanted := desiredSet[path]; wanted {
			continue
		}
		if err := controller.DeletePath(ctx, path); err != nil {
			return failReport(report, err)
		}
		report.Removed = append(report.Removed, path)
	}
	report.Current = append([]string(nil), report.Desired...)
	report.Status = "ready"
	report.Ready = true
	return report, nil
}

func failReport(report ReconcileReport, err error) (ReconcileReport, error) {
	if err != nil {
		report.Error = err.Error()
	}
	return report, err
}

func DesiredPaths(cameraIDs []string) ([]string, error) {
	paths := make([]string, 0, len(cameraIDs))
	for _, cameraID := range cameraIDs {
		path, err := validatePath(cameraID)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return normalizePaths(paths), nil
}

func normalizePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func validatePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if err := contract.ValidateIdentifier("stream path", path); err != nil {
		return "", err
	}
	if strings.Contains(path, "/") || path == "." || path == ".." {
		return "", errors.New("stream path contains an invalid separator")
	}
	return path, nil
}
