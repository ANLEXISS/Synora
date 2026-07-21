package calibrationledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func digest(prefix string, value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return prefix + hex.EncodeToString(h[:])
}

type recordIdentity struct {
	SchemaVersion                   string
	RecordID                        string
	PreviousRecordFingerprint       string
	ComparisonFingerprint           string
	Category                        string
	Comparable                      bool
	SignificantDivergence           bool
	AlignmentPermille               int
	DivergencePermille              int
	CoveragePermille                int
	HistoricalStateChanged          bool
	CognitiveTransitionFound        bool
	HistoricalMoreDecisive          bool
	CognitiveMoreConservative       bool
	HistoricalDecisionRevision      uint64
	CognitiveSituationRevision      uint64
	RecommendationSetRevision       uint64
	ComparisonRevision              uint64
	SituationPolicyFingerprint      string
	RecommendationPolicyFingerprint string
	ComparisonPolicyFingerprint     string
	Dimensions                      []CalibrationDimensionSummary
	SourceDecisionFingerprint       string
	SourceSituationFingerprint      string
	SourceRecommendationFingerprint string
	SourceObservedAtUnixNano        int64
	SourceDecidedAtUnixNano         int64
	Markers                         CalibrationRecordMarkers
}

func identityOf(r CalibrationRecord) recordIdentity {
	d := append([]CalibrationDimensionSummary(nil), r.Dimensions...)
	sort.Slice(d, func(i, j int) bool {
		if d[i].Kind != d[j].Kind {
			return d[i].Kind < d[j].Kind
		}
		return d[i].Fingerprint < d[j].Fingerprint
	})
	return recordIdentity{SchemaVersion: RecordSchemaVersion, RecordID: r.RecordID, PreviousRecordFingerprint: r.PreviousRecordFingerprint, ComparisonFingerprint: r.ComparisonFingerprint, Category: r.Category, Comparable: r.Comparable, SignificantDivergence: r.SignificantDivergence, AlignmentPermille: r.AlignmentPermille, DivergencePermille: r.DivergencePermille, CoveragePermille: r.CoveragePermille, HistoricalStateChanged: r.HistoricalStateChanged, CognitiveTransitionFound: r.CognitiveTransitionFound, HistoricalMoreDecisive: r.HistoricalMoreDecisive, CognitiveMoreConservative: r.CognitiveMoreConservative, HistoricalDecisionRevision: r.HistoricalDecisionRevision, CognitiveSituationRevision: r.CognitiveSituationRevision, RecommendationSetRevision: r.RecommendationSetRevision, ComparisonRevision: r.ComparisonRevision, SituationPolicyFingerprint: r.SituationPolicyFingerprint, RecommendationPolicyFingerprint: r.RecommendationPolicyFingerprint, ComparisonPolicyFingerprint: r.ComparisonPolicyFingerprint, Dimensions: d, SourceDecisionFingerprint: r.SourceDecisionFingerprint, SourceSituationFingerprint: r.SourceSituationFingerprint, SourceRecommendationFingerprint: r.SourceRecommendationFingerprint, SourceObservedAtUnixNano: r.SourceObservedAtUnixNano, SourceDecidedAtUnixNano: r.SourceDecidedAtUnixNano, Markers: r.Markers}
}

func recordFingerprint(r CalibrationRecord) string {
	return digest("calibration-record-v1:", identityOf(r))
}
func envelopeFingerprint(e JournalEnvelope) string {
	c := e
	c.EnvelopeHash = ""
	return digest("calibration-envelope-v1:", c)
}
func recordHash(r CalibrationRecord) string { return digest("calibration-record-envelope-v1:", r) }
func genesisHash(p Policy) string {
	// Repair and fsync are operational recovery choices. They do not change the
	// ledger schema, so toggling either is allowed when explicitly repairing a
	// terminal tail. The genesis still commits all semantic policy limits.
	root := p
	root.RepairTrailingRecord = false
	root.Fsync = true
	return digest(GenesisVersion+":", struct {
		PolicyFingerprint string `json:"policy_fingerprint"`
	}{root.Fingerprint()})
}

func snapshotFingerprint(s Snapshot) string {
	c := s
	c.Digest = ""
	return digest("calibration-ledger-snapshot-v1:", c)
}
func summaryFingerprint(s Snapshot) string { return digest("calibration-ledger-summary-v1:", s) }
