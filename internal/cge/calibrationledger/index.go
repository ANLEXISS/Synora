package calibrationledger

type ledgerIndex struct {
	records      []CalibrationRecord
	envelopes    []JournalEnvelope
	bySequence   map[uint64]int
	byRecord     map[string]int
	byComparison map[string]int
	categories   map[string]uint64
	aggregate    aggregateState
	bytes        int64
}

func newIndex() ledgerIndex {
	return ledgerIndex{bySequence: map[uint64]int{}, byRecord: map[string]int{}, byComparison: map[string]int{}, categories: map[string]uint64{}}
}
func (i *ledgerIndex) add(e JournalEnvelope, size int64) error {
	r := e.Record
	if _, ok := i.bySequence[r.Sequence]; ok {
		return ErrDuplicateSequence
	}
	if prior, ok := i.byComparison[r.ComparisonFingerprint]; ok && i.records[prior].RecordFingerprint != r.RecordFingerprint {
		return ErrDuplicateComparisonConflict
	}
	pos := len(i.records)
	i.records = append(i.records, r)
	i.envelopes = append(i.envelopes, e)
	i.bySequence[r.Sequence] = pos
	i.byRecord[r.RecordFingerprint] = pos
	i.byComparison[r.ComparisonFingerprint] = pos
	i.categories[r.Category]++
	i.aggregate.add(r)
	i.bytes += size
	return nil
}
func (i ledgerIndex) snapshot() Snapshot {
	s := Snapshot{SchemaVersion: SummarySchemaVersion, RecordCount: uint64(len(i.records)), LedgerBytes: i.bytes, CategoryCounts: map[string]uint64{}, Aggregate: i.aggregate.snapshot()}
	if len(i.records) > 0 {
		s.FirstSequence = i.records[0].Sequence
		s.LastSequence = i.records[len(i.records)-1].Sequence
		s.LastRecordFingerprint = i.records[len(i.records)-1].RecordFingerprint
		s.LastEnvelopeFingerprint = i.envelopes[len(i.envelopes)-1].EnvelopeHash
	}
	for k, v := range i.categories {
		s.CategoryCounts[k] = v
	}
	s.ComparableCount = 0
	s.SignificantDivergenceCount = 0
	for _, r := range i.records {
		if r.Comparable {
			s.ComparableCount++
		}
		if r.SignificantDivergence {
			s.SignificantDivergenceCount++
		}
		if r.Category == "incomparable" {
			s.IncomparableCount++
		}
		if r.Category == "stale" {
			s.StaleCount++
		}
		if r.Category == "invalidated" {
			s.InvalidatedCount++
		}
	}
	s.Digest = snapshotFingerprint(s)
	return s
}
