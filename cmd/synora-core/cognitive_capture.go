package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
	"synora/internal/engine"
	"synora/pkg/contract"
)

func (a *coreApp) captureBefore(event *contract.Event) (pending *cognitivePending) {
	defer func() {
		if recover() != nil {
			pending = nil
		}
	}()
	if a == nil || a.datasetCapture == nil || event == nil {
		return nil
	}
	values := map[string]any{
		"event":         event,
		"recent_events": a.eventStore.List(),
	}
	frame := a.buildCognitiveStateFrame(event)
	catalog := a.buildCognitiveActionCatalog()
	for _, action := range catalog.Actions {
		if action.Enabled {
			frame.AvailableCapabilities = append(frame.AvailableCapabilities, action.Capability)
			if action.Slot != "" {
				frame.AvailableActionIDs = append(frame.AvailableActionIDs, action.Slot)
			} else {
				frame.AvailableActionIDs = append(frame.AvailableActionIDs, action.ID)
			}
		}
	}
	encoded, encodeErr := (cognitive.V4StateEncoder{}).Encode(context.Background(), frame)
	if encodeErr == nil {
		values["state_frame"] = frame
		values["encoded_state"] = encoded
	}
	values["action_catalog"] = catalog
	if a.snapshotBuilder != nil {
		values["state_before"] = a.snapshotBuilder.StatePayload()
		values["topology"] = a.snapshotBuilder.TopologyTreeViews()
		values["residents"] = a.snapshotBuilder.ResidentViews()
	} else {
		values["state_before"] = map[string]any{}
		values["topology"] = []any{}
		values["residents"] = []any{}
	}
	return &cognitivePending{value: a.datasetCapture.Before(values)}
}

func (a *coreApp) buildCognitiveStateFrame(event *contract.Event) cognitive.StateFrame {
	now := time.Now().UTC()
	frame := cognitive.StateFrame{SchemaVersion: cognitive.StateFrameSchemaVersion, Revision: 1, CapturedAt: now, CurrentEvent: cognitive.StateEventFromContract(event), Devices: []cognitive.StateDevice{}, Topology: []cognitive.StateNode{}, Residents: []cognitive.StateResident{}, RecentEvents: []cognitive.StateEvent{}}
	if a == nil {
		return frame
	}
	if a.state != nil {
		frame.Revision = a.state.Revision()
		system := a.state.SystemState()
		frame.System = cognitive.StateSystem{LastState: system.LastState, DangerLevel: system.DangerLevel, DangerScore: system.DangerScoreCurrent, Armed: system.Armed, Degraded: system.Degraded}
		frame.SecurityMode = "disarmed"
		if system.Armed {
			frame.SecurityMode = "armed_away"
		}
		for _, item := range a.state.RecentEventsList() {
			if observed := cognitive.StateEventFromContract(item); observed != nil {
				frame.RecentEvents = append(frame.RecentEvents, *observed)
			}
		}
	}
	frame.KnownResidentCount = len(a.residents)
	frame.KnownResidentsPresent = false
	if a.device != nil {
		for _, device := range a.device.Ordered() {
			observed := cognitive.StateDevice{ID: device.ID, Type: device.Type, Role: device.Role, NodeID: device.NodeID, Enabled: device.Enabled}
			if a.state != nil {
				if current, ok := a.state.DeviceState(device.ID); ok && current != nil {
					observed.Online = current.Online
				}
			}
			frame.Devices = append(frame.Devices, observed)
		}
	}
	if a.topology != nil {
		ids := make([]string, 0, len(a.topology.Nodes))
		for id := range a.topology.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			node := a.topology.Nodes[id]
			if node == nil {
				continue
			}
			parentID := ""
			if node.Parent != nil {
				parentID = node.Parent.ID
			}
			dynamic := 0.0
			if a.state != nil {
				if current, ok := a.state.NodeState(node.ID); ok && current != nil {
					dynamic = current.DangerScore
				}
			}
			frame.Topology = append(frame.Topology, cognitive.StateNode{ID: node.ID, Type: string(node.Type), ParentID: parentID, ConnectedIDs: append([]string(nil), node.Connect...), DynamicScore: dynamic})
		}
	}
	residentIDs := make([]string, 0, len(a.residents))
	for id := range a.residents {
		residentIDs = append(residentIDs, id)
	}
	sort.Strings(residentIDs)
	for _, id := range residentIDs {
		resident := a.residents[id]
		if resident == nil {
			continue
		}
		observed := cognitive.StateResident{ID: resident.ID, Role: resident.Role, Enabled: resident.Enabled, Trusted: resident.Trusted, State: engine.StateAbsent}
		if a.state != nil {
			if presence, ok := a.state.PresenceState(id); ok && presence != nil {
				observed.State, observed.NodeID, observed.Confidence = presence.State, presence.Location, presence.Confidence
			} else if identity, ok := a.state.Identity(id); ok && identity != nil {
				observed.State, observed.NodeID, observed.Confidence = identity.State, identity.LastNodeID, identity.Confidence
			}
		}
		frame.Residents = append(frame.Residents, observed)
		if observed.Enabled && observed.State != engine.StateAbsent {
			frame.KnownResidentsPresent = true
		}
	}
	frame.Temporal = cognitive.EncoderV4TemporalContext{EvaluationTrigger: "event", EventCount: len(frame.RecentEvents) + 1}
	return frame.Normalized()
}

