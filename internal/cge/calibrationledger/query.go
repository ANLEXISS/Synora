package calibrationledger

func validateQuery(q Query) error {
	if q.Limit < 1 {
		return ErrInvalidQuery
	}
	if q.Limit > 1000 {
		return ErrQueryLimitExceeded
	}
	if q.SequenceFrom != 0 && q.SequenceTo != 0 && q.SequenceFrom > q.SequenceTo {
		return ErrInvalidQuery
	}
	return nil
}
func cloneRecord(r CalibrationRecord) CalibrationRecord {
	return r.Clone()
}
func cloneRecords(values []CalibrationRecord) []CalibrationRecord {
	out := make([]CalibrationRecord, len(values))
	for i := range values {
		out[i] = cloneRecord(values[i])
	}
	return out
}
