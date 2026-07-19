package replay

import "time"

// ReplayMode identifies the source state used by reconstruction.
type ReplayMode string

const (
	ReplayModeJournalOnly        ReplayMode = "journal_only"
	ReplayModeSnapshotAndJournal ReplayMode = "snapshot_and_journal"
)

// ReplayMetadata describes a completed replay without exposing mutable
// registry state. Sequence ranges are zero when no registry mutation was
// applied.
type ReplayMetadata struct {
	Mode ReplayMode

	JournalID string

	SnapshotUsed          bool
	SnapshotCreatedAt     time.Time
	SnapshotPayloadSHA256 string
	SnapshotChainCount    int
	CheckpointSequence    uint64
	CheckpointHash        string

	FirstAppliedSequence uint64
	LastAppliedSequence  uint64

	RecordsExamined               uint64
	RecordsApplied                uint64
	RecordsSkipped                uint64
	ChainsAdded                   uint64
	ObservationsAdded             uint64
	ContributionsAdded            uint64
	TransitionsApplied            uint64
	CheckpointsSkipped            uint64
	ResolutionsExamined           uint64
	ResolutionsApplied            uint64
	ResolutionObservationEffects  uint64
	ResolutionChainCreations      uint64
	ResolutionContributionEffects uint64
	ResolutionNoChainEffects      uint64

	FinalChainCount   int
	FinalHeadSequence uint64
	FinalHeadHash     string
}
