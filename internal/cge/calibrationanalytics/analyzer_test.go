package calibrationanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/decisioncomparison"
)

func comparisonFixture(seed string, alignment int) decisioncomparison.HistoricalDecisionComparison {
	h := decisioncomparison.HistoricalDecisionRef{ID: "historical-" + seed, SourceEventRef: "event-" + seed, PreviousStateCode: "stable", CurrentStateCode: "stable", DecidedAtUnixNano: 123, Revision: 1, HistoricalDecisionHasProductionAuthority: true}
	h.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(h)
	dimension := decisioncomparison.ComparisonDimension{Kind: decisioncomparison.DimensionStateContinuity, Status: decisioncomparison.DimensionAligned, Comparable: true, AlignmentPermille: alignment, DivergencePermille: 1000 - alignment, CoveragePermille: 1000}
	dimension.Fingerprint = decisioncomparison.ComparisonDimensionFingerprint(dimension)
	c := decisioncomparison.HistoricalDecisionComparison{ID: "comparison-" + seed, EpisodeID: "episode-" + seed, SituationID: "situation-" + seed, RecommendationSetID: "recommendation-set-" + seed, HistoricalDecision: h, Category: decisioncomparison.CategoryAligned, Status: decisioncomparison.ComparisonCurrent, Dimensions: []decisioncomparison.ComparisonDimension{dimension}, OverallAlignmentPermille: alignment, OverallDivergencePermille: 1000 - alignment, OverallCoveragePermille: 1000, Comparable: true, SourceSituationFingerprint: "situation-source-" + seed, SourceRecommendationFingerprint: "recommendation-source-" + seed, SourceHistoricalFingerprint: h.Fingerprint, Revision: 1, Markers: decisioncomparison.ComparisonMarkers{HistoricalDecisionRetainsAuthority: true, CognitiveRecommendationHasNoAuthority: true, NotAProductionDecision: true, DoesNotOverrideHistoricalDecision: true, NotAProbability: true, NotAnAlert: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NoSecurityMeaning: true, CalibrationOnly: true}}
	c.Fingerprint = decisioncomparison.ComparisonFingerprint(c)
	return c
}

func analyticsFixture(t testing.TB, count int, recentAlignment int) (calibrationledger.Snapshot, []calibrationledger.CalibrationRecord) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.ndjson")
	store, err := calibrationledger.OpenFileStore(path, calibrationledger.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var previous *calibrationledger.CalibrationRecord
	for i := 0; i < count; i++ {
		alignment := 900
		if i >= count/2 {
			alignment = recentAlignment
		}
		record, buildErr := calibrationledger.BuildRecord(calibrationledger.BuildRecordInput{Comparison: comparisonFixture(seedFor(i), alignment), SituationPolicyFingerprint: "situation-policy-" + cohortFor(i), RecommendationPolicyFingerprint: "recommendation-policy-" + cohortFor(i), ComparisonPolicyFingerprint: "comparison-policy-" + cohortFor(i), Previous: previous}, calibrationledger.DefaultPolicy())
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		if _, appendErr := store.Append(context.Background(), record); appendErr != nil {
			t.Fatal(appendErr)
		}
		previous = &record
	}
	snapshot := store.Snapshot()
	records := make([]calibrationledger.CalibrationRecord, 0, count)
	for from := uint64(1); from <= snapshot.LastSequence; {
		result, queryErr := store.Query(calibrationledger.Query{SequenceFrom: from, SequenceTo: snapshot.LastSequence, Limit: 1000})
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if len(result.Records) == 0 {
			t.Fatal("fixture query made no progress")
		}
		records = append(records, result.Records...)
		from = result.Records[len(result.Records)-1].Sequence + 1
	}
	return snapshot, records
}

