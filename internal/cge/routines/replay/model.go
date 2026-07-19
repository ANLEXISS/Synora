package replay

// Metadata describes one complete routine replay without exposing mutable
// registry state.
type Metadata struct {
	RecordsExamined uint64

	RoutinesCreated      uint64
	OccurrencesAdded     uint64
	StatusChangesApplied uint64

	FinalSequence uint64
	FinalHash     string
}
