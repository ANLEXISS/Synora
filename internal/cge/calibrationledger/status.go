package calibrationledger

// Status is a compact internal diagnostic view. It intentionally omits the
// path, records, identifiers, and detailed event state.
type Status struct {
	Enabled                        bool
	Available                      bool
	Degraded                       bool
	RecordCount                    uint64
	LastSequence                   uint64
	LastRecordFingerprint          string
	RecoveryCompleted              bool
	RecoveryRepairedTrailingRecord bool
	AppendFailures                 uint64
	DuplicateRecords               uint64
	IntegrityFailures              uint64
	LastErrorCode                  string
}