func (a *coreApp) buildCognitiveActionCatalog() cognitive.ActionCatalog {
	catalog := cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Actions: []cognitive.ActionDescriptor{}}
	if a == nil {
		return catalog
	}
	if a.state != nil {
		catalog.Revision = a.state.Revision()
	}
	if a.automation == nil {
		return catalog
	}
	for _, rule := range a.automation.List() {
		if !rule.Enabled || rule.DeletedAt != nil || strings.TrimSpace(rule.ConfigError) != "" {
			continue
		}
		for index, action := range rule.Actions {
			capability := strings.ToLower(strings.TrimSpace(action.Type))
			if capability == "" {
				capability = "automation"
			}
			parameters := make([]string, 0, len(action.Data))
			for key := range action.Data {
				parameters = append(parameters, key)
			}
			sort.Strings(parameters)
			slot := ""
			if scope, ok := action.Data["scope"].(string); ok && strings.TrimSpace(scope) != "" {
				slot = capability + "@" + strings.TrimSpace(scope)
			}
			catalog.Actions = append(catalog.Actions, cognitive.ActionDescriptor{ID: "automation:" + rule.ID + ":" + fmt.Sprint(index), Slot: slot, Capability: capability, RiskClass: "teacher_gated", Enabled: true, RequiresTeacherApproval: rule.RequiresValidation, ParameterNames: parameters})
		}
	}
	return catalog.Normalized()
}

func (a *coreApp) captureAfter(pending *cognitivePending, event *contract.Event, result *engine.Result, stateChanged bool) {
	defer func() { _ = recover() }()
	if a == nil || a.datasetCapture == nil || pending == nil || pending.value == nil || event == nil {
		return
	}
	teacher := cognitive.TeacherDecision{
		EventID:               event.ID,
		EventType:             event.Type,
		StateChanged:          stateChanged,
		CognitiveActionIssued: false,
	}
	if result != nil {
		if result.Decision != nil {
			teacher.Decision, _ = json.Marshal(result.Decision)
			teacher.InferredState = result.Decision.State
			teacher.DangerLevel = result.Decision.DangerLevel
			teacher.DangerScore = result.Decision.DangerScore
		}
		if result.DangerAssessment != nil {
			if teacher.DangerLevel == "" {
				teacher.DangerLevel = result.DangerAssessment.RiskLevel
			}
			if teacher.DangerScore == 0 {
				teacher.DangerScore = result.DangerAssessment.Score
			}
		}
	}
	if teacher.InferredState == "" && a.state != nil {
		teacher.InferredState = a.state.SystemState().LastState
	}
	a.datasetCapture.After(pending.value, teacher)
}

func (a *coreApp) closeDatasetCapture() {
	if a == nil || a.datasetCapture == nil {
		return
	}
	if err := a.datasetCapture.Close(); err != nil {
		log.Printf("cognitive capture close warning: %v", err)
	}
}

type cognitivePending struct {
	value *cognitive.CapturePending
}
