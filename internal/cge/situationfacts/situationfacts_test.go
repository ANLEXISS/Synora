package situationfacts

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/episodes"
)

var factsBase = time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC)

func factObservation(id string, at time.Time, subject episodes.SubjectRef, node, zone, track string) episodes.ObservationRef {
	return episodes.ObservationRef{EventID: id, ObservedAt: at, ReceivedAt: at.Add(time.Second), EventType: "vision.motion", Subject: subject, NodeID: node, ZoneID: zone, HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", ActivationID: "activation-1", TrackID: track, SequenceKey: "sequence-1", ChainID: "chain-1", RoutineIDs: []string{"routine-1"}}
}

func knownSubject(id string) episodes.SubjectRef {
	return episodes.SubjectRef{Kind: episodes.SubjectKnown, EntityID: id}
}
func unknownSubject() episodes.SubjectRef { return episodes.SubjectRef{Kind: episodes.SubjectUnknown} }

func fixtureEpisode(id string, values []episodes.ObservationRef, revision uint64) episodes.EpisodeSnapshot {
	if revision == 0 {
		revision = 1
	}
	episode := episodes.Episode{ID: episodes.EpisodeID(id), Status: episodes.StatusOpen, CreatedAt: values[0].ObservedAt, StartedAt: values[0].ObservedAt, LastObservedAt: values[len(values)-1].ObservedAt, StatusChangedAt: values[0].ObservedAt, Observations: append([]episodes.ObservationRef(nil), values...), Revision: revision}
	for _, value := range values {
		seenSubject := false
		for _, subject := range episode.Subjects {
			if subject.Kind == value.Subject.Kind && subject.EntityID == value.Subject.EntityID {
				seenSubject = true
			}
		}
		if !seenSubject {
			episode.Subjects = append(episode.Subjects, value.Subject)
		}
		seenNode := false
		for _, node := range episode.Nodes {
			if node.ID == value.NodeID && node.ZoneID == value.ZoneID {
				seenNode = true
			}
		}
		if value.NodeID != "" && !seenNode {
			episode.Nodes = append(episode.Nodes, episodes.NodeRef{ID: value.NodeID, ZoneID: value.ZoneID})
		}
		if value.ChainID != "" {
			found := false
			for _, ref := range episode.ChainRefs {
				if ref.ID == value.ChainID {
					found = true
				}
			}
			if !found {
				episode.ChainRefs = append(episode.ChainRefs, episodes.ChainRef{ID: value.ChainID})
			}
		}
		for _, routine := range value.RoutineIDs {
			found := false
			for _, ref := range episode.RoutineRefs {
				if ref.ID == routine {
					found = true
				}
			}
			if !found {
				episode.RoutineRefs = append(episode.RoutineRefs, episodes.RoutineRef{ID: routine})
			}
		}
		if value.EventType != "" {
			found := false
			for _, eventType := range episode.EventTypes {
				if eventType == value.EventType {
					found = true
				}
			}
			if !found {
				episode.EventTypes = append(episode.EventTypes, value.EventType)
			}
		}
		if value.ContextQuality != "" {
			found := false
			for _, quality := range episode.ContextQualities {
				if quality == value.ContextQuality {
					found = true
				}
			}
			if !found {
				episode.ContextQualities = append(episode.ContextQualities, value.ContextQuality)
			}
		}
	}
	episode.DurationObserved = episode.LastObservedAt.Sub(episode.StartedAt)
	if err := episode.Validate(); err != nil {
		panic(err)
	}
	return episode
}

func extractFixture(t *testing.T, episode episodes.EpisodeSnapshot, topology TopologyView) FactSet {
	t.Helper()
	set, err := Extract(ExtractionInput{Episode: episode, Topology: topology, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func findFact(set FactSet, code FactCode, subjectKind string) (Fact, bool) {
	for _, fact := range set.Facts {
		if fact.Code == code && fact.Subject.Kind == subjectKind {
			return fact, true
		}
	}
	return Fact{}, false
}
func boolFact(t *testing.T, set FactSet, code FactCode) bool {
	t.Helper()
	fact, ok := findFact(set, code, "episode")
	if !ok {
		t.Fatalf("missing %s", code)
	}
	return fact.Value.BoolValue
}
func intFact(t *testing.T, set FactSet, code FactCode) int64 {
	t.Helper()
	fact, ok := findFact(set, code, "episode")
	if !ok {
		for _, candidate := range set.Facts {
			if candidate.Code == code {
				fact, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		t.Fatalf("missing %s", code)
	}
	return fact.Value.IntValue
}

func episodeTopology() episodes.MapTopology {
	return episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent, "corridor\x00room": episodes.TopologyAdjacent}}
}

func TestQualificationAResidentEntryCorridor(t *testing.T) {
	values := []episodes.ObservationRef{factObservation("a-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-a"), factObservation("a-2", factsBase.Add(10*time.Second), knownSubject("resident-a"), "corridor", "ground", "track-a")}
	set := extractFixture(t, fixtureEpisode("episode-a", values, 1), episodeTopology())
	if intFact(t, set, CodeEpisodeObservationCount) != 2 || intFact(t, set, CodeSpatialTransitionCount) != 1 || intFact(t, set, CodeSpatialReachableTransitionCount) != 1 {
		t.Fatalf("unexpected episode/spatial facts")
	}
	start, _ := findFact(set, CodeSpatialStartNode, "episode")
	end, _ := findFact(set, CodeSpatialEndNode, "episode")
	if start.Value.StringValue != "entry" || end.Value.StringValue != "corridor" {
		t.Fatalf("start/end=%q/%q", start.Value.StringValue, end.Value.StringValue)
	}
	if !boolFact(t, set, CodeIdentityKnownPresent) || boolFact(t, set, CodeIdentityConflict) {
		t.Fatal("identity facts are not neutral")
	}
}

func TestQualificationBUnknownTrackContinuity(t *testing.T) {
	values := []episodes.ObservationRef{factObservation("b-1", factsBase, unknownSubject(), "entry", "ground", "track-17"), factObservation("b-2", factsBase.Add(10*time.Second), unknownSubject(), "corridor", "ground", "track-17")}
	set := extractFixture(t, fixtureEpisode("episode-b", values, 1), episodeTopology())
	if !boolFact(t, set, CodeContinuitySharedTrack) || !boolFact(t, set, CodeContinuityMultipleNodesSameTrack) || !boolFact(t, set, CodeIdentityUnknownPresent) {
		t.Fatal("unknown track facts missing")
	}
}

func TestQualificationCUncertainIdentity(t *testing.T) {
	value := factObservation("c-1", factsBase, episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, "entry", "ground", "track-c")
	set := extractFixture(t, fixtureEpisode("episode-c", []episodes.ObservationRef{value}, 1), nil)
	fact, ok := findFact(set, CodeIdentityCandidateEntitySet, "episode")
	if !ok || !reflect.DeepEqual(fact.Value.StringSetValue, []string{"resident-a", "resident-b"}) {
		t.Fatalf("candidate set=%v", fact.Value.StringSetValue)
	}
	if boolFact(t, set, CodeIdentityKnownPresent) || !boolFact(t, set, CodeIdentityUncertainPresent) {
		t.Fatal("uncertain identity was selected")
	}
}

func TestQualificationDTwoKnownEntitiesAreNotConflict(t *testing.T) {
	values := []episodes.ObservationRef{factObservation("d-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-a"), factObservation("d-2", factsBase, knownSubject("resident-b"), "entry", "ground", "track-b")}
	set := extractFixture(t, fixtureEpisode("episode-d", values, 1), nil)
	if !boolFact(t, set, CodeIdentityMultipleKnownEntities) || boolFact(t, set, CodeIdentityConflict) || len(set.Conflicts) != 0 {
		t.Fatalf("simultaneous entities incorrectly conflicted: %+v", set.Conflicts)
	}
}

func TestQualificationEAndFContextChangeAndConflict(t *testing.T) {
	changed := []episodes.ObservationRef{factObservation("e-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-e"), factObservation("e-2", factsBase.Add(time.Minute), knownSubject("resident-a"), "corridor", "ground", "track-e")}
	changed[0].HouseMode = "away"
	changed[1].HouseMode = "home"
	set := extractFixture(t, fixtureEpisode("episode-e", changed, 1), nil)
	if !boolFact(t, set, CodeContextHouseModeChanged) || boolFact(t, set, CodeContextHouseModeConflict) {
		t.Fatal("ordered mode change became conflict")
	}
	conflicting := []episodes.ObservationRef{changed[0], changed[0]}
	conflicting[1] = changed[1]
	conflicting[1].EventID = "f-2"
	conflicting[1].ObservedAt = factsBase
	conflicting[1].ReceivedAt = factsBase.Add(2 * time.Second)
	set = extractFixture(t, fixtureEpisode("episode-f", conflicting, 1), nil)
	if !boolFact(t, set, CodeContextHouseModeConflict) || len(set.Conflicts) == 0 {
		t.Fatal("same-instant mode conflict was not preserved")
	}
}

func TestQualificationGAndHMissingContextAndTopology(t *testing.T) {
	value := factObservation("g-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-g")
	value.HouseMode = ""
	value.Occupancy = ""
	value.ContextQuality = "partial"
	set := extractFixture(t, fixtureEpisode("episode-g", []episodes.ObservationRef{value}, 1), nil)
	if !boolFact(t, set, CodeContextPartialPresent) || !boolFact(t, set, CodeContextMissingPresent) {
		t.Fatal("partial/missing context facts absent")
	}
	values := []episodes.ObservationRef{factObservation("h-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-h"), factObservation("h-2", factsBase.Add(time.Second), knownSubject("resident-a"), "corridor", "ground", "track-h")}
	set = extractFixture(t, fixtureEpisode("episode-h", values, 1), nil)
	if boolFact(t, set, CodeSpatialTopologyAvailable) || intFact(t, set, CodeSpatialUnknownTransitionCount) != 1 || intFact(t, set, CodeSpatialUnreachableTransitionCount) != 0 {
		t.Fatal("missing topology became unreachable")
	}
}

func TestQualificationIAndJSpatialAndDeviation(t *testing.T) {
	values := []episodes.ObservationRef{factObservation("i-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-i"), factObservation("i-2", factsBase.Add(time.Second), knownSubject("resident-a"), "room", "ground", "track-i")}
	top := episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00room": episodes.TopologyUnreachable}}
	set := extractFixture(t, fixtureEpisode("episode-i", values, 1), top)
	if intFact(t, set, CodeSpatialUnreachableTransitionCount) != 1 {
		t.Fatal("unreachable transition not described")
	}
	values[0].Deviation = &episodes.DeviationRef{AssessmentID: "assessment-j", Status: "evaluated", Band: "high", ScorePermille: 700, CoveragePermille: 900, StructuralAvailable: true, TemporalAvailable: true, IntervalAvailable: false}
	set = extractFixture(t, fixtureEpisode("episode-j", values[:1], 1), nil)
	if !boolFact(t, set, CodeMemoryDeviationPresent) || !boolFact(t, set, CodeMemoryDeviationTemporalPositive) {
		t.Fatal("carried deviation facts missing")
	}
	if intFact(t, set, CodeMemoryDeviationMaximumScore) != 0 {
		t.Fatalf("permille helper read as int: %+v", set.Facts)
	}
}

func TestQualificationKAndLDeviationZeroAndMultipleAssessments(t *testing.T) {
	a := factObservation("k-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-k")
	a.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-zero", Status: "evaluated", Band: "aligned", ScorePermille: 0, CoveragePermille: 1000}
	b := a
	b.EventID = "k-2"
	b.ObservedAt = factsBase.Add(time.Second)
	b.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-high", Status: "evaluated", Band: "high", ScorePermille: 900, CoveragePermille: 800}
	set := extractFixture(t, fixtureEpisode("episode-k", []episodes.ObservationRef{a, b}, 1), nil)
	if !boolFact(t, set, CodeMemoryDeviationEvaluated) {
		t.Fatal("zero evaluated deviation confused with absent")
	}
	bands, _ := findFact(set, CodeMemoryDeviationBandSet, "episode")
	if !reflect.DeepEqual(bands.Value.StringSetValue, []string{"aligned", "high"}) {
		t.Fatalf("bands=%v", bands.Value.StringSetValue)
	}
	if len(bands.Provenance) != 2 {
		t.Fatalf("assessment provenance=%d", len(bands.Provenance))
	}
}

func TestQualificationMAndNOutOfOrderAndDiff(t *testing.T) {
	a := factObservation("m-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-m")
	b := factObservation("m-2", factsBase.Add(time.Second), knownSubject("resident-a"), "corridor", "ground", "track-m")
	a.ReceivedAt = factsBase.Add(2 * time.Second)
	b.ReceivedAt = factsBase.Add(time.Second)
	set := extractFixture(t, fixtureEpisode("episode-m", []episodes.ObservationRef{a, b}, 1), episodeTopology())
	if !boolFact(t, set, CodeTemporalOutOfOrderPresent) {
		t.Fatal("received order was not described")
	}
	before := extractFixture(t, fixtureEpisode("episode-n", []episodes.ObservationRef{a}, 1), nil)
	after := extractFixture(t, fixtureEpisode("episode-n", []episodes.ObservationRef{a, b}, 2), episodeTopology())
	diff, err := Diff(before, after)
	if err != nil || len(diff.Added)+len(diff.Changed) == 0 || diff.BeforeEpisodeRevision != 1 || diff.AfterEpisodeRevision != 2 {
		t.Fatalf("diff=%+v err=%v", diff, err)
	}
	if _, err := Diff(before, before); err != nil {
		t.Fatal(err)
	}
}

func TestQualificationOAndPRegistryIdempotenceAndStale(t *testing.T) {
	value := factObservation("o-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-o")
	set := extractFixture(t, fixtureEpisode("episode-o", []episodes.ObservationRef{value}, 1), nil)
	registry := NewRegistry()
	first, err := registry.Apply(set)
	if err != nil || !first.Applied {
		t.Fatalf("first apply=%+v err=%v", first, err)
	}
	revision := registry.Snapshot().Revision
	second, err := registry.Apply(set)
	if err != nil || !second.Idempotent || registry.Snapshot().Revision != revision {
		t.Fatalf("idempotence=%+v err=%v", second, err)
	}
	newValue := value
	newValue.EventID = "o-2"
	newValue.ObservedAt = factsBase.Add(time.Second)
	newer := extractFixture(t, fixtureEpisode("episode-o", []episodes.ObservationRef{value, newValue}, 2), nil)
	if _, err := registry.Apply(newer); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Apply(set); !errors.Is(err, ErrStaleEpisodeRevision) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestQualificationQRSAndTValidationConflictSnapshot(t *testing.T) {
	if err := (FactValue{Kind: ValueInt, StringValue: "wrong"}).Validate(256, 64); !errors.Is(err, ErrInvalidFactValue) {
		t.Fatalf("invalid union err=%v", err)
	}
	for _, code := range AllFactCodes() {
		lowered := strings.ToLower(string(code))
		for _, forbidden := range []string{"intrusion", "threat", "danger", "malicious", "suspicious", "intent", "visitor_expected", "emergency", "attack", "compromise", "safe", "unsafe"} {
			if strings.Contains(lowered, forbidden) {
				t.Fatalf("forbidden code=%s", code)
			}
		}
	}
	for _, definition := range Schema().Definitions {
		lowered := strings.ToLower(definition.Description)
		for _, forbidden := range []string{"intrusion", "threat", "danger", "malicious", "suspicious", "intent", "visitor_expected", "emergency", "attack", "compromise", "safe", "unsafe"} {
			if strings.Contains(lowered, forbidden) {
				t.Fatalf("forbidden description=%s", definition.Description)
			}
		}
	}
	for _, code := range []FactCode{"threat.level", "intent.state", "safe.state"} {
		candidate := FactSchema{Version: "test", Definitions: []FactDefinition{definition(code, ScopeEpisode, ValueString, false, ConflictSingleValue, "neutral")}}
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidFact) {
			t.Fatalf("forbidden schema code %q accepted: %v", code, err)
		}
	}
	if _, ok := Schema().Definition(FactCode("unknown.code")); ok {
		t.Fatal("unknown code in schema")
	}
	value := factObservation("r-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-r")
	mode := value
	mode.EventID = "r-2"
	mode.ObservedAt = factsBase
	mode.HouseMode = "away"
	set := extractFixture(t, fixtureEpisode("episode-r", []episodes.ObservationRef{value, mode}, 1), nil)
	clone := set.Clone()
	clone.Conflicts[0].FactIDs[0] = "mutated"
	if set.Conflicts[0].FactIDs[0] == "mutated" {
		t.Fatal("conflict escaped clone")
	}
	high := value
	high.Deviation = &episodes.DeviationRef{AssessmentID: "t-assessment", Status: "evaluated", Band: "high", ScorePermille: 999, CoveragePermille: 1000, TemporalAvailable: true}
	set = extractFixture(t, fixtureEpisode("episode-t", []episodes.ObservationRef{high, func() episodes.ObservationRef {
		next := high
		next.EventID = "t-2"
		next.ObservedAt = factsBase.Add(time.Second)
		return next
	}()}, 1), nil)
	if !boolFact(t, set, CodeContinuitySharedTrack) || !boolFact(t, set, CodeMemoryDeviationTemporalPositive) {
		t.Fatal("continuity and deviation facts not simultaneous")
	}
}

func TestDeterminismPropertyAndConcurrency(t *testing.T) {
	value := factObservation("det-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-det")
	episode := fixtureEpisode("episode-det", []episodes.ObservationRef{value}, 1)
	first := extractFixture(t, episode, nil)
	second := extractFixture(t, episode, nil)
	if first.Fingerprint != second.Fingerprint || FactSetFingerprint(first) != first.Fingerprint {
		t.Fatal("fact set fingerprint is not deterministic")
	}
	registry := NewRegistry()
	if _, err := registry.Apply(first); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot()
	snapshot.FactSets[0].Facts[0].Provenance[0].SourceID = "mutated"
	fresh := registry.Snapshot()
	if fresh.FactSets[0].Facts[0].Provenance[0].SourceID == "mutated" {
		t.Fatal("snapshot escaped registry")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, err := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
				if err != nil {
					t.Errorf("concurrent extraction: %v", err)
				}
				_ = registry.Snapshot()
			}
		}()
	}
	wg.Wait()
	firstSet := extractFixture(t, fixtureEpisode("episode-concurrent", []episodes.ObservationRef{value}, 1), nil)
	alternateValue := value
	alternateValue.HouseMode = "away"
	secondSet := extractFixture(t, fixtureEpisode("episode-concurrent", []episodes.ObservationRef{alternateValue}, 1), nil)
	concurrent := NewRegistry()
	results := make(chan error, 2)
	wg.Add(2)
	for _, set := range []FactSet{firstSet, secondSet} {
		go func(value FactSet) { defer wg.Done(); _, err := concurrent.Apply(value); results <- err }(set)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent apply successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestPropertyFactInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(39))
	policy := DefaultPolicy()
	for i := 0; i < 30; i++ {
		count := 1 + rng.Intn(5)
		values := make([]episodes.ObservationRef, 0, count)
		for j := 0; j < count; j++ {
			value := factObservation("property-"+fmt.Sprint(i)+"-"+fmt.Sprint(j), factsBase.Add(time.Duration(j)*time.Second), knownSubject("resident-a"), "node-"+fmt.Sprint(j%2), "zone", "track")
			values = append(values, value)
		}
		set := extractFixture(t, fixtureEpisode("episode-property-"+fmt.Sprint(i), values, 1), nil)
		seen := map[FactID]struct{}{}
		for _, fact := range set.Facts {
			if _, ok := seen[fact.ID]; ok {
				t.Fatal("duplicate fact id")
			}
			seen[fact.ID] = struct{}{}
			if err := fact.Validate(Schema(), policy); err != nil {
				t.Fatal(err)
			}
		}
		if FactSetFingerprint(set) != set.Fingerprint {
			t.Fatal("unstable fingerprint")
		}
	}
}

func TestCanonicalRepresentationsRemainStable(t *testing.T) {
	values := []FactValue{
		BoolFactValue(true),
		IntFactValue(42),
		DurationMSFactValue(1000),
		PermilleFactValue(700),
		StringFactValue("alpha"),
		RefFactValue("ref-1"),
		TimestampFactValue(factsBase),
		StringSetFactValue([]string{"alpha", "beta"}),
		StringListFactValue([]string{"entry", "corridor"}),
	}
	for _, value := range values {
		var legacy string
		switch value.Kind {
		case ValueBool:
			if value.BoolValue {
				legacy = "bool:true"
			} else {
				legacy = "bool:false"
			}
		case ValueInt, ValueDurationMS:
			legacy = fmt.Sprintf("%s:%d", value.Kind, value.IntValue)
		case ValuePermille:
			legacy = fmt.Sprintf("permille:%d", value.PermilleValue)
		case ValueString:
			legacy = "string:" + value.StringValue
		case ValueRef:
			legacy = "ref:" + value.RefValue
		case ValueTimestamp:
			legacy = "timestamp:" + value.TimestampValue.UTC().Round(0).Format(time.RFC3339Nano)
		case ValueStringSet:
			legacy = "string_set:" + fmt.Sprintf("%q", value.StringSetValue)
		case ValueStringList:
			legacy = "string_list:" + fmt.Sprintf("%q", value.StringListValue)
		}
		if value.Canonical() != legacy {
			t.Fatalf("canonical changed for %s: got=%q want=%q", value.Kind, value.Canonical(), legacy)
		}
	}
	left := ProvenanceRef{SourceKind: "observation", SourceID: "event-2", SourceRevision: 1, ObservedAt: factsBase.Add(time.Second), AlgorithmID: "algorithm", AlgorithmVersion: "v1"}
	right := ProvenanceRef{SourceKind: "observation", SourceID: "event-10", SourceRevision: 1, ObservedAt: factsBase, AlgorithmID: "algorithm", AlgorithmVersion: "v1"}
	legacyLeft := strings.Join([]string{left.SourceKind, left.SourceID, fmt.Sprint(left.SourceRevision), left.ObservedAt.UTC().Round(0).Format(time.RFC3339Nano), left.AlgorithmID, left.AlgorithmVersion}, "\x00")
	legacyRight := strings.Join([]string{right.SourceKind, right.SourceID, fmt.Sprint(right.SourceRevision), right.ObservedAt.UTC().Round(0).Format(time.RFC3339Nano), right.AlgorithmID, right.AlgorithmVersion}, "\x00")
	legacyComparison := 0
	if legacyLeft < legacyRight {
		legacyComparison = -1
	} else if legacyLeft > legacyRight {
		legacyComparison = 1
	}
	comparison := left.Compare(right)
	if comparison < 0 {
		comparison = -1
	} else if comparison > 0 {
		comparison = 1
	}
	if comparison != legacyComparison {
		t.Fatalf("provenance comparison changed: got=%d want=%d", comparison, legacyComparison)
	}
}

func TestReadinessAndFactSetValidation(t *testing.T) {
	readiness := BuildReadiness(ReadinessInput{SchemaImplemented: true, ExtractionDeterministic: true, ProvenancePreserved: true, ConflictsPreserved: true, UnknownDimensionsSafe: true, DiffImplemented: true, RegistrySafe: true, ConcurrencyValidated: true})
	if !readiness.ReadyForSituationHypotheses || readiness.RuntimeIntegrated || readiness.Durable || readiness.SituationHypothesesImplemented || readiness.SituationInterpretationImplemented || readiness.SecurityAuthority {
		t.Fatalf("readiness=%+v", readiness)
	}
	if err := Schema().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := DefaultPolicy().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIncrementalAppendMatchesFullExtraction(t *testing.T) {
	first := factObservation("incremental-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-inc")
	second := factObservation("incremental-2", factsBase.Add(time.Second), knownSubject("resident-a"), "corridor", "ground", "track-inc")
	second.HouseMode = "away"
	second.Deviation = &episodes.DeviationRef{AssessmentID: "incremental-assessment", Status: "evaluated", Band: "high", ScorePermille: 700, CoveragePermille: 800, TemporalAvailable: true}
	previousEpisode := fixtureEpisode("episode-incremental", []episodes.ObservationRef{first}, 1)
	currentEpisode := fixtureEpisode("episode-incremental", []episodes.ObservationRef{first, second}, 2)
	previous := extractFixture(t, previousEpisode, nil)
	full := extractFixture(t, currentEpisode, nil)
	result, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != IncrementalModeIncremental {
		t.Fatalf("mode=%s reason=%s", result.Mode, result.FallbackReason)
	}
	if result.FactSet.Fingerprint != full.Fingerprint || !reflect.DeepEqual(result.FactSet.Facts, full.Facts) || !reflect.DeepEqual(result.FactSet.Conflicts, full.Conflicts) || !reflect.DeepEqual(BuildExtractionReport(result.FactSet), BuildExtractionReport(full)) {
		for i := 0; i < len(result.FactSet.Facts) && i < len(full.Facts); i++ {
			if !reflect.DeepEqual(result.FactSet.Facts[i], full.Facts[i]) {
				t.Fatalf("incremental fact mismatch at %d: got=%s/%s/%s/%s want=%s/%s/%s/%s lengths=%d/%d", i, result.FactSet.Facts[i].Key, result.FactSet.Facts[i].Code, result.FactSet.Facts[i].Value.Canonical(), result.FactSet.Facts[i].Origin, full.Facts[i].Key, full.Facts[i].Code, full.Facts[i].Value.Canonical(), full.Facts[i].Origin, len(result.FactSet.Facts), len(full.Facts))
			}
		}
		t.Fatalf("incremental result differs: incremental=%s full=%s", result.FactSet.Fingerprint, full.Fingerprint)
	}
	if result.RecomputedFactCount == 0 {
		t.Fatalf("unexpected reuse accounting: %+v", result)
	}
}

func TestIncrementalModesAndFallbacks(t *testing.T) {
	first := factObservation("incremental-mode-1", factsBase, knownSubject("resident-a"), "entry", "ground", "track-inc-mode")
	previousEpisode := fixtureEpisode("episode-incremental-mode", []episodes.ObservationRef{first}, 1)
	previous := extractFixture(t, previousEpisode, nil)
	idempotent, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: previousEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil || idempotent.Mode != IncrementalModeIdempotent || !idempotent.DiffEmpty() {
		t.Fatalf("idempotent=%+v err=%v", idempotent, err)
	}
	changed := first
	changed.HouseMode = "away"
	changedEpisode := fixtureEpisode("episode-incremental-mode", []episodes.ObservationRef{changed}, 2)
	fallback, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: changedEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil || fallback.Mode != IncrementalModeFullFallback || fallback.FallbackReason != "historical_observation_changed" {
		t.Fatalf("historical fallback=%+v err=%v", fallback, err)
	}
	second := factObservation("incremental-mode-2", factsBase.Add(time.Second), knownSubject("resident-a"), "corridor", "ground", "track-inc-mode")
	currentEpisode := fixtureEpisode("episode-incremental-mode", []episodes.ObservationRef{first, second}, 2)
	topologyFallback, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: previous, Topology: episodeTopology(), ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil || topologyFallback.Mode != IncrementalModeFullFallback || topologyFallback.FallbackReason != "topology_identity_unproven" {
		t.Fatalf("topology fallback=%+v err=%v", topologyFallback, err)
	}
	invalidPrevious := previous.Clone()
	invalidPrevious.Fingerprint = "forged"
	invalidFallback, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: invalidPrevious, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
	if err != nil || invalidFallback.Mode != IncrementalModeFullFallback || invalidFallback.FallbackReason != "previous_fact_set_invalid" {
		t.Fatalf("invalid previous fallback=%+v err=%v", invalidFallback, err)
	}
}

func TestIncrementalEquivalenceCorpus(t *testing.T) {
	values := goldenObservations(100)
	values[10].Subject = episodes.SubjectRef{Kind: episodes.SubjectUnknown}
	values[20].Subject = episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}
	values[30].Subject = knownSubject("resident-b")
	values[40].HouseMode = "away"
	values[41].HouseMode = "home"
	values[50].Deviation = &episodes.DeviationRef{AssessmentID: "incremental-corpus-1", Status: "evaluated", Band: "aligned", ScorePermille: 0, CoveragePermille: 1000, TemporalAvailable: true}
	values[51].Deviation = &episodes.DeviationRef{AssessmentID: "incremental-corpus-2", Status: "evaluated", Band: "high", ScorePermille: 900, CoveragePermille: 900, TemporalAvailable: true}
	previousEpisode := fixtureEpisode("episode-incremental-corpus", values[:10], 1)
	currentEpisode := fixtureEpisode("episode-incremental-corpus", values, 2)
	policy := DefaultPolicy()
	policy.MaxProvenancePerFact = len(values)
	previous, err := Extract(ExtractionInput{Episode: previousEpisode, ExtractedAt: factsBase.Add(time.Hour)}, policy)
	if err != nil {
		t.Fatal(err)
	}
	full, err := Extract(ExtractionInput{Episode: currentEpisode, ExtractedAt: factsBase.Add(2 * time.Hour)}, policy)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(2 * time.Hour)}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != IncrementalModeIncremental {
		t.Fatalf("mode=%s reason=%s", result.Mode, result.FallbackReason)
	}
	if !reflect.DeepEqual(result.FactSet, full) {
		t.Fatalf("incremental corpus differs: got=%s want=%s", result.FactSet.Fingerprint, full.Fingerprint)
	}
	for _, count := range []int{10, 99} {
		previousEpisode := fixtureEpisode(fmt.Sprintf("episode-incremental-%d", count), values[:count], 1)
		currentEpisode := fixtureEpisode(fmt.Sprintf("episode-incremental-%d", count), values, 2)
		previous, err := Extract(ExtractionInput{Episode: previousEpisode, ExtractedAt: factsBase.Add(time.Hour)}, policy)
		if err != nil {
			t.Fatal(err)
		}
		full, err := Extract(ExtractionInput{Episode: currentEpisode, ExtractedAt: factsBase.Add(2 * time.Hour)}, policy)
		if err != nil {
			t.Fatal(err)
		}
		result, err := ExtractIncremental(IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(2 * time.Hour)}, policy)
		if err != nil || result.Mode != IncrementalModeIncremental || !reflect.DeepEqual(result.FactSet, full) {
			t.Fatalf("append %d->%d mode=%s err=%v got=%s want=%s", count, len(values), result.Mode, err, result.FactSet.Fingerprint, full.Fingerprint)
		}
	}
}

