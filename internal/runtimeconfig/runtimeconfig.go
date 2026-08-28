// Package runtimeconfig owns the paths, endpoints, and timeouts shared by
// Synora processes. Values are resolved from an injected environment reader so
// tests never need to mutate the process environment.
package runtimeconfig

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultConfigDir          = "/etc/synora"
	DefaultStatePath          = "/var/lib/synora/state/state.json"
	DefaultBusSocket          = "/run/synora/bus.sock"
	DefaultClipRoot           = "/var/lib/synora/clips"
	DefaultFaceDataRoot       = "/var/lib/synora/vision/face"
	DefaultModelRoot          = "/var/lib/synora/models"
	DefaultBackupRoot         = "/var/lib/synora/backups"
	DefaultWebRoot            = "/var/lib/synora/web"
	DefaultMediaMTXRTSPURL    = "rtsp://10.77.0.1:8554"
	DefaultHTTPAddr           = ":8080"
	DefaultHTTPSAddr          = ":8443"
	DefaultVisionHealth       = ":8091"
	DefaultVisionHTTPS        = ":7070"
	DefaultVisionWorkerSocket = "/run/synora/vision-worker.sock"
)

type Paths struct {
	ConfigDir          string
	Security           string
	Auth               string
	Topology           string
	Residents          string
	Devices            string
	Automations        string
	ActionPolicy       string
	CGEChains          string
	CGEProfile         string
	CGEFeedback        string
	NetworkConfig      string
	BusSocket          string
	VisionWorkerSocket string
	State              string
	ClipRoot           string
	FaceDataRoot       string
	ModelRoot          string
	BackupRoot         string
	WebRoot            string
	MediaMTXConfig     string
	OTAJournal         string
	CameraOTARoot      string
	ConnectivityRoot   string
	SessionStore       string
	IdentityRegistry   string
	VersionFile        string
	TLSCert            string
	TLSKey             string
}

type Endpoints struct {
	HTTP            string
	HTTPS           string
	VisionHealth    string
	VisionHTTPS     string
	MediaMTXRTSPURL string
}

type Timeouts struct {
	BusConnect     time.Duration
	BusRPC         time.Duration
	HTTPRead       time.Duration
	HTTPWrite      time.Duration
	HTTPIdle       time.Duration
	HTTPReadHeader time.Duration
	Shutdown       time.Duration
	VisionWorker   time.Duration
}

type Config struct {
	Paths     Paths
	Endpoints Endpoints
	Timeouts  Timeouts
}

func Defaults() Config {
	configDir := DefaultConfigDir
	return Config{
		Paths: Paths{
			ConfigDir:          configDir,
			Security:           filepath.Join(configDir, "security.yaml"),
			Auth:               filepath.Join(configDir, "auth.yaml"),
			Topology:           filepath.Join(configDir, "topology.yaml"),
			Residents:          filepath.Join(configDir, "residents.yaml"),
			Devices:            filepath.Join(configDir, "devices.yaml"),
			Automations:        filepath.Join(configDir, "automations.yaml"),
			ActionPolicy:       filepath.Join(configDir, "action_policy.yaml"),
			CGEChains:          filepath.Join(configDir, "cge_critical_chains.yaml"),
			CGEProfile:         filepath.Join(configDir, "cge_profile.yaml"),
			CGEFeedback:        filepath.Join("/var/lib/synora", "cge", "feedback.json"),
			NetworkConfig:      filepath.Join(configDir, "network.yaml"),
			BusSocket:          DefaultBusSocket,
			VisionWorkerSocket: DefaultVisionWorkerSocket,
			State:              DefaultStatePath,
			ClipRoot:           DefaultClipRoot,
			FaceDataRoot:       DefaultFaceDataRoot,
			ModelRoot:          DefaultModelRoot,
			BackupRoot:         DefaultBackupRoot,
			WebRoot:            DefaultWebRoot,
			MediaMTXConfig:     filepath.Join(configDir, "mediamtx.yml"),
			OTAJournal:         filepath.Join("/var/lib/synora", "ota", "update.json"),
			CameraOTARoot:      filepath.Join("/var/lib/synora", "camera-ota"),
			ConnectivityRoot:   filepath.Join("/var/lib/synora", "connectivity"),
			SessionStore:       filepath.Join("/var/lib/synora", "auth", "sessions.json"),
			IdentityRegistry:   filepath.Join("/var/lib/synora", "security", "identities.json"),
			VersionFile:        filepath.Join(configDir, "version.json"),
			TLSCert:            filepath.Join(configDir, "tls", "synora.crt"),
			TLSKey:             filepath.Join(configDir, "tls", "synora.key"),
		},
		Endpoints: Endpoints{
			HTTP:            DefaultHTTPAddr,
			HTTPS:           DefaultHTTPSAddr,
			VisionHealth:    DefaultVisionHealth,
			VisionHTTPS:     DefaultVisionHTTPS,
			MediaMTXRTSPURL: DefaultMediaMTXRTSPURL,
		},
		Timeouts: Timeouts{
			BusConnect:     2 * time.Second,
			BusRPC:         15 * time.Second,
			HTTPRead:       10 * time.Second,
			HTTPWrite:      10 * time.Second,
			HTTPIdle:       30 * time.Second,
			HTTPReadHeader: 5 * time.Second,
			Shutdown:       5 * time.Second,
			VisionWorker:   2 * time.Minute,
		},
	}
}

