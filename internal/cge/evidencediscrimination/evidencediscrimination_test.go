package evidencediscrimination

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/episodes"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

var evidenceBase = time.Date(2026, 1, 7, 18, 0, 0, 0, time.UTC)

func evidenceObservation(id string, at time.Time, subject episodes.SubjectRef, node, track string) episodes.ObservationRef {
	return episodes.ObservationRef{EventID: id, ObservedAt: at, ReceivedAt: at.Add(time.Second), EventType: "vision.motion", Subject: subject, NodeID: node, ZoneID: "ground", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", ActivationID: "activation-1", TrackID: track, SequenceKey: "sequence-1", ChainID: "chain-1", RoutineIDs: []string{"routine-1"}}
}
func evidenceEpisode(id string, values []episodes.ObservationRef, revision uint64) episodes.EpisodeSnapshot {
	ep := episodes.Episode{ID: episodes.EpisodeID(id), Status: episodes.StatusOpen, CreatedAt: values[0].ObservedAt, StartedAt: values[0].ObservedAt, LastObservedAt: values[len(values)-1].ObservedAt, StatusChangedAt: values[0].ObservedAt, Observations: append([]episodes.ObservationRef(nil), values...), Revision: revision}
	for _, v := range values {
		seen := false
		for _, s := range ep.Subjects {
			if s.Kind == v.Subject.Kind && s.EntityID == v.Subject.EntityID {
				seen = true
			}
		}
		if !seen {
			ep.Subjects = append(ep.Subjects, v.Subject)
		}
		seen = false
		for _, n := range ep.Nodes {
			if n.ID == v.NodeID {
				seen = true
			}
		}
		if v.NodeID != "" && !seen {
			ep.Nodes = append(ep.Nodes, episodes.NodeRef{ID: v.NodeID, ZoneID: v.ZoneID})
		}
		if v.ChainID != "" && len(ep.ChainRefs) == 0 {
			ep.ChainRefs = []episodes.ChainRef{{ID: v.ChainID}}
		}
		if len(v.RoutineIDs) > 0 && len(ep.RoutineRefs) == 0 {
			ep.RoutineRefs = []episodes.RoutineRef{{ID: v.RoutineIDs[0]}}
		}
		if v.EventType != "" {
			ep.EventTypes = appendUnique(ep.EventTypes, v.EventType)
		}
		if v.ContextQuality != "" {
			ep.ContextQualities = appendUnique(ep.ContextQualities, v.ContextQuality)
		}
	}
	ep.DurationObserved = ep.LastObservedAt.Sub(ep.StartedAt)
	if err := ep.Validate(); err != nil {
		panic(err)
	}
	return ep
}
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
func evidenceSet(t testing.TB, id string, values []episodes.ObservationRef, topology episodes.TopologyView, revision uint64) situationfacts.FactSet {
	t.Helper()
	set, err := situationfacts.Extract(situationfacts.ExtractionInput{Episode: evidenceEpisode(id, values, revision), Topology: topology, ExtractedAt: evidenceBase.Add(time.Hour * time.Duration(revision))}, situationfacts.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return set
}
func knownEvidence(id string) episodes.SubjectRef {
	return episodes.SubjectRef{Kind: episodes.SubjectKnown, EntityID: id}
}
func unknownEvidence() episodes.SubjectRef { return episodes.SubjectRef{Kind: episodes.SubjectUnknown} }
func uncertainEvidence() episodes.SubjectRef {
	return episodes.SubjectRef{Kind: episodes.SubjectUncertain, CandidateEntityIDs: []string{"resident-a", "resident-b"}}
}
func evaluateEvidence(t testing.TB, set situationfacts.FactSet) situationhypotheses.CompetingHypothesisSet {
	t.Helper()
	r, err := situationhypotheses.Evaluate(situationhypotheses.EvaluationInput{FactSet: set}, situationhypotheses.Schema(), situationhypotheses.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return r.Set
}
func hasKind(a DiscriminationAssessment, kind EvidenceCandidateKind) bool {
	for _, c := range a.Candidates {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

func TestCatalogNeutralAndDeterministic(t *testing.T) {
	a := Catalog()
	if err := ValidateCatalog(a); err != nil {
		t.Fatal(err)
	}
	if CatalogFingerprint(a) != CatalogFingerprint(Catalog()) {
		t.Fatal("catalog fingerprint changed")
	}
	for _, d := range a.Definitions {
		if forbiddenCandidateTerm(string(d.Kind)) || forbiddenCandidateTerm(d.Description) {
			t.Fatalf("non-neutral catalog entry %s", d.Kind)
		}
		for _, o := range d.Outcomes {
			if forbiddenCandidateTerm(o.DescriptionCode) {
				t.Fatalf("non-neutral outcome %s", o.DescriptionCode)
			}
		}
	}
}

func TestAnalyzePatternAndIdentityCandidates(t *testing.T) {
	obs := evidenceObservation("pattern-1", evidenceBase, knownEvidence("resident-a"), "entry", "track-pattern")
	obs.Deviation = &episodes.DeviationRef{AssessmentID: "assessment-1", Status: "evaluated", Band: "aligned", CoveragePermille: 1000}
	set := evidenceSet(t, "episode-pattern", []episodes.ObservationRef{obs}, nil, 1)
	h := evaluateEvidence(t, set)
	a, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: h, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(a, KindPatternAlignmentConfirmation) && !hasKind(a, KindTemporalRepetitionConfirmation) {
		t.Fatalf("candidates=%v", a.Candidates)
	}
	for _, c := range a.Candidates {
		e, err := Explain(c)
		if err != nil || !e.NotACommand || !e.NotAProbability || !e.NoSecurityMeaning {
			t.Fatalf("explanation=%+v err=%v", e, err)
		}
	}
}

func TestAnalyzeUnknownPartialAndConflictDimensions(t *testing.T) {
	first := evidenceObservation("unknown-1", evidenceBase, unknownEvidence(), "entry", "track-u")
	second := evidenceObservation("unknown-2", evidenceBase.Add(time.Minute), unknownEvidence(), "corridor", "track-u")
	set := evidenceSet(t, "episode-unknown", []episodes.ObservationRef{first, second}, episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent}}, 1)
	h := evaluateEvidence(t, set)
	a, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: h, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(a, KindIdentityConfirmation) && !hasKind(a, KindIdentityContinuityConfirmation) {
		t.Fatalf("identity candidates absent: %v", a.Candidates)
	}
	partial := evidenceObservation("partial-1", evidenceBase, unknownEvidence(), "entry", "track-p")
	partial.HouseMode = ""
	partial.Occupancy = ""
	partial.ContextQuality = "partial"
	partialSet := evidenceSet(t, "episode-partial", []episodes.ObservationRef{partial}, nil, 1)
	partialHyp := evaluateEvidence(t, partialSet)
	partialA, err := Analyze(AnalysisInput{FactSet: partialSet, HypothesisSet: partialHyp, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(partialA, KindContextCompletenessConfirmation) {
		t.Fatalf("completeness candidate absent: %v", partialA.Candidates)
	}
}

func TestRedundancyAndConflictAreDistinct(t *testing.T) {
	first := evidenceObservation("conflict-1", evidenceBase, knownEvidence("resident-a"), "entry", "track-c")
	second := evidenceObservation("conflict-2", evidenceBase, knownEvidence("resident-a"), "entry", "track-c")
	second.HouseMode = "away"
	set := evidenceSet(t, "episode-conflict", []episodes.ObservationRef{first, second}, nil, 1)
	h := evaluateEvidence(t, set)
	a, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: h, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range a.Candidates {
		if c.Kind == KindSourceConsistencyConfirmation {
			found = true
			if c.RedundancyPermille >= 1000 {
				t.Fatal("conflicting dimension marked fully redundant")
			}
		}
	}
	if !found {
		t.Fatalf("source consistency missing: %v", a.Candidates)
	}
}

func TestFullDiffAndRegistryIdempotence(t *testing.T) {
	first := evidenceObservation("diff-1", evidenceBase, unknownEvidence(), "entry", "track-d")
	previous := evidenceSet(t, "episode-diff", []episodes.ObservationRef{first}, nil, 1)
	previousHyp := evaluateEvidence(t, previous)
	previousAssessment, err := Analyze(AnalysisInput{FactSet: previous, HypothesisSet: previousHyp, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second := evidenceObservation("diff-2", evidenceBase.Add(time.Minute), unknownEvidence(), "corridor", "track-d")
	current := evidenceSet(t, "episode-diff", []episodes.ObservationRef{first, second}, episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"entry\x00corridor": episodes.TopologyAdjacent}}, 2)
	currentHyp := evaluateEvidence(t, current)
	fd, err := situationfacts.Diff(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	fromDiff, err := ReevaluateFromDiff(previous, current, fd, previousHyp, currentHyp, previousAssessment, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	full, err := Analyze(AnalysisInput{FactSet: current, HypothesisSet: currentHyp, HypothesisSchema: situationhypotheses.Schema(), PreviousAssessment: &previousAssessment}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, fromDiff) {
		t.Fatalf("full/diff differ\nfull=%+v\ndiff=%+v", full, fromDiff)
	}
	registry := NewRegistry()
	plan, err := Plan(previous, previousHyp, registry.Snapshot(), Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	firstApply, err := registry.ApplyPlan(plan)
	if err != nil || !firstApply.Applied {
		t.Fatalf("apply=%+v err=%v", firstApply, err)
	}
	same, err := registry.ApplyPlan(plan)
	if err != nil || !same.Idempotent || registry.Snapshot().Revision != 1 {
		t.Fatalf("idempotence=%+v err=%v", same, err)
	}
}

func TestRegistryConcurrentConflict(t *testing.T) {
	obs := evidenceObservation("race-1", evidenceBase, unknownEvidence(), "entry", "track-r")
	set := evidenceSet(t, "episode-race", []episodes.ObservationRef{obs}, nil, 1)
	hyp := evaluateEvidence(t, set)
	r := NewRegistry()
	base := r.Snapshot()
	p1, err := Plan(set, hyp, base, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	next := evidenceObservation("race-2", evidenceBase.Add(time.Minute), unknownEvidence(), "corridor", "track-r")
	set2 := evidenceSet(t, "episode-race", []episodes.ObservationRef{obs, next}, nil, 2)
	hyp2 := evaluateEvidence(t, set2)
	p2, err := Plan(set2, hyp2, base, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, p := range []EvidencePlan{p1, p2} {
		wg.Add(1)
		go func(value EvidencePlan) { defer wg.Done(); _, e := r.ApplyPlan(value); results <- e }(p)
	}
	wg.Wait()
	close(results)
	successes := 0
	conflicts := 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestPolicyForbiddenReferenceAndDefensiveSnapshot(t *testing.T) {
	obs := evidenceObservation("policy-1", evidenceBase, unknownEvidence(), "entry", "track-policy")
	set := evidenceSet(t, "episode-policy", []episodes.ObservationRef{obs}, nil, 1)
	hypotheses := evaluateEvidence(t, set)
	catalog := Catalog()
	for i := range catalog.Definitions {
		if catalog.Definitions[i].Kind == KindIdentityConfirmation {
			catalog.Definitions[i].DefaultSensitivityClass = SensitivityHigh
		}
	}
	withoutHigh, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, catalog, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if hasKind(withoutHigh, KindIdentityConfirmation) {
		t.Fatal("high sensitivity candidate was not filtered")
	}
	policy := DefaultPolicy()
	policy.IncludeHighSensitivityCandidates = true
	withHigh, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, catalog, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(withHigh, KindIdentityConfirmation) {
		t.Fatal("enabled high sensitivity candidate missing")
	}
	bad := Catalog()
	bad.Definitions[0].RequiredFactCodes = append(bad.Definitions[0].RequiredFactCodes, situationfacts.FactCode("unknown.code"))
	if !errors.Is(ValidateCatalog(bad), ErrUnknownFactCode) {
		t.Fatal("unknown catalog FactCode accepted")
	}
	forbidden := Catalog()
	forbidden.Definitions[0].Kind = EvidenceCandidateKind("danger_confirmation")
	if !errors.Is(ValidateCatalog(forbidden), ErrInvalidDefinition) {
		t.Fatal("interpretive catalog kind accepted")
	}
	for i := range hypotheses.Hypotheses {
		hypotheses.Hypotheses[i].ID = "forged-hypothesis"
		hypotheses.Hypotheses[i].Fingerprint = situationhypotheses.HypothesisFingerprint(hypotheses.Hypotheses[i])
		break
	}
	hypotheses.LeadingHypothesisID = ""
	hypotheses.Ambiguous = true
	hypotheses.Fingerprint = situationhypotheses.CompetingHypothesisSetFingerprint(hypotheses)
	if !errors.Is(func() error {
		_, e := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
		return e
	}(), ErrUnknownHypothesisReference) {
		t.Fatal("forged hypothesis reference accepted")
	}
	r := NewRegistry()
	plan, err := Plan(set, evaluateEvidence(t, set), r.Snapshot(), Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyPlan(plan); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	snapshot.Assessments[0].Candidates = nil
	snapshot.EpisodeIndex["mutated"] = 99
	clean := r.Snapshot()
	if len(clean.Assessments[0].Candidates) == 0 {
		t.Fatal("snapshot leaked candidate slice")
	}
	if _, ok := clean.EpisodeIndex["mutated"]; ok {
		t.Fatal("snapshot leaked index map")
	}
	policy.MinBestCandidateMarginPermille = 1000
	equal, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: evaluateEvidence(t, set), HypothesisSchema: situationhypotheses.Schema()}, Catalog(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if equal.BestCandidateID != "" {
		t.Fatal("insufficient leading margin selected a candidate")
	}
	noCandidatePolicy := DefaultPolicy()
	noCandidatePolicy.MinUtilityPermille = 1000
	noCandidate, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: evaluateEvidence(t, set), HypothesisSchema: situationhypotheses.Schema()}, Catalog(), noCandidatePolicy)
	if err != nil {
		t.Fatal(err)
	}
	if noCandidate.EvidenceUseful || len(noCandidate.Candidates) != 0 {
		t.Fatalf("high threshold fabricated evidence: %+v", noCandidate)
	}
}

func TestReadinessMarkers(t *testing.T) {
	readiness := Readiness()
	if !readiness.ReadyForAdvisoryEvidenceRequests || readiness.RuntimeIntegrated || readiness.Durable || readiness.ActiveRequestsImplemented || readiness.SensorCommandsImplemented || readiness.AutomaticObservationImplemented || readiness.SecurityAuthority {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestAdditionalQualificationCases(t *testing.T) {
	uncertain := evidenceObservation("uncertain-1", evidenceBase, uncertainEvidence(), "entry", "track-uncertain")
	uncertainSet := evidenceSet(t, "episode-uncertain", []episodes.ObservationRef{uncertain}, nil, 1)
	uncertainAssessment, err := Analyze(AnalysisInput{FactSet: uncertainSet, HypothesisSet: evaluateEvidence(t, uncertainSet), HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(uncertainAssessment, KindIdentityConfirmation) && !hasKind(uncertainAssessment, KindIdentityContinuityConfirmation) {
		t.Fatal("uncertain identity did not produce a descriptive candidate")
	}
	multiA := evidenceObservation("multi-a", evidenceBase, knownEvidence("resident-a"), "entry", "track-a")
	multiB := evidenceObservation("multi-b", evidenceBase, knownEvidence("resident-b"), "entry", "track-b")
	multiSet := evidenceSet(t, "episode-multi", []episodes.ObservationRef{multiA, multiB}, nil, 1)
	multiAssessment, err := Analyze(AnalysisInput{FactSet: multiSet, HypothesisSet: evaluateEvidence(t, multiSet), HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(multiAssessment, KindEntityCountConfirmation) && !hasKind(multiAssessment, KindIdentityConfirmation) {
		t.Fatal("multi-entity candidate absent")
	}
	customDefinition, ok := Catalog().Definition(KindTemporalRepetitionConfirmation)
	if !ok {
		t.Fatal("temporal catalog definition absent")
	}
	customDefinition.RequiredFactCodes = []situationfacts.FactCode{situationfacts.CodeIdentityCandidateEntitySet}
	customDefinition.Outcomes = []OutcomeDefinition{{ID: "same_effect", FactCode: situationfacts.CodeIdentityCandidateEntitySet, Operator: OutcomeFactPresent, DescriptionCode: "same_effect_for_competing_hypotheses", Supports: []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation, situationhypotheses.KindPossiblePatternShift}}}
	custom := EvidenceCatalog{Version: "v1", Definitions: []EvidenceCandidateDefinition{customDefinition}}
	lowPolicy := DefaultPolicy()
	lowPolicy.MinDiscriminationPermille = 0
	lowPolicy.MinCoverageGainPermille = 0
	lowPolicy.MinUtilityPermille = 0
	customObservation := evidenceObservation("custom-1", evidenceBase, knownEvidence("resident-a"), "entry", "track-custom")
	customObservation.Deviation = &episodes.DeviationRef{AssessmentID: "custom-assessment", Status: "evaluated", Band: "positive", ScorePermille: 800, CoveragePermille: 1000, TemporalAvailable: true}
	patternSet := evidenceSet(t, "episode-custom", []episodes.ObservationRef{customObservation}, nil, 1)
	patternHyp := evaluateEvidence(t, patternSet)
	selected := make([]situationhypotheses.SituationHypothesis, 0, 2)
	for _, hypothesis := range patternHyp.Hypotheses {
		if hypothesis.Kind == situationhypotheses.KindIsolatedDeviation || hypothesis.Kind == situationhypotheses.KindPossiblePatternShift {
			selected = append(selected, hypothesis)
		}
	}
	patternHyp.Hypotheses = selected
	patternHyp.LeadingHypothesisID = ""
	patternHyp.Ambiguous = true
	patternHyp.Fingerprint = situationhypotheses.CompetingHypothesisSetFingerprint(patternHyp)
	customAssessment, err := Analyze(AnalysisInput{FactSet: patternSet, HypothesisSet: patternHyp, HypothesisSchema: situationhypotheses.Schema()}, custom, lowPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(customAssessment.Candidates) != 1 || customAssessment.Candidates[0].DiscriminationPermille != 0 {
		t.Fatalf("non-contrast candidate was scored as discriminating: %+v", customAssessment.Candidates)
	}
}
