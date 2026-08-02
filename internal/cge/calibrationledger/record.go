package calibrationledger

// Clone returns an owned record snapshot. It is deliberately small because a
// record contains no mutable payloads; only the compact dimension slice needs
// copying.
func (r CalibrationRecord) Clone() CalibrationRecord {
	r.Dimensions = append([]CalibrationDimensionSummary(nil), r.Dimensions...)
	return r
}
