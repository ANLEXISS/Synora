package contract

import (
	"encoding/json"
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

// DangerDecayConfig controls the time-based projection of CGE danger into the
// current system state. Historical events and chain evaluations are never
// changed by this configuration.
type DangerDecayConfig struct {
	Enabled                 bool    `json:"enabled" yaml:"enabled"`
	TickSeconds             int     `json:"tick_seconds" yaml:"tick_seconds"`
	WindowMinutes           int     `json:"window_minutes" yaml:"window_minutes"`
	HalfLifeMinutes         int     `json:"half_life_minutes" yaml:"half_life_minutes"`
	IdleBelowScore          float64 `json:"idle_below_score" yaml:"idle_below_score"`
	IdleStableSeconds       int     `json:"idle_stable_seconds" yaml:"idle_stable_seconds"`
	DowngradeStableSeconds  int     `json:"downgrade_stable_seconds" yaml:"downgrade_stable_seconds"`
	LockIntrusionUntilReset bool    `json:"lock_intrusion_until_reset" yaml:"lock_intrusion_until_reset"`
}

func DefaultDangerDecayConfig() DangerDecayConfig {
	return DangerDecayConfig{
		Enabled: true, TickSeconds: 5, WindowMinutes: 30, HalfLifeMinutes: 10,
		IdleBelowScore: 0.25, IdleStableSeconds: 300, DowngradeStableSeconds: 60,
		LockIntrusionUntilReset: true,
	}
}

func NormalizeDangerDecayConfig(config DangerDecayConfig) DangerDecayConfig {
	defaults := DefaultDangerDecayConfig()
	if config.TickSeconds <= 0 {
		config.TickSeconds = defaults.TickSeconds
	}
	if config.WindowMinutes <= 0 {
		config.WindowMinutes = defaults.WindowMinutes
	}
	if config.HalfLifeMinutes <= 0 {
		config.HalfLifeMinutes = defaults.HalfLifeMinutes
	}
	if config.IdleStableSeconds <= 0 {
		config.IdleStableSeconds = defaults.IdleStableSeconds
	}
	if config.DowngradeStableSeconds <= 0 {
		config.DowngradeStableSeconds = defaults.DowngradeStableSeconds
	}
	if config.IdleBelowScore <= 0 || config.IdleBelowScore > 1 {
		config.IdleBelowScore = defaults.IdleBelowScore
	}
	return config
}

type CgeSecurityProfile struct {
	Mode                                CgeSecurityMode   `json:"mode" yaml:"mode"`
	GlobalSensitivity                   float64           `json:"global_sensitivity" yaml:"global_sensitivity"`
	UnknownPersonTolerance              string            `json:"unknown_person_tolerance" yaml:"unknown_person_tolerance"`
	NightSensitivityMultiplier          float64           `json:"night_sensitivity_multiplier" yaml:"night_sensitivity_multiplier"`
	ArmedSensitivityMultiplier          float64           `json:"armed_sensitivity_multiplier" yaml:"armed_sensitivity_multiplier"`
	CriticalRooms                       []string          `json:"critical_rooms" yaml:"critical_rooms"`
	IgnoredMotionRooms                  []string          `json:"ignored_motion_rooms" yaml:"ignored_motion_rooms"`
	MinimumNotifyDangerLevel            DangerLevel       `json:"minimum_notify_danger_level" yaml:"minimum_notify_danger_level"`
	MinimumAutoActionDangerLevel        DangerLevel       `json:"minimum_auto_action_danger_level" yaml:"minimum_auto_action_danger_level"`
	RequireHumanConfirmationForSiren    bool              `json:"require_human_confirmation_for_siren" yaml:"require_human_confirmation_for_siren"`
	AllowAutomaticLights                bool              `json:"allow_automatic_lights" yaml:"allow_automatic_lights"`
	AllowAutomaticRecording             bool              `json:"allow_automatic_recording" yaml:"allow_automatic_recording"`
	AllowAutomaticNotifications         bool              `json:"allow_automatic_notifications" yaml:"allow_automatic_notifications"`
	UnknownPersistenceSeconds           int               `json:"unknown_persistence_seconds" yaml:"unknown_persistence_seconds"`
	SignificantInactivityTimeoutSeconds int               `json:"significant_inactivity_timeout_seconds" yaml:"significant_inactivity_timeout_seconds"`
	DangerDecay                         DangerDecayConfig `json:"danger_decay" yaml:"danger_decay"`
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
		DangerDecay:                         DefaultDangerDecayConfig(),
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
	profile.DangerDecay = NormalizeDangerDecayConfig(profile.DangerDecay)

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
	case DangerNone, DangerLow, DangerMedium, DangerMediumHigh, DangerHigh, DangerCritical:
		return true
	default:
		return false
	}
}

type CgeCorrectionType string

const (
	CgeCorrectionFalseNegative      CgeCorrectionType = "false_negative"
	CgeCorrectionReactionTooStrong  CgeCorrectionType = "reaction_too_strong"
	CgeCorrectionReactionTooWeak    CgeCorrectionType = "reaction_too_weak"
	CgeCorrectionCorrectTuneActions CgeCorrectionType = "correct_but_tune_actions"
	CgeCorrectionFalsePositive      CgeCorrectionType = "false_positive"
	CgeCorrectionTooLow             CgeCorrectionType = "too_low"
	CgeCorrectionTooHigh            CgeCorrectionType = "too_high"
	CgeCorrectionWrongState         CgeCorrectionType = "wrong_state"
	CgeCorrectionWrongAction        CgeCorrectionType = "wrong_action"
	CgeCorrectionMarkNormal         CgeCorrectionType = "mark_normal"
	CgeCorrectionMarkCritical       CgeCorrectionType = "mark_critical"
)

