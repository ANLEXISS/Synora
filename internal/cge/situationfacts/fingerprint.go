package situationfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"synora/internal/cge/episodes"
)

func factIDFor(fact Fact) FactID {
	payload, _ := json.Marshal(struct {
		Key        FactKey
		Code       FactCode
		Scope      FactScope
		Subject    FactSubject
		Predicate  string
		Value      string
		Origin     FactOrigin
		Status     FactStatus
		ValidFrom  string
		ValidTo    string
		Quality    FactQuality
		Provenance []ProvenanceRef
	}{fact.Key, fact.Code, fact.Scope, fact.Subject, fact.Predicate, fact.Value.Canonical(), fact.Origin, fact.Status, fact.ValidFrom.UTC().Round(0).Format("2006-01-02T15:04:05.999999999Z07:00"), timeString(fact.ValidTo), fact.Quality, canonicalProvenance(fact.Provenance)})
	digest := sha256.Sum256(payload)
	return FactID("fact-" + hex.EncodeToString(digest[:]))
}

func conflictIDFor(conflict ConflictSet) string {
	ids := append([]FactID(nil), conflict.FactIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	payload, _ := json.Marshal(struct {
		Key     FactKey
		Code    string
		FactIDs []FactID
	}{conflict.Key, conflict.Code, ids})
	digest := sha256.Sum256(payload)
	return "conflict-" + hex.EncodeToString(digest[:])
}

func timeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Round(0).Format("2006-01-02T15:04:05.999999999Z07:00")
}

func FactSetFingerprint(set FactSet) string {
	copy := set
	copy.Fingerprint = ""
	if !factsSorted(copy.Facts) {
		copy.Facts = cloneFacts(copy.Facts)
		sortFacts(copy.Facts)
	}
	if !conflictsSorted(copy.Conflicts) {
		copy.Conflicts = cloneConflicts(copy.Conflicts)
		sort.Slice(copy.Conflicts, func(i, j int) bool { return copy.Conflicts[i].ID < copy.Conflicts[j].ID })
	}
	payload, _ := json.Marshal(struct {
		EpisodeID         episodes.EpisodeID
		EpisodeRevision   uint64
		ExtractedAt       string
		SchemaFingerprint string
		PolicyFingerprint string
		Facts             []Fact
		Conflicts         []ConflictSet
	}{copy.EpisodeID, copy.EpisodeRevision, copy.ExtractedAt.UTC().Round(0).Format("2006-01-02T15:04:05.999999999Z07:00"), copy.SchemaFingerprint, copy.PolicyFingerprint, copy.Facts, copy.Conflicts})
	digest := sha256.Sum256(payload)
	return "situation-facts-v1:" + hex.EncodeToString(digest[:])
}
