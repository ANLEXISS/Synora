package contract

import "time"

const (
	ResidentRoleOwner     = "owner"
	ResidentRoleResident  = "resident"
	ResidentRoleChild     = "child"
	ResidentRoleGuest     = "guest"
	ResidentRoleCaregiver = "caregiver"
)

type Resident struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	DisplayName string `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Role        string `json:"role" yaml:"role"`
	Admin       bool   `json:"admin" yaml:"admin"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Trusted     bool   `json:"trusted" yaml:"trusted"`

	Contact         Contact         `json:"contact,omitempty" yaml:"contact,omitempty"`
	Baseline        Baseline        `json:"baseline,omitempty" yaml:"baseline,omitempty"`
	PresenceProfile map[string]any  `json:"presence_profile,omitempty" yaml:"presence_profile,omitempty"`
	IdentityProfile IdentityProfile `json:"identity_profile,omitempty" yaml:"identity_profile,omitempty"`
	Permissions     map[string]any  `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	CreatedAt       time.Time       `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt       *time.Time      `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`
}

// ResidentView is the authenticated configuration view. PublicSnapshot keeps
// using its smaller resident projection and therefore does not expose contact
// or biometric identifiers.
type ResidentView = Resident

type ResidentPublicView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Role        string         `json:"role"`
	Admin       bool           `json:"admin"`
	Enabled     bool           `json:"enabled"`
	Trusted     bool           `json:"trusted"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
	DeletedAt   *time.Time     `json:"deleted_at,omitempty"`
}

type ResidentPatch struct {
	Name            *string          `json:"name,omitempty"`
	DisplayName     *string          `json:"display_name,omitempty"`
	Role            *string          `json:"role,omitempty"`
	Admin           *bool            `json:"admin,omitempty"`
	Enabled         *bool            `json:"enabled,omitempty"`
	Trusted         *bool            `json:"trusted,omitempty"`
	Contact         *Contact         `json:"contact,omitempty"`
	Baseline        *Baseline        `json:"baseline,omitempty"`
	PresenceProfile *map[string]any  `json:"presence_profile,omitempty"`
	IdentityProfile *IdentityProfile `json:"identity_profile,omitempty"`
	Permissions     *map[string]any  `json:"permissions,omitempty"`
	Metadata        *map[string]any  `json:"metadata,omitempty"`
}

type Contact struct {
	Email    string `json:"email,omitempty" yaml:"email,omitempty"`
	Phone    string `json:"phone,omitempty" yaml:"phone,omitempty"`
	WhatsApp string `json:"whatsapp,omitempty" yaml:"whatsapp,omitempty"`
}

type IdentityProfile struct {
	FaceIDs  []string `json:"face_ids,omitempty" yaml:"face_ids,omitempty"`
	VoiceIDs []string `json:"voice_ids,omitempty" yaml:"voice_ids,omitempty"`
	Aliases  []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
}

type Baseline struct {
	WakeTime  string             `json:"wake_time,omitempty" yaml:"wake_time,omitempty"`
	SleepTime string             `json:"sleep_time,omitempty" yaml:"sleep_time,omitempty"`
	Rooms     map[string]float64 `json:"rooms,omitempty" yaml:"rooms,omitempty"`
}
