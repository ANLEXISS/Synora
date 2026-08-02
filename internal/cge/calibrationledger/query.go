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
