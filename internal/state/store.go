package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"synora/internal/clipstore"
	"synora/internal/recovery"
	"synora/pkg/contract"
)

type Store struct {
	mu                  sync.RWMutex
	revision            atomic.Uint64
	persistenceStatusMu sync.RWMutex
	persistenceStatus   PersistenceHealth

	DeviceStates      map[string]*DeviceState
	CameraStates      map[string]*CameraState
	NodeStates        map[string]*NodeState
	Tracks            map[string]*Track
	ResidentTracks    map[string]*ResidentTrack
	EntityTracks      map[string]*EntityTrack
	Clusters          map[string]*Cluster
	Identities        map[string]*IdentityState
	Presence          map[string]*PresenceState
	Clips             map[string]*ClipState
	FacePhotos        map[string]*contract.FacePhoto
	FaceDataset       *contract.FaceDatasetState
	Validations       map[string]*contract.ValidationRequest
	BehaviorOverrides map[string]json.RawMessage
	ActionResults     map[string]*contract.ActionResult
	Danger            []*contract.DangerAssessment
	RecentEvents      []*contract.Event
	ValidationEvents  []*contract.Event
	EventWindows      map[string]*contract.EventWindow
	EventChains       map[string]*contract.EventChain
	CriticalChains    map[string]*contract.CriticalChainMemory
	Incidents         map[string]*contract.Incident
	System            *SystemState

	persistence   Persistence
	incidentLimit int
	inputEpoch    string
	inputSequence uint64
}

