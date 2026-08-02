package situationfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/episodes"
)

type TopologyView = episodes.TopologyView

type ExtractionInput struct {
	Episode     episodes.EpisodeSnapshot
	Topology    TopologyView
	ExtractedAt time.Time
}

type factDraft struct {
	semanticKey string
	key         FactKey
	code        FactCode
	scope       FactScope
	subject     FactSubject
	predicate   string
	value       FactValue
	origin      FactOrigin
	status      FactStatus
	validFrom   time.Time
	provenance  []ProvenanceRef
	canonical   string
	partial     bool
}

type extractionBuilder struct {
	input  ExtractionInput
	policy Policy
	schema FactSchema
	drafts map[string][]factDraft
	prov   []ProvenanceRef
}

func Extract(input ExtractionInput, policy Policy) (FactSet, error) {
	if err := policy.Validate(); err != nil {
		return FactSet{}, err
	}
	if input.Episode.ID == "" {
		return FactSet{}, ErrMissingEpisodeID
	}
	if input.Episode.Revision == 0 {
		return FactSet{}, ErrMissingEpisodeRevision
	}
	if input.ExtractedAt.IsZero() {
		return FactSet{}, ErrInvalidFactSet
	}
	if err := input.Episode.Validate(); err != nil {
		return FactSet{}, err
	}
	if validator, ok := input.Topology.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return FactSet{}, err
		}
	}
	builder := &extractionBuilder{input: input, policy: policy, schema: compiledSchema(), drafts: make(map[string][]factDraft, estimateFactCapacity(input.Episode, policy))}
	if err := builder.extract(); err != nil {
		return FactSet{}, err
	}
	set, err := builder.finish()
	if err != nil {
		return FactSet{}, err
	}
	return set, nil
}

func BuildExtractionReport(set FactSet) ExtractionReport {
	report := ExtractionReport{EpisodeID: set.EpisodeID, EpisodeRevision: set.EpisodeRevision, FactCount: len(set.Facts), ConflictCount: len(set.Conflicts), SchemaFingerprint: set.SchemaFingerprint, PolicyFingerprint: set.PolicyFingerprint, FactSetFingerprint: set.Fingerprint}
	seenCodes := map[FactCode]struct{}{}
	for _, fact := range set.Facts {
		if _, ok := seenCodes[fact.Code]; !ok {
			report.Codes = append(report.Codes, fact.Code)
			seenCodes[fact.Code] = struct{}{}
		}
		switch fact.Status {
		case StatusUnknown:
			report.UnknownCount++
		}
		switch fact.Origin {
		case OriginObserved:
			report.ObservedCount++
		case OriginDerived:
			report.DerivedCount++
		case OriginCarried:
			report.CarriedCount++
		}
	}
	sort.Slice(report.Codes, func(i, j int) bool { return report.Codes[i] < report.Codes[j] })
	return report
}

func (b *extractionBuilder) extract() error {
	if err := b.extractEpisode(); err != nil {
		return err
	}
	if err := b.extractIdentity(); err != nil {
		return err
	}
	if err := b.extractSpatial(); err != nil {
		return err
	}
	if err := b.extractTemporal(); err != nil {
		return err
	}
	if err := b.extractContext(); err != nil {
		return err
	}
	if err := b.extractContinuity(); err != nil {
		return err
	}
	if err := b.extractMemory(); err != nil {
		return err
	}
	return nil
}

func (b *extractionBuilder) add(code FactCode, scope FactScope, subject FactSubject, slot string, value FactValue, origin FactOrigin, status FactStatus, validFrom time.Time, provenance []ProvenanceRef, partial bool) error {
	definition, ok := b.schema.Definition(code)
	if !ok {
		return ErrUnknownFactCode
	}
	if err := subject.Validate(b.policy.MaxStringLength); err != nil {
		return err
	}
	maxValues := b.policy.MaxSetValues
	if value.Kind == ValueStringList {
		maxValues = b.policy.MaxSequenceValues
	}
	if err := value.Validate(b.policy.MaxStringLength, maxValues); err != nil {
		if status == StatusUnknown && b.policy.IncludeUnknownFacts {
			value = zeroValue(definition.ValueKind)
		} else {
			return err
		}
	}
	if validFrom.IsZero() {
		validFrom = b.input.ExtractedAt
	}
	semantic := semanticKey(b.input.Episode.ID, scope, subject, code, slot)
	canonical := value.Canonical()
	drafts := b.drafts[semantic]
	for i := range drafts {
		draft := &drafts[i]
		if draft.canonical == canonical && draft.status == status {
			draft.provenance = mergeCanonicalProvenance(draft.provenance, provenance)
			draft.partial = draft.partial || partial
			if originRank(origin) > originRank(draft.origin) {
				draft.origin = origin
			}
			b.drafts[semantic] = drafts
			return nil
		}
	}
	if len(b.drafts) >= b.policy.MaxFactsPerEpisode && len(drafts) == 0 {
		return ErrFactLimitReached
	}
	b.drafts[semantic] = append(drafts, factDraft{semanticKey: semantic, key: factKeyFor(semantic), code: code, scope: scope, subject: subject, predicate: string(code), value: value, origin: origin, status: status, validFrom: validFrom.UTC().Round(0), provenance: provenance, canonical: canonical, partial: partial})
	return nil
}

func (b *extractionBuilder) addUnknown(code FactCode, scope FactScope, subject FactSubject, slot string, validFrom time.Time, provenance []ProvenanceRef) error {
	if !b.policy.IncludeUnknownFacts {
		return nil
	}
	definition, ok := b.schema.Definition(code)
	if !ok {
		return ErrUnknownFactCode
	}
	return b.add(code, scope, subject, slot, zeroValue(definition.ValueKind), OriginDerived, StatusUnknown, validFrom, provenance, true)
}

