package fieldtrial

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type AnnotationLabel string

const (
	AnnotationOrdinary          AnnotationLabel = "ordinary"
	AnnotationBenignVariation   AnnotationLabel = "benign_variation"
	AnnotationRareLegitimate    AnnotationLabel = "rare_legitimate"
	AnnotationRoutineChange     AnnotationLabel = "expected_routine_change"
	AnnotationTestIntrusion     AnnotationLabel = "test_intrusion"
	AnnotationUnexpectedUnknown AnnotationLabel = "unexpected_unknown"
	AnnotationSensorIssue       AnnotationLabel = "sensor_issue"
	AnnotationContextIssue      AnnotationLabel = "context_issue"
	AnnotationFalseIdentity     AnnotationLabel = "false_identity"
	AnnotationReviewRequired    AnnotationLabel = "other_review_required"
)

type Annotation struct {
	SchemaVersion string          `json:"schema_version"`
	SessionID     string          `json:"session_id"`
	Sequence      uint64          `json:"sequence"`
	EventRef      string          `json:"event_ref"`
	Label         AnnotationLabel `json:"label"`
	AnnotatedAt   time.Time       `json:"annotated_at"`
	Source        string          `json:"source"`
	NoteCode      string          `json:"note_code,omitempty"`
}

type AnnotationInput struct {
	EventRef    string
	Label       AnnotationLabel
	AnnotatedAt time.Time
	Source      string
	NoteCode    string
}

func validAnnotationLabel(label AnnotationLabel) bool {
	switch label {
	case AnnotationOrdinary, AnnotationBenignVariation, AnnotationRareLegitimate, AnnotationRoutineChange, AnnotationTestIntrusion, AnnotationUnexpectedUnknown, AnnotationSensorIssue, AnnotationContextIssue, AnnotationFalseIdentity, AnnotationReviewRequired:
		return true
	default:
		return false
	}
}

func (r *Recorder) addAnnotationLocked(input AnnotationInput) error {
	if r == nil || r.status == SessionClosed {
		return ErrSessionClosed
	}
	if r.status == SessionDegraded {
		return ErrSessionDegraded
	}
	if !strings.HasPrefix(input.EventRef, "trial-ref-") || !validAnnotationLabel(input.Label) || input.AnnotatedAt.IsZero() || len(input.Source) > MaxAnnotationNoteCodeSize || len(input.NoteCode) > MaxAnnotationNoteCodeSize {
		return ErrAnnotationInvalid
	}
	if _, ok := r.eventRefs[input.EventRef]; !ok {
		return fmt.Errorf("%w: event not found", ErrAnnotationInvalid)
	}
	annotation := Annotation{SchemaVersion: SchemaVersion, SessionID: r.manifest.SessionID, Sequence: r.annotationSequence + 1, EventRef: input.EventRef, Label: input.Label, AnnotatedAt: input.AnnotatedAt.UTC(), Source: boundedCode(input.Source), NoteCode: boundedCode(input.NoteCode)}
	payload, err := json.Marshal(annotation)
	if err != nil {
		return err
	}
	payloadHash := hashBytes(payload)
	envelope := AnnotationEnvelope{Sequence: annotation.Sequence, PreviousHash: r.annotationHash, PayloadSHA256: payloadHash, Payload: annotation}
	envelope.RecordHash = recordHash(envelope.Sequence, envelope.PreviousHash, envelope.PayloadSHA256)
	data, err := encodeAnnotationEnvelope(envelope)
	if err != nil {
		return err
	}
	if r.config.MaximumTotalBytes > 0 && r.totalBytesLocked()+int64(len(data)) > r.config.MaximumTotalBytes {
		r.markDegradedLocked()
		return ErrQuotaExceeded
	}
	if _, err := r.annotationFile.Write(data); err != nil {
		r.markDegradedLocked()
		return err
	}
	if r.config.SyncEachEvent {
		if err := r.annotationFile.Sync(); err != nil {
			r.markDegradedLocked()
			return err
		}
	}
	r.annotationSequence, r.annotationHash = annotation.Sequence, envelope.RecordHash
	r.manifest.AnnotationCount++
	r.stats.AnnotationsAdded++
	r.stats.AnnotationBytes += int64(len(data))
	return nil
}

func openAnnotations(path string) (*os.File, uint64, string, uint64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o640)
	if err != nil {
		return nil, 0, "", 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		file.Close()
		return nil, 0, "", 0, err
	}
	var sequence uint64
	var count uint64
	previous := ""
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var envelope AnnotationEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.Sequence != sequence+1 || envelope.PreviousHash != previous || envelope.Payload.SessionID == "" {
			file.Close()
			return nil, 0, "", 0, fmt.Errorf("%w: annotation chain", ErrTelemetryCorrupt)
		}
		payload, _ := json.Marshal(envelope.Payload)
		if envelope.PayloadSHA256 != hashBytes(payload) || envelope.RecordHash != recordHash(envelope.Sequence, envelope.PreviousHash, envelope.PayloadSHA256) {
			file.Close()
			return nil, 0, "", 0, ErrTelemetryCorrupt
		}
		sequence, previous = envelope.Sequence, envelope.RecordHash
		count++
	}
	return file, sequence, previous, count, nil
}
