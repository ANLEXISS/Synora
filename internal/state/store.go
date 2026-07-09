package state

import (
	"sync"
	"time"

	"synora/pkg/contract"
)

type Store struct {
	mu sync.RWMutex

	DeviceStates  map[string]*DeviceState
	CameraStates  map[string]*CameraState
	NodeStates    map[string]*NodeState
	Tracks        map[string]*Track
	Clusters      map[string]*Cluster
	Identities    map[string]*IdentityState
	Presence      map[string]*PresenceState
	Clips         map[string]*ClipState
	Validations   map[string]*contract.ValidationRequest
	ActionResults map[string]*contract.ActionResult
	EventWindows  map[string]*contract.EventWindow
	System        *SystemState
}

func NewStore(_ ...string) *Store {
	now := time.Now().UTC()
	return &Store{
		DeviceStates:  make(map[string]*DeviceState),
		CameraStates:  make(map[string]*CameraState),
		NodeStates:    make(map[string]*NodeState),
		Tracks:        make(map[string]*Track),
		Clusters:      make(map[string]*Cluster),
		Identities:    make(map[string]*IdentityState),
		Presence:      make(map[string]*PresenceState),
		Clips:         make(map[string]*ClipState),
		Validations:   make(map[string]*contract.ValidationRequest),
		ActionResults: make(map[string]*contract.ActionResult),
		EventWindows:  make(map[string]*contract.EventWindow),
		System: &SystemState{
			LastState:     "idle",
			LastStateTime: now,
		},
	}
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
	defer s.mu.Unlock()
	cloned := *value
	s.Identities[value.ID] = &cloned
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
	defer s.mu.Unlock()
	delete(s.Identities, id)
}

func (s *Store) SetPresence(value *PresenceState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.Presence[value.ID] = &cloned
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
	defer s.mu.Unlock()
	delete(s.Presence, id)
}

func (s *Store) SetClip(value *ClipState) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *value
	s.Clips[value.ID] = &cloned
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
	defer s.mu.Unlock()
	delete(s.Clips, id)
}

func (s *Store) SetValidation(value *contract.ValidationRequest) {
	if value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Validations[value.ID] = cloneValidation(value)
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
	defer s.mu.Unlock()
	cloned := cloneActionResult(value)
	cloned.ID = id
	s.ActionResults[id] = cloned
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
	case "action_results":
		for id, value := range s.ActionResults {
			if value == nil {
				continue
			}
			out[id] = *cloneActionResult(value)
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
	case "action_results":
		s.mu.Lock()
		delete(s.ActionResults, id)
		s.mu.Unlock()
	case "windows":
		s.DeleteEventWindow(id)
	}
}

func (s *Store) Cleanup(now time.Time, cfg ExpirationConfig) CleanupResult {
	result := CleanupResult{Deleted: map[string][]string{}}

	s.mu.Lock()
	defer s.mu.Unlock()

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
		if value == nil || (!value.ExpiresAt.IsZero() && !value.ExpiresAt.After(now)) {
			delete(s.Presence, id)
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

func cloneValidation(value *contract.ValidationRequest) *contract.ValidationRequest {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Evidence = append([]string(nil), value.Evidence...)
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
	return &cloned
}