func (b *extractionBuilder) finish() (FactSet, error) {
	semanticKeys := make([]string, 0, len(b.drafts))
	for key := range b.drafts {
		semanticKeys = append(semanticKeys, key)
	}
	sort.Strings(semanticKeys)
	facts := make([]Fact, 0)
	conflicts := make([]ConflictSet, 0)
	for _, semantic := range semanticKeys {
		drafts := b.drafts[semantic]
		sort.Slice(drafts, func(i, j int) bool { return drafts[i].canonical < drafts[j].canonical })
		definition, _ := b.schema.Definition(drafts[0].code)
		conflicting := len(drafts) > 1 && definition.ConflictPolicy == ConflictSingleValue
		ids := make([]FactID, 0, len(drafts))
		provenance := make([]ProvenanceRef, 0)
		for _, draft := range drafts {
			status := draft.status
			if conflicting && status == StatusAsserted {
				status = StatusConflicting
			}
			fact := Fact{Key: draft.key, Code: draft.code, Scope: draft.scope, Subject: draft.subject, Predicate: draft.predicate, Value: draft.value, Origin: draft.origin, Status: status, ValidFrom: draft.validFrom, Quality: FactQuality{CompletenessPermille: completeness(draft.partial), ReliabilityPermille: 0, SourceCount: len(draft.provenance), Partial: draft.partial}, Provenance: draft.provenance}
			fact.ID = factIDFor(fact)
			if err := fact.Validate(b.schema, b.policy); err != nil {
				return FactSet{}, err
			}
			facts = append(facts, fact)
			ids = append(ids, fact.ID)
			provenance = append(provenance, fact.Provenance...)
		}
		if conflicting {
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			conflict := ConflictSet{Key: drafts[0].key, FactIDs: ids, Code: string(drafts[0].code), Provenance: canonicalProvenance(provenance)}
			conflict.ID = conflictIDFor(conflict)
			if err := conflict.Validate(b.policy); err != nil {
				return FactSet{}, err
			}
			conflicts = append(conflicts, conflict)
		}
	}
	if len(facts) > b.policy.MaxFactsPerEpisode {
		return FactSet{}, ErrFactLimitReached
	}
	sortFacts(facts)
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].ID < conflicts[j].ID })
	set := FactSet{EpisodeID: b.input.Episode.ID, EpisodeRevision: b.input.Episode.Revision, ExtractedAt: b.input.ExtractedAt.UTC().Round(0), SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: b.policy.Fingerprint(), Facts: facts, Conflicts: conflicts}
	set.Fingerprint = FactSetFingerprint(set)
	return set, nil
}

func subjectSemantic(subject FactSubject) string {
	return subject.Kind + "\x00" + subject.ID + "\x00" + subject.Role
}

func semanticKey(id episodes.EpisodeID, scope FactScope, subject FactSubject, code FactCode, slot string) string {
	var builder strings.Builder
	builder.Grow(len(id) + len(scope) + len(code) + len(slot) + len(subject.Kind) + len(subject.ID) + len(subject.Role) + 5)
	builder.WriteString(string(id))
	builder.WriteByte(0)
	builder.WriteString(string(scope))
	builder.WriteByte(0)
	builder.WriteString(subjectSemantic(subject))
	builder.WriteByte(0)
	builder.WriteString(string(code))
	builder.WriteByte(0)
	builder.WriteString(slot)
	return builder.String()
}

func estimateFactCapacity(episode episodes.EpisodeSnapshot, policy Policy) int {
	// The aggregate schema normally creates fewer than 96 semantic keys. A
	// small observation-dependent allowance covers track/context conflicts
	// without allocating the full configured limit for hostile input sizes.
	estimate := 80 + len(episode.Observations)/4
	if estimate > policy.MaxFactsPerEpisode {
		return policy.MaxFactsPerEpisode
	}
	return estimate
}

func (b *extractionBuilder) observationProvenance() []ProvenanceRef {
	if b.prov == nil {
		b.prov = observationProvenance(b.input.Episode.Observations)
	}
	return b.prov
}

func factKeyFor(semantic string) FactKey {
	digest := sha256.Sum256([]byte(semantic))
	return FactKey("fact-key-" + hex.EncodeToString(digest[:]))
}

func originRank(origin FactOrigin) int {
	switch origin {
	case OriginObserved:
		return 3
	case OriginCarried:
		return 2
	default:
		return 1
	}
}

func completeness(partial bool) int {
	if partial {
		return 700
	}
	return 1000
}

func episodeSubject(id episodes.EpisodeID) FactSubject {
	return FactSubject{Kind: "episode", ID: string(id), Role: "episode"}
}
func obsProvenance(observation episodes.ObservationRef) ProvenanceRef {
	return ProvenanceRef{SourceKind: "observation", SourceID: observation.EventID, SourceRevision: 1, ObservedAt: observation.ObservedAt.UTC().Round(0), AlgorithmID: "episode-facts-extractor", AlgorithmVersion: "v1"}
}
func episodeProvenance(episode episodes.EpisodeSnapshot) ProvenanceRef {
	return ProvenanceRef{SourceKind: "episode", SourceID: string(episode.ID), SourceRevision: episode.Revision, ObservedAt: episode.LastObservedAt.UTC().Round(0), AlgorithmID: "episode-facts-extractor", AlgorithmVersion: "v1"}
}

func uniqueSorted(values []string) []string { return canonicalStrings(values) }

func minTime(values []time.Time) time.Time {
	result := values[0]
	for _, value := range values[1:] {
		if value.Before(result) {
			result = value
		}
	}
	return result
}
