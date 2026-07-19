package connectivity

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/synora/connectivity.yaml"

type Config struct {
	Version    int              `yaml:"version"`
	Enabled    bool             `yaml:"enabled"`
	Interface  InterfaceConfig  `yaml:"interface"`
	Control    ControlConfig    `yaml:"control"`
	Connection ConnectionConfig `yaml:"connection"`
	Security   SecurityConfig   `yaml:"security"`
}

type InterfaceConfig struct {
	Name       string `yaml:"name"`
	ListenPort int    `yaml:"listen_port"`
	MTU        int    `yaml:"mtu"`
}

type ControlConfig struct {
	URL                 string `yaml:"url"`
	HeartbeatSeconds    int    `yaml:"heartbeat_seconds"`
	ReconnectMinSeconds int    `yaml:"reconnect_min_seconds"`
	ReconnectMaxSeconds int    `yaml:"reconnect_max_seconds"`
}

type ConnectionConfig struct {
	DirectEnabled        bool `yaml:"direct_enabled"`
	RelayEnabled         bool `yaml:"relay_enabled"`
	DirectTimeoutSeconds int  `yaml:"direct_timeout_seconds"`
	KeepaliveSeconds     int  `yaml:"keepalive_seconds"`
}

type SecurityConfig struct {
	ExposeLAN         bool `yaml:"expose_lan"`
	AllowIPForwarding bool `yaml:"allow_ip_forwarding"`
}

func DefaultConfig() Config {
	return Config{
		Version:    1,
		Enabled:    false,
		Interface:  InterfaceConfig{Name: "synora0", ListenPort: 41641, MTU: 1280},
		Control:    ControlConfig{HeartbeatSeconds: 30, ReconnectMinSeconds: 2, ReconnectMaxSeconds: 60},
		Connection: ConnectionConfig{DirectEnabled: true, RelayEnabled: true, DirectTimeoutSeconds: 8, KeepaliveSeconds: 25},
	}
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, errors.New("read connectivity configuration")
	}
	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, errors.New("parse connectivity configuration")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, errors.New("connectivity configuration must contain one YAML document")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported connectivity config version")
	}
	name := strings.TrimSpace(c.Interface.Name)
	if name == "" || len(name) > 15 || !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("invalid connectivity interface name")
	}
	if c.Interface.ListenPort < 1 || c.Interface.ListenPort > 65535 {
		return fmt.Errorf("invalid connectivity listen port")
	}
	if c.Interface.MTU < 576 || c.Interface.MTU > 9000 {
		return fmt.Errorf("invalid connectivity MTU")
	}
	if c.Control.HeartbeatSeconds <= 0 || c.Control.ReconnectMinSeconds <= 0 || c.Control.ReconnectMaxSeconds <= 0 || c.Control.ReconnectMaxSeconds < c.Control.ReconnectMinSeconds {
		return fmt.Errorf("invalid connectivity control durations")
	}
	if c.Connection.DirectTimeoutSeconds <= 0 || c.Connection.KeepaliveSeconds <= 0 {
		return fmt.Errorf("invalid connectivity connection durations")
	}
	if c.Security.ExposeLAN || c.Security.AllowIPForwarding {
		return fmt.Errorf("unsafe connectivity security option enabled")
	}
	controlURL := strings.TrimSpace(c.Control.URL)
	if controlURL != "" {
		parsed, err := url.Parse(controlURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return fmt.Errorf("invalid connectivity control URL")
		}
	}
	return nil
}
