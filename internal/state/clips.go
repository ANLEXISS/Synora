package state

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

type ClipRetentionConfig struct {
	MaxAge             time.Duration
	MaxCount           int
	MaxBytes           int64
	AcknowledgedMinAge time.Duration
	MinFreeBytes       int64
}

func DefaultClipRetentionConfig() ClipRetentionConfig {
	return ClipRetentionConfig{
		MaxAge:             24 * time.Hour,
		MaxCount:           500,
		MaxBytes:           5 << 30,
		AcknowledgedMinAge: 7 * 24 * time.Hour,
		MinFreeBytes:       512 << 20,
	}
}

// PurgeClips expires physical files without deleting their metadata. Active
// incident evidence is protected; if only protected clips remain, no file is
// removed and the caller can reject future intake based on the remaining
// capacity.
func (s *Store) PurgeClips(now time.Time, cfg ClipRetentionConfig) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if cfg.MaxAge <= 0 || cfg.MaxCount <= 0 || cfg.MaxBytes <= 0 || cfg.AcknowledgedMinAge <= 0 || cfg.MinFreeBytes <= 0 {
		defaults := DefaultClipRetentionConfig()
		if cfg.MaxAge <= 0 {
			cfg.MaxAge = defaults.MaxAge
		}
		if cfg.MaxCount <= 0 {
			cfg.MaxCount = defaults.MaxCount
		}
		if cfg.MaxBytes <= 0 {
			cfg.MaxBytes = defaults.MaxBytes
		}
		if cfg.AcknowledgedMinAge <= 0 {
			cfg.AcknowledgedMinAge = defaults.AcknowledgedMinAge
		}
		if cfg.MinFreeBytes <= 0 {
			cfg.MinFreeBytes = defaults.MinFreeBytes
		}
	}
	var removed []string
	for {
		candidate, totalBytes, count := s.clipPurgeCandidate(now, cfg)
		if candidate == nil {
			break
		}
		if totalBytes <= cfg.MaxBytes && count <= cfg.MaxCount && !clipPastAge(candidate, now, cfg) {
			break
		}
		if candidate.Path != "" {
			if err := os.Remove(candidate.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return removed, err
			}
		}
		s.mu.Lock()
		current := s.Clips[candidate.ID]
		if current != nil && current.Revision == candidate.Revision && current.Status != contract.ClipStatusExpired {
			current.Status = contract.ClipStatusExpired
			current.ExpiresAt = now
			current.UpdatedAt = now
			current.Revision++
			s.revision.Add(1)
			removed = append(removed, current.ID)
		}
		s.mu.Unlock()
		_ = s.SaveNow()
	}
	return removed, nil
}

func (s *Store) clipPurgeCandidate(now time.Time, cfg ClipRetentionConfig) (*contract.Clip, int64, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var totalBytes int64
	count := 0
	candidates := make([]contract.Clip, 0, len(s.Clips))
	for _, value := range s.Clips {
		if value == nil || value.Status == contract.ClipStatusExpired || value.Status == contract.ClipStatusMissing {
			continue
		}
		if value.Path != "" {
			if info, err := os.Lstat(value.Path); err == nil && info.Mode().IsRegular() {
				count++
				totalBytes += info.Size()
			} else if value.SizeBytes > 0 {
				count++
				totalBytes += value.SizeBytes
			}
		}
		if clipProtectedByIncidentLocked(s, value, now, cfg) {
			continue
		}
		candidates = append(candidates, cloneClip(value))
	}
	eligibleForAge := make([]contract.Clip, 0, len(candidates))
	for _, candidate := range candidates {
		if clipPastAge(&candidate, now, cfg) {
			eligibleForAge = append(eligibleForAge, candidate)
		}
	}
	byPolicy := func(values []contract.Clip) {
		sort.Slice(values, func(i, j int) bool {
			left, right := s.clipPurgeRankLocked(&values[i]), s.clipPurgeRankLocked(&values[j])
			if left != right {
				return left < right
			}
			if values[i].CreatedAt.Equal(values[j].CreatedAt) {
				return values[i].ID < values[j].ID
			}
			return values[i].CreatedAt.Before(values[j].CreatedAt)
		})
	}
	if len(candidates) == 0 {
		return nil, totalBytes, count
	}
	if totalBytes > cfg.MaxBytes || count > cfg.MaxCount {
		byPolicy(candidates)
		return &candidates[0], totalBytes, count
	}
	if len(eligibleForAge) == 0 {
		return nil, totalBytes, count
	}
	byPolicy(eligibleForAge)
	return &eligibleForAge[0], totalBytes, count
}

func (s *Store) clipPurgeRankLocked(value *contract.Clip) int {
	if value == nil {
		return 99
	}
	if clipHasAcknowledgedIncidentLocked(s, value) {
		// Acknowledged evidence is eligible only after the configured minimum
		// age and remains behind ordinary failed/processed clips.
		return 3
	}
	return purgeRank(*value)
}

func clipHasAcknowledgedIncidentLocked(s *Store, clip *contract.Clip) bool {
	if s == nil || clip == nil {
		return false
	}
	ids := append([]string(nil), clip.IncidentIDs...)
	for incidentID, incident := range s.Incidents {
		if incident != nil && containsString(incident.ClipIDs, clip.ID) && !containsString(ids, incidentID) {
			ids = append(ids, incidentID)
		}
	}
	for _, incidentID := range ids {
		if incident := s.Incidents[strings.TrimSpace(incidentID)]; incident != nil && incident.Status == contract.IncidentStatusAcknowledged {
			return true
		}
	}
	return false
}

func clipProtectedByIncidentLocked(s *Store, clip *ClipState, now time.Time, cfg ClipRetentionConfig) bool {
	incidentIDs := append([]string(nil), clip.IncidentIDs...)
	for incidentID, incident := range s.Incidents {
		if containsString(incident.ClipIDs, clip.ID) && !containsString(incidentIDs, incidentID) {
			incidentIDs = append(incidentIDs, incidentID)
		}
	}
	for _, incidentID := range incidentIDs {
		if incident := s.Incidents[strings.TrimSpace(incidentID)]; incident != nil {
			if incident.Status == contract.IncidentStatusNew || incident.Status == contract.IncidentStatusViewed {
				return true
			}
			if incident.Status == contract.IncidentStatusAcknowledged && now.Sub(incident.UpdatedAt) < cfg.AcknowledgedMinAge {
				return true
			}
		}
	}
	return false
}

func purgeRank(value contract.Clip) int {
	if value.Status == contract.ClipStatusFailed {
		return 0
	}
	if value.Status == contract.ClipStatusProcessed {
		return 1
	}
	if value.Status == contract.ClipStatusReady || value.Status == contract.ClipStatusProcessing {
		return 2
	}
	return 3
}

func clipPastAge(value *contract.Clip, now time.Time, cfg ClipRetentionConfig) bool {
	return value != nil && !value.CreatedAt.IsZero() && now.Sub(value.CreatedAt) >= cfg.MaxAge
}
