package rpc

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/idgen"
	"synora/internal/security"
	"synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type Sender interface {
	Send(contract.Message) error
}

type Requester interface {
	Request(msgType string, source string, payload []byte, target string) (*contract.Message, error)
}

type Metrics interface {
	Snapshot(*state.Store) map[string]any
	SourceStatus(string, time.Duration) map[string]any
}

type Handler func(contract.Message) (any, error)

type Server struct {
	bus        Sender
	state      *state.Store
	events     *event.Store
	devices    *device.Registry
	automation *automation.Engine
	snapshot   *snapshot.Builder
	metrics    Metrics

	topologyPath   string
	residentsPath  string
	devicePath     string
	automationPath string
	securityPath   string

	publishEvent   func(string, any, int)
	updateTopology func(*topology.Topology)
	pairing        *security.PairingService

	handlers map[string]Handler
}

type Config struct {
	Bus            Sender
	State          *state.Store
	Events         *event.Store
	Devices        *device.Registry
	Automation     *automation.Engine
	Snapshot       *snapshot.Builder
	Metrics        Metrics
	TopologyPath   string
	ResidentsPath  string
	DevicePath     string
	AutomationPath string
	SecurityPath   string
	PublishEvent   func(string, any, int)
	UpdateTopology func(*topology.Topology)
}

type MutationPayload struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type DeletePayload struct {
	ID string `json:"id"`
}

func NewServer(cfg Config) *Server {
	server := &Server{
		bus:            cfg.Bus,
		state:          cfg.State,
		events:         cfg.Events,
		devices:        cfg.Devices,
		automation:     cfg.Automation,
		snapshot:       cfg.Snapshot,
		metrics:        cfg.Metrics,
		topologyPath:   cfg.TopologyPath,
		residentsPath:  cfg.ResidentsPath,
		devicePath:     cfg.DevicePath,
		automationPath: cfg.AutomationPath,
		securityPath:   cfg.SecurityPath,
		publishEvent:   cfg.PublishEvent,
		updateTopology: cfg.UpdateTopology,
		pairing:        &security.PairingService{Path: cfg.SecurityPath},
		handlers:       map[string]Handler{},
	}
	server.register()
	return server
}

func (s *Server) Handle(msg contract.Message) {
	handler, ok := s.handlers[msg.Type]
	if !ok {
		return
	}
	result, err := handler(msg)
	response := contract.Message{
		ID:        msg.ID,
		Type:      msg.Type,
		Kind:      contract.KindRPC,
		Source:    "core",
		Target:    msg.Source,
		Timestamp: time.Now().UTC(),
	}
	if err != nil {
		payload, _ := json.Marshal(map[string]any{"error": err.Error()})
		response.Payload = payload
	} else if result != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			payload, _ = json.Marshal(map[string]any{"error": marshalErr.Error()})
		}
		response.Payload = payload
	} else {
		response.Payload = []byte("{}")
	}
	if sendErr := s.bus.Send(response); sendErr != nil {
		log.Println("core: rpc response send error", sendErr)
	}
}

func (s *Server) Handler(name string) Handler {
	return s.handlers[name]
}

func (s *Server) register() {
	s.handlers["core.state"] = s.coreState
	s.handlers["event.list"] = s.eventList
	s.handlers["system.health"] = s.systemHealth
	s.handlers["system.reset_intrusion"] = s.systemResetIntrusion
	s.handlers["device.list"] = s.deviceList
	s.handlers["device.update"] = s.deviceUpdate
	s.handlers["device.delete"] = s.deviceDelete
	s.handlers["devices.pairing.start"] = s.devicePairingStart
	s.handlers["devices.pairing.complete"] = s.devicePairingComplete
	s.handlers["residents.list"] = s.residentsList
	s.handlers["residents.create"] = s.residentsCreate
	s.handlers["resident.update"] = s.residentUpdate
	s.handlers["resident.delete"] = s.residentDelete
	s.handlers["automation.list"] = s.automationList
	s.handlers["automation.create"] = s.automationCreate
	s.handlers["automation.delete"] = s.automationDelete
	s.handlers["validations.list"] = s.validationsList
	s.handlers["validations.resolve"] = s.validationsResolve
	s.handlers["cge.summary"] = s.cgeSummary
	s.handlers["cge.sequences"] = s.cgeSequences
	s.handlers["cge.transitions"] = s.cgeTransitions
	s.handlers["cge.learned_behaviors"] = s.cgeLearnedBehaviors
	s.handlers["cge.sequence"] = s.cgeSequence
	s.handlers["cge.learned_behavior"] = s.cgeLearnedBehavior
	s.handlers["topology.snapshot"] = s.topologySnapshot
	s.handlers["topology.reset"] = s.topologyReset
	s.handlers["core.snapshot"] = s.legacySnapshot
	s.handlers["core.snapshot.apply"] = s.applySnapshot
}

func (s *Server) coreState(_ contract.Message) (any, error) {
	return s.snapshot.CoreState(), nil
}

