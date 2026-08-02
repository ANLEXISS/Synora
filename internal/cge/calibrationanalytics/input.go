package calibrationanalytics

import (
	"sort"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/decisioncomparison"
)

func (in AnalyticsInput) normalized(policy AnalyticsPolicy) ([]calibrationledger.CalibrationRecord, string, error) {
	if err := policy.Validate(); err != nil {
		return nil, "", err
	}
	records := make([]calibrationledger.CalibrationRecord, len(in.Records))
	for i := range in.Records {
		records[i] = in.Records[i].Clone()
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Sequence != records[j].Sequence {
			return records[i].Sequence < records[j].Sequence
		}
		return records[i].RecordFingerprint < records[j].RecordFingerprint
	})
	if in.LedgerSnapshot.SchemaVersion != "" && in.LedgerSnapshot.SchemaVersion != calibrationledger.SummarySchemaVersion {
		return nil, "", ErrUnsupportedSchema
	}
	if in.LedgerSnapshot.RecordCount != 0 && in.LedgerSnapshot.RecordCount != uint64(len(records)) {
		return nil, "", ErrInvalidSequenceRange
	}
	for i := range records {
		if records[i].Sequence == 0 {
			return nil, "", ErrInvalidSequenceRange
		}
		if records[i].AlignmentPermille < 0 || records[i].AlignmentPermille > 1000 || records[i].DivergencePermille < 0 || records[i].DivergencePermille > 1000 || records[i].CoveragePermille < 0 || records[i].CoveragePermille > 1000 {
			return nil, "", ErrInvalidPermille
		}
		if err := records[i].Validate(calibrationledger.DefaultPolicy()); err != nil {
			return nil, "", ErrInvalidInput
		}
		if i > 0 && records[i-1].Sequence >= records[i].Sequence {
			return nil, "", ErrInvalidInput
		}
		if i > 0 && records[i].PreviousRecordFingerprint != records[i-1].RecordFingerprint {
			return nil, "", ErrInvalidInput
		}
		if !validCategory(records[i].Category) {
			return nil, "", ErrUnsupportedCategory
		}
	}
	if len(records) > 0 {
		if in.LedgerSnapshot.FirstSequence != 0 && records[0].Sequence != in.LedgerSnapshot.FirstSequence {
			return nil, "", ErrInvalidSequenceRange
		}
		if in.LedgerSnapshot.LastSequence != 0 && records[len(records)-1].Sequence != in.LedgerSnapshot.LastSequence {
			return nil, "", ErrInvalidSequenceRange
		}
		if in.GeneratedFromSequence != 0 && records[len(records)-1].Sequence > in.GeneratedFromSequence {
			return nil, "", ErrInvalidSequenceRange
		}
	}
	ledgerFingerprint := in.LedgerFingerprint
	if ledgerFingerprint == "" {
		ledgerFingerprint = in.LedgerSnapshot.Digest
	}
	if ledgerFingerprint == "" && len(records) > 0 {
		return nil, "", ErrInvalidInput
	}
	return records, ledgerFingerprint, nil
}

func validCategory(value string) bool {
	switch decisioncomparison.ComparisonCategory(value) {
	case decisioncomparison.CategoryAligned, decisioncomparison.CategoryPartiallyAligned, decisioncomparison.CategoryDivergent, decisioncomparison.CategoryCognitiveMoreConservative, decisioncomparison.CategoryHistoricalMoreDecisive, decisioncomparison.CategoryCognitiveTransitionOnly, decisioncomparison.CategoryHistoricalTransitionOnly, decisioncomparison.CategoryIncomparable, decisioncomparison.CategoryInsufficientInformation, decisioncomparison.CategoryStale, decisioncomparison.CategoryInvalidated:
		return true
	default:
		return false
	}
}

func canonicalCategories(values []CategoryAnalytics) []CategoryAnalytics {
	out := append([]CategoryAnalytics(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out
}
