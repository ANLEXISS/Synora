package episodes

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
)

var testBase = time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC)

func observation(id string, at time.Time, subject SubjectRef, node, zone, track string) ObservationRef {
	return ObservationRef{EventID: id, ObservedAt: at, ReceivedAt: at.Add(time.Second), EventType: "vision.motion", Subject: subject, NodeID: node, ZoneID: zone, HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", TrackID: track, ActivationID: "activation-1", SequenceKey: "sequence-1", ChainID: "chain-1", RoutineIDs: []string{"routine-1"}}
}

func known(id string) SubjectRef { return SubjectRef{Kind: SubjectKnown, EntityID: id} }
func unknown() SubjectRef        { return SubjectRef{Kind: SubjectUnknown} }

func topology() MapTopology {
	return MapTopology{Relationships: map[string]TopologyRelationship{"entry\x00corridor": TopologyAdjacent, "corridor\x00room": TopologyAdjacent}}
}

type testFataler interface {
	Helper()
	Fatalf(string, ...any)
}

func ingest(t testFataler, registry *Registry, value ObservationRef, top TopologyView, policy Policy) ApplyResult {
	t.Helper()
	plan, err := PlanIngest(registry.Snapshot(), value, top, policy)
	if err != nil {
		t.Fatalf("plan %s: %v", value.EventID, err)
	}
	result, err := registry.ApplyIngestPlan(plan, value, value.ObservedAt)
	if err != nil {
		t.Fatalf("apply %s: %v (%s)", value.EventID, err, plan.Decision)
	}
	return result
}

func TestQualificationAEntryThenCorridorOneEpisode(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("a-1", testBase, known("resident-a"), "entry", "ground", "track-a"), topology(), policy)
	second := observation("a-2", testBase.Add(10*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	plan, err := PlanIngest(registry.Snapshot(), second, topology(), policy)
	if err != nil || plan.Decision != DecisionAttachExisting {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	if _, err := registry.ApplyIngestPlan(plan, second, second.ObservedAt); err != nil {
		t.Fatal(err)
	}
	if registry.Count() != 1 || len(registry.List()[0].Observations) != 2 {
		t.Fatalf("want one episode with two observations")
	}
}

func TestQualificationBNextDayCreatesEpisode(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("b-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	b := observation("b-2", testBase.Add(24*time.Hour), known("resident-a"), "entry", "ground", "track-b")
	plan, err := PlanIngest(registry.Snapshot(), b, nil, policy)
	if err != nil || plan.Decision != DecisionCreateEpisode {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	ingest(t, registry, b, nil, policy)
	if registry.Count() != 2 {
		t.Fatalf("count=%d", registry.Count())
	}
}

func TestQualificationCTwoKnownResidentsRemainDistinct(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("c-1", testBase, known("resident-a"), "entry", "ground", ""), nil, policy)
	b := observation("c-2", testBase.Add(time.Second), known("resident-b"), "entry", "ground", "")
	plan, err := PlanIngest(registry.Snapshot(), b, nil, policy)
	if err != nil || plan.Decision != DecisionCreateEpisode {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	ingest(t, registry, b, nil, policy)
	if registry.Count() != 2 {
		t.Fatal("known residents fused")
	}
}

func TestQualificationDUnknownContinuousTrack(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("d-1", testBase, unknown(), "entry", "ground", "track-17"), nil, policy)
	b := observation("d-2", testBase.Add(20*time.Second), unknown(), "corridor", "ground", "track-17")
	plan, err := PlanIngest(registry.Snapshot(), b, topology(), policy)
	if err != nil || plan.Decision != DecisionAttachExisting {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	ingest(t, registry, b, topology(), policy)
	if registry.Count() != 1 {
		t.Fatal("continuous track split")
	}
}

func TestQualificationEUnknownWithoutContinuityDoesNotForceMerge(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("e-1", testBase, unknown(), "entry", "ground", ""), nil, policy)
	b := observation("e-2", testBase.Add(10*time.Second), unknown(), "corridor", "ground", "")
	b.ActivationID = ""
	b.SequenceKey = ""
	b.ChainID = ""
	b.RoutineIDs = nil
	plan, err := PlanIngest(registry.Snapshot(), b, topology(), policy)
	if err != nil || plan.Decision != DecisionCreateEpisode {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
}

func TestQualificationFAmbiguousUncertainIdentityDoesNotMutate(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("f-1", testBase, known("resident-a"), "entry", "ground", "track-a"), nil, policy)
	ingest(t, registry, observation("f-2", testBase.Add(time.Second), known("resident-b"), "entry", "ground", "track-b"), nil, policy)
	uncertain := observation("f-3", testBase.Add(2*time.Second), SubjectRef{Kind: SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}, "entry", "ground", "")
	before := RegistryDigest(registry.Snapshot())
	plan, err := PlanIngest(registry.Snapshot(), uncertain, nil, policy)
	if err != nil || plan.Decision != DecisionAmbiguous || len(plan.Candidates) < 2 {
		t.Fatalf("decision=%s candidates=%d err=%v", plan.Decision, len(plan.Candidates), err)
	}
	if _, err := registry.ApplyIngestPlan(plan, uncertain, uncertain.ObservedAt); !errors.Is(err, ErrAmbiguousPlan) {
		t.Fatalf("err=%v", err)
	}
	if RegistryDigest(registry.Snapshot()) != before || registry.Count() != 2 {
		t.Fatal("ambiguous plan mutated registry")
	}
}

func TestQualificationGDuplicateIsIdempotent(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("g-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	before := RegistryDigest(registry.Snapshot())
	revision := registry.Snapshot().Revision
	plan, err := PlanIngest(registry.Snapshot(), a, nil, policy)
	if err != nil || plan.Decision != DecisionDuplicate {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	result, err := registry.ApplyIngestPlan(plan, a, a.ObservedAt)
	if err != nil || !result.Idempotent || RegistryDigest(registry.Snapshot()) != before || registry.Snapshot().Revision != revision {
		t.Fatalf("duplicate changed state: result=%+v err=%v", result, err)
	}
}

func TestQualificationHOutOfOrderIsSorted(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("h-0", testBase, known("resident-a"), "entry", "ground", "track-a")
	b := observation("h-2", testBase.Add(20*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	c := observation("h-1", testBase.Add(10*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	ingest(t, registry, a, topology(), policy)
	ingest(t, registry, b, topology(), policy)
	ingest(t, registry, c, topology(), policy)
	episode := registry.List()[0]
	if episode.StartedAt != testBase || episode.LastObservedAt != testBase.Add(20*time.Second) || episode.DurationObserved != 20*time.Second {
		t.Fatalf("bad bounds: %+v", episode)
	}
	for i, value := range episode.Observations {
		if value.EventID != []string{"h-0", "h-1", "h-2"}[i] {
			t.Fatalf("order=%v", episode.Observations)
		}
	}
}

func TestQualificationIPartialContextDoesNotMismatch(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("i-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	b := a
	b.EventID = "i-2"
	b.ObservedAt = testBase.Add(10 * time.Second)
	b.HouseMode = ""
	b.Occupancy = ""
	b.ContextQuality = "partial"
	plan, err := PlanIngest(registry.Snapshot(), b, nil, policy)
	if err != nil || plan.Decision != DecisionAttachExisting {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
}

func TestQualificationJUnreachableTopologyRejects(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("j-1", testBase, known("resident-a"), "entry", "ground", "track-a"), nil, policy)
	top := MapTopology{Relationships: map[string]TopologyRelationship{"entry\x00room": TopologyUnreachable}}
	b := observation("j-2", testBase.Add(5*time.Second), known("resident-a"), "room", "ground", "track-b")
	plan, err := PlanIngest(registry.Snapshot(), b, top, policy)
	if err != nil || plan.Decision != DecisionCreateEpisode {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Eligible {
		t.Fatalf("candidate was not rejected: %+v", plan.Candidates)
	}
}

func TestQualificationKMaxDurationCreatesNewEpisode(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("k-1", testBase, known("resident-a"), "entry", "ground", "track-a"), nil, policy)
	b := observation("k-2", testBase.Add(policy.MaxEpisodeDuration+time.Second), known("resident-a"), "corridor", "ground", "track-a")
	plan, err := PlanIngest(registry.Snapshot(), b, topology(), policy)
	if err != nil || plan.Decision != DecisionCreateEpisode {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
}

func TestQualificationLLifecycleAndReactivation(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("l-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	snapshot := registry.Snapshot()
	batch := EvaluateLifecycle(snapshot, testBase.Add(policy.QuiescentAfter), policy)
	if len(batch.Changes) != 1 || batch.Changes[0].To != StatusQuiescent {
		t.Fatalf("batch=%+v", batch)
	}
	if _, err := registry.ApplyLifecycleBatch(batch, "test"); err != nil {
		t.Fatal(err)
	}
	b := observation("l-2", testBase.Add(policy.QuiescentAfter+time.Second), known("resident-a"), "corridor", "ground", "track-a")
	ingest(t, registry, b, topology(), policy)
	if registry.List()[0].Status != StatusOpen {
		t.Fatal("quiescent episode did not reopen")
	}
	batch = EvaluateLifecycle(registry.Snapshot(), testBase.Add(policy.CloseAfter+time.Minute), policy)
	if len(batch.Changes) != 1 || batch.Changes[0].To != StatusQuiescent {
		t.Fatalf("open should quiesce before close: %+v", batch)
	}
	if _, err := registry.ApplyLifecycleBatch(batch, "test"); err != nil {
		t.Fatal(err)
	}
	closeBatch := EvaluateLifecycle(registry.Snapshot(), testBase.Add(policy.CloseAfter+time.Minute+policy.CloseAfter), policy)
	if len(closeBatch.Changes) != 1 || closeBatch.Changes[0].To != StatusClosed {
		t.Fatalf("batch=%+v", closeBatch)
	}
}

func TestQualificationMAndNLateObservations(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("m-0", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	late := observation("m-1", testBase.Add(10*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	ingest(t, registry, late, nil, policy)
	within := observation("m-before", testBase.Add(8*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	plan, err := PlanIngest(registry.Snapshot(), within, nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplyIngestPlan(plan, within, within.ObservedAt); err != nil {
		t.Fatalf("within grace: %v", err)
	}
	out := observation("n-before", testBase.Add(-time.Minute), known("resident-a"), "entry", "ground", "track-a")
	plan, err = PlanIngest(registry.Snapshot(), out, nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplyIngestPlan(plan, out, out.ObservedAt); !errors.Is(err, ErrLateObservationOutsideGrace) && !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("outside grace err=%v plan=%s", err, plan.Decision)
	}
}

func TestQualificationODeviationDoesNotSplitStrongContinuity(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("o-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	a.Deviation = &DeviationRef{AssessmentID: "assessment-1", Status: "evaluated", Band: "high", ScorePermille: 990, CoveragePermille: 1000, StructuralAvailable: true, TemporalAvailable: true, IntervalAvailable: true}
	ingest(t, registry, a, nil, policy)
	b := observation("o-2", testBase.Add(10*time.Second), known("resident-a"), "corridor", "ground", "track-a")
	b.Deviation = &DeviationRef{AssessmentID: "assessment-2", Status: "evaluated", Band: "high", ScorePermille: 1000, CoveragePermille: 1000, StructuralAvailable: true, TemporalAvailable: true, IntervalAvailable: true}
	plan, err := PlanIngest(registry.Snapshot(), b, topology(), policy)
	if err != nil || plan.Decision != DecisionAttachExisting {
		t.Fatalf("decision=%s err=%v", plan.Decision, err)
	}
}

func TestDeterminismAndDefensiveBoundaries(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("det-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	snapshot := registry.Snapshot()
	clone := snapshot.Clone()
	planA, err := PlanIngest(snapshot, observation("det-2", testBase.Add(time.Second), known("resident-a"), "corridor", "ground", "track-a"), nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	planB, err := PlanIngest(clone, observation("det-2", testBase.Add(time.Second), known("resident-a"), "corridor", "ground", "track-a"), nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	if planA.Decision != planB.Decision || planA.SelectedEpisodeID != planB.SelectedEpisodeID || EpisodeFingerprint(snapshot.Episodes[0]) != EpisodeFingerprint(clone.Episodes[0]) {
		t.Fatal("planner or episode fingerprint is not deterministic")
	}
	clone.EventIndex["det-1"] = "changed"
	if snapshot.EventIndex["det-1"] == "changed" {
		t.Fatal("snapshot map escaped")
	}
	policy2 := policy
	policy2.MinAttachScore++
	if policy.Fingerprint() == policy2.Fingerprint() {
		t.Fatal("policy fingerprint did not change")
	}
	idA, _ := DeriveEpisodeID(policy, a)
	idB, _ := DeriveEpisodeID(policy, a)
	if idA != idB {
		t.Fatal("episode id is not deterministic")
	}
}

func TestTypedValidationAndTerminalStates(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	bad := observation("", testBase, known("resident-a"), "", "", "")
	if !errors.Is(bad.Validate(), ErrMissingEventID) {
		t.Fatal("missing event id not typed")
	}
	if !errors.Is((Policy{MinAttachScore: 1001}).Validate(), ErrInvalidPolicy) {
		t.Fatal("invalid policy not typed")
	}
	if validLifecycleTransition(StatusClosed, StatusOpen) || validLifecycleTransition(StatusOpen, StatusOpen) {
		t.Fatal("transition table incorrect")
	}
	if registry.Count() != 0 {
		t.Fatal("unexpected registry state")
	}
}

func TestConcurrencyOptimisticConflictAndNoLeaks(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	a := observation("con-1", testBase, known("resident-a"), "entry", "ground", "track-a")
	ingest(t, registry, a, nil, policy)
	b := observation("con-2", testBase.Add(time.Second), known("resident-a"), "corridor", "ground", "track-a")
	snapshot := registry.Snapshot()
	planA, _ := PlanIngest(snapshot, b, nil, policy)
	planB, _ := PlanIngest(snapshot, b, nil, policy)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	for _, plan := range []IngestPlan{planA, planB} {
		go func(value IngestPlan) {
			defer wg.Done()
			_, err := registry.ApplyIngestPlan(value, b, b.ObservedAt)
			results <- err
		}(plan)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	if got := registry.Snapshot(); len(got.EventIndex) != 2 {
		t.Fatal("concurrent apply lost event")
	}
}

func TestContextTopologyAdapterAndMetrics(t *testing.T) {
	topologySnapshot := cgeTopologySnapshot()
	top, err := NewContextTopology(topologySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if top.Relationship("entry", "corridor") != TopologyAdjacent || top.Relationship("entry", "unknown") != TopologyUnknown {
		t.Fatal("context topology mapping incorrect")
	}
	registry := NewRegistry()
	value, err := BuildObservationRef(ExistingCGEOutput{EventID: "adapt-1", ObservedAt: testBase, EventType: "vision.motion", Subject: unknown()})
	if err != nil {
		t.Fatal(err)
	}
	ingest(t, registry, value, nil, DefaultPolicy())
	metrics := registry.Metrics()
	if metrics.EpisodeCount != 1 || metrics.ObservationCount != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestPropertiesCanonicalOrderBoundsAndTerminality(t *testing.T) {
	policy := DefaultPolicy()
	rng := rand.New(rand.NewSource(38))
	registry := NewRegistryWithPolicy(policy)
	for i := 0; i < 40; i++ {
		at := testBase.Add(time.Duration(rng.Intn(20)) * time.Second)
		value := observation("property-"+time.Duration(i).String(), at, known("resident-a"), "entry", "ground", "track-a")
		value.EventID = "property-" + fmtInt(i)
		if i%3 == 0 {
			value.TrackID = "track-" + fmtInt(i)
		}
		plan, err := PlanIngest(registry.Snapshot(), value, nil, policy)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Decision == DecisionAmbiguous || plan.Decision == DecisionRejected {
			continue
		}
		if _, err := registry.ApplyIngestPlan(plan, value, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, episode := range registry.List() {
		if episode.StartedAt.After(episode.LastObservedAt) {
			t.Fatal("negative episode duration")
		}
		seen := map[string]struct{}{}
		for i, value := range episode.Observations {
			if _, exists := seen[value.EventID]; exists {
				t.Fatal("duplicate event")
			}
			seen[value.EventID] = struct{}{}
			if i > 0 && (value.ObservedAt.Before(episode.Observations[i-1].ObservedAt) || value.ObservedAt.Equal(episode.Observations[i-1].ObservedAt) && value.EventID <= episode.Observations[i-1].EventID) {
				t.Fatal("observations not canonical")
			}
		}
		for _, candidate := range episode.Observations[0].RoutineIDs {
			if candidate == "" {
				t.Fatal("empty routine")
			}
		}
		if episode.Status == StatusClosed && CanTransition(StatusClosed, StatusOpen) {
			t.Fatal("closed is not terminal")
		}
		if episode.Status == StatusInvalidated && CanTransition(StatusInvalidated, StatusOpen) {
			t.Fatal("invalidated is not terminal")
		}
	}
}

func TestConcurrentSnapshotsAndPlans(t *testing.T) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	ingest(t, registry, observation("snap-0", testBase, known("resident-a"), "entry", "ground", "track-a"), nil, policy)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			value := observation("snap-"+fmtInt(index+1), testBase.Add(time.Duration(index+1)*time.Second), known("resident-a"), "corridor", "ground", "track-a")
			plan, err := PlanIngest(registry.Snapshot(), value, nil, policy)
			if err != nil {
				t.Errorf("plan: %v", err)
				return
			}
			_ = plan
		}(i)
	}
	wg.Wait()
	list := registry.List()
	list[0].Observations[0].EventID = "mutated"
	if registry.List()[0].Observations[0].EventID == "mutated" {
		t.Fatal("list leaked mutable observation")
	}
}

func TestReadinessBoundary(t *testing.T) {
	readiness := BuildReadiness(ReadinessInput{DomainImplemented: true, PlannerDeterministic: true, RegistrySafe: true, LifecycleImplemented: true, PartialContextSupported: true, OutOfOrderSupported: true, ConcurrencyValidated: true})
	if !readiness.ReadyForSituationFacts || readiness.RuntimeIntegrated || readiness.Durable || readiness.SituationInferenceImplemented || readiness.SecurityAuthority {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func fmtInt(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	out := make([]byte, 0, 12)
	for value > 0 {
		out = append([]byte{digits[value%10]}, out...)
		value /= 10
	}
	return string(out)
}

func cgeTopologySnapshot() cgecontext.TopologySnapshot {
	return cgecontext.TopologySnapshot{Revision: "test-topology", CapturedAt: testBase, Nodes: []cgecontext.Node{{ID: "corridor", Kind: cgecontext.NodeCorridor}, {ID: "entry", Kind: cgecontext.NodeEntrance}}, Edges: []cgecontext.Edge{{From: "corridor", To: "entry", TraversalKind: cgecontext.TraversalWalk}}}
}
