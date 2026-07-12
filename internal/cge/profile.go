package cge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"synora/pkg/contract"
)

type ProfileStore struct {
	mu      sync.RWMutex
	path    string
	profile contract.CgeSecurityProfile
	loaded  bool
}

func NewProfileStore(path string) *ProfileStore {
	return &ProfileStore{path: strings.TrimSpace(path), profile: contract.NormalizeCgeSecurityProfile(contract.DefaultCgeSecurityProfile())}
}

func (s *ProfileStore) Load() (contract.CgeSecurityProfile, bool, error) {
	if s == nil {
		return contract.DefaultCgeSecurityProfile(), false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		s.profile = contract.NormalizeCgeSecurityProfile(contract.DefaultCgeSecurityProfile())
		s.loaded = false
		return s.profile, false, nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.profile = contract.NormalizeCgeSecurityProfile(contract.DefaultCgeSecurityProfile())
		s.loaded = false
		return s.profile, false, nil
	}
	if err != nil {
		return contract.CgeSecurityProfile{}, false, err
	}
	profile, err := decodeProfile(data)
	if err != nil {
		return contract.CgeSecurityProfile{}, false, err
	}
	if err := ValidateProfile(profile); err != nil {
		return contract.CgeSecurityProfile{}, false, err
	}
	profile = contract.NormalizeCgeSecurityProfile(profile)
	s.profile = profile
	s.loaded = true
	return profile, true, nil
}

func (s *ProfileStore) Get() contract.CgeSecurityProfile {
	if s == nil {
		return contract.DefaultCgeSecurityProfile()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneProfile(s.profile)
}

func (s *ProfileStore) Update(raw []byte) (contract.CgeSecurityProfile, error) {
	if s == nil {
		return contract.CgeSecurityProfile{}, errors.New("cge profile unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.profile
	current = contract.NormalizeCgeSecurityProfile(current)
	updated, err := mergeJSONProfile(current, raw)
	if err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	if err := ValidateProfile(updated); err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	updated = contract.NormalizeCgeSecurityProfile(updated)
	if err := writeProfileAtomic(s.path, updated); err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	s.profile = updated
	s.loaded = true
	return cloneProfile(updated), nil
}

func ValidateProfile(profile contract.CgeSecurityProfile) error {
	switch profile.Mode {
	case contract.CgeSecurityRelaxed, contract.CgeSecurityBalanced, contract.CgeSecurityStrict, contract.CgeSecurityParanoid:
	default:
		return fmt.Errorf("invalid security mode %q", profile.Mode)
	}
	if profile.GlobalSensitivity < 0 || profile.GlobalSensitivity > 1 {
		return errors.New("global_sensitivity must be between 0 and 1")
	}
	if profile.NightSensitivityMultiplier <= 0 || profile.NightSensitivityMultiplier > 5 {
		return errors.New("night_sensitivity_multiplier must be greater than 0 and at most 5")
	}
	if profile.ArmedSensitivityMultiplier <= 0 || profile.ArmedSensitivityMultiplier > 5 {
		return errors.New("armed_sensitivity_multiplier must be greater than 0 and at most 5")
	}
	switch strings.ToLower(profile.UnknownPersonTolerance) {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("invalid unknown_person_tolerance %q", profile.UnknownPersonTolerance)
	}
	if !validDangerLevel(profile.MinimumNotifyDangerLevel) || !validDangerLevel(profile.MinimumAutoActionDangerLevel) {
		return errors.New("invalid minimum danger level")
	}
	if profile.UnknownPersistenceSeconds < 1 || profile.UnknownPersistenceSeconds > 86400 {
		return errors.New("unknown_persistence_seconds must be between 1 and 86400")
	}
	if profile.SignificantInactivityTimeoutSeconds < 1 || profile.SignificantInactivityTimeoutSeconds > 86400 {
		return errors.New("significant_inactivity_timeout_seconds must be between 1 and 86400")
	}
	return nil
}

func decodeProfile(data []byte) (contract.CgeSecurityProfile, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	if nested, ok := raw["security_profile"].(map[string]any); ok {
		raw = nested
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	profile, err := mergeJSONProfile(contract.DefaultCgeSecurityProfile(), jsonData)
	if err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	return contract.NormalizeCgeSecurityProfile(profile), nil
}

// NormalizeCgeSecurityProfileJSON merges a partial JSON profile with the
// defaults and returns a normalized profile for API boundaries.
func NormalizeCgeSecurityProfileJSON(raw []byte) (contract.CgeSecurityProfile, error) {
	profile, err := mergeJSONProfile(contract.DefaultCgeSecurityProfile(), raw)
	if err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	return contract.NormalizeCgeSecurityProfile(profile), nil
}

func mergeJSONProfile(current contract.CgeSecurityProfile, raw []byte) (contract.CgeSecurityProfile, error) {
	var patch map[string]any
	if err := json.Unmarshal(raw, &patch); err != nil {
		return contract.CgeSecurityProfile{}, fmt.Errorf("invalid cge security profile: %w", err)
	}
	if nested, ok := patch["security_profile"].(map[string]any); ok {
		patch = nested
	}
	baseBytes, _ := json.Marshal(current)
	var base map[string]any
	_ = json.Unmarshal(baseBytes, &base)
	for key, value := range patch {
		if value == nil {
			switch key {
			case "critical_rooms", "ignored_motion_rooms":
				value = []any{}
			default:
				continue
			}
		}
		base[key] = value
	}
	mergedBytes, _ := json.Marshal(base)
	var updated contract.CgeSecurityProfile
	if err := json.Unmarshal(mergedBytes, &updated); err != nil {
		return contract.CgeSecurityProfile{}, err
	}
	return updated, nil
}

func writeProfileAtomic(path string, profile contract.CgeSecurityProfile) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("cge profile path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", data, 0600)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cge-profile-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	data, err := yaml.Marshal(struct {
		SecurityProfile contract.CgeSecurityProfile `yaml:"security_profile"`
	}{profile})
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func validDangerLevel(level contract.DangerLevel) bool {
	switch level {
	case contract.DangerNone, contract.DangerLow, contract.DangerMedium, contract.DangerHigh, contract.DangerCritical:
		return true
	default:
		return false
	}
}

func cloneProfile(profile contract.CgeSecurityProfile) contract.CgeSecurityProfile {
	profile = contract.NormalizeCgeSecurityProfile(profile)
	profile.CriticalRooms = append([]string{}, profile.CriticalRooms...)
	profile.IgnoredMotionRooms = append([]string{}, profile.IgnoredMotionRooms...)
	return profile
}