type CgeFeedbackScope string

const (
	CgeFeedbackCaseOnly       CgeFeedbackScope = "case_only"
	CgeFeedbackApplyToSimilar CgeFeedbackScope = "apply_to_similar_future_chains"
)

type CgePreferredAction string

const (
	CgeActionObserve                   CgePreferredAction = "observe"
	CgeActionNotifyOwner               CgePreferredAction = "notify_owner"
	CgeActionNotifyEmergencyContact    CgePreferredAction = "notify_emergency_contact"
	CgeActionRecordClip                CgePreferredAction = "record_clip"
	CgeActionLockEvidence              CgePreferredAction = "lock_evidence"
	CgeActionCreateAlert               CgePreferredAction = "create_alert"
	CgeActionRequestUserValidation     CgePreferredAction = "request_user_validation"
	CgeActionIgnorePattern             CgePreferredAction = "ignore_pattern"
	CgeActionActivateRelatedAutomation CgePreferredAction = "activate_related_automation"
)

type CgeFinalOutcome string

const (
	CgeOutcomeNormal        CgeFinalOutcome = "normal"
	CgeOutcomeFalsePositive CgeFinalOutcome = "false_positive"
	CgeOutcomeRealIncident  CgeFinalOutcome = "real_incident"
	CgeOutcomeUncertain     CgeFinalOutcome = "uncertain"
)

type CgeEvaluationFeedback struct {
	ID                     string                   `json:"id"`
	ChainID                string                   `json:"chain_id"`
	EventID                string                   `json:"event_id"`
	EvaluationIndex        int                      `json:"evaluation_index"`
	CorrectionType         CgeCorrectionType        `json:"correction_type"`
	Scope                  CgeFeedbackScope         `json:"scope,omitempty"`
	PreferredActions       []string                 `json:"preferred_actions"`
	PreferredActionDetails []CgePreferredActionSpec `json:"preferred_action_details,omitempty"`
	BlockedActions         []CgeBlockedAction       `json:"blocked_actions,omitempty"`
	AdminNote              string                   `json:"admin_note,omitempty"`
	CorrectedState         string                   `json:"corrected_state,omitempty"`
	CorrectedDangerLevel   DangerLevel              `json:"corrected_danger_level,omitempty"`
	Note                   string                   `json:"note,omitempty"`
	CreatedBy              string                   `json:"created_by"`
	CreatedAt              time.Time                `json:"created_at"`
}

type CgeChainFeedback struct {
	ID                         string                   `json:"id"`
	ChainID                    string                   `json:"chain_id"`
	FinalOutcome               CgeFinalOutcome          `json:"final_outcome,omitempty"`
	CorrectionType             CgeCorrectionType        `json:"correction_type,omitempty"`
	Scope                      CgeFeedbackScope         `json:"scope,omitempty"`
	PreferredActions           []string                 `json:"preferred_actions"`
	PreferredActionDetails     []CgePreferredActionSpec `json:"preferred_action_details,omitempty"`
	BlockedActions             []CgeBlockedAction       `json:"blocked_actions,omitempty"`
	AdminNote                  string                   `json:"admin_note,omitempty"`
	CorrectedFinalDangerLevel  DangerLevel              `json:"corrected_final_danger_level,omitempty"`
	ApplyToSimilarFutureChains bool                     `json:"apply_to_similar_future_chains"`
	Note                       string                   `json:"note,omitempty"`
	CreatedBy                  string                   `json:"created_by"`
	CreatedAt                  time.Time                `json:"created_at"`
}

type CgePreferredActionSpec struct {
	Command string `json:"command"`
	Target  string `json:"target,omitempty"`
	Enabled bool   `json:"enabled"`
}

type CgeBlockedAction struct {
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

func (f *CgeEvaluationFeedback) UnmarshalJSON(data []byte) error {
	type alias CgeEvaluationFeedback
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	preferred := fields["preferred_actions"]
	delete(fields, "preferred_actions")
	base, _ := json.Marshal(fields)
	var value alias
	if err := json.Unmarshal(base, &value); err != nil {
		return err
	}
	if err := decodePreferredActions(preferred, &value.PreferredActions, &value.PreferredActionDetails); err != nil {
		return err
	}
	*f = CgeEvaluationFeedback(value)
	return nil
}

func (f *CgeChainFeedback) UnmarshalJSON(data []byte) error {
	type alias CgeChainFeedback
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	preferred := fields["preferred_actions"]
	delete(fields, "preferred_actions")
	base, _ := json.Marshal(fields)
	var value alias
	if err := json.Unmarshal(base, &value); err != nil {
		return err
	}
	if err := decodePreferredActions(preferred, &value.PreferredActions, &value.PreferredActionDetails); err != nil {
		return err
	}
	*f = CgeChainFeedback(value)
	return nil
}

func decodePreferredActions(raw json.RawMessage, stringsOut *[]string, details *[]CgePreferredActionSpec) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		*stringsOut = legacy
		return nil
	}
	var structured []CgePreferredActionSpec
	if err := json.Unmarshal(raw, &structured); err != nil {
		return err
	}
	for _, item := range structured {
		if item.Command == "" {
			continue
		}
		*stringsOut = append(*stringsOut, item.Command)
	}
	*details = structured
	return nil
}
