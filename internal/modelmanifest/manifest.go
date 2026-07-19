// Package modelmanifest describes non-secret model requirements for an image.
package modelmanifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Model struct {
	Required      bool   `yaml:"required" json:"required"`
	Component     string `yaml:"component" json:"component"`
	MissingStatus string `yaml:"missing_status,omitempty" json:"missing_status,omitempty"`
}

type Manifest struct {
	SchemaVersion int               `yaml:"schema_version" json:"schema_version"`
	Models        map[string]Model  `yaml:"models" json:"models"`
	Policy        map[string]string `yaml:"policy" json:"policy"`
}

func Default() Manifest {
	return Manifest{SchemaVersion: 1, Models: map[string]Model{
		"arcface_w600k_r50.rknn": {Required: true, Component: "face_recognition"},
		"det_10g.rknn":           {Required: true, Component: "face_detection"},
		"yolov8.rknn":            {Required: true, Component: "person_detection"},
		"weapon.rknn":            {Required: false, Component: "weapon_detection", MissingStatus: "degraded"},
	}, Policy: map[string]string{"missing_required": "fatal"}}
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read model manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse model manifest: %w", err)
	}
	if manifest.SchemaVersion <= 0 || len(manifest.Models) == 0 {
		return Manifest{}, fmt.Errorf("invalid model manifest")
	}
	return manifest, nil
}

func LoadOrDefault(path string) Manifest {
	manifest, err := Load(path)
	if err != nil {
		return Default()
	}
	return manifest
}

type Check struct {
	Name      string
	Required  bool
	Present   bool
	Status    string
	Component string
}

func (m Manifest) Check(root string) []Check {
	root = strings.TrimSpace(root)
	names := make([]string, 0, len(m.Models))
	for name := range m.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	checks := make([]Check, 0, len(names))
	for _, name := range names {
		model := m.Models[name]
		present := false
		if filepath.Base(name) == name && root != "" {
			if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
				present = true
			}
		}
		status := "ok"
		if !present {
			status = "degraded"
			if model.Required {
				status = "fatal"
			}
		}
		checks = append(checks, Check{Name: name, Required: model.Required, Present: present, Status: status, Component: model.Component})
	}
	return checks
}
