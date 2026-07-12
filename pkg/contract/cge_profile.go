package contract

import (
	"math"
	"strings"
	"time"
)

type CgeSecurityMode string

const (
	CgeSecurityRelaxed  CgeSecurityMode = "relaxed"
	CgeSecurityBalanced CgeSecurityMode = "balanced"
	CgeSecurityStrict   CgeSecurityMode = "strict"
	CgeSecurityParanoid CgeSecurityMode = "paranoid"
)

type CgeSecurityProfile struct {
	Mode                                CgeSecurityMode `json:"mode" yaml:"mode"`
	GlobalSensitivity                   float64         `json:"global_sensitivity" yaml:"global_sensitivity"`
	UnknownPersonTolerance              string          `json:"unknown_person_tolerance" yaml:"unknown_person_tolerance"`
	NightSensitivityMultiplier          float64         `json:"night_sensitivity_multiplier" yaml:"night_sensitivity_multiplier"`
	ArmedSensitivityMultiplier          float64         `json:"armed_sensitivity_multiplier" yaml:"armed_sensitivity_multiplier"`
	CriticalRooms                       []string        `json:"critical_rooms" yaml:"critical_rooms"`
	IgnoredMotionRooms                  []string        `json:"ignored_motion_rooms" yaml:"ignored_motion_rooms"`
	MinimumNotifyDangerLevel            DangerLevel     `json:"minimum_notify_danger_level" yaml:"minimum_notify_danger_level"`
	MinimumAutoActionDangerLevel        DangerLevel     `json:"minimum_auto_action_danger_level" yaml:"minimum_auto_action_danger_level"`
	RequireHumanConfirmationForSiren    bool            `json:"require_human_confirmation_for_siren" yaml:"require_human_confirmation_for_siren"`
	AllowAutomaticLights                bool            `json:"allow_automatic_lights" yaml:"allow_automatic_lights"`
	AllowAutomaticRecording             bool            `json:"allow_automatic_recording" yaml:"allow_automatic_recording"`
	AllowAutomaticNotifications         bool            `json:"allow_automatic_notifications" yaml:"allow_automatic_notifications"`
	UnknownPersistenceSeconds           int             `json:"unknown_persistence_seconds" yaml:"unknown_persistence_seconds"`
	SignificantInactivityTimeoutSeconds int             `json:"significant_inactivity_timeout_seconds" yaml:"significant_inactivity_timeout_seconds"`
}

func DefaultCgeSecurityProfile() CgeSecurityProfile {
	return CgeSecurityProfile{
		Mode:                                CgeSecurityBalanced,
		GlobalSensitivity:                   0.5,
		UnknownPersonTolerance:              "medium",
		NightSensitivityMultiplier:          1.3,
		ArmedSensitivityMultiplier:          1.5,
		CriticalRooms:                       []string{},
		IgnoredMotionRooms:                  []string{},
		MinimumNotifyDangerLevel:            DangerMedium,
		MinimumAutoActionDangerLevel:        DangerHigh,
		RequireHumanConfirmationForSiren:    true,
		AllowAutomaticLights:                true,
		AllowAutomaticRecording:             true,
		AllowAutomaticNotifications:         true,
		UnknownPersistenceSeconds:           10,
		SignificantInactivityTimeoutSeconds: 30,
	}
}

