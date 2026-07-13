package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"synora/internal/automation"
	"synora/internal/cge"
	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/idgen"
	"synora/internal/manager"
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
	chains     *event.ChainManager
	devices    *device.Registry
	automation *automation.Engine
	snapshot   *snapshot.Builder
	metrics    Metrics

	topologyPath   string
	residentsPath  string
	devicePath     string
	automationPath string
	securityPath   string
	cgeProfile     *cge.ProfileStore
	cgeFeedback    *cge.FeedbackStore

	publishEvent   func(string, any, int)
	updateTopology func(*topology.Topology)
	pairing        *security.PairingService
	cge            any
	notify         func(string, string)
	configMu       sync.Mutex
	startedAt      time.Time
	ingestEvent    func(*contract.Event)

	handlers map[string]Handler
}

type Config struct {
	Bus            Sender
	State          *state.Store
	Events         *event.Store
	Chains         *event.ChainManager
	Devices        *device.Registry
	Automation     *automation.Engine
	Snapshot       *snapshot.Builder
	Metrics        Metrics
	TopologyPath   string
	ResidentsPath  string
	DevicePath     string
	AutomationPath string
	SecurityPath   string
	CGEProfile     *cge.ProfileStore
	CGEFeedback    *cge.FeedbackStore
	PublishEvent   func(string, any, int)
	UpdateTopology func(*topology.Topology)
	CGE            any
	NotifyMutation func(string, string)
	IngestEvent    func(*contract.Event)
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
		chains:         cfg.Chains,
		devices:        cfg.Devices,
		automation:     cfg.Automation,
		snapshot:       cfg.Snapshot,
		metrics:        cfg.Metrics,
		topologyPath:   cfg.TopologyPath,
		residentsPath:  cfg.ResidentsPath,
		devicePath:     cfg.DevicePath,
		automationPath: cfg.AutomationPath,
		securityPath:   cfg.SecurityPath,
		cgeProfile:     cfg.CGEProfile,
		cgeFeedback:    cfg.CGEFeedback,
		publishEvent:   cfg.PublishEvent,
		updateTopology: cfg.UpdateTopology,
		pairing:        &security.PairingService{Path: cfg.SecurityPath},
		cge:            cfg.CGE,
		notify:         cfg.NotifyMutation,
		ingestEvent:    cfg.IngestEvent,
		handlers:       map[string]Handler{},
		startedAt:      time.Now().UTC(),
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
	s.handlers["event.chains"] = s.eventChains
	s.handlers["event.chain"] = s.eventChain
	s.handlers["cge.critical_chains"] = s.criticalChains
	s.handlers["cge.critical_chain"] = s.criticalChain
	s.handlers["cge.security_profile"] = s.cgeSecurityProfile
	s.handlers["cge.security_profile.update"] = s.cgeSecurityProfileUpdate
	s.handlers["cge.feedback.list"] = s.cgeFeedbackList
	s.handlers["cge.feedback.evaluation"] = s.cgeFeedbackEvaluation
	s.handlers["cge.feedback.chain"] = s.cgeFeedbackChain
	s.handlers["system.health"] = s.systemHealth
	s.handlers["system.reset_intrusion"] = s.systemResetIntrusion
	s.handlers[contract.RPCSystemResetState] = s.systemResetState
	s.handlers[contract.RPCManualRisk] = s.manualRisk
	s.handlers[contract.RPCManualRiskClear] = s.manualRiskClear
	s.handlers[contract.RPCSecurityMode] = s.securityMode
	s.handlers[contract.RPCSecurityModeUpdate] = s.securityModeUpdate
	s.handlers[contract.RPCSecurityArm] = s.securityArm
	s.handlers[contract.RPCSecurityDisarm] = s.securityDisarm
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
	items := s.events.List()
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		views = append(views, map[string]any{
			"id": item.ID, "type": item.Type, "source": item.Source, "timestamp": item.Timestamp,
			"device_id": item.DeviceID, "node_id": item.NodeID, "identity": item.Identity,
			"confidence": item.Confidence, "priority": item.Priority, "group_key": item.GroupKey,
			"track_id": item.TrackID, "clip_id": item.ClipID, "clip_index": item.ClipIndex,
			"activation_id": item.ActivationID, "sequence_key": item.SequenceKey,
			"payload": sanitizeConfigurationMap(item.Payload),
		})
	}
	return views, nil
}

