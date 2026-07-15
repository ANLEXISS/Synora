package rpc

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func (s *Server) deviceConfigList(_ contract.Message) (any, error) {
	return s.devices.PublicViews(), nil
}

func (s *Server) automationConfigList(_ contract.Message) (any, error) {
	items := s.automation.List()
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, automationPublicView(item))
	}
	return out, nil
}

func (s *Server) automationConfigGet(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	item, ok := s.automation.Get(strings.TrimSpace(req.ID))
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "automation not found")
	}
	return automationPublicView(item), nil
}

func (s *Server) automationConfigCreate(msg contract.Message) (any, error) {
	if err := validateObjectFields(msg.Payload,
		"id", "name", "enabled", "description", "priority", "trigger", "conditions",
		"condition_logic", "actions", "cooldown_ms", "timeout_ms", "retry_count",
		"dry_run", "requires_validation", "status"); err != nil {
		return nil, err
	}
	var value automation.Rule
	if err := decodePayload(msg.Payload, &value); err != nil {
		return nil, err
	}
	created, err := s.automation.Create(value)
	if err != nil {
		return nil, err
	}
	s.notifyMutation("automation.updated", created.ID)
	return automationPublicView(created), nil
}

func (s *Server) automationConfigUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	var patch contract.AutomationPatch
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	updated, err := s.automation.Patch(strings.TrimSpace(req.ID), patch)
	if err != nil {
		return nil, err
	}
	s.notifyMutation("automation.updated", updated.ID)
	return automationPublicView(updated), nil
}

func (s *Server) automationConfigDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	deleted, err := s.automation.SoftDelete(strings.TrimSpace(req.ID))
	if err != nil {
		return nil, err
	}
	s.notifyMutation("automation.updated", deleted.ID)
	return automationPublicView(deleted), nil
}

func automationPublicView(rule automation.Rule) map[string]any {
	data, _ := json.Marshal(rule)
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	return sanitizeConfigurationMap(value)
}

func sanitizeConfigurationMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "path" || lower == "clip_path" || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "credential") ||
			strings.HasSuffix(lower, "_token") || lower == "token" || lower == "private_key" {
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			out[key] = sanitizeConfigurationMap(typed)
		case []any:
			items := make([]any, 0, len(typed))
			for _, item := range typed {
				if mapped, ok := item.(map[string]any); ok {
					items = append(items, sanitizeConfigurationMap(mapped))
				} else {
					items = append(items, item)
				}
			}
			out[key] = items
		default:
			out[key] = value
		}
	}
	return out
}

func (s *Server) topologyConfigSnapshot(_ contract.Message) (any, error) {
	if s.snapshot == nil || s.snapshot.Mu == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "topology unavailable")
	}
	s.snapshot.Mu.RLock()
	config := s.snapshot.Topology.ConfigView()
	s.snapshot.Mu.RUnlock()
	return topologyPublicView(config), nil
}

func (s *Server) topologyConfigReplace(msg contract.Message) (any, error) {
	if err := validateObjectFields(msg.Payload, "version", "locked", "root_id", "house_id", "nodes", "links", "edges"); err != nil {
		return nil, err
	}
	var config contract.TopologyConfig
	if err := decodePayload(msg.Payload, &config); err != nil {
		return nil, err
	}
	prepared, err := topology.FromConfig(config)
	if err != nil {
		return nil, err
	}
	if err := s.commitTopologyReplacement(prepared); err != nil {
		return nil, err
	}
	s.notifyMutation("topology.updated", "topology")
	return topologyPublicView(prepared.ConfigView()), nil
}

func (s *Server) topologyConfigDelete(_ contract.Message) (any, error) {
	prepared, err := topology.FromConfig(contract.TopologyConfig{
		Version: topology.TopologyConfigVersion,
		Nodes:   []contract.TopologyNode{},
		Links:   []contract.TopologyLink{},
	})
	if err != nil {
		return nil, err
	}
	if err := s.commitTopologyReplacement(prepared); err != nil {
		return nil, err
	}
	s.notifyMutation("topology.updated", "topology")
	return topologyPublicView(prepared.ConfigView()), nil
}

