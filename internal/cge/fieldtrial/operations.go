package fieldtrial

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"synora/internal/version"
)

type CheckResult struct {
	Code    string `json:"code"`
	Success bool   `json:"success"`
	Detail  string `json:"detail,omitempty"`
}

type PreflightReport struct {
	GeneratedAt                       time.Time     `json:"generated_at"`
	Success                           bool          `json:"success"`
	CognitiveConfigurationFingerprint string        `json:"cognitive_configuration_fingerprint,omitempty"`
	ConfigurationChecks               []CheckResult `json:"configuration_checks"`
	StorageChecks                     []CheckResult `json:"storage_checks"`
	KeyChecks                         []CheckResult `json:"key_checks"`
	TopologyChecks                    []CheckResult `json:"topology_checks"`
	RuntimeChecks                     []CheckResult `json:"runtime_checks"`
	AvailableBytes                    int64         `json:"available_bytes"`
	ConfiguredQuotaBytes              int64         `json:"configured_quota_bytes"`
	BlockingReasons                   []string      `json:"blocking_reasons,omitempty"`
	Warnings                          []string      `json:"warnings,omitempty"`
}

type PreflightOptions struct {
	Config                            Config
	KeyFile                           string
	TopologyFile                      string
	CognitiveConfigurationFingerprint string
}

func RunPreflight(ctx context.Context, options PreflightOptions) (PreflightReport, error) {
	report := PreflightReport{GeneratedAt: time.Now().UTC(), ConfiguredQuotaBytes: options.Config.MaximumTotalBytes}
	if err := contextErr(ctx); err != nil {
		return report, err
	}
	add := func(list *[]CheckResult, code string, success bool, detail string) {
		*list = append(*list, CheckResult{Code: code, Success: success, Detail: detail})
		if !success {
			report.BlockingReasons = append(report.BlockingReasons, code)
		}
	}
	if err := options.Config.Validate(); err != nil {
		add(&report.ConfigurationChecks, "config.invalid", false, "configuration bounds or path invalid")
		return report, nil
	}
	add(&report.ConfigurationChecks, "config.valid", true, "field-trial configuration validated")
	if options.CognitiveConfigurationFingerprint != "" {
		report.CognitiveConfigurationFingerprint = options.CognitiveConfigurationFingerprint
	}
	root := options.Config.RootDir
	rootInfo, rootErr := os.Lstat(root)
	if rootErr == nil && rootInfo.Mode()&os.ModeSymlink != 0 {
		add(&report.StorageChecks, "storage.not_writable", false, "root is a symlink")
	} else if rootErr == nil && !rootInfo.IsDir() {
		add(&report.StorageChecks, "storage.not_writable", false, "root exists but is not a directory")
	} else {
		parent := filepath.Dir(root)
		if rootErr == nil && rootInfo.IsDir() {
			probe, err := os.CreateTemp(root, ".preflight-*")
			if err != nil {
				add(&report.StorageChecks, "storage.not_writable", false, "root cannot create a temporary file")
			} else {
				_ = probe.Close()
				_ = os.Remove(probe.Name())
				add(&report.StorageChecks, "storage.writable", true, "temporary write probe succeeded")
			}
		} else if info, err := os.Stat(parent); err != nil || !info.IsDir() {
			add(&report.StorageChecks, "storage.not_writable", false, "root parent is unavailable")
		} else {
			add(&report.StorageChecks, "storage.writable", true, "root can be created by the operator")
		}
		if stat, err := os.Stat(parent); err == nil {
			var usage syscall.Statfs_t
			if err := syscall.Statfs(parent, &usage); err == nil {
				report.AvailableBytes = int64(usage.Bavail) * int64(usage.Bsize)
			}
			_ = stat
		}
	}
	if report.AvailableBytes > 0 && options.Config.MaximumTotalBytes > 0 && report.AvailableBytes < options.Config.MaximumTotalBytes {
		add(&report.StorageChecks, "storage.low_space", false, "available space is below configured quota")
	} else {
		add(&report.StorageChecks, "storage.space", true, "available space is compatible with quota")
	}
	keyPath := options.KeyFile
	if keyPath == "" {
		keyPath = options.Config.PseudonymizationKeyFile
	}
	if keyPath == "" {
		add(&report.KeyChecks, "key.valid", false, "key path is not configured")
	} else if info, err := os.Lstat(keyPath); err != nil {
		add(&report.KeyChecks, "key.valid", false, "key file is unavailable")
	} else if info.Mode()&os.ModeSymlink != 0 {
		add(&report.KeyChecks, "key.symlink_refused", false, "key file is a symlink")
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() < 16 {
		add(&report.KeyChecks, "key.valid", false, "key must be regular, >=16 bytes and mode 0600")
	} else {
		file, readErr := os.Open(keyPath)
		if readErr == nil {
			buffer := make([]byte, 1)
			_, readErr = file.Read(buffer)
			_ = file.Close()
		}
		add(&report.KeyChecks, "key.valid", readErr == nil, "key file is protected and readable")
	}
	topologyPath := options.TopologyFile
	if topologyPath == "" {
		topologyPath = options.Config.TopologyFile
	}
	if topologyPath == "" {
		report.Warnings = append(report.Warnings, "topology.not_configured")
		report.TopologyChecks = append(report.TopologyChecks, CheckResult{Code: "topology.valid", Success: true, Detail: "no static topology configured; runtime provider remains partial"})
	} else if topology, err := LoadTopologyFile(topologyPath); err != nil {
		add(&report.TopologyChecks, "topology.invalid", false, "topology validation failed")
	} else {
		add(&report.TopologyChecks, "topology.valid", true, fmt.Sprintf("revision=%s nodes=%d edges=%d", topology.Revision, len(topology.Nodes), len(topology.Edges)))
	}
	if info, err := os.Lstat(version.DefaultPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			add(&report.RuntimeChecks, "runtime.version_missing", false, "version path is not a regular file")
		} else if _, loadErr := version.Load(version.DefaultPath); loadErr != nil {
			add(&report.RuntimeChecks, "runtime.version_missing", false, "version manifest is unreadable")
		} else {
			add(&report.RuntimeChecks, "runtime.version_available", true, "version manifest validated")
		}
	} else if os.IsNotExist(err) {
		report.Warnings = append(report.Warnings, "runtime.version_missing")
		report.RuntimeChecks = append(report.RuntimeChecks, CheckResult{Code: "runtime.version_missing", Success: true, Detail: "optional version manifest is not present in this environment"})
	} else {
		add(&report.RuntimeChecks, "runtime.version_missing", false, "version manifest cannot be inspected")
	}
	report.Success = len(report.BlockingReasons) == 0
	return report, nil
}

