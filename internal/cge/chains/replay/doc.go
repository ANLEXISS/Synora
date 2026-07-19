// Package replay reconstructs cognitive-chain registries from validated global
// journal snapshots. It is deliberately read-only: replay creates a new
// in-memory registry, applies validated deltas to private clones, and never
// writes a journal or snapshot.
//
// The journal-only mode starts with journal.genesis and requires every chain to
// be introduced by chain.added. The snapshot-and-journal mode starts from a
// deep copy of a validated registry snapshot and applies only records after
// the checkpoint that identifies that snapshot. The checkpoint itself has no
// registry effect. chain.observation_added is replayed through
// Chain.AddObservation on a private clone, so duplicate, stale, forbidden,
// timestamp, and revision inconsistencies are rejected without partial state.
// chain.contribution_added is replayed through Chain.AddContribution on a
// private clone; confidence and counters are never assigned directly.
// hypothesis.opened, hypothesis.status_changed, hypothesis.rebased, and
// hypothesis.superseded are valid global records but are skipped by this
// chain-only replay; hypotheses are reconstructed by
// internal/cge/hypotheses/replay from the complete journal.
package replay
