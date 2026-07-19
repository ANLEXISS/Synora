// Package journal provides an explicit, global, append-only audit journal for
// cognitive-chain registry deltas.
//
// It is deliberately distinct from chains.Chain's local mutation history:
// the local history explains one chain, while this journal orders registry
// events across chains. This pass only writes and validates NDJSON records; it
// does not mutate or replay a registry, load at startup, schedule writes, or
// integrate with Core or ShadowEngine.
// chain.observation_added is a compact delta containing the selected chain,
// detached ObservationRef, contiguous revisions, and the exact local
// RevisionRecord. It contains no full-chain snapshot or raw event payload.
// chain.contribution_added is a compact delta containing the detached
// ConfidenceContribution, exact confidence/counter projections, contiguous
// revisions, and the local RevisionRecord. It is accepted under schema v1 and
// is validated before append and during journal reads.
// hypothesis.opened contains the complete initial hypothesis snapshot;
// hypothesis.status_changed contains only the explicit local status delta.
// hypothesis.rebased contains only the newly appended assessment version and
// its exact local revision. hypothesis.superseded is one atomic global record:
// it contains the predecessor's terminal status delta and the complete
// revision-one successor snapshot. All four records share the same sequence
// and hash chain as chain records. Supersession is evidence-only in this pass;
// it never changes a chain or selects an alternative.
package journal
