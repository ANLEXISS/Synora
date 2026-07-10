package danger

import (
	"fmt"
	"math"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Context struct {
	NodeID            string
	DeviceID          string
	DeviceRole        string
	TimeBucket        string
	HomeMode          string
	ResidentsPresent  int
	RepetitionCount   int
	SequenceNovelty   bool
	SequenceSignature string
	Simulated         bool
	DryRun            bool
	DecisionReasons   []string
	DecisionEvidence  []string
	Now               time.Time
}

type Sequence struct {
	Signature      string
	EventTypes     []string
	Count          int
	SimulatedCount int
}

type scoreResult struct {
	level            int
	score            float64
	category         string
	title            string
	explanation      string
	reasons          []string
	validation       bool
	validationReason string
}

func AssessEvent(event *contract.Event, context Context) contract.DangerAssessment {
	now := context.Now
	if now.IsZero() && event != nil {
		now = event.Timestamp
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if context.TimeBucket == "" {
		context.TimeBucket = TimeBucket(now)
	}
	if event != nil {
		if context.NodeID == "" {
			context.NodeID = event.NodeID
		}
		if context.DeviceID == "" {
			context.DeviceID = event.DeviceID
		}
		if context.Simulated == false {
			context.Simulated = metadataBool(event.Payload, "simulated")
		}
		if context.DryRun == false {
			context.DryRun = metadataBool(event.Payload, "dry_run")
		}
	}

	result := ComputeDangerScore(event, context)
	evidence := evidenceFor(event, context)
	actions := RecommendedSystemActions(result.level, result.category, result.validation, context, result.reasons)
	metadata := eventMetadata(event)

	return contract.DangerAssessment{
		ID:                       idgen.New("danger"),
		EventID:                  eventID(event),
		EventType:                eventType(event),
		SequenceSignature:        context.SequenceSignature,
		TestRunID:                metadataString(metadata, "test_run_id"),
		ScenarioID:               metadataString(metadata, "scenario_id"),
		ScenarioStepID:           metadataString(metadata, "scenario_step_id"),
		EventInstanceID:          metadataString(metadata, "event_instance_id"),
		Level:                    result.level,
		Score:                    roundScore(result.score),
		Category:                 result.category,
		Title:                    result.title,
		Explanation:              result.explanation,
		Reasons:                  uniqueStrings(append(result.reasons, context.DecisionReasons...)),
		Evidence:                 uniqueStrings(append(evidence, context.DecisionEvidence...)),
		RecommendedSystemActions: actions,
		ValidationRequired:       result.validation,
		ValidationReason:         result.validationReason,
		CreatedAt:                now.UTC(),
		Simulated:                context.Simulated,
	}
}

func AssessSequence(sequence Sequence, context Context) contract.DangerAssessment {
	now := context.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	eventType := ""
	if len(sequence.EventTypes) > 0 {
		eventType = sequence.EventTypes[len(sequence.EventTypes)-1]
	}
	event := &contract.Event{
		ID:        sequence.Signature,
		Type:      eventType,
		Timestamp: now,
		NodeID:    context.NodeID,
		DeviceID:  context.DeviceID,
		Payload: map[string]any{
			"metadata": map[string]any{"simulated": sequence.SimulatedCount > 0 && sequence.SimulatedCount >= sequence.Count},
		},
	}
	context.SequenceSignature = sequence.Signature
	context.RepetitionCount = maxInt(context.RepetitionCount, sequence.Count)
	assessment := AssessEvent(event, context)
	assessment.EventID = ""
	assessment.SequenceSignature = sequence.Signature
	return assessment
}

func ComputeDangerScore(event *contract.Event, context Context) scoreResult {
	eventType := contract.EventSystemUnknown
	if event != nil {
		eventType = contract.NormalizeEventType(event.Type)
	}
	nodeKind := zoneCriticality(context.NodeID, context.DeviceRole)
	night := context.TimeBucket == "night"
	confidence := eventConfidence(event)
	repetition := context.RepetitionCount

	result := scoreResult{
		level:       0,
		score:       0.10,
		category:    contract.DangerCategoryActivity,
		title:       "Informational event",
		explanation: "The event does not indicate danger.",
		reasons:     []string{"informational_event"},
	}

	switch eventType {
	case contract.EventVisionIdentity:
		result = scoreResult{
			level:       1,
			score:       0.24,
			category:    contract.DangerCategoryActivity,
			title:       "Known resident activity",
			explanation: "A known resident was recognized in a coherent location.",
			reasons:     []string{"known_resident", "presence_update"},
		}
		if night && nodeKind == "sensitive" {
			result.level = 2
			result.score = 0.41
			result.reasons = append(result.reasons, "unusual_time")
			result.explanation = "A known resident was recognized at night in a sensitive area."
		}
		if context.SequenceNovelty {
			result.level = maxInt(result.level, 2)
			result.score = math.Max(result.score, 0.45)
			result.reasons = append(result.reasons, "sequence_novelty")
			result.validation = true
			result.validationReason = "identity_movement_incoherence"
		}
	case contract.EventVisionMotion:
		result = scoreResult{
			level:       0,
			score:       0.12,
			category:    contract.DangerCategoryActivity,
			title:       "Motion observed",
			explanation: "Motion was observed without a correlated high-risk signal.",
			reasons:     []string{"motion_only"},
		}
		if repetition >= 3 && (nodeKind == "entry" || nodeKind == "sensitive") {
			result.level = 2
			result.score = 0.43
			result.reasons = append(result.reasons, "repeated_motion_sensitive_zone")
		}
	case contract.EventVisionUncertain:
		result = scoreResult{
			level:            2,
			score:            0.46,
			category:         contract.DangerCategoryIdentity,
			title:            "Uncertain identity",
			explanation:      "The identity signal is ambiguous and should be kept as evidence.",
			reasons:          []string{"uncertain_identity"},
			validation:       confidence > 0 && confidence < 0.50,
			validationReason: "low_confidence_identity",
		}
		if nodeKind == "entry" || context.DeviceRole == "access_control" {
			result.level = 3
			result.score = 0.61
			result.category = contract.DangerCategorySecurity
			result.reasons = append(result.reasons, "access_control_zone")
			result.validation = true
			if result.validationReason == "" {
				result.validationReason = "uncertain_identity_at_access_point"
			}
		}
	case contract.EventVisionUnknown:
		result = scoreResult{
			level:            2,
			score:            0.48,
			category:         contract.DangerCategoryIdentity,
			title:            "Unknown presence",
			explanation:      "A person or subject was detected without a known identity.",
			reasons:          []string{"unknown_identity"},
			validation:       false,
			validationReason: "",
		}
		if nodeKind == "entry" || context.DeviceRole == "access_control" {
			result.level = 3
			result.score = 0.62
			result.category = contract.DangerCategorySecurity
			result.title = "Unknown presence at entrance"
			result.explanation = "An unknown subject was detected near an access-control area."
			result.reasons = append(result.reasons, "access_control_zone")
			result.validation = true
			result.validationReason = "unknown_at_access_point"
		}
		if night && (nodeKind == "entry" || nodeKind == "sensitive") {
			result.level = 4
			result.score = 0.82
			result.category = contract.DangerCategorySecurity
			result.title = "Unknown presence at night"
			result.explanation = "An unknown subject was detected at night in a sensitive or access-control area."
			result.reasons = append(result.reasons, "night")
			result.validation = true
			result.validationReason = "unknown_at_night"
		}
		if repetition >= 2 {
			result.score = math.Min(0.88, result.score+0.08)
			result.reasons = append(result.reasons, "repeated_unknown_signal")
			if result.level < 3 {
				result.level = 3
			}
			if result.level == 3 && repetition >= 3 {
				result.level = 4
				result.score = math.Max(result.score, 0.76)
			}
			result.validation = true
			if result.validationReason == "" {
				result.validationReason = "repeated_unknown_presence"
			}
		}
	case contract.EventVisionWeapon:
		result = scoreResult{
			level:            5,
			score:            0.95,
			category:         contract.DangerCategorySecurity,
			title:            "Weapon detected",
			explanation:      "A weapon-like object was detected and must be treated as a critical security situation.",
			reasons:          []string{"weapon_detected"},
			validation:       true,
			validationReason: "weapon_detected",
		}
	case contract.EventVisionFall:
		result = scoreResult{
			level:            5,
			score:            0.92,
			category:         contract.DangerCategoryMedicalEmergency,
			title:            "Fall detected",
			explanation:      "A fall was detected. This is treated as a medical emergency, not an intrusion.",
			reasons:          []string{"fall_detected"},
			validation:       true,
			validationReason: "fall_detected",
		}
	case contract.EventVisionTamper:
		result = scoreResult{
			level:            4,
			score:            0.81,
			category:         contract.DangerCategorySecurity,
			title:            "Camera tamper detected",
			explanation:      "Camera tampering can hide activity in the monitored area.",
			reasons:          []string{"camera_tamper"},
			validation:       true,
			validationReason: "camera_tamper",
		}
	case contract.EventDiscoveryCameraOffline, contract.EventDeviceOffline:
		result = scoreResult{
			level:       2,
			score:       0.42,
			category:    contract.DangerCategoryDeviceHealth,
			title:       "Device offline",
			explanation: "A device or camera became unavailable.",
			reasons:     []string{"device_offline"},
		}
		if nodeKind == "entry" || context.DeviceRole == "access_control" {
			result.level = 3
			result.score = 0.64
			result.category = contract.DangerCategorySecurity
			result.title = "Camera offline in access area"
			result.explanation = "A camera covering an access-control area became unavailable."
			result.reasons = append(result.reasons, "access_control_zone")
			result.validation = true
			result.validationReason = "camera_offline_access_control"
		}
		if night && result.level >= 3 {
			result.level = 4
			result.score = 0.79
			result.reasons = append(result.reasons, "night")
		}
	case contract.EventDiscoveryWorkerCrashed, contract.EventDiscoveryWorkerStopped:
		result = scoreResult{
			level:       1,
			score:       0.20,
			category:    contract.DangerCategorySystemHealth,
			title:       "Worker health degraded",
			explanation: "A runtime worker reported a degraded or crashed state. This is system health information.",
			reasons:     []string{"worker_health_degraded"},
		}
	case contract.EventDiscoveryWorkerStarted, contract.EventDiscoveryCameraOnline:
		result = scoreResult{
			level:       0,
			score:       0.08,
			category:    contract.DangerCategorySystemHealth,
			title:       "Runtime component online",
			explanation: "A runtime component became available.",
			reasons:     []string{"component_online"},
		}
	case contract.EventActionResult:
		result = scoreResult{
			level:       0,
			score:       0.10,
			category:    contract.DangerCategorySystemHealth,
			title:       "Action result received",
			explanation: "An action result was stored for traceability.",
			reasons:     []string{"action_result"},
		}
	}

	if isSystemHealthDiagnostic(eventType) {
		result = scoreResult{
			level:       1,
			score:       0.20,
			category:    contract.DangerCategorySystemHealth,
			title:       "System health diagnostic",
			explanation: "A runtime or network diagnostic event was reported.",
			reasons:     []string{"system_health_diagnostic"},
		}
	}

	if context.Simulated && result.category != contract.DangerCategorySystemHealth {
		result.reasons = append(result.reasons, "simulated_input")
	}
	if result.category == contract.DangerCategorySystemHealth {
		result.validation = false
		result.validationReason = ""
	}
	return result
}

func isSystemHealthDiagnostic(eventType string) bool {
	value := strings.ToLower(strings.TrimSpace(eventType))
	if value == "" {
		return false
	}
	return strings.Contains(value, "hostapd") ||
		strings.Contains(value, "dnsmasq") ||
		strings.Contains(value, "worker.crashed") ||
		strings.Contains(value, "worker.stopped") ||
		strings.Contains(value, "degraded")
}

func RecommendedSystemActions(level int, category string, validation bool, context Context, reasons []string) []contract.SystemActionRecommendation {
	add := func(out *[]contract.SystemActionRecommendation, actionType string, priority int, reason string) {
		*out = append(*out, contract.SystemActionRecommendation{
			Type:      actionType,
			Priority:  priority,
			Reason:    reason,
			Target:    context.NodeID,
			DryRun:    context.DryRun,
			Simulated: context.Simulated,
		})
	}

	out := make([]contract.SystemActionRecommendation, 0, 6)
	reason := firstNonEmpty(strings.Join(reasons, ","), "danger_assessment")
	switch {
	case category == contract.DangerCategorySystemHealth:
		add(&out, contract.SystemActionStoreEvent, contract.PriorityLow, reason)
		add(&out, contract.SystemActionSuppressNoise, contract.PriorityLow, "system_health_diagnostic")
	case level <= 0:
		add(&out, contract.SystemActionObserve, contract.PriorityLow, reason)
		add(&out, contract.SystemActionStoreEvent, contract.PriorityLow, reason)
	case level == 1:
		add(&out, contract.SystemActionUpdatePresence, contract.PriorityNormal, reason)
		add(&out, contract.SystemActionStoreEvent, contract.PriorityLow, reason)
		add(&out, contract.SystemActionLearnSequence, contract.PriorityLow, reason)
	case level == 2:
		add(&out, contract.SystemActionObserve, contract.PriorityNormal, reason)
		add(&out, contract.SystemActionStoreEvidence, contract.PriorityNormal, reason)
		if validation {
			add(&out, contract.SystemActionCreateValidation, contract.PriorityNormal, "validation_possible")
		}
	case level == 3:
		add(&out, contract.SystemActionStoreEvidence, contract.PriorityHigh, reason)
		if validation {
			add(&out, contract.SystemActionCreateValidation, contract.PriorityHigh, "validation_required")
		}
		add(&out, contract.SystemActionRecordClipIfAvailable, contract.PriorityHigh, reason)
		add(&out, contract.SystemActionMarkSuspicious, contract.PriorityHigh, reason)
	case level == 4:
		add(&out, contract.SystemActionCreateAlert, contract.PriorityHigh, reason)
		if validation {
			add(&out, contract.SystemActionCreateValidation, contract.PriorityHigh, "validation_required")
		}
		add(&out, contract.SystemActionRecordClipIfAvailable, contract.PriorityHigh, reason)
		add(&out, contract.SystemActionMarkIntrusionCandidate, contract.PriorityHigh, reason)
		add(&out, contract.SystemActionIncreaseRetention, contract.PriorityHigh, reason)
	default:
		add(&out, contract.SystemActionCreateAlert, contract.PriorityCritical, reason)
		if category == contract.DangerCategoryMedicalEmergency {
			add(&out, contract.SystemActionSetEmergencyState, contract.PriorityCritical, reason)
		} else {
			add(&out, contract.SystemActionSetIntrusionState, contract.PriorityCritical, reason)
		}
		add(&out, contract.SystemActionRecordClipIfAvailable, contract.PriorityCritical, reason)
		add(&out, contract.SystemActionLockEvidence, contract.PriorityCritical, reason)
		if validation {
			add(&out, contract.SystemActionCreateValidation, contract.PriorityCritical, "immediate_validation_required")
		}
	}
	return out
}

func TimeBucket(at time.Time) string {
	hour := at.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 18:
		return "day"
	case hour >= 18 && hour < 22:
		return "evening"
	default:
		return "night"
	}
}

func zoneCriticality(nodeID string, role string) string {
	value := strings.ToLower(strings.TrimSpace(nodeID + " " + role))
	switch {
	case strings.Contains(value, "access_control"), strings.Contains(value, "entry"), strings.Contains(value, "entree"), strings.Contains(value, "entrance"):
		return "entry"
	case strings.Contains(value, "chambre"), strings.Contains(value, "bedroom"), strings.Contains(value, "child_room"):
		return "sensitive"
	case strings.Contains(value, "salon"), strings.Contains(value, "couloir"), strings.Contains(value, "hall"), strings.Contains(value, "living"):
		return "medium"
	case strings.Contains(value, "tech"), strings.Contains(value, "system"):
		return "diagnostic"
	default:
		return "normal"
	}
}

func evidenceFor(event *contract.Event, context Context) []string {
	out := []string{}
	if event != nil {
		if event.ID != "" {
			out = append(out, "event:"+event.ID)
		}
		if event.Type != "" {
			out = append(out, "event_type:"+contract.NormalizeEventType(event.Type))
		}
		if event.DeviceID != "" {
			out = append(out, "device:"+event.DeviceID)
		}
		if event.NodeID != "" {
			out = append(out, "node:"+event.NodeID)
		}
		if event.Confidence > 0 {
			out = append(out, fmt.Sprintf("confidence:%.2f", event.Confidence))
		}
	}
	if context.TimeBucket != "" {
		out = append(out, "time_bucket:"+context.TimeBucket)
	}
	if context.RepetitionCount > 1 {
		out = append(out, fmt.Sprintf("repetition_count:%d", context.RepetitionCount))
	}
	return out
}

func eventID(event *contract.Event) string {
	if event == nil {
		return ""
	}
	return strings.TrimSpace(event.ID)
}

func eventType(event *contract.Event) string {
	if event == nil {
		return ""
	}
	return contract.NormalizeEventType(event.Type)
}

func eventConfidence(event *contract.Event) float64 {
	if event == nil {
		return 0
	}
	if event.Confidence > 0 {
		return event.Confidence
	}
	if event.Payload == nil {
		return 0
	}
	switch value := event.Payload["confidence"].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	default:
		return 0
	}
}

func metadataBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	if boolValue(payload[key]) {
		return true
	}
	metadata, _ := payload["metadata"].(map[string]any)
	return boolValue(metadata[key])
}

func eventMetadata(event *contract.Event) map[string]any {
	if event == nil || event.Payload == nil {
		return nil
	}
	metadata, _ := event.Payload["metadata"].(map[string]any)
	return metadata
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	switch typed := metadata[key].(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func roundScore(value float64) float64 {
	return math.Round(value*100) / 100
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