func (r IncrementalExtractionResult) DiffEmpty() bool {
	return len(r.Diff.Added) == 0 && len(r.Diff.Removed) == 0 && len(r.Diff.Changed) == 0 && len(r.Diff.ConflictsAdded) == 0 && len(r.Diff.ConflictsRemoved) == 0
}

func TestGoldenCorpusDeterministic(t *testing.T) {
	known := func(id string, at time.Time) episodes.ObservationRef {
		return factObservation(id, at, knownSubject("resident-a"), "entry", "ground", "golden-track")
	}
	corpus := []struct {
		name     string
		episode  episodes.EpisodeSnapshot
		topology TopologyView
	}{
		{name: "one", episode: fixtureEpisode("episode-golden-one", []episodes.ObservationRef{known("golden-one-1", factsBase)}, 1)},
		{name: "ten", episode: fixtureEpisode("episode-golden-ten", goldenObservations(10), 1)},
		{name: "hundred", episode: fixtureEpisode("episode-golden-hundred", goldenObservations(100), 1)},
		{name: "known", episode: fixtureEpisode("episode-golden-known", []episodes.ObservationRef{known("golden-known-1", factsBase)}, 1)},
		{name: "unknown", episode: fixtureEpisode("episode-golden-unknown", []episodes.ObservationRef{factObservation("golden-unknown-1", factsBase, unknownSubject(), "entry", "ground", "unknown-track")}, 1)},
		{name: "uncertain", episode: fixtureEpisode("episode-golden-uncertain", []episodes.ObservationRef{{EventID: "golden-uncertain-1", ObservedAt: factsBase, ReceivedAt: factsBase, Subject: episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, NodeID: "entry", ZoneID: "ground"}}, 1)},
		{name: "two-entities", episode: fixtureEpisode("episode-golden-two", []episodes.ObservationRef{known("golden-two-a", factsBase), func() episodes.ObservationRef {
			value := known("golden-two-b", factsBase)
			value.Subject = knownSubject("resident-b")
			value.TrackID = "golden-track-b"
			return value
		}()}, 1)},
		{name: "mode-change", episode: fixtureEpisode("episode-golden-mode", goldenContextObservations(false), 1)},
		{name: "mode-conflict", episode: fixtureEpisode("episode-golden-mode-conflict", goldenContextObservations(true), 1)},
		{name: "topology-missing", episode: fixtureEpisode("episode-golden-topology-missing", goldenTransitionObservations(), 1)},
		{name: "topology-inaccessible", episode: fixtureEpisode("episode-golden-topology-inaccessible", goldenTransitionObservations(), 1), topology: episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyUnreachable}}},
		{name: "partial", episode: fixtureEpisode("episode-golden-partial", []episodes.ObservationRef{func() episodes.ObservationRef {
			value := known("golden-partial-1", factsBase)
			value.ContextQuality = "partial"
			value.HouseMode = ""
			return value
		}()}, 1)},
		{name: "out-of-order", episode: fixtureEpisode("episode-golden-out-of-order", goldenOutOfOrderObservations(), 1)},
		{name: "deviation-zero", episode: fixtureEpisode("episode-golden-zero", goldenDeviationObservations(false), 1)},
		{name: "deviation-positive", episode: fixtureEpisode("episode-golden-positive", goldenDeviationObservations(true), 1)},
		{name: "multiple-assessments", episode: fixtureEpisode("episode-golden-multiple", goldenMultipleAssessments(), 1)},
		{name: "continuity-deviation", episode: fixtureEpisode("episode-golden-continuity", goldenDeviationObservations(true), 1)},
	}
	for _, item := range corpus {
		t.Run(item.name, func(t *testing.T) {
			policy := DefaultPolicy()
			if len(item.episode.Observations) > policy.MaxProvenancePerFact {
				policy.MaxProvenancePerFact = len(item.episode.Observations)
			}
			first, err := Extract(ExtractionInput{Episode: item.episode, Topology: item.topology, ExtractedAt: factsBase.Add(time.Hour)}, policy)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Extract(ExtractionInput{Episode: item.episode, Topology: item.topology, ExtractedAt: factsBase.Add(time.Hour)}, policy)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, second) || first.Fingerprint != FactSetFingerprint(first) || !reflect.DeepEqual(BuildExtractionReport(first), BuildExtractionReport(second)) {
				t.Fatal("golden corpus result is not stable")
			}
		})
	}
}