func (s *Server) eventList(_ contract.Message) (any, error) {
	return s.events.List(), nil
}

func (s *Server) systemHealth(_ contract.Message) (any, error) {
	if requester, ok := s.bus.(Requester); ok {
		response, err := requester.Request(
			contract.RPCRuntimeHealth,
			"core",
			nil,
			"runtime-manager",
		)

		if err == nil && response != nil && len(response.Payload) > 0 {
			var health contract.RuntimeHealth
			if decodeErr := json.Unmarshal(
				response.Payload,
				&health,
			); decodeErr == nil {
				return health, nil
			}
		}
	}

	now := time.Now().UTC()

	return contract.RuntimeHealth{
		Services: map[string]contract.RuntimeServiceHealth{
			"synora-core": {
				Name:    "synora-core",
				Status:  "ok",
				Active:  true,
				Checked: now,
			},
		},
		Network: contract.RuntimeNetworkHealth{
			Status: "unknown",
		},
		MediaMTX: contract.RuntimeMediaMTXHealth{
			Status: "unknown",
		},
		Disk: contract.RuntimeDiskHealth{
			Status: "unknown",
		},
		Uptime:    0,
		Timestamp: now,
	}, nil
}

func (s *Server) systemResetIntrusion(_ contract.Message) (any, error) {
	current := s.state.SystemState()
	current.LastState = "idle"
	current.LastStateTime = time.Now().UTC()
	current.IntrusionActive = false
	current.IntrusionTime = time.Time{}
	s.state.SetSystemState(current)
	if s.publishEvent != nil {
		s.publishEvent(contract.EventSystemStateChanged, current, contract.PriorityHigh)
	}
	return map[string]any{"status": "ok"}, nil
}

func (s *Server) deviceList(_ contract.Message) (any, error) {
	return s.snapshot.DeviceViews(), nil
}

func (s *Server) deviceUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, errors.New("id is required")
	}
	dev, ok := s.devices.Get(req.ID)
	if !ok || dev == nil {
		return nil, errors.New("device not found")
	}
	var patch map[string]any
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	if room, ok := patch["room"].(string); ok && strings.TrimSpace(room) != "" {
		dev.Room = strings.TrimSpace(room)
		dev.NodeID = dev.Room
	}
	if role, ok := patch["role"].(string); ok {
		dev.Role = strings.TrimSpace(role)
	}

	items := s.devices.Ordered()
	for i := range items {
		if items[i].ID == dev.ID {
			items[i] = *dev
		}
	}
	if err := device.Save(s.devicePath, items); err != nil {
		return nil, err
	}
	s.devices.Replace(items)
	now := time.Now().UTC()
	current, _ := s.state.DeviceState(dev.ID)
	if current == nil {
		current = &state.DeviceState{ID: dev.ID, CreatedAt: now}
	}
	current.Type = dev.Type
	current.Role = dev.Role
	current.Room = dev.Room
	current.NodeID = dev.NodeID
	current.UpdatedAt = now
	s.state.SetDeviceState(current)
	if dev.Type == "camera" {
		cameraState, _ := s.state.CameraState(dev.ID)
		if cameraState == nil {
			cameraState = &state.CameraState{ID: dev.ID, CreatedAt: now}
		}
		cameraState.NodeID = dev.NodeID
		cameraState.UpdatedAt = now
		s.state.SetCameraState(cameraState)
	} else {
		s.state.Delete("cameras", dev.ID)
	}
	return s.snapshot.DeviceViews(), nil
}

func (s *Server) deviceDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, errors.New("id is required")
	}
	items := s.devices.Ordered()
	filtered := make([]device.DeviceConfig, 0, len(items))
	for _, item := range items {
		if item.ID != req.ID {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == len(items) {
		return nil, errors.New("device not found")
	}
	if err := device.Save(s.devicePath, filtered); err != nil {
		return nil, err
	}
	s.devices.Replace(filtered)
	s.state.Delete("devices", req.ID)
	s.state.Delete("cameras", req.ID)
	return map[string]any{"ok": true, "id": req.ID}, nil
}

func (s *Server) devicePairingStart(_ contract.Message) (any, error) {
	if s.pairing == nil {
		return nil, errors.New("pairing unavailable")
	}
	return s.pairing.Start()
}

func (s *Server) devicePairingComplete(msg contract.Message) (any, error) {
	if s.pairing == nil {
		return nil, errors.New("pairing unavailable")
	}
	var req security.PairingCompleteRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	return s.pairing.Complete(req)
}

func (s *Server) residentsList(_ contract.Message) (any, error) {
	return s.snapshot.ResidentViews(), nil
}

func (s *Server) residentsCreate(msg contract.Message) (any, error) {
	var resident topology.Resident
	if err := decodePayload(msg.Payload, &resident); err != nil {
		return nil, err
	}
	resident.ID = strings.TrimSpace(resident.ID)
	if resident.ID == "" {
		return nil, errors.New("resident id is required")
	}
	s.snapshot.Mu.Lock()
	if _, exists := s.snapshot.Residents[resident.ID]; exists {
		s.snapshot.Mu.Unlock()
		return nil, errors.New("resident already exists")
	}
	s.snapshot.Residents[resident.ID] = &resident
	err := topology.SaveResidents(s.residentsPath, s.snapshot.Residents)
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.snapshot.ResidentViews(), nil
}

func (s *Server) residentUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	var patch map[string]any
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	s.snapshot.Mu.Lock()
	resident, ok := s.snapshot.Residents[req.ID]
	if !ok || resident == nil {
		s.snapshot.Mu.Unlock()
		return nil, errors.New("resident not found")
	}
	if name, ok := patch["name"].(string); ok {
		resident.Name = strings.TrimSpace(name)
	}
	if role, ok := patch["role"].(string); ok {
		resident.Role = strings.TrimSpace(role)
	}
	if admin, ok := patch["admin"].(bool); ok {
		resident.Admin = admin
	}
	err := topology.SaveResidents(s.residentsPath, s.snapshot.Residents)
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.snapshot.ResidentViews(), nil
}

