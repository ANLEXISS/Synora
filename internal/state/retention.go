package state

import (
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

// PurgeRecentEvents expires diagnostic/event history while preserving events
// referenced by a retained incident. This keeps incident timelines
// referentially valid even when the transient event window is short-lived.
func (s *Store) PurgeRecentEvents(now time.Time, maxAge time.Duration) int {
	if s == nil || maxAge <= 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	removed := 0
	keep := make([]*contract.Event, 0, len(s.RecentEvents))
	for _, event := range s.RecentEvents {
		if event == nil {
			removed++
			continue
		}
		if event.Timestamp.IsZero() || now.Sub(event.Timestamp.UTC()) < maxAge || s.eventReferencedByIncidentLocked(event.ID) {
			keep = append(keep, event)
			continue
		}
		removed++
	}
	s.RecentEvents = keep
	validationKeep := make([]*contract.Event, 0, len(s.ValidationEvents))
	for _, event := range s.ValidationEvents {
		if event == nil || (!event.Timestamp.IsZero() && now.Sub(event.Timestamp.UTC()) >= maxAge) {
			removed++
			continue
		}
		validationKeep = append(validationKeep, event)
	}
	s.ValidationEvents = validationKeep
	if removed > 0 {
		s.revision.Add(1)
	}
	s.mu.Unlock()
	if removed > 0 {
		_ = s.SaveNow()
	}
	return removed
}

// PurgeIncidents removes only resolved or acknowledged incidents older than
// maxAge. Active incidents remain protected. References are detached from
// clip metadata in the same critical section before persistence.
func (s *Store) PurgeIncidents(now time.Time, maxAge time.Duration) []string {
	if s == nil || maxAge <= 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	candidates := make([]*contract.Incident, 0)
	for _, incident := range s.Incidents {
		if incident == nil || (incident.Status != contract.IncidentStatusAcknowledged && incident.Status != contract.IncidentStatusResolved) {
			continue
		}
		at := incident.UpdatedAt
		if at.IsZero() {
			at = incident.CreatedAt
		}
		if !at.IsZero() && now.Sub(at.UTC()) >= maxAge {
			candidates = append(candidates, incident)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := incidentRetentionTime(candidates[i]), incidentRetentionTime(candidates[j])
		if left.Equal(right) {
			return candidates[i].ID < candidates[j].ID
		}
		return left.Before(right)
	})
	removed := make([]string, 0, len(candidates))
	for _, incident := range candidates {
		delete(s.Incidents, incident.ID)
		removed = append(removed, incident.ID)
		for _, clipID := range incident.ClipIDs {
			if clip := s.Clips[strings.TrimSpace(clipID)]; clip != nil {
				clip.IncidentIDs = removeString(clip.IncidentIDs, incident.ID)
				clip.UpdatedAt = now
				clip.Revision++
			}
		}
	}
	if len(removed) > 0 {
		s.revision.Add(uint64(len(removed)))
	}
	s.mu.Unlock()
	if len(removed) > 0 {
		_ = s.SaveNow()
	}
	return removed
}

func (s *Store) eventReferencedByIncidentLocked(eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	for _, incident := range s.Incidents {
		if incident != nil && containsString(incident.EventIDs, eventID) {
			return true
		}
	}
	return false
}

func incidentRetentionTime(incident *contract.Incident) time.Time {
	if incident == nil {
		return time.Time{}
	}
	if !incident.UpdatedAt.IsZero() {
		return incident.UpdatedAt.UTC()
	}
	return incident.CreatedAt.UTC()
}

func removeString(values []string, target string) []string {
	result := values[:0]
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}
