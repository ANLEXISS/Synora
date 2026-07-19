package routines

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"synora/internal/cge/context"
)

type RoutineID string
type OccurrenceID string

const (
	routineIDPrefix    = "cge-routine-"
	occurrenceIDPrefix = "cge-routine-occurrence-"
)

type Status string

const (
	StatusCandidate   Status = "candidate"
	StatusActive      Status = "active"
	StatusDeclining   Status = "declining"
	StatusDormant     Status = "dormant"
	StatusArchived    Status = "archived"
	StatusInvalidated Status = "invalidated"
)

func validText(value string, max int) bool {
	return strings.TrimSpace(value) == value && value != "" && len([]rune(value)) <= max && !strings.ContainsAny(value, "\r\n")
}
func validOptionalText(value string) bool {
	return value == "" || (strings.TrimSpace(value) == value && len([]rune(value)) <= 256 && !strings.ContainsAny(value, "\r\n"))
}

func canonicalJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

func DeriveRoutineID(namespace string, subject Subject, kind Kind, pattern Pattern) (RoutineID, error) {
	if !validText(namespace, 128) || !validKind(kind) || subject.Validate() != nil || pattern.Validate() != nil || pattern.Kind != kind {
		return "", ErrInvalidRoutineID
	}
	material := struct {
		Namespace string  `json:"namespace"`
		Subject   Subject `json:"subject"`
		Kind      Kind    `json:"kind"`
		Pattern   Pattern `json:"pattern"`
	}{namespace, subject, kind, pattern}
	digest := sha256.Sum256([]byte(canonicalJSON(material)))
	return RoutineID(routineIDPrefix + hex.EncodeToString(digest[:])), nil
}

func DeriveOccurrenceID(namespace string, routineID RoutineID, kind Kind, observationIDs []string) (OccurrenceID, error) {
	if !validText(namespace, 128) || !validRoutineID(routineID) || !validKind(kind) || len(observationIDs) == 0 {
		return "", ErrInvalidOccurrenceID
	}
	ids := append([]string(nil), observationIDs...)
	if kind == KindPresence {
		sortStrings(ids)
	} else if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return "", ErrInvalidOccurrenceID
	}
	for i, id := range ids {
		if !validText(id, 256) || (i > 0 && kind == KindPresence && ids[i-1] == id) {
			return "", ErrInvalidOccurrenceID
		}
	}
	material := struct {
		Namespace      string    `json:"namespace"`
		RoutineID      RoutineID `json:"routine_id"`
		Kind           Kind      `json:"kind"`
		ObservationIDs []string  `json:"observation_ids"`
	}{namespace, routineID, kind, ids}
	digest := sha256.Sum256([]byte(canonicalJSON(material)))
	return OccurrenceID(occurrenceIDPrefix + hex.EncodeToString(digest[:])), nil
}

func validRoutineID(id RoutineID) bool {
	return len(id) == len(routineIDPrefix)+64 && strings.HasPrefix(string(id), routineIDPrefix) && isHex(string(id)[len(routineIDPrefix):])
}
func validOccurrenceID(id OccurrenceID) bool {
	return len(id) == len(occurrenceIDPrefix)+64 && strings.HasPrefix(string(id), occurrenceIDPrefix) && isHex(string(id)[len(occurrenceIDPrefix):])
}
func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
func validKind(k Kind) bool { return k == KindPresence || k == KindTransition }
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

type OccurrenceRef struct {
	ID                OccurrenceID           `json:"id"`
	ObservedAt        time.Time              `json:"observed_at"`
	ObservationIDs    []string               `json:"observation_ids"`
	Weekday           time.Weekday           `json:"weekday"`
	TimeBucket        int                    `json:"time_bucket"`
	DayPart           context.DayPart        `json:"day_part"`
	LocalDate         string                 `json:"local_date"`
	ContextQuality    context.ContextQuality `json:"context_quality"`
	TopologyRevisions []string               `json:"topology_revisions,omitempty"`
}

type Occurrence struct {
	ID                        OccurrenceID           `json:"id"`
	RoutineID                 RoutineID              `json:"routine_id"`
	Kind                      Kind                   `json:"kind"`
	Subject                   Subject                `json:"subject"`
	Pattern                   Pattern                `json:"pattern"`
	ObservedAt                time.Time              `json:"observed_at"`
	ObservationIDs            []string               `json:"observation_ids"`
	Weekday                   time.Weekday           `json:"weekday"`
	MinuteOfDay               int                    `json:"minute_of_day"`
	TimeBucket                int                    `json:"time_bucket"`
	DayPart                   context.DayPart        `json:"day_part"`
	LocalDate                 string                 `json:"local_date"`
	Timezone                  string                 `json:"timezone"`
	ContextQuality            context.ContextQuality `json:"context_quality"`
	TopologyRevisions         []string               `json:"topology_revisions,omitempty"`
	ExtractionPolicyNamespace string                 `json:"extraction_policy_namespace"`
	ExtractionPolicyVersion   string                 `json:"extraction_policy_version"`
}

func (o Occurrence) Ref() OccurrenceRef {
	return OccurrenceRef{ID: o.ID, ObservedAt: o.ObservedAt, ObservationIDs: append([]string(nil), o.ObservationIDs...), Weekday: o.Weekday, TimeBucket: o.TimeBucket, DayPart: o.DayPart, LocalDate: o.LocalDate, ContextQuality: o.ContextQuality, TopologyRevisions: append([]string(nil), o.TopologyRevisions...)}
}

func (o Occurrence) Validate() error {
	if !validOccurrenceID(o.ID) || !validRoutineID(o.RoutineID) || !validKind(o.Kind) || o.Pattern.Validate() != nil || o.Pattern.Kind != o.Kind || o.Subject.Validate() != nil || o.ObservedAt.IsZero() || !validText(o.LocalDate, 32) || !validText(o.Timezone, 128) || o.Weekday < time.Sunday || o.Weekday > time.Saturday || o.MinuteOfDay < 0 || o.MinuteOfDay >= 1440 || o.TimeBucket < 0 || !validDayPart(o.DayPart) || !validContextQuality(o.ContextQuality) || !validText(o.ExtractionPolicyNamespace, 128) || !validText(o.ExtractionPolicyVersion, 128) {
		return fmt.Errorf("%w: fields", ErrInvalidOccurrence)
	}
	if len(o.ObservationIDs) == 0 || (o.Kind == KindTransition && len(o.ObservationIDs) != 2) || (o.Kind == KindPresence && len(o.ObservationIDs) != 1) {
		return fmt.Errorf("%w: observation ids", ErrInvalidOccurrence)
	}
	for i, id := range o.ObservationIDs {
		if !validText(id, 256) || (i > 0 && o.ObservationIDs[i-1] == id) {
			return fmt.Errorf("%w: observation ids", ErrInvalidOccurrence)
		}
	}
	for i := 1; i < len(o.TopologyRevisions); i++ {
		if o.TopologyRevisions[i] == o.TopologyRevisions[i-1] {
			return fmt.Errorf("%w: topology revisions", ErrInvalidOccurrence)
		}
	}
	return nil
}

func validContextQuality(q context.ContextQuality) bool {
	return q == context.QualityUnknown || q == context.QualityPartial || q == context.QualityComplete
}
func validDayPart(v context.DayPart) bool {
	return v == context.DayPartNight || v == context.DayPartMorning || v == context.DayPartDay || v == context.DayPartEvening
}