func (s *Server) commitTopologyReplacement(prepared *topology.Topology) error {
	if prepared == nil {
		return contract.NewAPIError(contract.ErrorValidationFailed, "topology is required")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()

	s.snapshot.Mu.RLock()
	previousTopology := s.snapshot.Topology.Clone()
	s.snapshot.Mu.RUnlock()
	previousDevices := s.devices.Ordered()
	previousAutomations := s.automation.List()

	if err := topology.Save(s.topologyPath, prepared); err != nil {
		return err
	}
	valid := prepared.NodeIDs()
	devices, err := s.devices.MoveMissingNodesToUnlocated(valid)
	if err != nil {
		s.rollbackTopologyFiles(previousTopology, previousDevices, previousAutomations)
		return err
	}
	if _, err := s.automation.DisableMissingTopologyNodes(valid); err != nil {
		s.rollbackTopologyFiles(previousTopology, previousDevices, previousAutomations)
		return err
	}

	s.snapshot.SetTopology(prepared)
	if s.updateTopology != nil {
		s.updateTopology(prepared)
	}
	for i := range devices {
		value := devices[i]
		s.syncDeviceConfigState(&value)
	}
	return nil
}

func (s *Server) rollbackTopologyFiles(previousTopology *topology.Topology, devices []device.DeviceConfig, automations []automation.Rule) {
	if previousTopology != nil {
		_ = topology.Save(s.topologyPath, previousTopology)
	}
	if err := device.Save(s.devicePath, devices); err == nil {
		s.devices.Replace(devices)
	}
	if err := automation.SaveToFile(s.automationPath, automations); err == nil {
		_ = s.automation.Load()
	}
}

func topologyPublicView(config contract.TopologyConfig) map[string]any {
	data, _ := json.Marshal(config)
	var view map[string]any
	_ = json.Unmarshal(data, &view)
	return sanitizeConfigurationMap(view)
}

func (s *Server) deviceConfigGet(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	item, ok := s.devices.Get(strings.TrimSpace(req.ID))
	if !ok || item == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "device not found")
	}
	return item.PublicView(), nil
}

func (s *Server) deviceConfigCreate(msg contract.Message) (any, error) {
	if err := validateObjectFields(msg.Payload,
		"id", "name", "type", "vendor", "model", "serial", "pairing_method", "status",
		"role", "node_id", "zone_role", "room_name",
		"enabled", "trusted", "capabilities", "config", "metadata"); err != nil {
		return nil, err
	}
	var value device.DeviceConfig
	if err := decodePayload(msg.Payload, &value); err != nil {
		return nil, err
	}
	created, err := s.devices.Create(value)
	if err != nil {
		return nil, err
	}
	s.syncDeviceConfigState(created)
	s.notifyMutation("device.updated", created.ID)
	return created.PublicView(), nil
}

func (s *Server) deviceConfigUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if err := validateObjectFields(req.Data,
		"name", "display_name", "room", "node_id", "role", "zone_role", "room_name",
		"enabled", "trusted", "capabilities", "config", "metadata", "network"); err != nil {
		return nil, err
	}
	var patch contract.DevicePatch
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	if _, ok := s.devices.Get(id); !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "device %q not found", id)
	}
	if patch.NodeID != nil {
		if err := s.validateDeviceNode(*patch.NodeID); err != nil {
			return nil, err
		}
	} else if patch.Room != nil {
		if err := s.validateDeviceNode(*patch.Room); err != nil {
			return nil, err
		}
	}
	updated, err := s.devices.Patch(id, patch)
	if err != nil {
		return nil, err
	}
	s.syncDeviceConfigState(updated)
	s.notifyMutation("device.updated", updated.ID)
	return updated.PublicView(), nil
}

func (s *Server) validateDeviceNode(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || nodeID == device.UnlocatedNodeID || s == nil || s.snapshot == nil || s.snapshot.Mu == nil || s.snapshot.Topology == nil {
		return nil
	}
	s.snapshot.Mu.RLock()
	defer s.snapshot.Mu.RUnlock()
	if len(s.snapshot.Topology.Nodes) == 0 {
		return nil
	}
	node, ok := s.snapshot.Topology.Nodes[nodeID]
	if !ok || node == nil {
		return contract.NewAPIError(contract.ErrorInvalidRequest, "node_id %q does not identify an existing room", nodeID)
	}
	if node.Type != topology.NodeRoom {
		return contract.NewAPIError(contract.ErrorInvalidRequest, "node_id %q must identify a room", nodeID)
	}
	return nil
}

func (s *Server) deviceConfigDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	deleted, err := s.devices.Remove(strings.TrimSpace(req.ID))
	if err != nil {
		return nil, err
	}
	if s.state != nil {
		s.state.Delete("devices", deleted.ID)
		s.state.Delete("cameras", deleted.ID)
	}
	s.notifyMutation("device.deleted", deleted.ID)
	return map[string]any{"status": "deleted", "device_id": deleted.ID}, nil
}

