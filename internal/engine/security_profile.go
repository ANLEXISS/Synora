package engine

import (
	"strings"

	"synora/internal/engine/danger"
	"synora/pkg/contract"
)

type feedbackHint struct {
	eventType        string
	nodeID           string
	correctionType   contract.CgeCorrectionType
	correctedState   string
	correctedLevel   contract.DangerLevel
	preferredActions []string
}

func (e *Engine) SetSecurityProfile(profile *contract.CgeSecurityProfile) {
	if e == nil {
		return
	}
	e.securityProfileMu.Lock()
	if profile == nil {
		e.securityProfile = nil
	} else {
		copy := contract.NormalizeCgeSecurityProfile(*profile)
		copy.CriticalRooms = append([]string{}, copy.CriticalRooms...)
		copy.IgnoredMotionRooms = append([]string{}, copy.IgnoredMotionRooms...)
		e.securityProfile = &copy
	}
	e.securityProfileMu.Unlock()
}

func (e *Engine) SecurityProfile() *contract.CgeSecurityProfile {
	if e == nil {
		return nil
	}
	e.securityProfileMu.RLock()
	defer e.securityProfileMu.RUnlock()
	if e.securityProfile == nil {
		return nil
	}
	copy := contract.NormalizeCgeSecurityProfile(*e.securityProfile)
	copy.CriticalRooms = append([]string{}, copy.CriticalRooms...)
	copy.IgnoredMotionRooms = append([]string{}, copy.IgnoredMotionRooms...)
	return &copy
}

func (e *Engine) AddEvaluationFeedback(feedback contract.CgeEvaluationFeedback, chain *contract.EventChain) error {
	if e == nil || chain == nil {
		return nil
	}
	for _, event := range chain.RecentEvents {
		if event.ID != feedback.EventID {
			continue
		}
		e.feedbackMu.Lock()
		e.feedbackHints = append(e.feedbackHints, feedbackHint{
			eventType:        contract.NormalizeEventType(event.Type),
			nodeID:           event.NodeID,
			correctionType:   feedback.CorrectionType,
			correctedState:   feedback.CorrectedState,
			correctedLevel:   feedback.CorrectedDangerLevel,
			preferredActions: append([]string(nil), feedback.PreferredActions...),
		})
		if len(e.feedbackHints) > 200 {
			e.feedbackHints = e.feedbackHints[len(e.feedbackHints)-200:]
		}
		e.feedbackMu.Unlock()
		return nil
	}
	return nil
}

func (e *Engine) applyFeedbackHint(event *contract.Event, result *Result) {
	if e == nil || event == nil || result == nil {
		return
	}
	e.feedbackMu.RLock()
	var matched *feedbackHint
	for index := len(e.feedbackHints) - 1; index >= 0; index-- {
		hint := e.feedbackHints[index]
		if hint.eventType == contract.NormalizeEventType(event.Type) && (hint.nodeID == "" || hint.nodeID == event.NodeID) {
			copy := hint
			matched = &copy
			break
		}
	}
	e.feedbackMu.RUnlock()
	if matched == nil {
		return
	}
	assessment := result.DangerAssessment
	decision := result.Decision
	if assessment == nil || decision == nil {
		return
	}
	level := assessment.Level
	switch matched.correctionType {
	case contract.CgeCorrectionFalsePositive, contract.CgeCorrectionMarkNormal:
		level = 0
		assessment.Score = 0.05
		assessment.ExpectedState = "idle"
		decision.State = "idle"
	case contract.CgeCorrectionMarkCritical:
		level = 5
		assessment.Score = 0.95
		assessment.ExpectedState = "intrusion"
		decision.State = "intrusion"
	case contract.CgeCorrectionTooLow:
		level = maxInt(level, dangerLevelNumber(matched.correctedLevel))
	case contract.CgeCorrectionTooHigh:
		if matched.correctedLevel != "" {
			level = dangerLevelNumber(matched.correctedLevel)
		} else if level > 0 {
			level--
		}
	case contract.CgeCorrectionWrongState:
		if matched.correctedState != "" {
			decision.State = matched.correctedState
			assessment.ExpectedState = matched.correctedState
		}
	}
	if matched.correctedLevel != "" && matched.correctionType != contract.CgeCorrectionTooHigh && matched.correctionType != contract.CgeCorrectionTooLow {
		level = dangerLevelNumber(matched.correctedLevel)
	}
	assessment.Level = level
	assessment.RiskLevel = dangerLevelName(level)
	decision.Score = assessment.Score
	decision.EffectiveScore = assessment.Score
	assessment.Reasons = appendUnique(assessment.Reasons, "admin_feedback_applied")
	decision.Reason = "admin_feedback_applied"
	for _, action := range matched.preferredActions {
		if strings.TrimSpace(action) == "" {
			continue
		}
		assessment.RecommendedSystemActions = append(assessment.RecommendedSystemActions, contract.SystemActionRecommendation{Type: action, Target: event.NodeID, Reason: "admin_feedback_applied"})
	}
}

func (e *Engine) ConfigureDangerProfile(profile *contract.CgeSecurityProfile, chainsTimeout func(int)) {
	e.SetSecurityProfile(profile)
	if profile != nil && chainsTimeout != nil {
		normalized := contract.NormalizeCgeSecurityProfile(*profile)
		chainsTimeout(normalized.SignificantInactivityTimeoutSeconds)
	}
}

func (e *Engine) dangerProfileContext(event *contract.Event, context *danger.Context) {
	profile := e.SecurityProfile()
	if profile == nil || context == nil {
		return
	}
	context.ProfileEnabled = true
	context.GlobalSensitivity = profile.GlobalSensitivity
	context.NightMultiplier = profile.NightSensitivityMultiplier
	context.ArmedMultiplier = profile.ArmedSensitivityMultiplier
	context.CriticalRoom = contains(profile.CriticalRooms, event.NodeID)
	if contract.NormalizeEventType(event.Type) == contract.EventVisionMotion && contains(profile.IgnoredMotionRooms, event.NodeID) {
		context.GlobalSensitivity = 0
		context.NightMultiplier = 0
		context.CriticalRoom = false
	}
}

func dangerLevelNumber(level contract.DangerLevel) int {
	switch level {
	case contract.DangerCritical:
		return 5
	case contract.DangerHigh:
		return 4
	case contract.DangerMedium:
		return 3
	case contract.DangerLow:
		return 1
	default:
		return 0
	}
}

func dangerLevelName(level int) string {
	switch {
	case level >= 5:
		return string(contract.DangerCritical)
	case level >= 4:
		return string(contract.DangerHigh)
	case level >= 3:
		return string(contract.DangerMedium)
	case level >= 1:
		return string(contract.DangerLow)
	default:
		return string(contract.DangerNone)
	}
}

func contains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target && target != "" {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
