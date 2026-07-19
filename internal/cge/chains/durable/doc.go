// Package durable coordinates durable cognitive-chain mutations with the
// append-only global journal.
//
// A Coordinator owns one in-memory registry and one FileJournal. Every write
// prepares a deeply cloned registry, appends and syncs the corresponding
// journal delta, then publishes the prepared registry under the coordinator
// lock. The journal is therefore never behind a successfully published state.
// If durability becomes uncertain or publication is deliberately interrupted,
// the coordinator enters degraded state and requires explicit recovery from
// the journal.
//
// Snapshot generation is an explicit, serialized operation: it writes and
// syncs an immutable generation, appends and syncs its checkpoint, and only
// then publishes the active-generation manifest. It does not create snapshots
// automatically, clean up orphaned generations, start background work, or
// connect to Core or ShadowEngine.
// AddObservation is likewise explicit and caller-selected. Its WAL sequence
// is validate, clone, apply and validate the candidate, append+sync the compact
// observation delta, then publish. Domain time and record time are separate;
// no routing, confidence calculation, contribution, reactivation, merge, or
// split is performed.
// AddContribution is explicit and uses the same WAL boundary for the existing
// domain confidence formula. It never changes lifecycle status or historical
// reliability and is never called by ShadowEngine.
// Association planning is a pure snapshot operation exposed through
// PlanAssociation. ApplyAssociationPlan revalidates the selected revision or
// deterministic candidate ID, never replans, and uses only the existing
// chain.added and chain.observation_added records. Ambiguity is never resolved
// automatically.
// Evidence batches are also explicit: EvaluateEvidenceBatch captures a
// defensive view and delegates to the pure evidence package, while
// ApplyEvidenceProposals validates the whole selection before applying one
// contribution WAL transaction per ChainID. Batches are not journaled or
// snapshotted, application is not globally atomic, and degraded coordinators
// may be read but cannot apply proposals.
// Hypothesis sets are a second in-memory registry owned by the same
// coordinator. AddHypothesis and SetHypothesisStatus use the same global WAL
// lock and append+sync-before-publication order. They are replayed from the
// complete journal, including during generation-manifest recovery; generation
// snapshots still contain chains only, so the journal cannot yet be compacted.
// RebaseHypothesis explicitly appends a new immutable assessment version and
// preserves the current status. Identical fingerprints are idempotent; stale
// proposals are rejected and never recalculated. SupersedeHypothesis handles
// only evidence subjects whose fingerprint changed. It appends one WAL record
// containing the predecessor's terminal delta and the successor opening, then
// publishes both registry entries together. A superseded dossier is terminal;
// the successor is open and remains an independent dossier. Hypotheses are
// still replayed from the complete journal because generations contain chains
// only. ResolveHypothesis is the sole explicit bridge for one selected
// schema-1 alternative: it appends one hypothesis.resolved record containing
// both the hypothesis revision and exact chain delta, then publishes both
// registries together. It never chooses alternatives, recalculates policy,
// applies lifecycle, or integrates with ShadowEngine/Core.
package durable
