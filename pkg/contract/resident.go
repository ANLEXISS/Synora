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
	ID              string      `json:"id" yaml:"id"`
	Name            string      `json:"name" yaml:"name"`
	FirstName       string      `json:"first_name,omitempty" yaml:"first_name,omitempty"`
	LastName        string      `json:"last_name,omitempty" yaml:"last_name,omitempty"`
	DisplayName     string      `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Role            string      `json:"role" yaml:"role"`
	Admin           bool        `json:"admin" yaml:"admin"`
	Enabled         bool        `json:"enabled" yaml:"enabled"`
	Trusted         bool        `json:"trusted" yaml:"trusted"`
	ReferenceNodeID string      `json:"reference_node_id,omitempty" yaml:"reference_node_id,omitempty"`
	AccountID       string      `json:"account_id,omitempty" yaml:"account_id,omitempty"`
	FaceProfile     FaceProfile `json:"face_profile,omitempty" yaml:"face_profile,omitempty"`

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

type FacePhoto struct {
	ID        string    `json:"id" yaml:"id"`
	Filename  string    `json:"filename" yaml:"filename"`
	Path      string    `json:"path" yaml:"path"`
	View      string    `json:"view,omitempty" yaml:"view,omitempty"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"`
	Source    string    `json:"source" yaml:"source"`
}

type FaceProfile struct {
	Status      string      `json:"status" yaml:"status"`
	BasePhotos  []FacePhoto `json:"base_photos,omitempty" yaml:"base_photos,omitempty"`
	AutoCount   int         `json:"auto_count" yaml:"auto_count"`
	ReviewCount int         `json:"review_count" yaml:"review_count"`
	// PendingCount is retained for compatibility with older residents.yaml files.
	PendingCount int `json:"pending_count,omitempty" yaml:"pending_count,omitempty"`
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
	FirstName       *string          `json:"first_name,omitempty"`
	LastName        *string          `json:"last_name,omitempty"`
	DisplayName     *string          `json:"display_name,omitempty"`
	Role            *string          `json:"role,omitempty"`
	Admin           *bool            `json:"admin,omitempty"`
	Enabled         *bool            `json:"enabled,omitempty"`
	Trusted         *bool            `json:"trusted,omitempty"`
	ReferenceNodeID *string          `json:"reference_node_id,omitempty"`
	AccountID       *string          `json:"account_id,omitempty"`
	FaceProfile     *FaceProfile     `json:"face_profile,omitempty"`
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
