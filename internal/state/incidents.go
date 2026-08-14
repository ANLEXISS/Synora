package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const (
	DefaultIncidentLimit       = 200
	DefaultIncidentListLimit   = 50
	maxIncidentTimelineEntries = 100
	maxIncidentEventIDs        = 100
	maxIncidentClipIDs         = 100
	incidentGroupingWindow     = time.Minute
)

// IncidentObservation is the small Core-to-StateStore boundary used when a
// real decision has already been applied to the operational state.
type IncidentObservation struct {
	EventID       string
	EventType     string
	Timestamp     time.Time
	CameraID      string
	NodeID        string
	IdentityKind  contract.IncidentIdentityKind
	ResidentID    string
	EntityID      string
	TrackID       string
	ClipID        string
	SequenceKey   string
	ActivationID  string
	GroupKey      string
	Score         float64
	Confidence    float64
	SecurityState string
	Severity      string
	Cause         contract.IncidentCause
}

// WithIncidentLimit bounds the durable incident collection. Values below one
// are ignored so a caller cannot accidentally disable retention.
func WithIncidentLimit(limit int) Option {
	return func(s *Store) {
		if limit > 0 {
			s.incidentLimit = limit
		}
	}
}

func (s *Store) SetIncident(value *contract.Incident) {
	if s == nil || value == nil || strings.TrimSpace(value.ID) == "" {
		return
	}
	cloned := cloneIncident(value)
	if cloned.Status == "" {
		cloned.Status = contract.IncidentStatusNew
	}
	if cloned.IdentityKind == "" {
		cloned.IdentityKind = contract.IncidentIdentityNone
	}
	s.mu.Lock()
	s.Incidents[cloned.ID] = cloned
	s.trimIncidentsLocked(s.incidentLimit)
	s.revision.Add(1)
	s.mu.Unlock()
}

func (s *Store) Incident(id string) (*contract.Incident, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Incidents[strings.TrimSpace(id)]
	if !ok || value == nil {
		return nil, false
	}
	return cloneIncident(value), true
}