func GenerateKey(path string, force bool) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", ErrInvalidConfig
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: key path is a symlink", ErrKeyUnavailable)
		}
		if !force {
			return "", fmt.Errorf("%w: key exists", ErrKeyUnavailable)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := file.Chmod(0o600); err == nil {
		_, err = file.Write(key)
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	digest := sha256.Sum256(key)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type DeploymentManifest struct {
	SchemaVersion                     string    `json:"schema_version"`
	PreparedAt                        time.Time `json:"prepared_at"`
	SynoraVersion                     string    `json:"synora_version,omitempty"`
	Commit                            string    `json:"commit,omitempty"`
	Architecture                      string    `json:"architecture"`
	CognitiveConfigurationFingerprint string    `json:"cognitive_configuration_fingerprint"`
	EnvironmentTemplateFingerprint    string    `json:"environment_template_fingerprint,omitempty"`
	TopologyFingerprint               string    `json:"topology_fingerprint,omitempty"`
	KeyFingerprint                    string    `json:"key_fingerprint,omitempty"`
	FieldTrialRoot                    string    `json:"field_trial_root"`
	PlannedSessionID                  string    `json:"planned_session_id,omitempty"`
	PreflightPassed                   bool      `json:"preflight_passed"`
}

func WriteDeploymentManifest(path string, manifest DeploymentManifest) error {
	if path == "" || filepath.Clean(path) == "." {
		return ErrInvalidConfig
	}
	manifest.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".deployment-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	_ = tmp.Chmod(0o640)
	if _, err := tmp.Write(append(data, '\n')); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmpPath, path)
}

func FingerprintFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type OperationalTrialStatus struct {
	SessionID                         string        `json:"session_id"`
	Status                            string        `json:"status"`
	StartedAt                         time.Time     `json:"started_at"`
	Duration                          time.Duration `json:"duration"`
	CognitiveConfigurationFingerprint string        `json:"cognitive_configuration_fingerprint,omitempty"`
	EventCount                        uint64        `json:"event_count"`
	AnnotationCount                   uint64        `json:"annotation_count"`
	SegmentCount                      int           `json:"segment_count"`
	TotalBytes                        int64         `json:"total_bytes"`
	RecorderState                     string        `json:"recorder_state"`
	RecorderErrors                    uint64        `json:"recorder_errors"`
	AvailableBytes                    int64         `json:"available_bytes"`
	QuotaRemainingBytes               int64         `json:"quota_remaining_bytes"`
	LastCheckpointAt                  *time.Time    `json:"last_checkpoint_at,omitempty"`
	LastEventAt                       *time.Time    `json:"last_event_at,omitempty"`
	ConfigurationDrift                bool          `json:"configuration_drift"`
	Warnings                          []string      `json:"warnings,omitempty"`
	BlockingReasons                   []string      `json:"blocking_reasons,omitempty"`
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
