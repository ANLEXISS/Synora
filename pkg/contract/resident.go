package contract

import (
	"fmt"
	"time"
)

const (
	ResidentRoleOwner     = "owner"
	ResidentRoleResident  = "resident"
	ResidentRoleChild     = "child"
	ResidentRoleGuest     = "guest"
	ResidentRoleCaregiver = "caregiver"
)

const (
	FacePhotoStatusReceiving      = FacePhotoReceiving
	FacePhotoStatusStored         = FacePhotoStored
	FacePhotoStatusValidating     = FacePhotoValidating
	FacePhotoStatusActive         = FacePhotoActive
	FacePhotoStatusRejected       = FacePhotoRejected
	FacePhotoStatusMissing        = FacePhotoMissing
	FacePhotoStatusRemovalPending = FacePhotoRemovalPending
	FacePhotoStatusRemoved        = FacePhotoRemoved
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
	ID         string `json:"id" yaml:"id"`
	ResidentID string `json:"resident_id,omitempty" yaml:"resident_id,omitempty"`
	Filename   string `json:"filename,omitempty" yaml:"filename,omitempty"`
	// Path and StorageKey are internal reconciliation fields. Path is retained
	// for source compatibility with the legacy face endpoints but is never
	// serialized into a public contract.
	Path           string     `json:"-" yaml:"-"`
	StorageKey     string     `json:"-" yaml:"-"`
	View           string     `json:"view,omitempty" yaml:"view,omitempty"`
	CreatedAt      time.Time  `json:"created_at" yaml:"created_at"`
	ReceivedAt     *time.Time `json:"received_at,omitempty" yaml:"received_at,omitempty"`
	ValidatedAt    *time.Time `json:"validated_at,omitempty" yaml:"validated_at,omitempty"`
	ActivatedAt    *time.Time `json:"activated_at,omitempty" yaml:"activated_at,omitempty"`
	RemovedAt      *time.Time `json:"removed_at,omitempty" yaml:"removed_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at" yaml:"updated_at"`
	Status         string     `json:"status" yaml:"status"`
	SizeBytes      int64      `json:"size_bytes" yaml:"size_bytes"`
	Checksum       string     `json:"checksum" yaml:"checksum"`
	MediaType      string     `json:"media_type" yaml:"media_type"`
	Width          int        `json:"width" yaml:"width"`
	Height         int        `json:"height" yaml:"height"`
	FaceCount      int        `json:"face_count,omitempty" yaml:"face_count,omitempty"`
	Quality        string     `json:"quality,omitempty" yaml:"quality,omitempty"`
	DatasetVersion string     `json:"dataset_version,omitempty" yaml:"dataset_version,omitempty"`
	FailureCode    string     `json:"failure_code,omitempty" yaml:"failure_code,omitempty"`
	Revision       uint64     `json:"revision" yaml:"revision"`
	Source         string     `json:"source,omitempty" yaml:"source,omitempty"`
}

type FacePhotoStatus string

const (
	FacePhotoReceiving      FacePhotoStatus = "receiving"
	FacePhotoStored         FacePhotoStatus = "stored"
	FacePhotoValidating     FacePhotoStatus = "validating"
	FacePhotoActive         FacePhotoStatus = "active"
	FacePhotoRejected       FacePhotoStatus = "rejected"
	FacePhotoMissing        FacePhotoStatus = "missing"
	FacePhotoRemovalPending FacePhotoStatus = "removal_pending"
	FacePhotoRemoved        FacePhotoStatus = "removed"
)

func (s FacePhotoStatus) Validate() error {
	switch s {
	case FacePhotoReceiving, FacePhotoStored, FacePhotoValidating,
		FacePhotoActive, FacePhotoRejected, FacePhotoMissing,
		FacePhotoRemovalPending, FacePhotoRemoved:
		return nil
	default:
		return fmt.Errorf("invalid face photo status %q", s)
	}
}

func ValidFacePhotoTransition(from, to FacePhotoStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case FacePhotoReceiving:
		return to == FacePhotoStored || to == FacePhotoRejected
	case FacePhotoStored:
		return to == FacePhotoValidating || to == FacePhotoMissing || to == FacePhotoRemovalPending
	case FacePhotoValidating:
		return to == FacePhotoActive || to == FacePhotoRejected || to == FacePhotoStored || to == FacePhotoMissing
	case FacePhotoActive:
		return to == FacePhotoRemovalPending || to == FacePhotoMissing
	case FacePhotoMissing:
		return to == FacePhotoStored || to == FacePhotoRemovalPending
	case FacePhotoRemovalPending:
		return to == FacePhotoRemoved || to == FacePhotoStored || to == FacePhotoMissing
	case FacePhotoRejected:
		return to == FacePhotoRemoved
	case FacePhotoRemoved:
		return false
	default:
		return false
	}
}

type FaceDatasetStatus string

const (
	FaceDatasetIdle        FaceDatasetStatus = "idle"
	FaceDatasetBuilding    FaceDatasetStatus = "building"
	FaceDatasetReady       FaceDatasetStatus = "ready"
	FaceDatasetActive      FaceDatasetStatus = "active"
	FaceDatasetFailed      FaceDatasetStatus = "failed"
	FaceDatasetUnavailable FaceDatasetStatus = "unavailable"
)

type FaceDatasetState struct {
	SchemaVersion      int               `json:"schema_version"`
	DesiredRevision    uint64            `json:"desired_revision"`
	ActiveVersion      string            `json:"active_version,omitempty"`
	ActiveRevision     uint64            `json:"active_revision"`
	BuiltAt            time.Time         `json:"built_at,omitempty"`
	ActivatedAt        time.Time         `json:"activated_at,omitempty"`
	ResidentIDs        []string          `json:"resident_ids,omitempty"`
	PhotoIDs           []string          `json:"photo_ids,omitempty"`
	ManifestChecksum   string            `json:"manifest_checksum,omitempty"`
	ModelFingerprint   string            `json:"model_fingerprint,omitempty"`
	EmbeddingDimension int               `json:"embedding_dimension,omitempty"`
	Status             FaceDatasetStatus `json:"status"`
	FailureCode        string            `json:"failure_code,omitempty"`
}

func (s *FaceDatasetState) Validate() error {
	if s == nil {
		return fmt.Errorf("face dataset state is nil")
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	switch s.Status {
	case "", FaceDatasetIdle, FaceDatasetBuilding, FaceDatasetReady, FaceDatasetActive, FaceDatasetFailed, FaceDatasetUnavailable:
		return nil
	default:
		return fmt.Errorf("invalid face dataset status %q", s.Status)
	}
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
