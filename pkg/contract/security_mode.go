package contract

import "time"

type SecurityMode string

const (
	SecurityModeHome         SecurityMode = "home"
	SecurityModeNight        SecurityMode = "night"
	SecurityModeAway         SecurityMode = "away"
	SecurityModeHighSecurity SecurityMode = "high_security"
)

type ExpectedOccupancy string

const (
	ExpectedOccupancyUnknown  ExpectedOccupancy = "unknown"
	ExpectedOccupancyOccupied ExpectedOccupancy = "occupied"
	ExpectedOccupancyEmpty    ExpectedOccupancy = "empty"
)

type SecurityModeState struct {
	Mode              SecurityMode      `json:"mode"`
	Armed             bool              `json:"armed"`
	ExpectedOccupancy ExpectedOccupancy `json:"expected_occupancy"`
	SetBy             string            `json:"set_by"`
	Reason            string            `json:"reason"`
	Since             time.Time         `json:"since"`
	ExpiresAt         *time.Time        `json:"expires_at"`
	Source            string            `json:"source"`
}

type SecurityModeRequest struct {
	Mode            SecurityMode `json:"mode"`
	Reason          string       `json:"reason,omitempty"`
	DurationSeconds int          `json:"duration_seconds,omitempty"`
	SetBy           string       `json:"set_by,omitempty"`
	Source          string       `json:"source,omitempty"`
}

type SecurityArmRequest = SecurityModeRequest

type SecurityDisarmRequest struct {
	Reason string `json:"reason,omitempty"`
	SetBy  string `json:"set_by,omitempty"`
	Source string `json:"source,omitempty"`
}

func DefaultSecurityModeState(now time.Time) SecurityModeState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return SecurityModeState{Mode: SecurityModeHome, ExpectedOccupancy: ExpectedOccupancyUnknown, SetBy: "system", Reason: "default", Since: now.UTC(), Source: "system"}
}

func NormalizeSecurityModeState(value SecurityModeState, now time.Time) SecurityModeState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if value.Mode == "" {
		value.Mode = SecurityModeHome
	}
	switch value.Mode {
	case SecurityModeHome:
		value.Armed = false
		if value.ExpectedOccupancy == "" {
			value.ExpectedOccupancy = ExpectedOccupancyUnknown
		}
	case SecurityModeNight:
		value.Armed = true
		value.ExpectedOccupancy = ExpectedOccupancyOccupied
	case SecurityModeAway, SecurityModeHighSecurity:
		value.Armed = true
		value.ExpectedOccupancy = ExpectedOccupancyEmpty
	default:
		value.Mode = SecurityModeHome
		value.Armed = false
		value.ExpectedOccupancy = ExpectedOccupancyUnknown
	}
	if value.SetBy == "" {
		value.SetBy = "system"
	}
	if value.Source == "" {
		value.Source = "system"
	}
	if value.Since.IsZero() {
		value.Since = now.UTC()
	}
	return value
}
