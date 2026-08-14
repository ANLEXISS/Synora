package contract

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

type ClipStatus string

const (
	DefaultClipListLimit = 50
	MaxClipListLimit     = 100
)

const (
	ClipStatusReceiving  ClipStatus = "receiving"
	ClipStatusReady      ClipStatus = "ready"
	ClipStatusProcessing ClipStatus = "processing"
	ClipStatusProcessed  ClipStatus = "processed"
	ClipStatusFailed     ClipStatus = "failed"
	ClipStatusMissing    ClipStatus = "missing"
	ClipStatusExpired    ClipStatus = "expired"
)

func (status ClipStatus) Validate() error {
	switch status {
	case ClipStatusReceiving, ClipStatusReady, ClipStatusProcessing,
		ClipStatusProcessed, ClipStatusFailed, ClipStatusMissing, ClipStatusExpired:
		return nil
	default:
		return fmt.Errorf("invalid clip status %q", status)
	}
}

// Clip is the shared metadata contract. Path is persisted for Core's physical
// file reconciliation but is cleared by every public RPC/REST adapter.
type Clip struct {
	ID           string     `json:"id"`
	ActivationID string     `json:"activation_id,omitempty"`
	ClipIndex    int        `json:"clip_index,omitempty"`
	SequenceKey  string     `json:"sequence_key,omitempty"`
	TrackID      string     `json:"track_id,omitempty"`
	CameraID     string     `json:"camera_id"`
	NodeID       string     `json:"node_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ReceivedAt   time.Time  `json:"received_at,omitempty"`
	ReadyAt      time.Time  `json:"ready_at,omitempty"`
	ProcessingAt time.Time  `json:"processing_started_at,omitempty"`
	ProcessedAt  time.Time  `json:"processed_at,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at,omitempty"`
	Status       ClipStatus `json:"status"`
	SizeBytes    int64      `json:"size_bytes"`
	Checksum     string     `json:"checksum,omitempty"`
	MediaType    string     `json:"media_type,omitempty"`
	Container    string     `json:"container,omitempty"`
	Duration     float64    `json:"duration,omitempty"`
	EventIDs     []string   `json:"event_ids,omitempty"`
	IncidentIDs  []string   `json:"incident_ids,omitempty"`
	FailureCode  string     `json:"failure_code,omitempty"`
	Revision     uint64     `json:"revision"`

	// Legacy/internal fields retained for compatibility with existing engine
	// adapters and persisted state. They are not exposed as an absolute path.
	EventID string    `json:"event_id,omitempty"`
	Path    string    `json:"path,omitempty"`
	Start   time.Time `json:"start,omitempty"`
	End     time.Time `json:"end,omitempty"`
}

type ClipLifecyclePayload struct {
	Clip        Clip   `json:"clip"`
	ClipID      string `json:"clip_id"`
	CameraID    string `json:"camera_id,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
}

func (clip Clip) Validate() error {
	if strings.TrimSpace(clip.ID) == "" {
		return fmt.Errorf("clip id is required")
	}
	if strings.TrimSpace(clip.CameraID) == "" {
		return fmt.Errorf("clip camera id is required")
	}
	if err := clip.Status.Validate(); err != nil {
		return err
	}
	if clip.SizeBytes < 0 {
		return fmt.Errorf("clip size cannot be negative")
	}
	if clip.ClipIndex < 0 {
		return fmt.Errorf("clip index cannot be negative")
	}
	if clip.Duration < 0 || math.IsNaN(clip.Duration) || math.IsInf(clip.Duration, 0) {
		return fmt.Errorf("clip duration cannot be negative")
	}
	if checksum := strings.TrimSpace(clip.Checksum); checksum != "" {
		if len(checksum) != sha256HexLength || !isHex(checksum) {
			return fmt.Errorf("clip checksum must be a SHA-256 hex digest")
		}
	}
	return nil
}

const sha256HexLength = 64

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidClipTransition(from, to ClipStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case ClipStatusReceiving:
		return to == ClipStatusReady || to == ClipStatusFailed
	case ClipStatusReady:
		return to == ClipStatusProcessing || to == ClipStatusProcessed || to == ClipStatusMissing || to == ClipStatusExpired || to == ClipStatusFailed
	case ClipStatusProcessing:
		return to == ClipStatusProcessed || to == ClipStatusReady || to == ClipStatusFailed || to == ClipStatusMissing || to == ClipStatusExpired
	case ClipStatusProcessed:
		return to == ClipStatusMissing || to == ClipStatusExpired
	case ClipStatusFailed:
		return to == ClipStatusReady || to == ClipStatusExpired
	case ClipStatusMissing:
		return to == ClipStatusReady || to == ClipStatusExpired
	case ClipStatusExpired:
		return false
	default:
		return false
	}
}