func (s *Server) residentDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	s.snapshot.Mu.Lock()
	if _, ok := s.snapshot.Residents[req.ID]; !ok {
		s.snapshot.Mu.Unlock()
		return nil, errors.New("resident not found")
	}
	delete(s.snapshot.Residents, req.ID)
	err := topology.SaveResidents(s.residentsPath, s.snapshot.Residents)
	s.snapshot.Mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.state.Delete("presence", req.ID)
	s.state.Delete("identities", req.ID)
	return map[string]any{"ok": true, "id": req.ID}, nil
}

func (s *Server) automationList(_ contract.Message) (any, error) {
	return s.automation.List(), nil
}

func (s *Server) automationCreate(msg contract.Message) (any, error) {
	var rule automation.Rule
	if err := decodePayload(msg.Payload, &rule); err != nil {
		return nil, err
	}
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = idgen.New("automation")
	}
	if err := s.automation.Add(rule); err != nil {
		return nil, err
	}
	return s.automation.List(), nil
}

func (s *Server) automationDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, errors.New("id is required")
	}
	if err := s.automation.Remove(req.ID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": req.ID}, nil
}

func (s *Server) validationsList(_ contract.Message) (any, error) {
	validations := s.state.ValidationsList()
	sort.Slice(validations, func(i, j int) bool {
		return validations[i].CreatedAt.Before(validations[j].CreatedAt)
	})
	return validations, nil
}

func (s *Server) validationsResolve(msg contract.Message) (any, error) {
	var req contract.ValidationResolveRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return nil, errors.New("validation id is required")
	}

	validation, ok := s.state.Validation(req.ID)
	if !ok || validation == nil {
		return nil, errors.New("validation not found")
	}

	status, proposedIdentity, err := resolveValidationStatus(req)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	validation.Status = status
	validation.ProposedIdentity = proposedIdentity
	validation.ResolvedAt = &now
	s.state.SetValidation(validation)

	return validation, nil
}

func resolveValidationStatus(req contract.ValidationResolveRequest) (string, string, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	proposedIdentity := strings.TrimSpace(req.ProposedIdentity)
	switch action {
	case contract.ValidationActionAccept:
		return contract.ValidationStatusAccepted, proposedIdentity, nil
	case contract.ValidationActionReject:
		return contract.ValidationStatusRejected, proposedIdentity, nil
	case contract.ValidationActionIgnore:
		return contract.ValidationStatusIgnored, proposedIdentity, nil
	case contract.ValidationActionAssignIdentity:
		if proposedIdentity == "" {
			return "", "", errors.New("proposed_identity is required")
		}
		return contract.ValidationStatusAccepted, proposedIdentity, nil
	default:
		return "", "", errors.New("unsupported validation action")
	}
}

func (s *Server) topologySnapshot(_ contract.Message) (any, error) {
	return s.snapshot.TopologyTreeViews(), nil
}

func (s *Server) topologyReset(msg contract.Message) (any, error) {
	if len(msg.Payload) > 0 && string(msg.Payload) != "null" && string(msg.Payload) != "{}" {
		if err := os.WriteFile(s.topologyPath, msg.Payload, 0644); err != nil {
			return nil, err
		}
	}
	loaded := &topology.Topology{Nodes: map[string]*topology.Node{}}
	if err := topology.Load(s.topologyPath, loaded); err != nil {
		return nil, err
	}
	s.snapshot.SetTopology(loaded)
	if s.updateTopology != nil {
		s.updateTopology(loaded)
	}
	return s.snapshot.TopologyTreeViews(), nil
}

func (s *Server) legacySnapshot(_ contract.Message) (any, error) {
	return s.snapshot.LegacySnapshot(), nil
}

func (s *Server) applySnapshot(_ contract.Message) (any, error) {
	return map[string]any{"ok": true}, nil
}

func decodePayload(raw []byte, out interface{}) error {
	if len(raw) == 0 {
		return errors.New("payload is required")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	return nil
}
