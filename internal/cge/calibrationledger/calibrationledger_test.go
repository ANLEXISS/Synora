package calibrationledger

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/internal/cge/decisioncomparison"
)

func testComparison(seed string) decisioncomparison.HistoricalDecisionComparison {
	h := decisioncomparison.HistoricalDecisionRef{ID: "historical-" + seed, SourceEventRef: "event-" + seed, PreviousStateCode: "stable", CurrentStateCode: "stable", DecidedAtUnixNano: 123, Revision: 1, HistoricalDecisionHasProductionAuthority: true}
	h.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(h)
	dimensions := []decisioncomparison.ComparisonDimension{{Kind: decisioncomparison.DimensionStateContinuity, Status: decisioncomparison.DimensionAligned, Comparable: true, AlignmentPermille: 900, DivergencePermille: 100, CoveragePermille: 1000}, {Kind: decisioncomparison.DimensionFreshness, Status: decisioncomparison.DimensionPartiallyAligned, Comparable: true, AlignmentPermille: 700, DivergencePermille: 300, CoveragePermille: 800}}
	for i := range dimensions {
		dimensions[i].Fingerprint = decisioncomparison.ComparisonDimensionFingerprint(dimensions[i])
	}
	c := decisioncomparison.HistoricalDecisionComparison{ID: "comparison-" + seed, EpisodeID: "episode-" + seed, SituationID: "situation-" + seed, RecommendationSetID: "recommendation-set-" + seed, HistoricalDecision: h, Category: decisioncomparison.CategoryAligned, Status: decisioncomparison.ComparisonCurrent, Dimensions: dimensions, OverallAlignmentPermille: 800, OverallDivergencePermille: 200, OverallCoveragePermille: 900, Comparable: true, SourceSituationFingerprint: "situation-fp-" + seed, SourceRecommendationFingerprint: "recommendation-fp-" + seed, SourceHistoricalFingerprint: h.Fingerprint, Revision: 1, Markers: decisioncomparison.ComparisonMarkers{HistoricalDecisionRetainsAuthority: true, CognitiveRecommendationHasNoAuthority: true, NotAProductionDecision: true, DoesNotOverrideHistoricalDecision: true, NotAProbability: true, NotAnAlert: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NoSecurityMeaning: true, CalibrationOnly: true}}
	c.Fingerprint = decisioncomparison.ComparisonFingerprint(c)
	return c
}