// Load resolves SYNORA_* overrides against production defaults. The reader
// argument is mandatory so callers can inject deterministic environments.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("runtime configuration environment reader is required")
	}
	cfg := Defaults()
	set := func(key string, destination *string) {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			*destination = value
		}
	}
	set("SYNORA_CONFIG_DIR", &cfg.Paths.ConfigDir)
	if configDir := strings.TrimSpace(getenv("SYNORA_CONFIG_DIR")); configDir != "" {
		cfg.Paths.Security = filepath.Join(configDir, "security.yaml")
		cfg.Paths.Auth = filepath.Join(configDir, "auth.yaml")
		cfg.Paths.Topology = filepath.Join(configDir, "topology.yaml")
		cfg.Paths.Residents = filepath.Join(configDir, "residents.yaml")
		cfg.Paths.Devices = filepath.Join(configDir, "devices.yaml")
		cfg.Paths.Automations = filepath.Join(configDir, "automations.yaml")
		cfg.Paths.ActionPolicy = filepath.Join(configDir, "action_policy.yaml")
		cfg.Paths.CGEChains = filepath.Join(configDir, "cge_critical_chains.yaml")
		cfg.Paths.CGEProfile = filepath.Join(configDir, "cge_profile.yaml")
		cfg.Paths.NetworkConfig = filepath.Join(configDir, "network.yaml")
		cfg.Paths.MediaMTXConfig = filepath.Join(configDir, "mediamtx.yml")
		cfg.Paths.VersionFile = filepath.Join(configDir, "version.json")
		cfg.Paths.TLSCert = filepath.Join(configDir, "tls", "synora.crt")
		cfg.Paths.TLSKey = filepath.Join(configDir, "tls", "synora.key")
	}
	set("SYNORA_SECURITY", &cfg.Paths.Security)
	set("SYNORA_AUTH", &cfg.Paths.Auth)
	set("SYNORA_TOPOLOGY", &cfg.Paths.Topology)
	set("SYNORA_RESIDENTS", &cfg.Paths.Residents)
	set("SYNORA_DEVICE", &cfg.Paths.Devices)
	set("SYNORA_AUTOMATION", &cfg.Paths.Automations)
	set("SYNORA_ACTION_POLICY", &cfg.Paths.ActionPolicy)
	set("SYNORA_CGE_CRITICAL_CHAINS", &cfg.Paths.CGEChains)
	set("SYNORA_CGE_PROFILE", &cfg.Paths.CGEProfile)
	set("SYNORA_CGE_FEEDBACK", &cfg.Paths.CGEFeedback)
	set("SYNORA_NETWORK_CONFIG", &cfg.Paths.NetworkConfig)
	set("SYNORA_CONNECTIVITY_CONFIG", &cfg.Paths.NetworkConfig)
	set("SYNORA_BUS", &cfg.Paths.BusSocket)
	set("SYNORA_VISION_WORKER_SOCKET", &cfg.Paths.VisionWorkerSocket)
	set("SYNORA_STATE_PATH", &cfg.Paths.State)
	set("SYNORA_CLIP_ROOT", &cfg.Paths.ClipRoot)
	set("SYNORA_CLIP_DIR", &cfg.Paths.ClipRoot)
	set("SYNORA_FACE_DATA_ROOT", &cfg.Paths.FaceDataRoot)
	set("SYNORA_MODEL_ROOT", &cfg.Paths.ModelRoot)
	set("SYNORA_BACKUP_ROOT", &cfg.Paths.BackupRoot)
	set("SYNORA_WEB_ROOT", &cfg.Paths.WebRoot)
	set("SYNORA_MEDIAMTX_CONFIG", &cfg.Paths.MediaMTXConfig)
	set("SYNORA_OTA_JOURNAL", &cfg.Paths.OTAJournal)
	set("SYNORA_CAMERA_OTA_ROOT", &cfg.Paths.CameraOTARoot)
	set("SYNORA_CONNECTIVITY_DIR", &cfg.Paths.ConnectivityRoot)
	set("SYNORA_SESSION_STORE", &cfg.Paths.SessionStore)
	set("SYNORA_IDENTITY_REGISTRY", &cfg.Paths.IdentityRegistry)
	set("SYNORA_VERSION_FILE", &cfg.Paths.VersionFile)
	set("SYNORA_TLS_CERT_FILE", &cfg.Paths.TLSCert)
	set("SYNORA_TLS_KEY_FILE", &cfg.Paths.TLSKey)
	set("SYNORA_HTTP_ADDR", &cfg.Endpoints.HTTP)
	set("SYNORA_HTTPS_ADDR", &cfg.Endpoints.HTTPS)
	set("SYNORA_VISION_HEALTH_ADDR", &cfg.Endpoints.VisionHealth)
	set("SYNORA_VISION_HTTPS_ADDR", &cfg.Endpoints.VisionHTTPS)
	set("SYNORA_MEDIAMTX_RTSP_URL", &cfg.Endpoints.MediaMTXRTSPURL)

	for key, destination := range map[string]*time.Duration{
		"SYNORA_BUS_CONNECT_TIMEOUT": &cfg.Timeouts.BusConnect,
		"SYNORA_BUS_RPC_TIMEOUT":     &cfg.Timeouts.BusRPC,
		"SYNORA_HTTP_READ_TIMEOUT":   &cfg.Timeouts.HTTPRead,
		"SYNORA_HTTP_WRITE_TIMEOUT":  &cfg.Timeouts.HTTPWrite,
		"SYNORA_HTTP_IDLE_TIMEOUT":   &cfg.Timeouts.HTTPIdle,
		"SYNORA_HTTP_HEADER_TIMEOUT": &cfg.Timeouts.HTTPReadHeader,
		"SYNORA_SHUTDOWN_TIMEOUT":    &cfg.Timeouts.Shutdown,
		"SYNORA_VISION_TIMEOUT":      &cfg.Timeouts.VisionWorker,
	} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("runtime configuration %s: invalid duration %q: %w", key, value, err)
			}
			*destination = parsed
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	paths := map[string]string{
		"config_dir": c.Paths.ConfigDir, "security": c.Paths.Security, "auth": c.Paths.Auth,
		"topology": c.Paths.Topology, "residents": c.Paths.Residents, "devices": c.Paths.Devices,
		"automations": c.Paths.Automations, "action_policy": c.Paths.ActionPolicy, "cge_chains": c.Paths.CGEChains,
		"cge_profile": c.Paths.CGEProfile, "cge_feedback": c.Paths.CGEFeedback,
		"network_config": c.Paths.NetworkConfig, "bus_socket": c.Paths.BusSocket, "vision_worker_socket": c.Paths.VisionWorkerSocket,
		"state": c.Paths.State, "clip_root": c.Paths.ClipRoot, "face_data_root": c.Paths.FaceDataRoot,
		"model_root": c.Paths.ModelRoot, "backup_root": c.Paths.BackupRoot, "web_root": c.Paths.WebRoot,
		"mediamtx_config": c.Paths.MediaMTXConfig, "ota_journal": c.Paths.OTAJournal,
		"camera_ota_root": c.Paths.CameraOTARoot, "connectivity_root": c.Paths.ConnectivityRoot, "session_store": c.Paths.SessionStore, "identity_registry": c.Paths.IdentityRegistry, "version_file": c.Paths.VersionFile, "tls_cert": c.Paths.TLSCert, "tls_key": c.Paths.TLSKey,
	}
	for name, path := range paths {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("runtime path %s must be a clean absolute path", name)
		}
	}
	for name, address := range map[string]string{"http": c.Endpoints.HTTP, "https": c.Endpoints.HTTPS, "vision_health": c.Endpoints.VisionHealth, "vision_https": c.Endpoints.VisionHTTPS} {
		if err := validateListenAddress(address); err != nil {
			return fmt.Errorf("runtime endpoint %s: %w", name, err)
		}
	}
	u, err := url.Parse(c.Endpoints.MediaMTXRTSPURL)
	if err != nil || u.Scheme != "rtsp" || u.Host == "" || u.Path == "." {
		return fmt.Errorf("runtime endpoint mediamtx_rtsp_url must be an rtsp URL")
	}
	for name, timeout := range map[string]time.Duration{"bus_connect": c.Timeouts.BusConnect, "bus_rpc": c.Timeouts.BusRPC, "http_read": c.Timeouts.HTTPRead, "http_write": c.Timeouts.HTTPWrite, "http_idle": c.Timeouts.HTTPIdle, "http_header": c.Timeouts.HTTPReadHeader, "shutdown": c.Timeouts.Shutdown, "vision": c.Timeouts.VisionWorker} {
		if timeout <= 0 {
			return fmt.Errorf("runtime timeout %s must be positive", name)
		}
	}
	return nil
}

func validateListenAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errors.New("listen address is required")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host != "" && net.ParseIP(host) == nil {
		return fmt.Errorf("invalid host %q", host)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 0 || parsedPort > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return nil
}