func (s *Server) eventChains(msg contract.Message) (any, error) {
	if s.chains == nil {
		return map[string]any{"chains": []any{}, "generated_at": time.Now().UTC()}, nil
	}
	var request struct {
		Status    string `json:"status"`
		Limit     int    `json:"limit"`
		Since     string `json:"since"`
		Severity  string `json:"severity"`
		Simulated *bool  `json:"simulated"`
	}
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &request); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid event chain filter")
		}
	}
	var since time.Time
	if strings.TrimSpace(request.Since) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(request.Since))
		if err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid event chain since")
		}
		since = parsed
	}
	return map[string]any{
		"chains":       s.chains.List(event.ChainFilter{Status: request.Status, Limit: request.Limit, Since: since, Severity: request.Severity, Simulated: request.Simulated}),
		"generated_at": time.Now().UTC(),
	}, nil
}

func (s *Server) eventChain(msg contract.Message) (any, error) {
	if s.chains == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "event chain not found")
	}
	var request DeletePayload
	if err := json.Unmarshal(msg.Payload, &request); err != nil || strings.TrimSpace(request.ID) == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "event chain id is required")
	}
	chain, ok := s.chains.Get(request.ID)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "event chain not found")
	}
	return chain, nil
}

func (s *Server) criticalChains(msg contract.Message) (any, error) {
	if s.chains == nil {
		return []any{}, nil
	}
	limit := 50
	if len(msg.Payload) > 0 {
		var request struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(msg.Payload, &request)
		if request.Limit > 0 {
			limit = request.Limit
		}
	}
	return s.chains.CriticalMemories(limit), nil
}

func (s *Server) criticalChain(msg contract.Message) (any, error) {
	var request DeletePayload
	if err := json.Unmarshal(msg.Payload, &request); err != nil || strings.TrimSpace(request.ID) == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "critical chain id is required")
	}
	if s.chains == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "critical chain memory not found")
	}
	item, ok := s.chains.CriticalMemory(request.ID)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "critical chain memory not found")
	}
	return item, nil
}

func (s *Server) systemHealth(_ contract.Message) (any, error) {
	if requester, ok := s.bus.(Requester); ok {
		var response *contract.Message
		var err error
		if bounded, supportsBounded := s.bus.(interface {
			RequestWithTimeout(string, string, []byte, string, time.Duration) (*contract.Message, error)
		}); supportsBounded {
			response, err = bounded.RequestWithTimeout(contract.RPCRuntimeHealth, "core", nil, "runtime-manager", 400*time.Millisecond)
		} else {
			response, err = requester.Request(contract.RPCRuntimeHealth, "core", nil, "runtime-manager")
		}

		if err == nil && response != nil && len(response.Payload) > 0 {
			var health contract.RuntimeHealth
			if decodeErr := json.Unmarshal(
				response.Payload,
				&health,
			); decodeErr == nil {
				return s.mergeStateRuntimeHealth(health), nil
			}
		}
	}

	// Keep health useful when the optional runtime-manager process is not
	// running. The same manager implementation is used locally as a bounded
	// fallback, so active systemd services are not downgraded to synthetic
	// unavailable records merely because the RPC probe failed.
	localHealth := manager.New(manager.Config{}).Health(context.Background())
	return s.mergeStateRuntimeHealth(localHealth), nil
}

func (s *Server) mergeStateRuntimeHealth(health contract.RuntimeHealth) contract.RuntimeHealth {
	now := time.Now().UTC()
	if s != nil && s.state != nil {
		current := s.state.SystemState()
		return contract.MergeRuntimeComponentStatusDetailed(
			health,
			current.RuntimeComponents,
			current.RuntimeComponentInfo,
			current.RuntimeModels,
			now,
		)
	}
	return contract.NormalizeRuntimeHealth(health, now)
}

func (s *Server) systemResetIntrusion(msg contract.Message) (any, error) {
	var request contract.SystemStateResetRequest
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &request); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid intrusion reset payload")
		}
	}
	request.TargetState = "idle"
	if request.Reason == "" {
		request.Reason = "manual_intrusion_reset"
	}
	return s.resetSystemState(request)
}

func (s *Server) systemResetState(msg contract.Message) (any, error) {
	var request contract.SystemStateResetRequest
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &request); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid state reset payload")
		}
	}
	if request.TargetState == "" {
		request.TargetState = "idle"
	}
	if request.Reason == "" {
		request.Reason = "manual_admin_reset"
	}
	if request.TargetState != "idle" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "only idle state reset is supported")
	}
	return s.resetSystemState(request)
}