func TestPerformanceReadiness(t *testing.T) {
	readiness := BuildPerformanceReadiness(PerformanceReadinessInput{
		SemanticEquivalenceValidated:     true,
		GoldenCorpusStable:               true,
		FullExtractionOptimized:          true,
		IncrementalExtractionImplemented: true,
		IncrementalFallbackSafe:          true,
		DiffOptimized:                    true,
		RegistryOptimized:                true,
		SnapshotOptimized:                true,
		DigestCacheSafe:                  true,
		ConcurrencyValidated:             true,
		SchemaFingerprintUnchanged:       true,
		PolicyFingerprintUnchanged:       true,
		FactIdentifiersUnchanged:         true,
	})
	if !readiness.ReadyForSituationHypotheses || readiness.RuntimeIntegrated || readiness.Durable || readiness.SituationHypothesesImplemented || readiness.SecurityAuthority {
		t.Fatalf("performance readiness=%+v", readiness)
	}
	report := BuildPerformanceReport(IncrementalExtractionResult{Mode: IncrementalModeFullFallback, FallbackReason: "historical_observation_changed", FactSet: FactSet{Facts: make([]Fact, 2), Conflicts: make([]ConflictSet, 1)}, RecomputedFactCount: 2})
	if !report.FullFallback || report.ExtractionMode != string(IncrementalModeFullFallback) || report.FactCount != 2 || report.ConflictCount != 1 {
		t.Fatalf("performance report=%+v", report)
	}
}