func (s *Store) IncidentsList(limit int) []contract.Incident {
	if s == nil {
		return []contract.Incident{}
	}
	if limit <= 0 {
		limit = DefaultIncidentListLimit
	}
	if limit > s.incidentLimit {
		limit = s.incidentLimit
	}
	s.mu.RLock()
	items := make([]contract.Incident, 0, len(s.Incidents))
	for _, value := range s.Incidents {
		if value != nil {
			items = append(items, *cloneIncident(value))
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		left := items[i].UpdatedAt
		right := items[j].UpdatedAt
		if left.Equal(right) {
			return items[i].ID > items[j].ID
		}
		return left.After(right)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// RecordIncident creates or enriches one incident. It returns whether the
// observation changed the collection. An event already present in any
// incident is ignored, including after acknowledgement, so replay cannot
// reopen or duplicate evidence.
func (s *Store) RecordIncident(input IncidentObservation) (contract.Incident, bool, bool, error) {
	if s == nil {
		return contract.Incident{}, false, false, fmt.Errorf("state store unavailable")
	}
	if strings.TrimSpace(input.SecurityState) != "intrusion" {
		return contract.Incident{}, false, false, fmt.Errorf("incident requires intrusion state")
	}
	if strings.TrimSpace(input.EventID) == "" && input.Timestamp.IsZero() {
		return contract.Incident{}, false, false, fmt.Errorf("incident observation requires event identity or timestamp")
	}
	normalizeIncidentObservation(&input)
	if err := input.IdentityKind.Validate(); err != nil {
		return contract.Incident{}, false, false, contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}

	s.mu.Lock()
	if existing, ok := s.incidentContainingEventLocked(input.EventID, input.TimelineKey()); ok {
		result := *cloneIncident(existing)
		s.mu.Unlock()
		return result, false, false, nil
	}

	incident := s.findActiveIncidentLocked(input)
	created := false
	if incident == nil {
		created = true
		incident = &contract.Incident{
			ID:            idgen.New("incident"),
			Status:        contract.IncidentStatusNew,
			CreatedAt:     input.Timestamp,
			UpdatedAt:     input.Timestamp,
			StartedAt:     input.Timestamp,
			LastEventAt:   input.Timestamp,
			SecurityState: "intrusion",
			Severity:      input.Severity,
			Cause:         input.Cause,
			Score:         input.Score,
			CameraID:      input.CameraID,
			NodeID:        input.NodeID,
			IdentityKind:  input.IdentityKind,
			ResidentID:    input.ResidentID,
			EntityID:      input.EntityID,
			TrackID:       input.TrackID,
		}
		s.Incidents[incident.ID] = incident
	}
	mergeIncidentObservation(incident, input)
	incident.Revision++
	s.trimIncidentsLocked(s.incidentLimit)
	result := *cloneIncident(incident)
	s.revision.Add(1)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, created, true, err
	}
	return result, created, true, nil
}

func (input IncidentObservation) TimelineKey() string {
	if strings.TrimSpace(input.EventID) != "" {
		return strings.TrimSpace(input.EventID)
	}
	material := strings.Join([]string{
		input.EventType, input.Timestamp.UTC().Format(time.RFC3339Nano), input.CameraID,
		input.NodeID, input.TrackID, input.ClipID, input.SequenceKey,
	}, "|")
	digest := sha256.Sum256([]byte(material))
	return "observation-" + hex.EncodeToString(digest[:])
}

func (s *Store) MarkIncidentViewed(id string) (contract.Incident, bool, error) {
	return s.transitionIncident(strings.TrimSpace(id), contract.IncidentStatusViewed)
}

func (s *Store) AcknowledgeIncident(id string) (contract.Incident, bool, error) {
	return s.transitionIncident(strings.TrimSpace(id), contract.IncidentStatusAcknowledged)
}

func (s *Store) ResolveIncident(id string) (contract.Incident, bool, error) {
	return s.transitionIncident(strings.TrimSpace(id), contract.IncidentStatusResolved)
}

func (s *Store) transitionIncident(id string, target contract.IncidentStatus) (contract.Incident, bool, error) {
	if s == nil {
		return contract.Incident{}, false, contract.NewAPIError(contract.ErrorInternal, "state store unavailable")
	}
	if id == "" {
		return contract.Incident{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "incident id is required")
	}
	s.mu.Lock()
	incident, ok := s.Incidents[id]
	if !ok || incident == nil {
		s.mu.Unlock()
		return contract.Incident{}, false, contract.NewAPIError(contract.ErrorNotFound, "incident not found")
	}
	if incident.Status == target {
		result := *cloneIncident(incident)
		s.mu.Unlock()
		return result, false, nil
	}
	if !validIncidentTransition(incident.Status, target) {
		from, to := incident.Status, target
		s.mu.Unlock()
		return contract.Incident{}, false, contract.NewAPIErrorWithDetails(contract.ErrorConflict,
			map[string]any{"from": from, "to": to}, "incident transition from %s to %s is not allowed", from, to)
	}
	now := time.Now().UTC()
	incident.Status = target
	incident.UpdatedAt = now
	incident.Revision++
	if target == contract.IncidentStatusViewed {
		incident.ViewedAt = timePtr(now)
	} else {
		incident.AcknowledgedAt = timePtr(now)
	}
	if target == contract.IncidentStatusResolved {
		incident.ResolvedAt = timePtr(now)
	}
	result := *cloneIncident(incident)
	s.revision.Add(1)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func validIncidentTransition(from, to contract.IncidentStatus) bool {
	return (from == contract.IncidentStatusNew && (to == contract.IncidentStatusViewed || to == contract.IncidentStatusAcknowledged || to == contract.IncidentStatusResolved)) ||
		(from == contract.IncidentStatusViewed && (to == contract.IncidentStatusAcknowledged || to == contract.IncidentStatusResolved)) ||
		(from == contract.IncidentStatusAcknowledged && to == contract.IncidentStatusResolved)
}

func normalizeIncidentObservation(input *IncidentObservation) {
	input.EventID = strings.TrimSpace(input.EventID)
	input.EventType = strings.TrimSpace(input.EventType)
	input.CameraID = strings.TrimSpace(input.CameraID)
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.ResidentID = strings.TrimSpace(input.ResidentID)
	input.EntityID = strings.TrimSpace(input.EntityID)
	input.TrackID = strings.TrimSpace(input.TrackID)
	input.ClipID = strings.TrimSpace(input.ClipID)
	input.SequenceKey = strings.TrimSpace(input.SequenceKey)
	input.ActivationID = strings.TrimSpace(input.ActivationID)
	input.GroupKey = strings.TrimSpace(input.GroupKey)
	input.SecurityState = strings.TrimSpace(input.SecurityState)
	input.Severity = strings.TrimSpace(input.Severity)
	if input.Timestamp.IsZero() {
		input.Timestamp = time.Now().UTC()
	} else {
		input.Timestamp = input.Timestamp.UTC()
	}
	if input.IdentityKind == "" {
		input.IdentityKind = contract.IncidentIdentityNone
	}
	if input.Cause.EventType == "" {
		input.Cause.EventType = input.EventType
	}
	if input.Cause.SequenceKey == "" {
		input.Cause.SequenceKey = input.SequenceKey
	}
	if input.Cause.ActivationID == "" {
		input.Cause.ActivationID = input.ActivationID
	}
	if input.Cause.GroupKey == "" {
		input.Cause.GroupKey = input.GroupKey
	}
	input.Cause.Contributors = boundedUniqueStrings(input.Cause.Contributors, 20)
	input.Cause.Evidence = boundedUniqueStrings(input.Cause.Evidence, 20)
}

func (s *Store) incidentContainingEventLocked(eventID, timelineKey string) (*contract.Incident, bool) {
	for _, incident := range s.Incidents {
		if incident == nil {
			continue
		}
		if eventID != "" && containsString(incident.EventIDs, eventID) {
			return incident, true
		}
		for _, entry := range incident.Timeline {
			if entry.Key == timelineKey {
				return incident, true
			}
		}
	}
	return nil, false
}

func (s *Store) findActiveIncidentLocked(input IncidentObservation) *contract.Incident {
	var best *contract.Incident
	for _, incident := range s.Incidents {
		if incident == nil || incident.Status == contract.IncidentStatusAcknowledged {
			continue
		}
		if incidentMatchesObservation(incident, input) {
			if best == nil || incident.LastEventAt.After(best.LastEventAt) || (incident.LastEventAt.Equal(best.LastEventAt) && incident.ID > best.ID) {
				best = incident
			}
		}
	}
	return best
}

func incidentMatchesObservation(incident *contract.Incident, input IncidentObservation) bool {
	if incident == nil {
		return false
	}
	withinWindow := func() bool {
		return absDuration(input.Timestamp.Sub(incident.LastEventAt)) <= incidentGroupingWindow
	}
	if input.SequenceKey != "" && incident.Cause.SequenceKey == input.SequenceKey {
		return withinWindow()
	}
	if input.ActivationID != "" && incident.Cause.ActivationID == input.ActivationID {
		return withinWindow()
	}
	if input.TrackID != "" && incident.TrackID == input.TrackID {
		return withinWindow()
	}
	if input.EntityID != "" && incident.EntityID == input.EntityID {
		return withinWindow()
	}
	if input.CameraID == "" && input.NodeID == "" {
		return false
	}
	if input.CameraID != "" && incident.CameraID != input.CameraID {
		return false
	}
	if input.NodeID != "" && incident.NodeID != input.NodeID {
		return false
	}
	return absDuration(input.Timestamp.Sub(incident.LastEventAt)) <= incidentGroupingWindow
}

func mergeIncidentObservation(incident *contract.Incident, input IncidentObservation) {
	if incident == nil {
		return
	}
	if input.Timestamp.Before(incident.StartedAt) || incident.StartedAt.IsZero() {
		incident.StartedAt = input.Timestamp
	}
	if input.Timestamp.After(incident.LastEventAt) {
		incident.LastEventAt = input.Timestamp
	}
	if input.Timestamp.After(incident.UpdatedAt) {
		incident.UpdatedAt = input.Timestamp
	}
	if input.Score > incident.Score {
		incident.Score = input.Score
	}
	if severityRank(input.Severity) > severityRank(incident.Severity) {
		incident.Severity = input.Severity
	}
	if incident.CameraID == "" {
		incident.CameraID = input.CameraID
	}
	if incident.NodeID == "" {
		incident.NodeID = input.NodeID
	}
	if incident.IdentityKind == "" || incident.IdentityKind == contract.IncidentIdentityNone {
		incident.IdentityKind = input.IdentityKind
	}
	if incident.ResidentID == "" {
		incident.ResidentID = input.ResidentID
	}
	if incident.EntityID == "" {
		incident.EntityID = input.EntityID
	}
	if incident.TrackID == "" {
		incident.TrackID = input.TrackID
	}
	incident.SecurityState = "intrusion"
	incident.Cause.EventType = firstNonEmpty(incident.Cause.EventType, input.Cause.EventType)
	incident.Cause.DecisionType = firstNonEmpty(input.Cause.DecisionType, incident.Cause.DecisionType)
	incident.Cause.Reason = firstNonEmpty(input.Cause.Reason, incident.Cause.Reason)
	incident.Cause.DecisionID = firstNonEmpty(input.Cause.DecisionID, incident.Cause.DecisionID)
	incident.Cause.SequenceKey = firstNonEmpty(incident.Cause.SequenceKey, input.Cause.SequenceKey)
	incident.Cause.ActivationID = firstNonEmpty(incident.Cause.ActivationID, input.Cause.ActivationID)
	incident.Cause.GroupKey = firstNonEmpty(incident.Cause.GroupKey, input.Cause.GroupKey)
	incident.Cause.Contributors = boundedUniqueStrings(append(incident.Cause.Contributors, input.Cause.Contributors...), 20)
	incident.Cause.Evidence = boundedUniqueStrings(append(incident.Cause.Evidence, input.Cause.Evidence...), 20)
	if input.EventID != "" && !containsString(incident.EventIDs, input.EventID) {
		incident.EventIDs = append(incident.EventIDs, input.EventID)
	}
	if input.ClipID != "" && !containsString(incident.ClipIDs, input.ClipID) {
		incident.ClipIDs = append(incident.ClipIDs, input.ClipID)
	}
	entry := contract.IncidentTimelineEntry{
		Key: input.TimelineKey(), Timestamp: input.Timestamp, Type: input.EventType, EventID: input.EventID,
		CameraID: input.CameraID, NodeID: input.NodeID, IdentityKind: input.IdentityKind,
		ResidentID: input.ResidentID, EntityID: input.EntityID, Score: input.Score, Confidence: input.Confidence,
	}
	for _, existing := range incident.Timeline {
		if existing.Key == entry.Key {
			return
		}
	}
	incident.Timeline = append(incident.Timeline, entry)
	sort.SliceStable(incident.Timeline, func(i, j int) bool {
		if incident.Timeline[i].Timestamp.Equal(incident.Timeline[j].Timestamp) {
			return incident.Timeline[i].Key < incident.Timeline[j].Key
		}
		return incident.Timeline[i].Timestamp.Before(incident.Timeline[j].Timestamp)
	})
	if len(incident.Timeline) > maxIncidentTimelineEntries {
		incident.Timeline = incident.Timeline[len(incident.Timeline)-maxIncidentTimelineEntries:]
	}
	if len(incident.EventIDs) > maxIncidentEventIDs {
		incident.EventIDs = incident.EventIDs[len(incident.EventIDs)-maxIncidentEventIDs:]
	}
	if len(incident.ClipIDs) > maxIncidentClipIDs {
		incident.ClipIDs = incident.ClipIDs[len(incident.ClipIDs)-maxIncidentClipIDs:]
	}
}

func (s *Store) trimIncidentsLocked(limit int) {
	if limit <= 0 || len(s.Incidents) <= limit {
		return
	}
	items := make([]*contract.Incident, 0, len(s.Incidents))
	for _, value := range s.Incidents {
		if value != nil {
			items = append(items, value)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		leftRank, rightRank := incidentRetentionRank(items[i].Status), incidentRetentionRank(items[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return incidentSortTime(items[i]).Before(incidentSortTime(items[j]))
	})
	for len(s.Incidents) > limit && len(items) > 0 {
		victim := items[0]
		items = items[1:]
		delete(s.Incidents, victim.ID)
	}
}

func trimPersistedIncidents(items map[string]contract.Incident, limit int) {
	if limit <= 0 || len(items) <= limit {
		return
	}
	values := make([]contract.Incident, 0, len(items))
	for _, value := range items {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		leftRank, rightRank := incidentRetentionRank(values[i].Status), incidentRetentionRank(values[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return incidentSortTime(&values[i]).Before(incidentSortTime(&values[j]))
	})
	for len(items) > limit && len(values) > 0 {
		delete(items, values[0].ID)
		values = values[1:]
	}
}

func incidentRetentionRank(status contract.IncidentStatus) int {
	switch status {
	case contract.IncidentStatusAcknowledged:
		return 0
	case contract.IncidentStatusViewed:
		return 1
	default:
		return 2
	}
}

func incidentSortTime(value *contract.Incident) time.Time {
	if value == nil {
		return time.Time{}
	}
	if !value.UpdatedAt.IsZero() {
		return value.UpdatedAt
	}
	return value.CreatedAt
}

func cloneIncident(value *contract.Incident) *contract.Incident {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.EventIDs = append([]string(nil), value.EventIDs...)
	cloned.ClipIDs = append([]string(nil), value.ClipIDs...)
	cloned.Cause.Contributors = append([]string(nil), value.Cause.Contributors...)
	cloned.Cause.Evidence = append([]string(nil), value.Cause.Evidence...)
	cloned.Timeline = append([]contract.IncidentTimelineEntry(nil), value.Timeline...)
	if value.AcknowledgedAt != nil {
		at := *value.AcknowledgedAt
		cloned.AcknowledgedAt = &at
	}
	if value.ViewedAt != nil {
		at := *value.ViewedAt
		cloned.ViewedAt = &at
	}
	return &cloned
}

func boundedUniqueStrings(values []string, limit int) []string {
	out := make([]string, 0, minInt(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func severityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium_high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
