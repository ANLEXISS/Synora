package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/internal/actions"
	"synora/pkg/contract"
)

type Config struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Provider        string `yaml:"provider" json:"provider"`
	GraphVersion    string `yaml:"graph_version" json:"graph_version"`
	PhoneNumberID   string `yaml:"phone_number_id" json:"phone_number_id"`
	AccessTokenFile string `yaml:"access_token_file" json:"access_token_file"`
	DefaultTo       string `yaml:"default_to" json:"default_to"`
	DefaultTemplate string `yaml:"default_template" json:"default_template"`
	LanguageCode    string `yaml:"language_code" json:"language_code"`
	DryRun          bool   `yaml:"dry_run" json:"dry_run"`
	BaseURL         string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Adapter struct {
	Config Config
	Client HTTPDoer
	Now    func() time.Time
}

func DefaultConfig() Config {
	return Config{Provider: "cloud_api", GraphVersion: "v23.0", AccessTokenFile: "/etc/synora/secrets/whatsapp_token", DefaultTemplate: "synora_security_alert", LanguageCode: "fr", DryRun: true, BaseURL: "https://graph.facebook.com"}
}

func ConfigFromEnv() Config {
	config := DefaultConfig()
	path := os.Getenv("SYNORA_ACTIONS_CONFIG")
	if path == "" {
		path = "/etc/synora/actions.yaml"
	}
	if data, err := os.ReadFile(path); err == nil {
		var wrapper map[string]any
		if yaml.Unmarshal(data, &wrapper) == nil {
			if value, ok := wrapper["whatsapp"].(map[string]any); ok {
				mergeConfigMap(&config, value)
			}
		}
	}
	if value := os.Getenv("SYNORA_WHATSAPP_ENABLED"); value != "" {
		config.Enabled = strings.EqualFold(value, "true") || value == "1"
	}
	if value := os.Getenv("SYNORA_WHATSAPP_PHONE_NUMBER_ID"); value != "" {
		config.PhoneNumberID = value
	}
	if value := os.Getenv("SYNORA_WHATSAPP_DEFAULT_TO"); value != "" {
		config.DefaultTo = value
	}
	if value := os.Getenv("SYNORA_WHATSAPP_TOKEN_FILE"); value != "" {
		config.AccessTokenFile = value
	}
	if value := os.Getenv("SYNORA_WHATSAPP_ACCESS_TOKEN"); value != "" { /* token is read at execution time */
	}
	if value := os.Getenv("SYNORA_WHATSAPP_DRY_RUN"); value != "" {
		config.DryRun = strings.EqualFold(value, "true") || value == "1"
	}
	if value := os.Getenv("SYNORA_WHATSAPP_GRAPH_VERSION"); value != "" {
		config.GraphVersion = value
	}
	return config
}

func mergeConfigMap(dst *Config, src map[string]any) {
	stringValue := func(key string) string { value, _ := src[key].(string); return strings.TrimSpace(value) }
	if value := stringValue("provider"); value != "" {
		dst.Provider = value
	}
	if value := stringValue("graph_version"); value != "" {
		dst.GraphVersion = value
	}
	if value := stringValue("phone_number_id"); value != "" {
		dst.PhoneNumberID = value
	}
	if value := stringValue("access_token_file"); value != "" {
		dst.AccessTokenFile = value
	}
	if value := stringValue("default_to"); value != "" {
		dst.DefaultTo = value
	}
	if value := stringValue("default_template"); value != "" {
		dst.DefaultTemplate = value
	}
	if value := stringValue("language_code"); value != "" {
		dst.LanguageCode = value
	}
	if value := stringValue("base_url"); value != "" {
		dst.BaseURL = value
	}
	if value, ok := src["enabled"].(bool); ok {
		dst.Enabled = value
	}
	if value, ok := src["dry_run"].(bool); ok {
		dst.DryRun = value
	}
}

func (a Adapter) Execute(ctx context.Context, request contract.ActionRequest) (actions.ExecutionResult, error) {
	config := a.Config
	if config.Provider == "" {
		defaults := DefaultConfig()
		config.Provider = defaults.Provider
		if config.GraphVersion == "" {
			config.GraphVersion = defaults.GraphVersion
		}
		if config.DefaultTemplate == "" {
			config.DefaultTemplate = defaults.DefaultTemplate
		}
		if config.LanguageCode == "" {
			config.LanguageCode = defaults.LanguageCode
		}
		if config.AccessTokenFile == "" {
			config.AccessTokenFile = defaults.AccessTokenFile
		}
		if config.BaseURL == "" {
			config.BaseURL = defaults.BaseURL
		}
	}
	if !config.Enabled {
		return result(actions.StatusSkipped, map[string]any{"provider": "whatsapp_cloud", "reason": "provider_disabled"}), nil
	}
	to := strings.TrimSpace(config.DefaultTo)
	if request.Target != "" && request.Target != "owner" {
		to = strings.TrimSpace(request.Target)
	}
	message := stringValue(request.Data, "message")
	template := stringValue(request.Data, "template")
	if template == "" {
		template = config.DefaultTemplate
	}
	if message == "" {
		message = "Alerte Synora"
	}
	if config.DryRun || boolValue(request.Metadata, "dry_run") {
		return result(actions.StatusSimulatedSuccess, map[string]any{"provider": "whatsapp_cloud", "status": "dry_run", "to": maskPhone(to), "template": template, "message": message}), nil
	}
	if to == "" || strings.TrimSpace(config.PhoneNumberID) == "" {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "config_missing"}), nil
	}
	token, err := accessToken(config)
	if err != nil || token == "" {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "config_missing"}), nil
	}
	payload := map[string]any{"messaging_product": "whatsapp", "to": to}
	if template != "" {
		payload["type"] = "template"
		payload["template"] = map[string]any{"name": template, "language": map[string]string{"code": firstNonEmpty(config.LanguageCode, "fr")}}
	} else {
		payload["type"] = "text"
		payload["text"] = map[string]any{"preview_url": false, "body": message}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "request_encode_failed"}), nil
	}
	baseURL := strings.TrimRight(firstNonEmpty(config.BaseURL, "https://graph.facebook.com"), "/")
	version := strings.Trim(config.GraphVersion, "/ ")
	if version == "" {
		version = "v23.0"
	}
	endpoint := baseURL + "/" + url.PathEscape(version) + "/" + url.PathEscape(config.PhoneNumberID) + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "request_build_failed"}), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "network_error"}), nil
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result(actions.StatusError, map[string]any{"provider": "whatsapp_cloud", "reason": "http_error", "http_status": response.StatusCode}), nil
	}
	return result(actions.StatusSuccess, map[string]any{"provider": "whatsapp_cloud", "to": maskPhone(to), "template": template, "http_status": response.StatusCode}), nil
}

func accessToken(config Config) (string, error) {
	if value := strings.TrimSpace(os.Getenv("SYNORA_WHATSAPP_ACCESS_TOKEN")); value != "" {
		return value, nil
	}
	if strings.TrimSpace(config.AccessTokenFile) == "" {
		return "", errors.New("access token file missing")
	}
	data, err := os.ReadFile(filepath.Clean(config.AccessTokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func result(status string, details map[string]any) actions.ExecutionResult {
	return actions.ExecutionResult{Status: status, Details: details}
}
func stringValue(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
func boolValue(data map[string]any, key string) bool { value, _ := data[key].(bool); return value }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func maskPhone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not_configured"
	}
	if len(value) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}