func (s *Server) syncDeviceConfigState(value *device.Device) {
	if value == nil || s.state == nil {
		return
	}
	now := time.Now().UTC()
	current, _ := s.state.DeviceState(value.ID)
	if current == nil {
		current = &state.DeviceState{ID: value.ID, CreatedAt: value.CreatedAt}
		if current.CreatedAt.IsZero() {
			current.CreatedAt = now
		}
	}
	current.Type = value.Type
	current.Role = value.Role
	current.Room = value.Room
	current.NodeID = value.NodeID
	if !value.Enabled {
		current.Online = false
	}
	current.UpdatedAt = now
	s.state.SetDeviceState(current)
	if value.Type != contract.DeviceTypeCamera {
		return
	}
	camera, _ := s.state.CameraState(value.ID)
	if camera == nil {
		camera = &state.CameraState{ID: value.ID, CreatedAt: current.CreatedAt}
	}
	camera.NodeID = value.NodeID
	if !value.Enabled {
		camera.Online = false
	}
	camera.UpdatedAt = now
	s.state.SetCameraState(camera)
}

func (s *Server) residentConfigList(_ contract.Message) (any, error) {
	s.snapshot.Mu.RLock()
	defer s.snapshot.Mu.RUnlock()
	ids := make([]string, 0, len(s.snapshot.Residents))
	for id := range s.snapshot.Residents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]contract.ResidentView, 0, len(ids))
	for _, id := range ids {
		if value := s.snapshot.Residents[id]; value != nil {
			out = append(out, value.ConfigView())
		}
	}
	return out, nil
}

func (s *Server) residentConfigGet(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	s.snapshot.Mu.RLock()
	item, ok := topology.GetResident(s.snapshot.Residents, strings.TrimSpace(req.ID))
	s.snapshot.Mu.RUnlock()
	if !ok || item == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "resident not found")
	}
	return item.ConfigView(), nil
}

func (s *Server) residentConfigCreate(msg contract.Message) (any, error) {
	if err := validateObjectFields(msg.Payload,
		"id", "name", "first_name", "last_name", "display_name", "role", "admin", "enabled", "trusted",
		"reference_node_id", "account_id", "face_profile",
		"contact", "baseline", "presence_profile", "identity_profile", "permissions", "metadata"); err != nil {
		return nil, err
	}
	var value topology.Resident
	if err := decodePayload(msg.Payload, &value); err != nil {
		return nil, err
	}
	if err := s.validateResidentReference(value.ReferenceNodeID); err != nil {
		return nil, err
	}
	s.snapshot.Mu.Lock()
	created, err := topology.CreateResident(s.residentsPath, s.snapshot.Residents, value)
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.notifyMutation("resident.updated", created.ID)
	return created.ConfigView(), nil
}

func (s *Server) residentConfigUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	var patch contract.ResidentPatch
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	if patch.ReferenceNodeID != nil {
		if err := s.validateResidentReference(*patch.ReferenceNodeID); err != nil {
			return nil, err
		}
	}
	s.snapshot.Mu.Lock()
	updated, err := topology.PatchResident(s.residentsPath, s.snapshot.Residents, strings.TrimSpace(req.ID), patch)
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.notifyMutation("resident.updated", updated.ID)
	return updated.ConfigView(), nil
}

func (s *Server) validateResidentReference(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || s == nil || s.snapshot == nil || s.snapshot.Topology == nil {
		return nil
	}
	node, ok := s.snapshot.Topology.Nodes[nodeID]
	if !ok || node == nil || node.Type != topology.NodeRoom {
		return contract.NewAPIError(contract.ErrorValidationFailed, "reference_node_id must identify an existing room")
	}
	return nil
}

func (s *Server) residentConfigDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	s.snapshot.Mu.Lock()
	deleted, err := topology.SoftDeleteResident(s.residentsPath, s.snapshot.Residents, strings.TrimSpace(req.ID))
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	// Runtime identity, presence and source events are intentionally preserved.
	s.notifyMutation("resident.updated", deleted.ID)
	return deleted.ConfigView(), nil
}

func validateObjectFields(raw []byte, allowed ...string) error {
	var fields map[string]json.RawMessage
	if err := decodePayload(raw, &fields); err != nil {
		return err
	}
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	for key := range fields {
		if !set[key] {
			return contract.NewAPIError(contract.ErrorValidationFailed, "unknown field %q", key)
		}
	}
	return nil
}
