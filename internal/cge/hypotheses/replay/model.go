package replay

// ReplayMetadata describes one complete hypothesis replay.
type ReplayMetadata struct {
	JournalID string

	RecordsExamined uint64
	RecordsApplied  uint64
	RecordsSkipped  uint64

	SetsOpened           uint64
	StatusChangesApplied uint64
	RebasesApplied       uint64
	SupersessionsApplied uint64
	ResolutionsExamined  uint64
	ResolutionsApplied   uint64
	FinalSetCount        int

	FinalHeadSequence uint64
	FinalHeadHash     string
}