// NormalizeCgeSecurityProfile returns a safe, JSON-serializable CGE profile.
// In particular, all profile slices are always non-nil so they serialize as
// [] rather than null.
func NormalizeCgeSecurityProfile(profile CgeSecurityProfile) CgeSecurityProfile {
	if profile.Mode == "" {
		defaults := DefaultCgeSecurityProfile()
		profile.Mode = defaults.Mode
		if profile.GlobalSensitivity == 0 {
			profile.GlobalSensitivity = defaults.GlobalSensitivity
		}
		if profile.NightSensitivityMultiplier == 0 {
			profile.NightSensitivityMultiplier = defaults.NightSensitivityMultiplier
		}
		if profile.ArmedSensitivityMultiplier == 0 {
			profile.ArmedSensitivityMultiplier = defaults.ArmedSensitivityMultiplier
		}
		if profile.UnknownPersonTolerance == "" {
			profile.UnknownPersonTolerance = defaults.UnknownPersonTolerance
		}
		if profile.MinimumNotifyDangerLevel == "" {
			profile.MinimumNotifyDangerLevel = defaults.MinimumNotifyDangerLevel
		}
		if profile.MinimumAutoActionDangerLevel == "" {
			profile.MinimumAutoActionDangerLevel = defaults.MinimumAutoActionDangerLevel
		}
		if profile.UnknownPersistenceSeconds == 0 {
			profile.UnknownPersistenceSeconds = defaults.UnknownPersistenceSeconds
		}
		if profile.SignificantInactivityTimeoutSeconds == 0 {
			profile.SignificantInactivityTimeoutSeconds = defaults.SignificantInactivityTimeoutSeconds
		}
	}

	switch profile.Mode {
	case CgeSecurityRelaxed, CgeSecurityBalanced, CgeSecurityStrict, CgeSecurityParanoid:
	default:
		profile.Mode = CgeSecurityBalanced
	}

	if math.IsNaN(profile.GlobalSensitivity) || math.IsInf(profile.GlobalSensitivity, 0) {
		profile.GlobalSensitivity = 0.5
	}
	profile.GlobalSensitivity = clampFloat(profile.GlobalSensitivity, 0, 1)

	if math.IsNaN(profile.NightSensitivityMultiplier) || math.IsInf(profile.NightSensitivityMultiplier, 0) {
		profile.NightSensitivityMultiplier = 1.3
	}
	profile.NightSensitivityMultiplier = clampFloat(profile.NightSensitivityMultiplier, 0.1, 5)

	if math.IsNaN(profile.ArmedSensitivityMultiplier) || math.IsInf(profile.ArmedSensitivityMultiplier, 0) {
		profile.ArmedSensitivityMultiplier = 1.5
	}
	profile.ArmedSensitivityMultiplier = clampFloat(profile.ArmedSensitivityMultiplier, 0.1, 5)

	profile.UnknownPersonTolerance = strings.ToLower(strings.TrimSpace(profile.UnknownPersonTolerance))
	switch profile.UnknownPersonTolerance {
	case "low", "medium", "high":
	default:
		profile.UnknownPersonTolerance = "medium"
	}
	if !validCgeDangerLevel(profile.MinimumNotifyDangerLevel) {
		profile.MinimumNotifyDangerLevel = DangerMedium
	}
	if !validCgeDangerLevel(profile.MinimumAutoActionDangerLevel) {
		profile.MinimumAutoActionDangerLevel = DangerHigh
	}

	profile.UnknownPersistenceSeconds = clampIntWithDefault(profile.UnknownPersistenceSeconds, 1, 86400, 10)
	profile.SignificantInactivityTimeoutSeconds = clampIntWithDefault(profile.SignificantInactivityTimeoutSeconds, 1, 86400, 30)

	if profile.CriticalRooms == nil {
		profile.CriticalRooms = []string{}
	}
	if profile.IgnoredMotionRooms == nil {
		profile.IgnoredMotionRooms = []string{}
	}
	return profile
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func clampIntWithDefault(value, minimum, maximum, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func validCgeDangerLevel(level DangerLevel) bool {
	switch level {
	case DangerNone, DangerLow, DangerMedium, DangerHigh, DangerCritical:
		return true
	default:
		return false
	}
}

type CgeCorrectionType string

const (
	CgeCorrectionFalsePositive CgeCorrectionType = "false_positive"
	CgeCorrectionTooLow        CgeCorrectionType = "too_low"
	CgeCorrectionTooHigh       CgeCorrectionType = "too_high"
	CgeCorrectionWrongState    CgeCorrectionType = "wrong_state"
	CgeCorrectionWrongAction   CgeCorrectionType = "wrong_action"
	CgeCorrectionMarkNormal    CgeCorrectionType = "mark_normal"
	CgeCorrectionMarkCritical  CgeCorrectionType = "mark_critical"
)

type CgeFinalOutcome string

const (
	CgeOutcomeNormal        CgeFinalOutcome = "normal"
	CgeOutcomeFalsePositive CgeFinalOutcome = "false_positive"
	CgeOutcomeRealIncident  CgeFinalOutcome = "real_incident"
	CgeOutcomeUncertain     CgeFinalOutcome = "uncertain"
)

type CgeEvaluationFeedback struct {
	ID                   string            `json:"id"`
	ChainID              string            `json:"chain_id"`
	EventID              string            `json:"event_id"`
	EvaluationIndex      int               `json:"evaluation_index"`
	CorrectionType       CgeCorrectionType `json:"correction_type"`
	CorrectedState       string            `json:"corrected_state,omitempty"`
	CorrectedDangerLevel DangerLevel       `json:"corrected_danger_level,omitempty"`
	PreferredActions     []string          `json:"preferred_actions,omitempty"`
	Note                 string            `json:"note,omitempty"`
	CreatedBy            string            `json:"created_by"`
	CreatedAt            time.Time         `json:"created_at"`
}

type CgeChainFeedback struct {
	ID                         string          `json:"id"`
	ChainID                    string          `json:"chain_id"`
	FinalOutcome               CgeFinalOutcome `json:"final_outcome"`
	CorrectedFinalDangerLevel  DangerLevel     `json:"corrected_final_danger_level,omitempty"`
	ApplyToSimilarFutureChains bool            `json:"apply_to_similar_future_chains"`
	Note                       string          `json:"note,omitempty"`
	CreatedBy                  string          `json:"created_by"`
	CreatedAt                  time.Time       `json:"created_at"`
}