// PersistenceHealth is the last observed durability result. A failed write
// never becomes invisible merely because callers use fire-and-forget state
// mutators; operators can inspect this evidence and Core can fail closed.
type PersistenceHealth struct {
	Healthy   bool      `json:"healthy"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func (s *Store) PersistenceHealth() PersistenceHealth {
	if s == nil {
		return PersistenceHealth{}
	}
	s.persistenceStatusMu.RLock()
	defer s.persistenceStatusMu.RUnlock()
	return s.persistenceStatus
}

func (s *Store) recordPersistenceResult(err error) {
	if s == nil {
		return
	}
	health := PersistenceHealth{Healthy: err == nil, CheckedAt: time.Now().UTC()}
	if err != nil {
		health.Error = err.Error()
	}
	s.persistenceStatusMu.Lock()
	s.persistenceStatus = health
	s.persistenceStatusMu.Unlock()
}

func (s *Store) InputCursor() (string, uint64) {
	if s == nil {
		return "", 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inputEpoch, s.inputSequence
}

func (s *Store) SetInputCursor(epoch string, sequence uint64) {
	if s == nil {
		return
	}
	epoch = strings.TrimSpace(epoch)
	s.mu.Lock()
	if s.inputEpoch == epoch && s.inputSequence == sequence {
		s.mu.Unlock()
		return
	}
	s.inputEpoch, s.inputSequence = epoch, sequence
	s.revision.Add(1)
	s.mu.Unlock()
	_ = s.SaveNow()
}

const (
	maxActionResults = 200
	maxDanger        = 100
	maxRecentEvents  = 200
)

type Option func(*Store)

func WithPersistence(persistence Persistence) Option {
	return func(s *Store) {
		s.persistence = persistence
	}
}

func WithPersistencePath(path string) Option {
	return WithPersistence(NewFilePersistence(path))
}

func (s *Store) SetPersistence(persistence Persistence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistence = persistence
}

func NewStore(options ...Option) *Store {
	now := time.Now().UTC()
	store := &Store{
		DeviceStates:      make(map[string]*DeviceState),
		CameraStates:      make(map[string]*CameraState),
		NodeStates:        make(map[string]*NodeState),
		Tracks:            make(map[string]*Track),
		ResidentTracks:    make(map[string]*ResidentTrack),
		EntityTracks:      make(map[string]*EntityTrack),
		Clusters:          make(map[string]*Cluster),
		Identities:        make(map[string]*IdentityState),
		Presence:          make(map[string]*PresenceState),
		Clips:             make(map[string]*ClipState),
		FacePhotos:        make(map[string]*contract.FacePhoto),
		FaceDataset:       &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetIdle},
		Validations:       make(map[string]*contract.ValidationRequest),
		BehaviorOverrides: make(map[string]json.RawMessage),
		ActionResults:     make(map[string]*contract.ActionResult),
		Danger:            []*contract.DangerAssessment{},
		RecentEvents:      []*contract.Event{},
		ValidationEvents:  []*contract.Event{},
		EventWindows:      make(map[string]*contract.EventWindow),
		EventChains:       make(map[string]*contract.EventChain),
		CriticalChains:    make(map[string]*contract.CriticalChainMemory),
		Incidents:         make(map[string]*contract.Incident),
		incidentLimit:     DefaultIncidentLimit,
		System: &SystemState{
			LastState:            "idle",
			LastStateTime:        now,
			DangerLevel:          "unknown",
			DangerSource:         "unknown",
			DegradationReasons:   []string{},
			RuntimeComponents:    map[string]string{},
			RuntimeComponentInfo: map[string]string{},
			RuntimeModels:        map[string]string{},
			BlockingReasons:      []string{},
			BlockedActionsRecent: []map[string]any{},
			Security:             contract.DefaultSecurityModeState(now),
		},
	}
	store.revision.Store(1)
	for _, option := range options {
		if option != nil {
			option(store)
		}
	}
	return store
}

func (s *Store) SetDeviceState(value *DeviceState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.DeviceStates[value.ID] = &cloned
	s.revision.Add(1)
}

func (s *Store) DeviceState(id string) (*DeviceState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.DeviceStates[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) SetCameraState(value *CameraState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.CameraStates[value.ID] = &cloned
	s.revision.Add(1)
}

func (s *Store) CameraState(id string) (*CameraState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.CameraStates[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) SetNodeState(value *NodeState) {
	if value == nil || value.NodeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.NodeStates[value.NodeID] = &cloned
	s.revision.Add(1)
}

func (s *Store) NodeState(id string) (*NodeState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.NodeStates[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) SetTrack(value *Track) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.Tracks[value.ID] = &cloned
	s.revision.Add(1)
}

func (s *Store) Track(id string) (*Track, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Tracks[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) DeleteTrack(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Tracks, id)
	s.revision.Add(1)
}

func (s *Store) SetCluster(value *Cluster) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.Clusters[value.ID]; current != nil && newerThan(current.UpdatedAt, value.UpdatedAt) {
		return
	}
	cloned := *value
	cloned.EventIDs = append([]string(nil), value.EventIDs...)
	s.Clusters[value.ID] = &cloned
	s.revision.Add(1)
}

func (s *Store) Cluster(id string) (*Cluster, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Clusters[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	cloned.EventIDs = append([]string(nil), value.EventIDs...)
	return &cloned, true
}

func (s *Store) DeleteCluster(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Clusters, id)
	s.revision.Add(1)
}

func (s *Store) SetIdentity(value *IdentityState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	cloned := *value
	s.Identities[value.ID] = &cloned
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) Identity(id string) (*IdentityState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Identities[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) DeleteIdentity(id string) {
	s.mu.Lock()
	delete(s.Identities, id)
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetPresence(value *PresenceState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	if current := s.Presence[value.ID]; current != nil && newerThan(current.LastSeen, value.LastSeen) {
		s.mu.Unlock()
		return
	}
	cloned := *value
	if current := s.Presence[value.ID]; current != nil && cloned.LastSeen.IsZero() {
		// LastSeen is historical runtime data. An absent/cleared update may
		// reset the current state, but it must never erase the last observation.
		cloned.LastSeen = current.LastSeen
	}
	s.Presence[value.ID] = &cloned
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) PresenceState(id string) (*PresenceState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Presence[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := *value
	return &cloned, true
}

func (s *Store) DeletePresence(id string) {
	s.mu.Lock()
	delete(s.Presence, id)
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetClip(value *ClipState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	cloned := cloneClip(value)
	if cloned.Status == "" {
		cloned.Status = contract.ClipStatusProcessed
	}
	if cloned.UpdatedAt.IsZero() {
		cloned.UpdatedAt = cloned.CreatedAt
	}
	s.Clips[value.ID] = &cloned
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) Clip(id string) (*ClipState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Clips[id]
	if !ok || value == nil {
		return nil, false
	}
	cloned := cloneClip(value)
	return &cloned, true
}

// ClipStorageReferences returns the internal paths that remain owned by
// durable clip metadata. Callers use the snapshot to reconcile files without
// holding the StateStore lock during filesystem operations.
func (s *Store) ClipStorageReferences() map[string]struct{} {
	references := make(map[string]struct{})
	if s == nil {
		return references
	}
	s.mu.RLock()
	for _, value := range s.Clips {
		if value != nil && strings.TrimSpace(value.Path) != "" {
			references[filepath.Clean(value.Path)] = struct{}{}
		}
	}
	s.mu.RUnlock()
	return references
}

const (
	DefaultClipListLimit = 50
	MaxClipListLimit     = 100
)

func (s *Store) ClipsList(limit int) []contract.Clip {
	return s.clipsListBefore(limit, time.Time{}, "")
}

// ClipsListBefore returns a stable descending page for recovery. The cursor
// is the last UpdatedAt/ID pair from the previous page; using both fields
// keeps equal-timestamp clips from being skipped or replayed indefinitely.
func (s *Store) ClipsListBefore(limit int, beforeUpdatedAt time.Time, beforeID string) []contract.Clip {
	return s.clipsListBefore(limit, beforeUpdatedAt, beforeID)
}

func (s *Store) clipsListBefore(limit int, beforeUpdatedAt time.Time, beforeID string) []contract.Clip {
	if s == nil {
		return []contract.Clip{}
	}
	if limit <= 0 {
		limit = DefaultClipListLimit
	}
	if limit > MaxClipListLimit {
		limit = MaxClipListLimit
	}
	s.mu.RLock()
	items := make([]contract.Clip, 0, len(s.Clips))
	for _, value := range s.Clips {
		if value != nil {
			items = append(items, cloneClip(value))
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].UpdatedAt, items[j].UpdatedAt
		if left.Equal(right) {
			return items[i].ID > items[j].ID
		}
		return left.After(right)
	})
	if !beforeUpdatedAt.IsZero() {
		filtered := items[:0]
		for _, item := range items {
			if item.UpdatedAt.Before(beforeUpdatedAt) ||
				(item.UpdatedAt.Equal(beforeUpdatedAt) && item.ID < beforeID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *Store) RegisterClip(value *ClipState) (contract.Clip, bool, error) {
	if s == nil || value == nil {
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorInternal, "state store unavailable")
	}
	cloned := cloneClip(value)
	if cloned.Status == "" {
		cloned.Status = contract.ClipStatusReady
	}
	if err := cloned.Validate(); err != nil {
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}
	s.mu.Lock()
	if existing, ok := s.Clips[cloned.ID]; ok && existing != nil {
		if !sameClipContent(existing, &cloned) {
			s.mu.Unlock()
			return contract.Clip{}, false, contract.NewAPIError(contract.ErrorConflict, "clip id collision")
		}
		result := cloneClip(existing)
		s.mu.Unlock()
		return result, false, nil
	}
	if cloned.Revision == 0 {
		cloned.Revision = 1
	}
	if cloned.UpdatedAt.IsZero() {
		cloned.UpdatedAt = cloned.ReadyAt
		if cloned.UpdatedAt.IsZero() {
			cloned.UpdatedAt = cloned.CreatedAt
		}
	}
	s.Clips[cloned.ID] = &cloned
	for incidentID, incident := range s.Incidents {
		if incident == nil || !containsString(incident.ClipIDs, cloned.ID) || containsString(cloned.IncidentIDs, incidentID) {
			continue
		}
		cloned.IncidentIDs = append(cloned.IncidentIDs, incidentID)
		for _, eventID := range incident.EventIDs {
			if !containsString(cloned.EventIDs, eventID) {
				cloned.EventIDs = append(cloned.EventIDs, eventID)
			}
		}
	}
	s.revision.Add(1)
	result := cloneClip(&cloned)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (s *Store) TransitionClip(id string, target contract.ClipStatus, failureCode string) (contract.Clip, bool, error) {
	if s == nil {
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorInternal, "state store unavailable")
	}
	if err := target.Validate(); err != nil {
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "clip id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	value, ok := s.Clips[id]
	if !ok || value == nil {
		s.mu.Unlock()
		return contract.Clip{}, false, contract.NewAPIError(contract.ErrorNotFound, "clip not found")
	}
	if value.Status == target {
		result := cloneClip(value)
		s.mu.Unlock()
		return result, false, nil
	}
	if !contract.ValidClipTransition(value.Status, target) {
		from := value.Status
		s.mu.Unlock()
		return contract.Clip{}, false, contract.NewAPIErrorWithDetails(contract.ErrorConflict,
			map[string]any{"from": from, "to": target}, "clip transition from %s to %s is not allowed", from, target)
	}
	value.Status = target
	value.UpdatedAt = now
	value.Revision++
	value.FailureCode = strings.TrimSpace(failureCode)
	switch target {
	case contract.ClipStatusProcessing:
		value.ProcessingAt = now
	case contract.ClipStatusProcessed:
		value.ProcessedAt = now
	case contract.ClipStatusExpired:
		value.ExpiresAt = now
	}
	result := cloneClip(value)
	s.revision.Add(1)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (s *Store) AttachClipReferences(clipID, eventID, incidentID string) bool {
	if s == nil || strings.TrimSpace(clipID) == "" {
		return false
	}
	clipID = strings.TrimSpace(clipID)
	s.mu.Lock()
	value, ok := s.Clips[clipID]
	if !ok || value == nil {
		s.mu.Unlock()
		return false
	}
	changed := false
	if eventID = strings.TrimSpace(eventID); eventID != "" && !containsString(value.EventIDs, eventID) {
		value.EventIDs = append(value.EventIDs, eventID)
		if value.EventID == "" {
			value.EventID = eventID
		}
		changed = true
	}
	if incidentID = strings.TrimSpace(incidentID); incidentID != "" && !containsString(value.IncidentIDs, incidentID) {
		value.IncidentIDs = append(value.IncidentIDs, incidentID)
		changed = true
	}
	if changed {
		value.UpdatedAt = time.Now().UTC()
		value.Revision++
		s.revision.Add(1)
	}
	result := changed
	s.mu.Unlock()
	if changed {
		_ = s.SaveNow()
	}
	return result
}

func (s *Store) ReconcileClipFiles(now time.Time) int {
	if s == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changed := 0
	s.mu.Lock()
	for _, value := range s.Clips {
		if value == nil || strings.TrimSpace(value.Path) == "" {
			continue
		}
		fileAvailable, _ := clipstore.VerifyRegularFile(value.Path, value.SizeBytes, value.Checksum)
		target := value.Status
		failure := value.FailureCode
		switch {
		case fileAvailable && value.Status == contract.ClipStatusMissing:
			target, failure = contract.ClipStatusReady, ""
		case !fileAvailable && value.Status != contract.ClipStatusExpired:
			target, failure = contract.ClipStatusMissing, "file_missing_or_unsafe"
		case fileAvailable && value.Status == contract.ClipStatusProcessing:
			target, failure = contract.ClipStatusReady, ""
		}
		if target != value.Status || failure != value.FailureCode {
			value.Status = target
			value.FailureCode = failure
			value.UpdatedAt = now
			value.Revision++
			changed++
		}
	}
	if changed > 0 {
		s.revision.Add(uint64(changed))
	}
	s.mu.Unlock()
	if changed > 0 {
		_ = s.SaveNow()
	}
	return changed
}

func cloneClip(value *ClipState) contract.Clip {
	if value == nil {
		return contract.Clip{}
	}
	cloned := contract.Clip(*value)
	cloned.EventIDs = append([]string(nil), value.EventIDs...)
	cloned.IncidentIDs = append([]string(nil), value.IncidentIDs...)
	return cloned
}

func sameClipContent(left, right *ClipState) bool {
	if left == nil || right == nil {
		return false
	}
	if left.CameraID != right.CameraID || left.SizeBytes != right.SizeBytes {
		return false
	}
	if left.Checksum != "" && right.Checksum != "" {
		return left.Checksum == right.Checksum
	}
	return left.ActivationID == right.ActivationID && left.ClipIndex == right.ClipIndex
}

func (s *Store) DeleteClip(id string) {
	s.mu.Lock()
	delete(s.Clips, id)
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetValidation(value *contract.ValidationRequest) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	s.Validations[value.ID] = cloneValidation(value)
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) Validation(id string) (*contract.ValidationRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.Validations[id]
	if !ok || value == nil {
		return nil, false
	}
	return cloneValidation(value), true
}

func (s *Store) ValidationsList() []contract.ValidationRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.ValidationRequest, 0, len(s.Validations))
	for _, value := range s.Validations {
		if value == nil {
			continue
		}
		out = append(out, *cloneValidation(value))
	}
	return out
}

// SaveValidation persists a user-authored validation transactionally. Runtime
// generated validation requests may continue to use SetValidation.
func (s *Store) SaveValidation(value *contract.ValidationRequest) error {
	if value == nil || value.ID == "" {
		return contract.NewAPIError(contract.ErrorValidationFailed, "validation id is required")
	}
	if err := s.BackupNow(); err != nil {
		return err
	}
	s.mu.Lock()
	previous, existed := s.Validations[value.ID]
	s.Validations[value.ID] = cloneValidation(value)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		s.mu.Lock()
		if existed {
			s.Validations[value.ID] = previous
		} else {
			delete(s.Validations, value.ID)
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) BehaviorOverride(id string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.BehaviorOverrides[id]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), value...), true
}

func (s *Store) BehaviorOverridesList() map[string]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]json.RawMessage, len(s.BehaviorOverrides))
	for id, value := range s.BehaviorOverrides {
		out[id] = append(json.RawMessage(nil), value...)
	}
	return out
}

func (s *Store) SaveBehaviorOverride(id string, value json.RawMessage) error {
	if id == "" || !json.Valid(value) {
		return contract.NewAPIError(contract.ErrorValidationFailed, "valid behavior override is required")
	}
	if err := s.BackupNow(); err != nil {
		return err
	}
	s.mu.Lock()
	previous, existed := s.BehaviorOverrides[id]
	s.BehaviorOverrides[id] = append(json.RawMessage(nil), value...)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		s.mu.Lock()
		if existed {
			s.BehaviorOverrides[id] = previous
		} else {
			delete(s.BehaviorOverrides, id)
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) DeleteBehaviorOverride(id string) error {
	if err := s.BackupNow(); err != nil {
		return err
	}
	s.mu.Lock()
	previous, existed := s.BehaviorOverrides[id]
	delete(s.BehaviorOverrides, id)
	s.mu.Unlock()
	if !existed {
		return nil
	}
	if err := s.SaveNow(); err != nil {
		s.mu.Lock()
		s.BehaviorOverrides[id] = previous
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) SetActionResult(value *contract.ActionResult) {
	if value == nil {
		return
	}
	id := value.ID
	if id == "" {
		id = value.RequestID
	}
	if id == "" {
		id = value.ActionID
	}
	if id == "" {
		return
	}
	s.mu.Lock()
	cloned := cloneActionResult(value)
	cloned.ID = id
	s.ActionResults[id] = cloned
	s.revision.Add(1)
	s.trimActionResultsLocked(maxActionResults)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) AddDangerAssessment(value *contract.DangerAssessment) {
	if !contract.IsPersistableDangerAssessment(value) {
		return
	}
	s.mu.Lock()
	s.Danger = append(s.Danger, cloneDangerAssessment(value))
	s.revision.Add(1)
	s.trimDangerLocked(maxDanger)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) DangerAssessmentsList() []contract.DangerAssessment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.DangerAssessment, 0, len(s.Danger))
	for _, value := range s.Danger {
		if value == nil {
			continue
		}
		out = append(out, *cloneDangerAssessment(value))
	}
	return out
}

func (s *Store) ActionResultsList() []contract.ActionResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]contract.ActionResult, 0, len(s.ActionResults))
	for _, value := range s.ActionResults {
		if value == nil {
			continue
		}
		out = append(out, *cloneActionResult(value))
	}
	return out
}

func (s *Store) SetRecentEvents(events []*contract.Event) {
	s.mu.Lock()
	s.RecentEvents = cloneEvents(trimEvents(events, maxRecentEvents))
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) RecentEventsList() []*contract.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEvents(s.RecentEvents)
}

func (s *Store) AddValidationEvent(event *contract.Event) {
	if event == nil {
		return
	}
	s.mu.Lock()
	s.ValidationEvents = cloneEvents(trimEvents(append(s.ValidationEvents, event), maxRecentEvents))
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) ValidationEventsList() []*contract.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEvents(s.ValidationEvents)
}

func (s *Store) ClearValidationEvents() int {
	s.mu.Lock()
	count := len(s.ValidationEvents)
	s.ValidationEvents = []*contract.Event{}
	s.revision.Add(1)
	s.mu.Unlock()
	s.SaveNow()
	return count
}

func (s *Store) SetEventWindow(nodeID string, value *contract.EventWindow) {
	if nodeID == "" || value == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventWindows[nodeID] = cloneWindow(value)
	s.revision.Add(1)
}

func (s *Store) EventWindow(nodeID string) (*contract.EventWindow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.EventWindows[nodeID]
	if !ok || value == nil {
		return nil, false
	}
	return cloneWindow(value), true
}

func (s *Store) DeleteEventWindow(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.EventWindows, nodeID)
	s.revision.Add(1)
}

func (s *Store) SystemState() SystemState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.systemStateLocked()
}

// ContextSnapshot returns a defensive copy of only the operational facts
// needed by a read-only context provider. The Store remains the sole owner of
// the source state.
func (s *Store) ContextSnapshot() ContextSourceSnapshot {
	if s == nil {
		return ContextSourceSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := ContextSourceSnapshot{Revision: s.revision.Load()}
	if out.Revision == 0 {
		out.Revision = 1
	}
	if s.System != nil {
		out.System = ContextSystemState{LastState: s.System.LastState, Armed: s.System.Armed, SecurityMode: string(s.System.Security.Mode)}
	} else {
		out.System = ContextSystemState{LastState: "idle", SecurityMode: "unknown"}
	}
	for _, value := range s.DeviceStates {
		if value != nil {
			out.Devices = append(out.Devices, *value)
		}
	}
	for _, value := range s.CameraStates {
		if value != nil {
			out.Cameras = append(out.Cameras, *value)
		}
	}
	for _, value := range s.Presence {
		if value != nil {
			out.Presence = append(out.Presence, *value)
		}
	}
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].ID < out.Devices[j].ID })
	sort.Slice(out.Cameras, func(i, j int) bool { return out.Cameras[i].ID < out.Cameras[j].ID })
	sort.Slice(out.Presence, func(i, j int) bool { return out.Presence[i].ID < out.Presence[j].ID })
	return out
}

// DecisionSnapshot returns the system facts, target existence, and effective
// revision under one read lock. It is the atomic read boundary used when a
// decision is bound to StateStore state.
func (s *Store) DecisionSnapshot(kind, id string) (uint64, SystemState, bool) {
	if s == nil {
		return 0, SystemState{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	revision := s.revision.Load()
	if revision == 0 {
		revision = 1
	}
	exists := false
	switch kind {
	case "system":
		exists = id == "system"
	case "node":
		_, exists = s.NodeStates[id]
	case "device":
		_, exists = s.DeviceStates[id]
		if !exists {
			_, exists = s.CameraStates[id]
		}
	case "resident":
		_, exists = s.Presence[id]
	}
	return revision, s.systemStateLocked(), exists
}

func (s *Store) systemStateLocked() SystemState {
	if s.System == nil {
		return SystemState{LastState: "idle", DangerLevel: "unknown", DangerSource: "unknown", Security: contract.DefaultSecurityModeState(time.Now().UTC())}
	}
	cloned := *s.System
	if cloned.LastState == "" {
		cloned.LastState = "idle"
	}
	if cloned.DangerLevel == "" {
		cloned.DangerLevel = "unknown"
	}
	if cloned.DangerSource == "" {
		cloned.DangerSource = "unknown"
	}
	if cloned.DegradationReasons == nil {
		cloned.DegradationReasons = []string{}
	}
	if cloned.RuntimeComponents == nil {
		cloned.RuntimeComponents = map[string]string{}
	}
	if cloned.RuntimeComponentInfo == nil {
		cloned.RuntimeComponentInfo = map[string]string{}
	}
	if cloned.BlockingReasons == nil {
		cloned.BlockingReasons = []string{}
	}
	if cloned.RuntimeModels == nil {
		cloned.RuntimeModels = map[string]string{}
	}
	if cloned.BlockedActionsRecent == nil {
		cloned.BlockedActionsRecent = []map[string]any{}
	}
	cloned.Security = contract.NormalizeSecurityModeState(cloned.Security, time.Now().UTC())
	return cloned
}

func (s *Store) SetSystemState(value SystemState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := value
	if cloned.LastState == "" {
		cloned.LastState = "idle"
	}
	if cloned.DangerLevel == "" {
		cloned.DangerLevel = "unknown"
	}
	if cloned.DangerSource == "" {
		cloned.DangerSource = "unknown"
	}
	if cloned.DegradationReasons == nil {
		cloned.DegradationReasons = []string{}
	}
	if cloned.RuntimeComponents == nil {
		cloned.RuntimeComponents = map[string]string{}
	}
	if cloned.RuntimeComponentInfo == nil {
		cloned.RuntimeComponentInfo = map[string]string{}
	}
	if cloned.BlockingReasons == nil {
		cloned.BlockingReasons = []string{}
	}
	if cloned.RuntimeModels == nil {
		cloned.RuntimeModels = map[string]string{}
	}
	if cloned.BlockedActionsRecent == nil {
		cloned.BlockedActionsRecent = []map[string]any{}
	}
	cloned.Security = contract.NormalizeSecurityModeState(cloned.Security, time.Now().UTC())
	s.System = &cloned
	s.revision.Add(1)
}

// SetRecoveryStatus records Core lifecycle evidence without allowing a
// consumer to mutate the underlying recovery machine. Persistence is done
// after releasing the StateStore lock.
func (s *Store) SetRecoveryStatus(status recovery.Snapshot) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	current := s.systemStateLocked()
	current.LifecycleState = string(status.State)
	current.LifecycleReason = status.Reason
	current.LifecycleUpdatedAt = status.UpdatedAt
	current.Ready = status.Ready
	current.Healthy = status.Healthy
	current.RecoveryComplete = status.RecoveryComplete
	s.System = &current
	s.revision.Add(1)
	s.mu.Unlock()
	return s.SaveNow()
}

// Revision is the monotonic effective StateStore revision used to bind a
// decision and its target snapshot to the same operational state.
func (s *Store) Revision() uint64 {
	if s == nil {
		return 0
	}
	value := s.revision.Load()
	if value == 0 {
		return 1
	}
	return value
}

func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.DeviceStates) + len(s.CameraStates) + len(s.NodeStates) + len(s.Tracks) + len(s.ResidentTracks) + len(s.EntityTracks) + len(s.Clusters) + len(s.Identities) + len(s.Presence) + len(s.Clips) + len(s.EventWindows) + len(s.EventChains) + len(s.CriticalChains)
}

func (s *Store) ActiveTracks() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Tracks)
}

func (s *Store) ActiveClusters() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Clusters)
}

func (s *Store) Snapshot(collection string) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := map[string]interface{}{}
	switch collection {
	case "devices", "device":
		for id, value := range s.DeviceStates {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "cameras":
		for id, value := range s.CameraStates {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "nodes":
		for id, value := range s.NodeStates {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "tracks":
		for id, value := range s.Tracks {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "clusters":
		for id, value := range s.Clusters {
			if value == nil {
				continue
			}
			cloned := *value
			cloned.EventIDs = append([]string(nil), value.EventIDs...)
			out[id] = cloned
		}
	case "identities":
		for id, value := range s.Identities {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "presence":
		for id, value := range s.Presence {
			if value == nil {
				continue
			}
			cloned := *value
			out[id] = cloned
		}
	case "clips":
		for id, value := range s.Clips {
			if value == nil {
				continue
			}
			out[id] = cloneClip(value)
		}
	case "validations":
		for id, value := range s.Validations {
			if value == nil {
				continue
			}
			out[id] = *cloneValidation(value)
		}
	case "behavior_overrides":
		for id, value := range s.BehaviorOverrides {
			var decoded any
			if json.Unmarshal(value, &decoded) == nil {
				out[id] = decoded
			}
		}
	case "action_results":
		for id, value := range s.ActionResults {
			if value == nil {
				continue
			}
			out[id] = *cloneActionResult(value)
		}
	case "danger", "danger_assessments":
		for _, value := range s.Danger {
			if value == nil || value.ID == "" {
				continue
			}
			out[value.ID] = *cloneDangerAssessment(value)
		}
	case "events":
		for _, value := range s.RecentEvents {
			if value == nil || value.ID == "" {
				continue
			}
			out[value.ID] = *cloneEvent(value)
		}
	case "windows":
		for id, value := range s.EventWindows {
			if value == nil {
				continue
			}
			out[id] = *cloneWindow(value)
		}
	case "event_chains", "chains":
		for id, value := range s.EventChains {
			if value != nil {
				out[id] = *cloneEventChain(value)
			}
		}
	case "critical_chain_memories", "critical_chains":
		for id, value := range s.CriticalChains {
			if value != nil {
				out[id] = *cloneCriticalChainMemory(value)
			}
		}
	case "incidents":
		for id, value := range s.Incidents {
			if value != nil {
				out[id] = cloneIncident(value)
			}
		}
	}
	return out
}

func (s *Store) Get(collection string, id string) (interface{}, bool) {
	items := s.Snapshot(collection)
	value, ok := items[id]
	return value, ok
}

func (s *Store) Upsert(collection string, id string, data interface{}) {
	switch value := data.(type) {
	case DeviceState:
		s.SetDeviceState(&value)
	case *DeviceState:
		s.SetDeviceState(value)
	case CameraState:
		s.SetCameraState(&value)
	case *CameraState:
		s.SetCameraState(value)
	case NodeState:
		s.SetNodeState(&value)
	case *NodeState:
		s.SetNodeState(value)
	case Track:
		s.SetTrack(&value)
	case *Track:
		s.SetTrack(value)
	case Cluster:
		s.SetCluster(&value)
	case *Cluster:
		s.SetCluster(value)
	case IdentityState:
		s.SetIdentity(&value)
	case *IdentityState:
		s.SetIdentity(value)
	case PresenceState:
		s.SetPresence(&value)
	case *PresenceState:
		s.SetPresence(value)
	case ClipState:
		s.SetClip(&value)
	case *ClipState:
		s.SetClip(value)
	case contract.ValidationRequest:
		s.SetValidation(&value)
	case *contract.ValidationRequest:
		s.SetValidation(value)
	case contract.ActionResult:
		s.SetActionResult(&value)
	case *contract.ActionResult:
		s.SetActionResult(value)
	case contract.DangerAssessment:
		s.AddDangerAssessment(&value)
	case *contract.DangerAssessment:
		s.AddDangerAssessment(value)
	case contract.Event:
		s.SetRecentEvents(append(s.RecentEventsList(), &value))
	case *contract.Event:
		s.SetRecentEvents(append(s.RecentEventsList(), value))
	case contract.EventWindow:
		s.SetEventWindow(id, &value)
	case *contract.EventWindow:
		s.SetEventWindow(id, value)
	case contract.EventChain:
		s.SetEventChain(&value)
	case *contract.EventChain:
		s.SetEventChain(value)
	case contract.CriticalChainMemory:
		s.SetCriticalChainMemory(&value)
	case *contract.CriticalChainMemory:
		s.SetCriticalChainMemory(value)
	case contract.Incident:
		s.SetIncident(&value)
	case *contract.Incident:
		s.SetIncident(value)
	case SystemState:
		s.SetSystemState(value)
	case *SystemState:
		if value != nil {
			s.SetSystemState(*value)
		}
	}
}

func (s *Store) Delete(collection string, id string) {
	switch collection {
	case "devices", "device":
		s.mu.Lock()
		delete(s.DeviceStates, id)
		s.mu.Unlock()
	case "cameras":
		s.mu.Lock()
		delete(s.CameraStates, id)
		s.mu.Unlock()
	case "nodes":
		s.mu.Lock()
		delete(s.NodeStates, id)
		s.mu.Unlock()
	case "tracks":
		s.DeleteTrack(id)
	case "clusters":
		s.DeleteCluster(id)
	case "identities":
		s.DeleteIdentity(id)
	case "presence":
		s.DeletePresence(id)
	case "clips":
		s.DeleteClip(id)
	case "validations":
		s.mu.Lock()
		delete(s.Validations, id)
		s.mu.Unlock()
		s.SaveNow()
	case "action_results":
		s.mu.Lock()
		delete(s.ActionResults, id)
		s.mu.Unlock()
		s.SaveNow()
	case "danger", "danger_assessments":
		s.deleteDangerAssessment(id)
	case "events":
		s.deleteRecentEvent(id)
	case "windows":
		s.DeleteEventWindow(id)
	case "event_chains", "chains":
		s.DeleteEventChain(id)
	case "critical_chain_memories", "critical_chains":
		s.DeleteCriticalChainMemory(id)
	case "incidents":
		s.mu.Lock()
		delete(s.Incidents, id)
		s.revision.Add(1)
		s.mu.Unlock()
		s.SaveNow()
	}
}

func (s *Store) Cleanup(now time.Time, cfg ExpirationConfig) CleanupResult {
	result := CleanupResult{Deleted: map[string][]string{}}

	s.mu.Lock()

	for id, value := range s.Tracks {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.Tracks, id)
			result.Deleted["tracks"] = append(result.Deleted["tracks"], id)
		}
	}
	for id, value := range s.ResidentTracks {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.ResidentTracks, id)
			result.Deleted["resident_tracks"] = append(result.Deleted["resident_tracks"], id)
		}
	}
	for id, value := range s.EntityTracks {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.EntityTracks, id)
			result.Deleted["entity_tracks"] = append(result.Deleted["entity_tracks"], id)
		}
	}
	for id, value := range s.Clusters {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.Clusters, id)
			result.Deleted["clusters"] = append(result.Deleted["clusters"], id)
		}
	}
	for id, value := range s.Identities {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.Identities, id)
			result.Deleted["identities"] = append(result.Deleted["identities"], id)
		}
	}
	for id, value := range s.Presence {
		if value == nil {
			delete(s.Presence, id)
			result.Deleted["presence"] = append(result.Deleted["presence"], id)
			continue
		}
		if !value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now) {
			// Preserve the last observation while making the runtime state
			// explicitly absent. The core will clear only the config-side
			// convenience projection and publish a fresh snapshot.
			value.State = "absent"
			value.Location = ""
			value.Confidence = 0
			value.UpdatedAt = now
			value.ExpiresAt = time.Time{}
			result.Deleted["presence"] = append(result.Deleted["presence"], id)
		}
	}
	// Clip metadata is durable evidence. Physical retention changes a clip to
	// expired while leaving its ID and incident associations queryable; the
	// generic runtime cleanup must never delete that metadata.
	for id, value := range s.EventWindows {
		if value == nil || value.LastUpdate.Add(cfg.Windows).Before(now) {
			delete(s.EventWindows, id)
			result.Deleted["windows"] = append(result.Deleted["windows"], id)
		}
	}

	s.mu.Unlock()
	if len(result.Deleted) > 0 {
		_ = s.SaveNow()
	}
	return result
}

func cloneWindow(value *contract.EventWindow) *contract.EventWindow {

	if value == nil {
		return nil
	}

	cloned := *value

	cloned.Events = make([]*contract.Event, 0, len(value.Events))

	for _, event := range value.Events {

		if event == nil {
			continue
		}

		eventCopy := *event

		if event.Payload != nil {
			eventCopy.Payload = cloneMap(event.Payload)
		}

		cloned.Events = append(cloned.Events, &eventCopy)

	}

	return &cloned
}

func (s *Store) LoadPersisted() (PersistedSummary, error) {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence == nil {
		return PersistedSummary{}, nil
	}
	persisted, err := persistence.Load()
	if err != nil {
		s.recordPersistenceResult(err)
		return PersistedSummary{}, err
	}
	if persisted == nil {
		err := errors.New("persistence returned nil state")
		s.recordPersistenceResult(err)
		return PersistedSummary{}, err
	}
	s.applyPersistedState(persisted)
	s.recordPersistenceResult(nil)
	return persistedSummary(persisted), nil
}

func (s *Store) SaveNow() error {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence == nil {
		return nil
	}
	persisted := s.PersistedState()
	err := persistence.Save(persisted)
	s.recordPersistenceResult(err)
	return err
}

func (s *Store) BackupNow() error {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence == nil {
		return nil
	}
	if backup, ok := persistence.(BackupPersistence); ok {
		err := backup.Backup()
		s.recordPersistenceResult(err)
		return err
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.RLock()
	persistence := s.persistence
	s.mu.RUnlock()
	if persistence == nil {
		return nil
	}
	err := persistence.Close()
	s.recordPersistenceResult(err)
	return err
}

func (s *Store) PersistedState() *PersistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persistedStateLocked(time.Now().UTC())
}

func (s *Store) applyPersistedState(persisted *PersistedState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputEpoch = strings.TrimSpace(persisted.InputEpoch)
	s.inputSequence = persisted.InputSequence
	s.Clips = make(map[string]*ClipState, len(persisted.Clips))
	if persisted.Clips != nil {
		for id, value := range persisted.Clips {
			cloned := value
			cloned.EventIDs = append([]string(nil), value.EventIDs...)
			cloned.IncidentIDs = append([]string(nil), value.IncidentIDs...)
			if cloned.ID == "" {
				cloned.ID = id
			}
			if cloned.Status == "" {
				if cloned.Path != "" {
					if info, err := os.Lstat(cloned.Path); err == nil && info.Mode().IsRegular() {
						cloned.Status = contract.ClipStatusProcessed
					} else {
						cloned.Status = contract.ClipStatusMissing
					}
				} else {
					cloned.Status = contract.ClipStatusMissing
				}
			}
			if cloned.UpdatedAt.IsZero() {
				cloned.UpdatedAt = cloned.CreatedAt
			}
			s.Clips[id] = &cloned
		}
	}
	s.FacePhotos = make(map[string]*contract.FacePhoto, len(persisted.FacePhotos))
	for id, value := range persisted.FacePhotos {
		cloned := value.Photo
		cloned.StorageKey = value.StorageKey
		if cloned.ID == "" {
			cloned.ID = id
		}
		if cloned.Status == "" {
			cloned.Status = string(contract.FacePhotoStored)
		}
		if cloned.CreatedAt.IsZero() {
			cloned.CreatedAt = time.Now().UTC()
		}
		if cloned.UpdatedAt.IsZero() {
			cloned.UpdatedAt = cloned.CreatedAt
		}
		s.FacePhotos[id] = &cloned
	}
	if persisted.FaceDataset != nil {
		s.FaceDataset = cloneFaceDataset(persisted.FaceDataset)
	} else {
		s.FaceDataset = &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetIdle}
	}
	if persisted.Validations != nil {
		s.Validations = make(map[string]*contract.ValidationRequest, len(persisted.Validations))
		for id, value := range persisted.Validations {
			cloned := cloneValidation(&value)
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.Validations[id] = cloned
		}
	}
	if persisted.BehaviorOverrides != nil {
		s.BehaviorOverrides = make(map[string]json.RawMessage, len(persisted.BehaviorOverrides))
		for id, value := range persisted.BehaviorOverrides {
			s.BehaviorOverrides[id] = append(json.RawMessage(nil), value...)
		}
	}
	if persisted.ActionResults != nil {
		s.ActionResults = make(map[string]*contract.ActionResult, len(persisted.ActionResults))
		for id, value := range persisted.ActionResults {
			cloned := cloneActionResult(&value)
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.ActionResults[id] = cloned
		}
		s.trimActionResultsLocked(maxActionResults)
	}
	if persisted.Danger != nil {
		s.Danger = cloneDangerAssessments(trimDanger(persisted.Danger, maxDanger))
	}
	if persisted.Events != nil {
		s.RecentEvents = cloneEvents(trimEvents(persisted.Events, maxRecentEvents))
	}
	if persisted.ValidationEvents != nil {
		s.ValidationEvents = cloneEvents(trimEvents(persisted.ValidationEvents, maxRecentEvents))
	}
	if persisted.Identities != nil {
		s.Identities = make(map[string]*IdentityState, len(persisted.Identities))
		for id, value := range persisted.Identities {
			cloned := value
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.Identities[id] = &cloned
		}
	}
	if persisted.Presence != nil {
		s.Presence = make(map[string]*PresenceState, len(persisted.Presence))
		for id, value := range persisted.Presence {
			cloned := value
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.Presence[id] = &cloned
		}
	}
	if persisted.ResidentTracks != nil {
		s.ResidentTracks = make(map[string]*ResidentTrack, len(persisted.ResidentTracks))
		for id, value := range persisted.ResidentTracks {
			cloned := value
			if cloned.ResidentID == "" {
				cloned.ResidentID = id
			}
			s.ResidentTracks[id] = &cloned
		}
	}
	if persisted.EntityTracks != nil {
		s.EntityTracks = make(map[string]*EntityTrack, len(persisted.EntityTracks))
		for id, value := range persisted.EntityTracks {
			cloned := value
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.EntityTracks[id] = &cloned
		}
	}
	if persisted.EventChains != nil {
		s.EventChains = make(map[string]*contract.EventChain, len(persisted.EventChains))
		for id, value := range persisted.EventChains {
			cloned := cloneEventChain(&value)
			if cloned == nil {
				continue
			}
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.EventChains[id] = cloned
		}
	}
	if persisted.CriticalChains != nil {
		s.CriticalChains = make(map[string]*contract.CriticalChainMemory, len(persisted.CriticalChains))
		for id, value := range persisted.CriticalChains {
			cloned := cloneCriticalChainMemory(&value)
			if cloned == nil {
				continue
			}
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.CriticalChains[id] = cloned
		}
	}
	if persisted.Incidents != nil {
		s.Incidents = make(map[string]*contract.Incident, len(persisted.Incidents))
		for id, value := range persisted.Incidents {
			cloned := cloneIncident(&value)
			if cloned == nil {
				continue
			}
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.Incidents[id] = cloned
		}
		s.trimIncidentsLocked(s.incidentLimit)
	}
	if persisted.System != nil {
		s.System = persisted.System
	}
}

func (s *Store) persistedStateLocked(savedAt time.Time) *PersistedState {
	persisted := emptyPersistedState()
	persisted.SavedAt = savedAt
	persisted.InputEpoch = s.inputEpoch
	persisted.InputSequence = s.inputSequence
	for id, value := range s.Clips {
		if value == nil {
			continue
		}
		persisted.Clips[id] = cloneClip(value)
	}
	for id, value := range s.FacePhotos {
		if value != nil {
			persisted.FacePhotos[id] = PersistedFacePhoto{Photo: *cloneFacePhoto(value), StorageKey: value.StorageKey}
		}
	}
	persisted.FaceDataset = cloneFaceDataset(s.FaceDataset)
	for id, value := range s.Validations {
		if value == nil {
			continue
		}
		persisted.Validations[id] = *cloneValidation(value)
	}
	for id, value := range s.BehaviorOverrides {
		persisted.BehaviorOverrides[id] = append(json.RawMessage(nil), value...)
	}
	for id, value := range s.ActionResults {
		if value == nil {
			continue
		}
		persisted.ActionResults[id] = *cloneActionResult(value)
	}
	persisted.Danger = cloneDangerAssessments(trimDanger(s.Danger, maxDanger))
	persisted.Events = cloneEvents(trimEvents(s.RecentEvents, maxRecentEvents))
	persisted.ValidationEvents = cloneEvents(trimEvents(s.ValidationEvents, maxRecentEvents))
	for id, value := range s.Identities {
		if value == nil {
			continue
		}
		persisted.Identities[id] = *value
	}
	for id, value := range s.Presence {
		if value == nil {
			continue
		}
		persisted.Presence[id] = *value
	}
	for id, value := range s.ResidentTracks {
		if value != nil {
			persisted.ResidentTracks[id] = *value
		}
	}
	for id, value := range s.EntityTracks {
		if value != nil {
			cloned := *value
			cloned.CandidateResidentID = ""
			persisted.EntityTracks[id] = cloned
		}
	}
	for id, value := range s.EventChains {
		if value != nil {
			persisted.EventChains[id] = *cloneEventChain(value)
		}
	}
	for id, value := range s.CriticalChains {
		if value != nil {
			persisted.CriticalChains[id] = *cloneCriticalChainMemory(value)
		}
	}
	for id, value := range s.Incidents {
		if value != nil {
			persisted.Incidents[id] = *cloneIncident(value)
		}
	}
	trimPersistedIncidents(persisted.Incidents, s.incidentLimit)
	system := s.systemStateLocked()
	persisted.System = &system
	return persisted
}

func persistedSummary(value *PersistedState) PersistedSummary {
	if value == nil {
		return PersistedSummary{}
	}
	return PersistedSummary{
		Events:        len(value.Events),
		Clips:         len(value.Clips),
		Validations:   len(value.Validations),
		ActionResults: len(value.ActionResults),
		Danger:        len(value.Danger),
		Identities:    len(value.Identities),
		Presence:      len(value.Presence),
		Incidents:     len(value.Incidents),
	}
}

func (s *Store) deleteRecentEvent(id string) {
	s.mu.Lock()
	out := s.RecentEvents[:0]
	for _, value := range s.RecentEvents {
		if value == nil || value.ID == id {
			continue
		}
		out = append(out, value)
	}
	s.RecentEvents = out
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) deleteDangerAssessment(id string) {
	s.mu.Lock()
	out := s.Danger[:0]
	for _, value := range s.Danger {
		if value == nil || value.ID == id {
			continue
		}
		out = append(out, value)
	}
	s.Danger = out
	s.mu.Unlock()
	s.SaveNow()
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func cloneEvents(source []*contract.Event) []*contract.Event {
	if source == nil {
		return nil
	}
	out := make([]*contract.Event, 0, len(source))
	for _, value := range source {
		if value == nil {
			continue
		}
		out = append(out, cloneEvent(value))
	}
	return out
}

func trimEvents(events []*contract.Event, limit int) []*contract.Event {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

func cloneEvent(value *contract.Event) *contract.Event {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Payload != nil {
		cloned.Payload = cloneMap(value.Payload)
	}
	return &cloned
}

func cloneValidation(value *contract.ValidationRequest) *contract.ValidationRequest {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Evidence = append([]string(nil), value.Evidence...)
	cloned.Correction = cloneMap(value.Correction)
	if value.DeletedAt != nil {
		deletedAt := *value.DeletedAt
		cloned.DeletedAt = &deletedAt
	}
	if value.ResolvedAt != nil {
		resolvedAt := *value.ResolvedAt
		cloned.ResolvedAt = &resolvedAt
	}
	return &cloned
}

func cloneActionResult(value *contract.ActionResult) *contract.ActionResult {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Details != nil {
		cloned.Details = cloneMap(value.Details)
	}
	if value.Data != nil {
		cloned.Data = cloneMap(value.Data)
	}
	return &cloned
}

func cloneDangerAssessment(value *contract.DangerAssessment) *contract.DangerAssessment {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Reasons = append([]string(nil), value.Reasons...)
	cloned.Evidence = append([]string(nil), value.Evidence...)
	cloned.RecommendedSystemActions = make([]contract.SystemActionRecommendation, 0, len(value.RecommendedSystemActions))
	for _, action := range value.RecommendedSystemActions {
		actionCopy := action
		if action.Data != nil {
			actionCopy.Data = cloneMap(action.Data)
		}
		cloned.RecommendedSystemActions = append(cloned.RecommendedSystemActions, actionCopy)
	}
	if value.ExpiresAt != nil {
		expiresAt := *value.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	return &cloned
}

func cloneDangerAssessments(source []*contract.DangerAssessment) []*contract.DangerAssessment {
	if source == nil {
		return nil
	}
	out := make([]*contract.DangerAssessment, 0, len(source))
	for _, value := range source {
		if !contract.IsPersistableDangerAssessment(value) {
			continue
		}
		out = append(out, cloneDangerAssessment(value))
	}
	return out
}

func trimDanger(items []*contract.DangerAssessment, limit int) []*contract.DangerAssessment {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func (s *Store) trimActionResultsLocked(limit int) {
	if limit <= 0 || len(s.ActionResults) <= limit {
		return
	}
	for len(s.ActionResults) > limit {
		var oldestID string
		var oldestTime time.Time
		for id, value := range s.ActionResults {
			if value == nil {
				oldestID = id
				break
			}
			ts := value.FinishedAt
			if ts.IsZero() {
				ts = value.StartedAt
			}
			if ts.IsZero() {
				ts = value.Timestamp
			}
			if oldestID == "" || ts.Before(oldestTime) {
				oldestID = id
				oldestTime = ts
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.ActionResults, oldestID)
	}
}

func (s *Store) trimDangerLocked(limit int) {
	if limit <= 0 || len(s.Danger) <= limit {
		return
	}
	s.Danger = s.Danger[len(s.Danger)-limit:]
}