func seedFor(i int) string { return "analytics-" + string(rune('a'+i%26)) + "-" + strconv.Itoa(i) }
func cohortFor(i int) string {
	if i%2 == 0 {
		return "a"
	}
	return "b"
}
func TestAnalyzeIsDeterministicAndRedacted(t *testing.T) {
	snapshot, records := analyticsFixture(t, 20, 400)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 10
	policy.MinimumComparableRecords = 5
	policy.MinimumRecordsPerCohort = 5
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 5
	policy.DriftMinimumSampleSize = 5
	first, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence, LedgerFingerprint: "ledger-test"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence, LedgerFingerprint: "ledger-test"}, policy)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("determinism err=%v equal=%v", err, reflect.DeepEqual(first, second))
	}
	if !first.Markers.DescriptiveOnly || !first.Markers.NotAutomaticCalibration || !first.PolicyEvaluation.Markers.NoWinnerSelected {
		t.Fatalf("markers=%+v", first.Markers)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"\"payload\":", "\"image\":", "\"frame\":", "\"clip\":", "\"audio\":", "\"embedding\":", "\"resident\":", "\"identity\":", "\"device_id\":", "\"event_id\":", "\"episode_id\":", "\"hypothesis_id\":", "\"recommendation_id\":", "\"advisory_request_id\":", "\"network_address\":", "\"token\":", "\"credential\":", "\"grant\":", "\"command\":", "\"action\":", "\"record_fingerprint\":", "\"source_decision_fingerprint\":"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("forbidden report field %q", forbidden)
		}
	}
	if first.Global.Fingerprint == "" || first.ReportFingerprint == "" || first.DataSufficiency.Fingerprint == "" {
		t.Fatal("missing fingerprints")
	}
	clone := first.Clone()
	clone.Categories[0].Category = "mutated"
	if first.Categories[0].Category == "mutated" {
		t.Fatal("report clone exposed category storage")
	}
}

func TestAnalyzeSufficiencyAndBounds(t *testing.T) {
	snapshot, records := analyticsFixture(t, 2, 400)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 100
	report, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if report.DataSufficiency.SufficientForGlobalAnalysis || report.DataSufficiency.MissingRecords != 98 || report.DataSufficiency.MissingComparableRecords != 48 {
		t.Fatalf("sufficiency=%+v", report.DataSufficiency)
	}
	bad := records
	bad[1].Sequence = bad[0].Sequence
	if _, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: bad, GeneratedFromSequence: snapshot.LastSequence}, policy); !errors.Is(err, calibrationledger.ErrRecordFingerprintMismatch) && !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad input err=%v", err)
	}
}

func TestAnalyzeCanonicalizesRecordOrderWithoutMutatingInput(t *testing.T) {
	snapshot, records := analyticsFixture(t, 12, 400)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 3
	policy.DriftMinimumSampleSize = 1
	shuffled := append([]calibrationledger.CalibrationRecord(nil), records...)
	for left, right := 0, len(shuffled)-1; left < right; left, right = left+1, right-1 {
		shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
	}
	first, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: shuffled, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReportFingerprint != second.ReportFingerprint {
		t.Fatalf("canonical fingerprint mismatch: %s != %s", first.ReportFingerprint, second.ReportFingerprint)
	}
	if records[0].Sequence != 1 || records[len(records)-1].Sequence != snapshot.LastSequence {
		t.Fatal("analytics mutated caller records")
	}
}

func TestAnalyticsReadinessAndTypedValidationErrors(t *testing.T) {
	readiness := Readiness()
	if !readiness.AnalyticsModelImplemented || !readiness.DeterministicAnalyzerImplemented || !readiness.FingerprintsValidated || !readiness.ReadyForControlledPolicyExperiments {
		t.Fatalf("readiness incomplete: %+v", readiness)
	}
	if readiness.AutomaticCalibrationImplemented || readiness.ThresholdUpdatesImplemented || readiness.WeightUpdatesImplemented || readiness.PolicySelectionImplemented || readiness.PolicyDeploymentImplemented || readiness.ProductionFeedbackImplemented || readiness.DecisionOverrideImplemented || readiness.ActionExecutionImplemented || readiness.SecurityAuthority {
		t.Fatalf("analytics crossed authority boundary: %+v", readiness)
	}
	policy := DefaultAnalyticsPolicy()
	policy.WindowSizeRecords = 0
	if !errors.Is(policy.Validate(), ErrInvalidAnalyticsPolicy) {
		t.Fatal("invalid policy was not typed")
	}
	markers := analyticsMarkers()
	markers.NotAnAlert = false
	if !errors.Is(markers.Validate(), ErrInvalidAnalyticsMarkers) {
		t.Fatal("invalid analytics markers were not typed")
	}
	evaluationMarkers := policyEvaluationMarkers()
	evaluationMarkers.NoWinnerSelected = false
	if !errors.Is(evaluationMarkers.Validate(), ErrInvalidPolicyEvaluationMarkers) {
		t.Fatal("invalid evaluation markers were not typed")
	}
}

func TestAnalyzeEmptyInputIsDescriptiveAndStable(t *testing.T) {
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	one, err := Analyze(AnalyticsInput{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Analyze(AnalyticsInput{}, policy)
	if err != nil || one.ReportFingerprint != two.ReportFingerprint {
		t.Fatalf("empty report is not deterministic: %v %v", err, two)
	}
	if one.RecordCount != 0 || one.DataSufficiency.SufficientForGlobalAnalysis || !one.Markers.DescriptiveOnly {
		t.Fatalf("empty report=%+v", one)
	}
}
