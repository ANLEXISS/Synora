package state

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

func (s *Store) SetResidentTrack(value *ResidentTrack) {
	if s == nil || value == nil || strings.TrimSpace(value.ResidentID) == "" {
		return
	}
	cloned := *value
	cloned.ResidentID = strings.TrimSpace(cloned.ResidentID)
	cloned.Confidence = boundConfidence(cloned.Confidence)
	s.mu.Lock()
	if current := s.ResidentTracks[cloned.ResidentID]; current != nil && newerThan(current.LastSeen, cloned.LastSeen) {
		s.mu.Unlock()
		return
	}
	s.ResidentTracks[cloned.ResidentID] = &cloned
	s.revision.Add(1)
	s.mu.Unlock()
	_ = s.SaveNow()
}

func (s *Store) ResidentTrack(id string) (*ResidentTrack, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.ResidentTracks[strings.TrimSpace(id)]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) SetEntityTrack(value *EntityTrack) {
	if s == nil || value == nil || strings.TrimSpace(value.ID) == "" {
		return
	}
	cloned := *value
	cloned.ID = strings.TrimSpace(cloned.ID)
	cloned.TrackID = strings.TrimSpace(cloned.TrackID)
	cloned.ResidentID = strings.TrimSpace(cloned.ResidentID)
	cloned.CandidateResidentID = strings.TrimSpace(cloned.CandidateResidentID)
	cloned.Confidence = boundConfidence(cloned.Confidence)
	s.mu.Lock()
	if current := s.EntityTracks[cloned.ID]; current != nil && newerThan(current.LastSeen, cloned.LastSeen) {
		s.mu.Unlock()
		return
	}
	s.EntityTracks[cloned.ID] = &cloned
	s.revision.Add(1)
	s.mu.Unlock()
	_ = s.SaveNow()
}

func (s *Store) EntityTrack(id string) (*EntityTrack, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.EntityTracks[strings.TrimSpace(id)]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) DeleteEntityTracksByActivation(activationID string) []string {
	if s == nil || strings.TrimSpace(activationID) == "" {
		return nil
	}
	activationID = strings.TrimSpace(activationID)
	s.mu.Lock()
	deleted := make([]string, 0)
	for id, value := range s.EntityTracks {
		if value != nil && value.ActivationID == activationID {
			delete(s.EntityTracks, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.revision.Add(1)
	}
	s.mu.Unlock()
	if len(deleted) > 0 {
		_ = s.SaveNow()
	}
	return deleted
}

func EntityTrackID(trackID, sequenceKey, activationID, deviceID, nodeID string) string {
	trackID = strings.TrimSpace(trackID)
	sequenceKey = strings.TrimSpace(sequenceKey)
	activationID = strings.TrimSpace(activationID)
	deviceID = strings.TrimSpace(deviceID)
	nodeID = strings.TrimSpace(nodeID)
	// Detector track identifiers are only locally stable. Camera and
	// activation boundaries are part of the identity so a reused detector ID
	// cannot merge two subjects or two cameras into one durable entity.
	material := strings.Join([]string{trackID, activationID, deviceID}, "|")
	if trackID == "" {
		material = strings.Join([]string{sequenceKey, activationID, deviceID, nodeID}, "|")
	}
	if strings.Trim(material, "|") == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(material))
	return "entity-" + hex.EncodeToString(digest[:8])
}

func boundConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func newerThan(previous, candidate time.Time) bool {
	return !previous.IsZero() && !candidate.IsZero() && candidate.Before(previous)
}
