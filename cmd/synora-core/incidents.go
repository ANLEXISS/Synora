package main

import (
	"strings"

	"synora/internal/engine"
	"synora/internal/state"
	"synora/internal/stateapply"
	"synora/pkg/contract"
)

func (a *coreApp) recordIncident(event *contract.Event, result *engine.Result) (contract.Incident, bool, bool, error) {
	if a == nil || a.state == nil || event == nil || result == nil || result.System == nil {
		return contract.Incident{}, false, false, nil
	}
	incidentEligible := result.System.LastState == "intrusion" && result.System.IntrusionActive
	if !incidentEligible || eventIsSimulated(event) {
		return contract.Incident{}, false, false, nil
	}
	if result.DangerAssessment != nil && result.DangerAssessment.Simulated {
		return contract.Incident{}, false, false, nil
	}

	identityKind, residentID := a.incidentIdentity(event)
	clipID := strings.TrimSpace(event.ClipID)
	severity := strings.TrimSpace(result.System.DangerLevel)
	score := result.System.DangerScore
	cause := contract.IncidentCause{
		EventType:    event.Type,
		GroupKey:     event.GroupKey,
		SequenceKey:  event.SequenceKey,
		ActivationID: event.ActivationID,
	}
	if result.Decision != nil {
		cause.DecisionType = result.Decision.Type
		cause.DecisionID = result.Decision.ID
		cause.Reason = result.Decision.Reason
		if cause.SequenceKey == "" {
			cause.SequenceKey = result.Decision.SequenceKey
		}
		if score == 0 {
			score = result.Decision.DangerScore
			if score == 0 {
				score = result.Decision.Score
			}
		}
		if severity == "" {
			severity = result.Decision.DangerLevel
		}
	}
	if result.DangerAssessment != nil {
		cause.Contributors = append(cause.Contributors, result.DangerAssessment.Reasons...)
		cause.Evidence = append(cause.Evidence, result.DangerAssessment.Evidence...)
		cause.Reason = incidentFirstNonEmpty(cause.Reason, result.DangerAssessment.Explanation)
		if severity == "" {
			severity = result.DangerAssessment.RiskLevel
		}
		if score == 0 {
			score = result.DangerAssessment.Score
		}
	}

	incident, created, changed, err := a.state.RecordIncident(state.IncidentObservation{
		EventID: event.ID, EventType: event.Type, Timestamp: event.Timestamp,
		CameraID: event.DeviceID, NodeID: event.NodeID,
		IdentityKind: identityKind, ResidentID: residentID, EntityID: a.entityIDForEvent(event), TrackID: event.TrackID,
		ClipID: clipID, SequenceKey: event.SequenceKey, ActivationID: event.ActivationID, GroupKey: event.GroupKey,
		// Incident storage intentionally keeps its existing durable security
		// classification. The Core global state remains the Engine result
		// (suspicious for a first unknown at the access point).
		Score: score, Confidence: event.Confidence, SecurityState: "intrusion", Severity: severity, Cause: cause,
	})
	return incident, created, changed, err
}

func (a *coreApp) incidentIdentity(event *contract.Event) (contract.IncidentIdentityKind, string) {
	if event == nil {
		return contract.IncidentIdentityNone, ""
	}
	identity := strings.TrimSpace(event.ResidentID)
	if identity == "" && a.residents != nil {
		candidate := strings.TrimSpace(event.Identity)
		if resident, ok := a.residents[candidate]; ok && resident != nil {
			identity = candidate
		}
	}
	switch contract.NormalizeEventType(event.Type) {
	case contract.EventVisionUnknown:
		return contract.IncidentIdentityUnknown, ""
	case contract.EventVisionUncertain:
		return contract.IncidentIdentityUncertain, ""
	}
	if identity != "" && a.residents != nil {
		if resident, ok := a.residents[identity]; ok && resident != nil && event.Confidence >= stateapply.ResidentPresenceEnterConfidence {
			return contract.IncidentIdentityResident, identity
		}
	}
	if contract.NormalizeEventType(event.Type) == contract.EventVisionIdentity || identity != "" {
		return contract.IncidentIdentityUncertain, ""
	}
	return contract.IncidentIdentityNone, ""
}

func incidentFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
