package state

import (
	"encoding/json"
	"sync"
	"time"

	"synora/pkg/contract"
)

type Store struct {
	mu sync.RWMutex

	DeviceStates      map[string]*DeviceState
	CameraStates      map[string]*CameraState
	NodeStates        map[string]*NodeState
	Tracks            map[string]*Track
	Clusters          map[string]*Cluster
	Identities        map[string]*IdentityState
	Presence          map[string]*PresenceState
	Clips             map[string]*ClipState
	Validations       map[string]*contract.ValidationRequest
	BehaviorOverrides map[string]json.RawMessage
	ActionResults     map[string]*contract.ActionResult
	Danger            []*contract.DangerAssessment
	RecentEvents      []*contract.Event
	EventWindows      map[string]*contract.EventWindow
	System            *SystemState

	persistence Persistence
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
		Clusters:          make(map[string]*Cluster),
		Identities:        make(map[string]*IdentityState),
		Presence:          make(map[string]*PresenceState),
		Clips:             make(map[string]*ClipState),
		Validations:       make(map[string]*contract.ValidationRequest),
		BehaviorOverrides: make(map[string]json.RawMessage),
		ActionResults:     make(map[string]*contract.ActionResult),
		Danger:            []*contract.DangerAssessment{},
		RecentEvents:      []*contract.Event{},
		EventWindows:      make(map[string]*contract.EventWindow),
		System: &SystemState{
			LastState:     "idle",
			LastStateTime: now,
		},
	}
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
}

func (s *Store) SetCluster(value *Cluster) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	cloned.EventIDs = append([]string(nil), value.EventIDs...)
	s.Clusters[value.ID] = &cloned
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
}

func (s *Store) SetIdentity(value *IdentityState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	cloned := *value
	s.Identities[value.ID] = &cloned
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
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetPresence(value *PresenceState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	cloned := *value
	if current := s.Presence[value.ID]; current != nil && cloned.LastSeen.IsZero() {
		// LastSeen is historical runtime data. An absent/cleared update may
		// reset the current state, but it must never erase the last observation.
		cloned.LastSeen = current.LastSeen
	}
	s.Presence[value.ID] = &cloned
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
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetClip(value *ClipState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	cloned := *value
	s.Clips[value.ID] = &cloned
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
	cloned := *value
	return &cloned, true
}

func (s *Store) DeleteClip(id string) {
	s.mu.Lock()
	delete(s.Clips, id)
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) SetValidation(value *contract.ValidationRequest) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	s.Validations[value.ID] = cloneValidation(value)
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
	s.mu.Unlock()
	s.SaveNow()
}

func (s *Store) RecentEventsList() []*contract.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEvents(s.RecentEvents)
}

func (s *Store) SetEventWindow(nodeID string, value *contract.EventWindow) {
	if nodeID == "" || value == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventWindows[nodeID] = cloneWindow(value)
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
}

func (s *Store) SystemState() SystemState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.System == nil {
		return SystemState{}
	}
	cloned := *s.System
	return cloned
}

func (s *Store) SetSystemState(value SystemState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := value
	s.System = &cloned
}

func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.DeviceStates) + len(s.CameraStates) + len(s.NodeStates) + len(s.Tracks) + len(s.Clusters) + len(s.Identities) + len(s.Presence) + len(s.Clips) + len(s.EventWindows)
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
			cloned := *value
			out[id] = cloned
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
	for id, value := range s.Clips {
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.Clips, id)
			result.Deleted["clips"] = append(result.Deleted["clips"], id)
		}
	}
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
	if s.persistence == nil {
		return PersistedSummary{}, nil
	}
	persisted, err := s.persistence.Load()
	if persisted == nil {
		return PersistedSummary{}, err
	}
	s.applyPersistedState(persisted)
	return persistedSummary(persisted), err
}

func (s *Store) SaveNow() error {
	if s.persistence == nil {
		return nil
	}
	persisted := s.PersistedState()
	return s.persistence.Save(persisted)
}

func (s *Store) BackupNow() error {
	if s.persistence == nil {
		return nil
	}
	if persistence, ok := s.persistence.(BackupPersistence); ok {
		return persistence.Backup()
	}
	return nil
}

func (s *Store) Close() error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.Close()
}

func (s *Store) PersistedState() *PersistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persistedStateLocked(time.Now().UTC())
}

func (s *Store) applyPersistedState(persisted *PersistedState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if persisted.Clips != nil {
		s.Clips = make(map[string]*ClipState, len(persisted.Clips))
		for id, value := range persisted.Clips {
			cloned := value
			if cloned.ID == "" {
				cloned.ID = id
			}
			s.Clips[id] = &cloned
		}
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
}

func (s *Store) persistedStateLocked(savedAt time.Time) *PersistedState {
	persisted := emptyPersistedState()
	persisted.SavedAt = savedAt
	for id, value := range s.Clips {
		if value == nil {
			continue
		}
		persisted.Clips[id] = *value
	}
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
