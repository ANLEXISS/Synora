// Package version contains the non-secret image identity exposed by Synora.
package version

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

const DefaultPath = "/opt/synora/version.json"

// Manifest is intentionally limited to build and compatibility metadata.
// Never add credentials, device identifiers, tokens, or private key material.
type Manifest struct {
	ImageVersion        string `json:"image_version"`
	SynoraVersion       string `json:"synora_version"`
	GitCommit           string `json:"git_commit"`
	BuildTime           string `json:"build_time"`
	TargetBoard         string `json:"target_board"`
	OSBase              string `json:"os_base"`
	KernelExpected      string `json:"kernel_expected"`
	RKNNRuntimeExpected string `json:"rknn_runtime_expected"`
	ConfigSchemaVersion int    `json:"config_schema_version"`
	BundleID            string `json:"bundle_id"`
}

func Fallback() Manifest {
	return Manifest{
		ImageVersion:        "unknown",
		SynoraVersion:       "unknown",
		GitCommit:           "unknown",
		BuildTime:           "unknown",
		TargetBoard:         "unknown",
		OSBase:              "unknown",
		KernelExpected:      "unknown",
		RKNNRuntimeExpected: "unknown",
		ConfigSchemaVersion: 1,
		BundleID:            "unversioned",
	}
}

func Load(path string) (Manifest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read version manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse version manifest: %w", err)
	}
	if manifest.ConfigSchemaVersion <= 0 {
		return Manifest{}, errors.New("version manifest has invalid config_schema_version")
	}
	return manifest, nil
}

func LoadOrFallback(path string) Manifest {
	manifest, err := Load(path)
	if err != nil {
		return Fallback()
	}
	return manifest
}

type Runtime struct {
	Manifest
	RuntimeKernel string   `json:"runtime_kernel"`
	RuntimeArch   string   `json:"runtime_arch"`
	UptimeSeconds *float64 `json:"uptime_seconds,omitempty"`
	SlotCurrent   string   `json:"slot_current"`
	SlotSource    string   `json:"slot_source"`
}

func Current(path string) Runtime {
	manifest := LoadOrFallback(path)
	kernel := KernelRelease()
	return Runtime{
		Manifest:      manifest,
		RuntimeKernel: kernel,
		RuntimeArch:   runtime.GOARCH,
		UptimeSeconds: readUptime(),
		SlotCurrent:   "unmanaged",
		SlotSource:    "placeholder-rauc-not-installed",
	}
}

func KernelRelease() string {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return "unknown"
	}
	var raw []byte
	for _, value := range uts.Release {
		if value == 0 {
			break
		}
		raw = append(raw, byte(value))
	}
	if len(raw) == 0 {
		return "unknown"
	}
	return string(raw)
}

func readUptime() *float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil
	}
	var uptime float64
	if _, err := fmt.Sscanf(string(data), "%f", &uptime); err != nil {
		return nil
	}
	return &uptime
}