func testRecord(t testing.TB, seed string, previous *CalibrationRecord) CalibrationRecord {
	t.Helper()
	r, err := BuildRecord(BuildRecordInput{Comparison: testComparison(seed), SituationPolicyFingerprint: "situation-policy", RecommendationPolicyFingerprint: "recommendation-policy", ComparisonPolicyFingerprint: "comparison-policy", Previous: previous}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func openTestStore(t testing.TB, policy Policy) *FileStore {
	t.Helper()
	s, err := OpenFileStore(filepath.Join(t.TempDir(), "ledger.ndjson"), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFirstMultipleDuplicateAndConflict(t *testing.T) {
	s := openTestStore(t, DefaultPolicy())
	first := testRecord(t, "a", nil)
	result, err := s.Append(context.Background(), first)
	if err != nil || !result.Appended || result.Sequence != 1 {
		t.Fatalf("first=%+v err=%v", result, err)
	}
	second := testRecord(t, "b", &first)
	result, err = s.Append(context.Background(), second)
	if err != nil || !result.Appended || result.Sequence != 2 {
		t.Fatalf("second=%+v err=%v", result, err)
	}
	dup, err := s.Append(context.Background(), first)
	if err != nil || !dup.Duplicate || dup.Appended || dup.Sequence != 1 {
		t.Fatalf("duplicate=%+v err=%v", dup, err)
	}
	conflict := first
	conflict.Category = string(decisioncomparison.CategoryDivergent)
	conflict.RecordFingerprint = recordFingerprint(conflict)
	_, err = s.Append(context.Background(), conflict)
	if !errors.Is(err, ErrDuplicateComparisonConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	if got := s.Snapshot(); got.RecordCount != 2 || got.FirstSequence != 1 || got.LastSequence != 2 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestRedactedCanonicalJSONAndDefensiveSnapshot(t *testing.T) {
	s := openTestStore(t, DefaultPolicy())
	r := testRecord(t, "redaction", nil)
	if _, err := s.Append(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	raw := strings.ToLower(string(b))
	for _, forbidden := range []string{"\"payload\":", "\"image\":", "\"frame\":", "\"clip\":", "\"audio\":", "\"embedding\":", "\"resident\":", "\"identity\":", "\"device_id\":", "\"event_id\":", "\"episode_id\":", "\"hypothesis_id\":", "\"recommendation_id\":", "\"advisory_request_id\":", "\"network_address\":", "\"token\":", "\"credential\":", "\"grant\":", "\"command\":", "\"action\":"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("forbidden field %q in %s", forbidden, raw)
		}
	}
	path := filepath.Join(t.TempDir(), "json.ndjson")
	fileStore, err := OpenFileStore(path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileStore.Append(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	_ = fileStore.Close()
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = strings.ToLower(string(fileBytes))
	for _, forbidden := range []string{"\"payload\":", "\"image\":", "\"frame\":", "\"clip\":", "\"audio\":", "\"embedding\":", "\"resident\":", "\"identity\":", "\"device_id\":", "\"event_id\":", "\"episode_id\":", "\"hypothesis_id\":", "\"recommendation_id\":", "\"advisory_request_id\":", "\"network_address\":", "\"token\":", "\"credential\":", "\"grant\":", "\"command\":", "\"action\":"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("forbidden ledger field %q", forbidden)
		}
	}
	for _, forbiddenValue := range []string{"historical-redaction", "event-redaction", "episode-redaction", "situation-redaction", "recommendation-set-redaction"} {
		if strings.Contains(raw, forbiddenValue) {
			t.Fatalf("raw source value persisted: %q", forbiddenValue)
		}
	}
	snap := s.Snapshot()
	snap.CategoryCounts["aligned"] = 999
	if s.Snapshot().CategoryCounts["aligned"] != 1 {
		t.Fatal("snapshot map was not defensive")
	}
	q, err := s.Query(Query{Limit: 1})
	if err != nil || len(q.Records) != 1 {
		t.Fatalf("query=%+v err=%v", q, err)
	}
	q.Records[0].Dimensions[0].Kind = "mutated"
	if s.Snapshot().Aggregate.TotalRecords != 1 {
		t.Fatal("query exposed state")
	}
}

func TestRecoveryRestartQueryAndLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.ndjson")
	p := DefaultPolicy()
	s, err := OpenFileStore(path, p)
	if err != nil {
		t.Fatal(err)
	}
	first := testRecord(t, "restart-a", nil)
	second := testRecord(t, "restart-b", &first)
	if _, err = s.Append(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	restarted, err := OpenFileStore(path, p)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	recovery, err := restarted.Recover(context.Background())
	if err != nil || !recovery.Completed || recovery.RecordCount != 2 {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
	result, err := restarted.Query(Query{SequenceFrom: 2, SequenceTo: 2, Limit: 1})
	if err != nil || len(result.Records) != 1 || result.Records[0].Sequence != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err = restarted.Query(Query{Limit: 1001}); !errors.Is(err, ErrQueryLimitExceeded) {
		t.Fatalf("query limit err=%v", err)
	}
}

func TestPolicyAndHistogram(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Fingerprint(), PolicySchemaVersion+":") {
		t.Fatal(p.Fingerprint())
	}
	var h PermilleHistogram
	for _, v := range []int{0, 500, 1000, 1000} {
		h.Add(v)
	}
	if h.Percentile(500) != 500 || h.Percentile(950) != 1000 || h.Mean() != 625 {
		t.Fatalf("hist=%+v", h)
	}
}

func TestReadinessMarkers(t *testing.T) {
	r := Readiness()
	if !r.ComparisonHistoryDurable || !r.ReadyForCalibrationAnalytics || r.AutomaticCalibrationImplemented || r.ThresholdUpdatesImplemented || r.WeightUpdatesImplemented || r.ProductionFeedbackImplemented || r.DecisionOverrideImplemented || r.ActionExecutionImplemented || r.SecurityAuthority {
		t.Fatalf("readiness=%+v", r)
	}
	bad := testRecord(t, "markers", nil)
	bad.Markers.NoSecurityMeaning = false
	bad.RecordFingerprint = recordFingerprint(bad)
	if !errors.Is(bad.Validate(DefaultPolicy()), ErrInvalidMarkers) {
		t.Fatalf("bad markers accepted")
	}
}