func goldenObservations(count int) []episodes.ObservationRef {
	values := make([]episodes.ObservationRef, count)
	for i := range values {
		values[i] = factObservation(fmt.Sprintf("golden-%03d", i), factsBase.Add(time.Duration(i)*time.Second), knownSubject("resident-a"), "node", "ground", "golden-track")
	}
	return values
}

func goldenContextObservations(conflict bool) []episodes.ObservationRef {
	first := factObservation("golden-context-1", factsBase, knownSubject("resident-a"), "entry", "ground", "golden-context-track")
	second := factObservation("golden-context-2", factsBase.Add(time.Minute), knownSubject("resident-a"), "corridor", "ground", "golden-context-track")
	first.HouseMode = "away"
	second.HouseMode = "home"
	if conflict {
		second.ObservedAt = first.ObservedAt
		second.ReceivedAt = first.ReceivedAt.Add(time.Second)
	}
	return []episodes.ObservationRef{first, second}
}

func goldenTransitionObservations() []episodes.ObservationRef {
	return []episodes.ObservationRef{factObservation("golden-transition-1", factsBase, knownSubject("resident-a"), "entry", "ground", "golden-transition"), factObservation("golden-transition-2", factsBase.Add(time.Second), knownSubject("resident-a"), "corridor", "ground", "golden-transition")}
}

func goldenOutOfOrderObservations() []episodes.ObservationRef {
	values := goldenTransitionObservations()
	values[0].ReceivedAt = factsBase.Add(2 * time.Second)
	values[1].ReceivedAt = factsBase.Add(time.Second)
	return values
}

func goldenDeviationObservations(positive bool) []episodes.ObservationRef {
	value := factObservation("golden-deviation", factsBase, knownSubject("resident-a"), "entry", "ground", "golden-deviation")
	value.Deviation = &episodes.DeviationRef{AssessmentID: "golden-assessment", Status: "evaluated", Band: "aligned", CoveragePermille: 1000, TemporalAvailable: true}
	if positive {
		value.Deviation.ScorePermille = 700
	}
	return []episodes.ObservationRef{value}
}

func goldenMultipleAssessments() []episodes.ObservationRef {
	values := goldenDeviationObservations(false)
	second := goldenDeviationObservations(true)[0]
	second.EventID = "golden-assessment-2"
	second.ObservedAt = factsBase.Add(time.Second)
	second.Deviation.AssessmentID = "golden-assessment-2"
	second.Deviation.Band = "high"
	values = append(values, second)
	return values
}
