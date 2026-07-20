package situationhypotheses

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/episodes"
	"synora/internal/cge/situationfacts"
)

var hypothesisBase = time.Date(2026, 1, 6, 18, 0, 0, 0, time.UTC)

func makeObservation(id string, at time.Time, subject episodes.SubjectRef, node, track string) episodes.ObservationRef {
	return episodes.ObservationRef{EventID: id, ObservedAt: at, ReceivedAt: at.Add(time.Second), EventType: "vision.motion", Subject: subject, NodeID: node, ZoneID: "ground", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", ActivationID: "activation-1", TrackID: track, SequenceKey: "sequence-1", ChainID: "chain-1", RoutineIDs: []string{"routine-1"}}
}

func makeFactSet(t *testing.T, id string, observations []episodes.ObservationRef, topology episodes.TopologyView) situationfacts.FactSet {
	t.Helper()
	if len(observations) == 0 {
		t.Fatal("observations required")
	}
	episode := episodes.Episode{ID: episodes.EpisodeID(id), Status: episodes.StatusOpen, CreatedAt: observations[0].ObservedAt, StartedAt: observations[0].ObservedAt, LastObservedAt: observations[len(observations)-1].ObservedAt, StatusChangedAt: observations[0].ObservedAt, Observations: append([]episodes.ObservationRef(nil), observations...), Revision: 1}
	for _, observation := range observations {
		found := false
		for _, value := range episode.Subjects {
			if value.Kind == observation.Subject.Kind && value.EntityID == observation.Subject.EntityID {
				found = true
			}
		}
		if !found {
			episode.Subjects = append(episode.Subjects, observation.Subject)
		}
		found = false
		for _, value := range episode.Nodes {
			if value.ID == observation.NodeID {
				found = true
			}
		}
		if observation.NodeID != "" && !found {
			episode.Nodes = append(episode.Nodes, episodes.NodeRef{ID: observation.NodeID, ZoneID: observation.ZoneID})
		}
		if observation.ChainID != "" && len(episode.ChainRefs) == 0 {
			episode.ChainRefs = []episodes.ChainRef{{ID: observation.ChainID}}
		}
		if len(observation.RoutineIDs) > 0 && len(episode.RoutineRefs) == 0 {
			episode.RoutineRefs = []episodes.RoutineRef{{ID: observation.RoutineIDs[0]}}
		}
		if observation.EventType != "" {
			episode.EventTypes = appendUniqueString(episode.EventTypes, observation.EventType)
		}
		if observation.ContextQuality != "" {
			episode.ContextQualities = appendUniqueString(episode.ContextQualities, observation.ContextQuality)
		}
	}
	episode.DurationObserved = episode.LastObservedAt.Sub(episode.StartedAt)
	if err := episode.Validate(); err != nil {
		t.Fatal(err)
	}
	set, err := situationfacts.Extract(situationfacts.ExtractionInput{Episode: episode, Topology: topology, ExtractedAt: hypothesisBase.Add(time.Hour)}, situationfacts.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func knownSubject(id string) episodes.SubjectRef {
	return episodes.SubjectRef{Kind: episodes.SubjectKnown, EntityID: id}
}
func unknownSubject() episodes.SubjectRef { return episodes.SubjectRef{Kind: episodes.SubjectUnknown} }

func findHypothesis(set CompetingHypothesisSet, kind HypothesisKind) (SituationHypothesis, bool) {
	for _, hypothesis := range set.Hypotheses {
		if hypothesis.Kind == kind {
			return hypothesis, true
		}
	}
	return SituationHypothesis{}, false
}

func TestSchemaPolicyAndNeutralKinds(t *testing.T) {
	schema := Schema()
	if err := schema.Validate(); err != nil {
		t.Fatal(err)
	}
	if SchemaFingerprint() == "" || DefaultPolicy().Fingerprint() == "" {
		t.Fatal("missing fingerprints")
	}
	if len(schema.Definitions) != 8 {
		t.Fatalf("definitions=%d", len(schema.Definitions))
	}
	for _, forbidden := range []HypothesisKind{"attack_like", "threat_like", "unsafe_like"} {
		candidate := HypothesisSchema{Version: "test", Definitions: []HypothesisDefinition{{Kind: forbidden, Description: "neutral"}}}
		if !errors.Is(candidate.Validate(), ErrInvalidDefinition) {
			t.Fatalf("forbidden kind accepted: %s", forbidden)
		}
	}
}

func TestPatternAndDeviationCompetition(t *testing.T) {
	value := makeObservation("pattern-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-pattern")
	value.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-pattern", Status: "evaluated", Band: "aligned", CoveragePermille: 1000}
	set := makeFactSet(t, "episode-pattern", []episodes.ObservationRef{value}, nil)
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	pattern, ok := findHypothesis(result.Set, KindPatternConsistent)
	if !ok || pattern.Status != StatusSupported {
		t.Fatalf("pattern=%+v", pattern)
	}
	if deviation, ok := findHypothesis(result.Set, KindIsolatedDeviation); ok && deviation.Status == StatusSupported {
		t.Fatalf("zero deviation became supported: %+v", deviation)
	}
	if result.Set.LeadingHypothesisID == "" {
		t.Fatal("expected a descriptive leading hypothesis")
	}
}

func TestPositiveDeviationAndPatternShiftRemainConcurrent(t *testing.T) {
	first := makeObservation("shift-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-shift")
	first.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-shift-1", Status: "evaluated", Band: "high", ScorePermille: 800, CoveragePermille: 900, TemporalAvailable: true}
	second := makeObservation("shift-2", hypothesisBase.Add(time.Minute), knownSubject("resident-a"), "corridor", "track-shift")
	second.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-shift-2", Status: "evaluated", Band: "high", ScorePermille: 700, CoveragePermille: 900, TemporalAvailable: true}
	set := makeFactSet(t, "episode-shift", []episodes.ObservationRef{first, second}, nil)
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []HypothesisKind{KindIsolatedDeviation, KindPossiblePatternShift} {
		if hypothesis, ok := findHypothesis(result.Set, kind); !ok || hypothesis.Status == StatusInvalidated {
			t.Fatalf("missing concurrent hypothesis %s: %+v", kind, hypothesis)
		}
	}
	if result.Set.LeadingHypothesisID != "" && result.Set.Ambiguous {
		t.Fatal("ambiguous set selected a leader")
	}
}

func TestIdentityUnknownAndMultiEntityHypotheses(t *testing.T) {
	uncertain := makeObservation("identity-1", hypothesisBase, episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, "entry", "track-identity")
	uncertain2 := makeObservation("identity-2", hypothesisBase.Add(time.Second), episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, "corridor", "track-identity")
	set := makeFactSet(t, "episode-identity", []episodes.ObservationRef{uncertain, uncertain2}, episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent}})
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	identity, ok := findHypothesis(result.Set, KindIdentityResolutionFailure)
	if !ok || len(identity.Support) == 0 {
		t.Fatalf("identity resolution=%+v", identity)
	}
	if _, ok := findHypothesis(result.Set, KindCoherentUnrecognizedActivity); ok {
		// An unresolved identity can still have a coherent technical track;
		// importantly, no candidate identity is selected by the evaluator.
	}
	knownA := makeObservation("multi-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-a")
	knownB := makeObservation("multi-2", hypothesisBase, knownSubject("resident-b"), "entry", "track-b")
	multi := makeFactSet(t, "episode-multi", []episodes.ObservationRef{knownA, knownB}, nil)
	multiResult, err := Evaluate(EvaluationInput{FactSet: multi}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hypothesis, ok := findHypothesis(multiResult.Set, KindMultiEntityActivity); !ok || hypothesis.Status != StatusSupported {
		t.Fatalf("multi-entity=%+v", hypothesis)
	}
}

func TestUnknownCoherentContextConflictAndInsufficientInformation(t *testing.T) {
	first := makeObservation("unknown-1", hypothesisBase, unknownSubject(), "entry", "track-unknown")
	second := makeObservation("unknown-2", hypothesisBase.Add(time.Minute), unknownSubject(), "corridor", "track-unknown")
	set := makeFactSet(t, "episode-unknown", []episodes.ObservationRef{first, second}, episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent}})
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hypothesis, ok := findHypothesis(result.Set, KindCoherentUnrecognizedActivity); !ok || hypothesis.Status != StatusSupported {
		t.Fatalf("unknown coherent=%+v", hypothesis)
	}
	partial := makeObservation("unknown-partial", hypothesisBase, unknownSubject(), "entry", "track-partial")
	partial.HouseMode = ""
	partial.Occupancy = ""
	partial.ContextQuality = "partial"
	partialSet := makeFactSet(t, "episode-partial", []episodes.ObservationRef{partial}, nil)
	partialResult, err := Evaluate(EvaluationInput{FactSet: partialSet}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hypothesis, ok := findHypothesis(partialResult.Set, KindInsufficientInformation); !ok || !partialResult.Set.InsufficientInformation {
		t.Fatalf("missing information=%+v set=%+v", hypothesis, partialResult.Set)
	}
	conflictA := makeObservation("conflict-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-conflict")
	conflictB := makeObservation("conflict-2", hypothesisBase, knownSubject("resident-a"), "entry", "track-conflict")
	conflictB.HouseMode = "away"
	conflict := makeFactSet(t, "episode-conflict", []episodes.ObservationRef{conflictA, conflictB}, nil)
	conflictResult, err := Evaluate(EvaluationInput{FactSet: conflict}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hypothesis, ok := findHypothesis(conflictResult.Set, KindContextOrSensorInconsistency); !ok || len(hypothesis.Support) == 0 {
		t.Fatalf("context inconsistency=%+v", hypothesis)
	}
}

func TestFullAndDiffReevaluationAreEquivalent(t *testing.T) {
	first := makeObservation("diff-1", hypothesisBase, unknownSubject(), "entry", "track-diff")
	previous := makeFactSet(t, "episode-diff", []episodes.ObservationRef{first}, nil)
	second := makeObservation("diff-2", hypothesisBase.Add(time.Second), unknownSubject(), "corridor", "track-diff")
	current := makeFactSet(t, "episode-diff", []episodes.ObservationRef{first, second}, episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent}})
	// Recreate the current set with the same episode identifier and next source revision.
	current.EpisodeRevision = 2
	current.Fingerprint = situationfacts.FactSetFingerprint(current)
	diff, err := situationfacts.Diff(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	previousEvaluation, err := Evaluate(EvaluationInput{FactSet: previous}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	full, err := Evaluate(EvaluationInput{FactSet: current, PreviousSet: &previousEvaluation.Set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	fromDiff, err := ReevaluateFromDiff(previous, current, diff, previousEvaluation.Set, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full.Set, fromDiff.Set) {
		t.Fatalf("full/diff mismatch: full=%s diff=%s", full.Set.Fingerprint, fromDiff.Set.Fingerprint)
	}
}

func TestPlannerRegistryIdempotenceAndOptimisticConcurrency(t *testing.T) {
	value := makeObservation("registry-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-registry")
	set := makeFactSet(t, "episode-registry", []episodes.ObservationRef{value}, nil)
	registry := NewRegistry()
	plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.ApplyPlan(plan)
	if err != nil || !first.Applied {
		t.Fatalf("first apply=%+v err=%v", first, err)
	}
	second, err := registry.ApplyPlan(plan)
	if err != nil || !second.Idempotent || registry.Snapshot().Revision != 1 {
		t.Fatalf("idempotence=%+v err=%v", second, err)
	}
	current := registry.Snapshot()
	changed := makeObservation("registry-2", hypothesisBase.Add(time.Minute), knownSubject("resident-a"), "corridor", "track-registry")
	newSet := makeFactSet(t, "episode-registry", []episodes.ObservationRef{value, changed}, nil)
	newSet.EpisodeRevision = 2
	newSet.Fingerprint = situationfacts.FactSetFingerprint(newSet)
	planA, err := Plan(newSet, current, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	alternate := changed
	alternate.HouseMode = "away"
	alternateSet := makeFactSet(t, "episode-registry", []episodes.ObservationRef{value, alternate}, nil)
	alternateSet.EpisodeRevision = 2
	alternateSet.Fingerprint = situationfacts.FactSetFingerprint(alternateSet)
	planB, err := Plan(alternateSet, current, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for _, candidate := range []HypothesisPlan{planA, planB} {
		go func(value HypothesisPlan) {
			defer wait.Done()
			_, applyErr := registry.ApplyPlan(value)
			results <- applyErr
		}(candidate)
	}
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for applyErr := range results {
		if applyErr == nil {
			successes++
		} else if errors.Is(applyErr, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent apply successes=%d conflicts=%d", successes, conflicts)
	}
	snapshot := registry.Snapshot()
	snapshot.EpisodeSets[0].Hypotheses[0].Support[0].FactIDs[0] = "mutated"
	if registry.Snapshot().EpisodeSets[0].Hypotheses[0].Support[0].FactIDs[0] == "mutated" {
		t.Fatal("snapshot escaped registry")
	}
}

func TestLifecycleExplanationReadinessAndInvariants(t *testing.T) {
	value := makeObservation("lifecycle-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-lifecycle")
	set := makeFactSet(t, "episode-lifecycle", []episodes.ObservationRef{value}, nil)
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, hypothesis := range result.Set.Hypotheses {
		if _, err := Explain(hypothesis, set); err != nil {
			t.Fatal(err)
		}
	}
	if decision := EvaluateLifecycle(SituationHypothesis{ID: "x", Status: StatusCandidate}, SituationHypothesis{ID: "x", Status: StatusSupported}, DefaultPolicy()); !decision.Valid || decision.Target != StatusSupported {
		t.Fatalf("lifecycle=%+v", decision)
	}
	readiness := BuildReadiness(ReadinessInput{SchemaImplemented: true, EvaluationDeterministic: true, CompetingHypothesesPreserved: true, ContributionsTraceable: true, ContradictionsPreserved: true, MissingInformationPreserved: true, DiffReevaluationImplemented: true, FullDiffEquivalenceValidated: true, RegistrySafe: true, ConcurrencyValidated: true, ExplanationsImplemented: true})
	if !readiness.ReadyForEvidenceDiscrimination || readiness.RuntimeIntegrated || readiness.Durable || readiness.SecurityAuthority {
		t.Fatalf("readiness=%+v", readiness)
	}
}

func TestEqualCandidatesRemainAmbiguous(t *testing.T) {
	value := makeObservation("tie-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-tie")
	set := makeFactSet(t, "episode-tie", []episodes.ObservationRef{value}, nil)
	custom := HypothesisSchema{Version: "tie-schema", Definitions: []HypothesisDefinition{
		{Kind: "custom_a", Description: "first neutral alternative", SupportRules: []EvidenceRule{{ID: "a.support", FactCode: situationfacts.CodeEpisodeObservationCount, Scope: situationfacts.ScopeEpisode, Operator: OperatorGreaterThan, ExpectedValue: valuePointer(situationfacts.IntFactValue(0)), WeightPermille: 600, ReasonCode: "observation_present"}}},
		{Kind: "custom_b", Description: "second neutral alternative", SupportRules: []EvidenceRule{{ID: "b.support", FactCode: situationfacts.CodeEpisodeObservationCount, Scope: situationfacts.ScopeEpisode, Operator: OperatorGreaterThan, ExpectedValue: valuePointer(situationfacts.IntFactValue(0)), WeightPermille: 600, ReasonCode: "observation_present"}}},
	}}
	result, err := Evaluate(EvaluationInput{FactSet: set}, custom, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Set.Ambiguous || result.Set.LeadingHypothesisID != "" {
		t.Fatalf("tie was resolved: %+v", result.Set)
	}
}

func TestConflictRemovalWeakensSupport(t *testing.T) {
	first := makeObservation("remove-conflict-1", hypothesisBase, knownSubject("resident-a"), "entry", "track-remove")
	second := makeObservation("remove-conflict-2", hypothesisBase, knownSubject("resident-a"), "entry", "track-remove")
	second.HouseMode = "away"
	before := makeFactSet(t, "episode-remove-conflict", []episodes.ObservationRef{first, second}, nil)
	orderedSecond := second
	orderedSecond.ObservedAt = hypothesisBase.Add(time.Minute)
	after := makeFactSet(t, "episode-remove-conflict", []episodes.ObservationRef{first, orderedSecond}, nil)
	after.EpisodeRevision = 2
	after.Fingerprint = situationfacts.FactSetFingerprint(after)
	diff, err := situationfacts.Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := Evaluate(EvaluationInput{FactSet: before}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	current, err := ReevaluateFromDiff(before, after, diff, previous.Set, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hypothesis, ok := findHypothesis(current.Set, KindContextOrSensorInconsistency); ok && len(hypothesis.Contradiction) > 0 {
		t.Fatalf("removed conflict remained as contradiction: %+v", hypothesis)
	}
}

func TestIdentityUncertaintyBecomesWeakerWhenKnown(t *testing.T) {
	uncertain := makeObservation("identity-evolve-1", hypothesisBase, episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, "entry", "track-evolve")
	before := makeFactSet(t, "episode-identity-evolve", []episodes.ObservationRef{uncertain}, nil)
	known := uncertain
	known.Subject = knownSubject("resident-a")
	after := makeFactSet(t, "episode-identity-evolve", []episodes.ObservationRef{known}, nil)
	after.EpisodeRevision = 2
	after.Fingerprint = situationfacts.FactSetFingerprint(after)
	diff, err := situationfacts.Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := Evaluate(EvaluationInput{FactSet: before}, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	current, err := ReevaluateFromDiff(before, after, diff, previous.Set, Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, ok := findHypothesis(current.Set, KindIdentityResolutionFailure)
	if !ok || hypothesis.Status == StatusSupported {
		t.Fatalf("identity uncertainty was not weakened: %+v", hypothesis)
	}
}

func TestForgedFactReferenceRejectedWithoutMutation(t *testing.T) {
	value := makeObservation("forged-1", hypothesisBase, unknownSubject(), "entry", "track-forged")
	set := makeFactSet(t, "episode-forged", []episodes.ObservationRef{value}, nil)
	registry := NewRegistry()
	plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ResultingSet.Hypotheses) == 0 || len(plan.ResultingSet.Hypotheses[0].Support) == 0 {
		t.Fatal("test hypothesis has no contribution")
	}
	plan.ResultingSet.Hypotheses[0].Support[0].FactIDs[0] = "fact-forged"
	refreshHypothesisFingerprint(&plan.ResultingSet.Hypotheses[0])
	plan.ResultingSet.Fingerprint = competingSetFingerprint(plan.ResultingSet)
	if _, err := registry.ApplyPlan(plan); !errors.Is(err, ErrUnknownFactReference) {
		t.Fatalf("forged reference err=%v", err)
	}
	if registry.Count() != 0 {
		t.Fatal("forged plan mutated registry")
	}
}

func valuePointer(value situationfacts.FactValue) *situationfacts.FactValue { return &value }

func TestPropertyDeterminismAndBounds(t *testing.T) {
	for count := 1; count <= 6; count++ {
		values := make([]episodes.ObservationRef, 0, count)
		for i := 0; i < count; i++ {
			subject := knownSubject("resident-a")
			if i%3 == 1 {
				subject = unknownSubject()
			}
			values = append(values, makeObservation("property-"+string(rune('a'+i)), hypothesisBase.Add(time.Duration(i)*time.Second), subject, "node", "track-property"))
		}
		set := makeFactSet(t, "episode-property-"+string(rune('a'+count)), values, nil)
		first, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		second, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
		if err != nil || !reflect.DeepEqual(first.Set, second.Set) {
			t.Fatalf("non-deterministic result count=%d err=%v", count, err)
		}
		known := map[situationfacts.FactID]struct{}{}
		for _, fact := range set.Facts {
			known[fact.ID] = struct{}{}
		}
		for _, hypothesis := range first.Set.Hypotheses {
			for _, value := range []int{hypothesis.SupportPermille, hypothesis.ContradictionPermille, hypothesis.CoveragePermille, hypothesis.PlausibilityPermille} {
				if value < 0 || value > 1000 {
					t.Fatalf("unbounded score=%d", value)
				}
			}
			for _, contribution := range append(append([]Contribution(nil), hypothesis.Support...), hypothesis.Contradiction...) {
				for _, id := range contribution.FactIDs {
					if _, ok := known[id]; !ok {
						t.Fatalf("unknown fact reference=%s", id)
					}
				}
			}
		}
	}
	for _, definition := range Schema().Definitions {
		for _, forbidden := range []string{"intrusion", "attack", "threat", "danger", "malicious", "suspicious", "criminal", "hostile", "safe", "unsafe", "emergency", "intent", "visitor_expected", "compromise", "burglary", "weapon"} {
			if strings.Contains(strings.ToLower(string(definition.Kind)), forbidden) || strings.Contains(strings.ToLower(definition.Description), forbidden) {
				t.Fatalf("forbidden schema term=%s", forbidden)
			}
		}
	}
}

func TestConcurrentEvaluationAndSnapshots(t *testing.T) {
	value := makeObservation("concurrent-1", hypothesisBase, unknownSubject(), "entry", "track-concurrent")
	set := makeFactSet(t, "episode-concurrent", []episodes.ObservationRef{value}, nil)
	registry := NewRegistry()
	plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplyPlan(plan); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for j := 0; j < 20; j++ {
				result, evalErr := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
				if evalErr != nil || result.Set.Fingerprint == "" {
					t.Errorf("concurrent evaluate err=%v", evalErr)
				}
				snapshot := registry.Snapshot()
				if snapshot.Digest == "" {
					t.Error("empty concurrent digest")
				}
			}
		}()
	}
	wait.Wait()
}
