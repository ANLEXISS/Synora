package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/event"
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
	cge            any
	notify         func(string, string)
	configMu       sync.Mutex

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
	CGE            any
	NotifyMutation func(string, string)
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
		cge:            cfg.CGE,
		notify:         cfg.NotifyMutation,
		handlers:       map[string]Handler{},
	}
	if server.devices != nil && strings.TrimSpace(server.devicePath) != "" {
		server.devices.SetPersistencePath(server.devicePath)
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
		code := contract.APIErrorCode(err)
		message := err.Error()
		if code == contract.ErrorInternal {
			log.Printf("core: rpc %s failed: %v", msg.Type, err)
			message = "internal error"
		}
		payload, _ := json.Marshal(map[string]any{
			"error":   code,
			"message": message,
		})
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
	s.handlers["device.list"] = s.deviceConfigList
	s.handlers["device.get"] = s.deviceConfigGet
	s.handlers["device.create"] = s.deviceConfigCreate
	s.handlers["device.update"] = s.deviceConfigUpdate
	s.handlers["device.delete"] = s.deviceConfigDelete
	s.handlers["devices.pairing.start"] = s.devicePairingStart
	s.handlers["devices.pairing.complete"] = s.devicePairingComplete
	s.handlers["residents.list"] = s.residentConfigList
	s.handlers["resident.get"] = s.residentConfigGet
	s.handlers["residents.create"] = s.residentConfigCreate
	s.handlers["resident.update"] = s.residentConfigUpdate
	s.handlers["resident.delete"] = s.residentConfigDelete
	s.handlers["automation.list"] = s.automationConfigList
	s.handlers["automation.get"] = s.automationConfigGet
	s.handlers["automation.create"] = s.automationConfigCreate
	s.handlers["automation.update"] = s.automationConfigUpdate
	s.handlers["automation.delete"] = s.automationConfigDelete
	s.handlers["validations.list"] = s.validationsList
	s.handlers["validations.get"] = s.validationGet
	s.handlers["validations.create"] = s.validationCreate
	s.handlers["validations.update"] = s.validationUpdate
	s.handlers["validations.delete"] = s.validationDelete
	s.handlers["validations.resolve"] = s.validationsResolve
	s.handlers["cge.summary"] = s.cgeSummary
	s.handlers["cge.sequences"] = s.cgeSequences
	s.handlers["cge.transitions"] = s.cgeTransitions
	s.handlers["cge.learned_behaviors"] = s.cgeLearnedBehaviors
	s.handlers["cge.sequence"] = s.cgeSequence
	s.handlers["cge.learned_behavior"] = s.cgeLearnedBehavior
	s.handlers["cge.critical_seeds"] = s.cgeCriticalSeeds
	s.handlers["cge.critical_seed"] = s.cgeCriticalSeed
	s.handlers["cge.critical_seed.create"] = s.cgeCriticalSeedCreate
	s.handlers["cge.critical_seed.update"] = s.cgeCriticalSeedUpdate
	s.handlers["cge.critical_seed.delete"] = s.cgeCriticalSeedDelete
	s.handlers["cge.learned_behavior.update"] = s.cgeLearnedBehaviorUpdate
	s.handlers["cge.learned_behavior.delete"] = s.cgeLearnedBehaviorDelete
	s.handlers["cge.learned_behavior.action"] = s.cgeLearnedBehaviorAction
	s.handlers["cge.danger_assessments"] = s.dangerAssessments
	s.handlers["cge.danger_assessment"] = s.dangerAssessment
	s.handlers["topology.snapshot"] = s.topologyConfigSnapshot
	s.handlers["topology.replace"] = s.topologyConfigReplace
	s.handlers["topology.delete"] = s.topologyConfigDelete
	s.handlers["topology.reset"] = s.topologyConfigReplace
	s.handlers["core.snapshot"] = s.legacySnapshot
	s.handlers["core.snapshot.apply"] = s.applySnapshot
}

func (s *Server) notifyMutation(kind string, id string) {
	if s != nil && s.notify != nil {
		s.notify(kind, id)
	}
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

func (s *Server) validationsList(_ contract.Message) (any, error) {
	return sortedValidations(s.state.ValidationsList()), nil
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
	validation.UpdatedAt = now
	if err := s.applyValidationGuidance(validation); err != nil {
		return nil, err
	}
	s.applyValidationStateCorrection(validation)
	if err := s.state.SaveValidation(validation); err != nil {
		return nil, err
	}
	s.notifyMutation("validation.updated", validation.ID)

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

func (s *Server) legacySnapshot(_ contract.Message) (any, error) {
	return s.snapshot.LegacySnapshot(), nil
}

func (s *Server) applySnapshot(_ contract.Message) (any, error) {
	return map[string]any{"ok": true}, nil
}

func decodePayload(raw []byte, out interface{}) error {
	if len(raw) == 0 {
		return contract.NewAPIError(contract.ErrorInvalidJSON, "payload is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return contract.NewAPIError(contract.ErrorInvalidJSON, "invalid JSON: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return contract.NewAPIError(contract.ErrorInvalidJSON, "JSON body must contain one value")
	}
	return nil
}