func (s *Server) resetSystemState(request contract.SystemStateResetRequest) (any, error) {
	current := s.state.SystemState()
	current.LastState = "idle"
	current.LastStateTime = time.Now().UTC()
	current.DangerLevel = string(contract.DangerNone)
	current.DangerScore = 0
	current.DangerKnown = true
	current.DangerSource = "manual"
	current.ManualRiskActive = false
	current.ManualRiskTest = false
	current.ManualRiskLevel = ""
	current.ManualRiskScore = 0
	current.ManualRiskExpiresAt = time.Time{}
	current.BlockingReasons = []string{}
	current.IntrusionActive = false
	current.IntrusionTime = time.Time{}
	current.EmergencyActive = false
	current.EmergencyTime = time.Time{}
	s.state.SetSystemState(current)
	if s.publishEvent != nil {
		s.publishEvent(contract.EventSystemStateReset, map[string]any{
			"manual": true, "reason": request.Reason, "created_by": request.CreatedBy,
			"target_state": request.TargetState,
		}, contract.PriorityHigh)
		s.publishEvent(contract.EventSystemStateChanged, current, contract.PriorityHigh)
	}
	return map[string]any{"status": "ok", "target_state": "idle", "reason": request.Reason}, nil
}

func (s *Server) manualRisk(msg contract.Message) (any, error) {
	var request contract.ManualRiskRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid manual risk payload")
	}
	request.DangerLevel = strings.ToLower(strings.TrimSpace(request.DangerLevel))
	switch request.DangerLevel {
	case "low", "medium", "high", "critical":
	default:
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "danger_level must be low, medium, high or critical")
	}
	if request.DurationSeconds <= 0 {
		request.DurationSeconds = 60
	}
	if request.DurationSeconds > 3600 {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "duration_seconds exceeds one hour")
	}
	if strings.TrimSpace(request.Reason) == "" {
		request.Reason = "manual risk test"
	}
	now := time.Now().UTC()
	eventID := idgen.New("evt")
	expiresAt := now.Add(time.Duration(request.DurationSeconds) * time.Second)
	payload := map[string]any{
		"manual": true, "danger_level": request.DangerLevel, "duration_seconds": request.DurationSeconds,
		"reason": request.Reason, "test": request.Test, "created_by": request.CreatedBy, "timestamp": now,
		"event_id":   eventID,
		"expires_at": expiresAt,
	}
	if request.Test {
		payload["metadata"] = map[string]any{"simulated": true, "dry_run": true, "manual_test": true}
	}
	if s.publishEvent != nil {
		s.publishEvent(contract.EventManualRisk, payload, contract.PriorityHigh)
	}
	if s.ingestEvent != nil {
		s.ingestEvent(&contract.Event{
			ID:        eventID,
			Type:      contract.EventManualRisk,
			Source:    "admin",
			Timestamp: now,
			Payload:   payload,
			Priority:  contract.PriorityHigh,
		})
	}
	return map[string]any{"status": "queued", "event_type": contract.EventManualRisk, "event_id": eventID, "danger_level": request.DangerLevel, "test": request.Test, "expires_at": expiresAt}, nil
}

func (s *Server) manualRiskClear(msg contract.Message) (any, error) {
	var request struct {
		Reason string `json:"reason"`
	}
	if len(msg.Payload) > 0 && json.Unmarshal(msg.Payload, &request) != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid manual risk clear payload")
	}
	current := s.state.SystemState()
	current.ManualRiskActive = false
	current.ManualRiskTest = false
	current.ManualRiskLevel = ""
	current.ManualRiskScore = 0
	current.ManualRiskExpiresAt = time.Time{}
	current.LastState = "idle"
	current.LastStateTime = time.Now().UTC()
	current.DangerLevel = string(contract.DangerNone)
	current.DangerScore = 0
	current.DangerKnown = true
	current.DangerSource = "none"
	current.IntrusionActive = false
	s.state.SetSystemState(current)
	if err := s.state.SaveNow(); err != nil {
		return nil, err
	}
	if s.publishEvent != nil {
		s.publishEvent(contract.EventSystemStateChanged, map[string]any{"reason": request.Reason, "manual_risk_cleared": true, "state": current}, contract.PriorityHigh)
	}
	return map[string]any{"status": "cleared", "reason": request.Reason}, nil
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
