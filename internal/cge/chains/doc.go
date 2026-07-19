// Package chains defines the autonomous CGE domain model for cognitive chains.
//
// A Chain is a caller-owned, non-concurrent aggregate of immutable observation
// references with a local append-only mutation history and explicit lifecycle
// transitions. It is distinct from the runtime EventChain used to group
// operational events: a future CGE component may interpret one or more runtime
// chains, but this package does not depend on that model.
// This package does not build chains from Core events, expire or decay them,
// persist them, merge or split them, or make security decisions.
// EvaluateLifecycle is a pure, explicitly dated policy evaluation: it can
// return a validated transition proposal but never applies one or starts a
// timer, scheduler, or background task.
// Its temporal anchors are derived from the revision history: creation,
// latest observation (or creation for an empty chain), and the latest actual
// lifecycle transition.
// Restore reconstructs a validated, deeply independent aggregate from a
// Snapshot without appending a domain revision; local file persistence uses it
// as the only safe restoration boundary.
// AddObservationCommand is the explicit, caller-selected observation mutation
// command. It accepts only candidate, active, confirmed, declining, and
// reactivated chains; it never reactivates historical chains, computes
// confidence, creates contributions, or selects a chain automatically.
// AddContributionCommand is the explicit, caller-selected evidence mutation.
// Contributions are support, contradiction, or neutral interpretations of
// existing observation IDs. They never change lifecycle status or historical
// reliability, and dormant or archived chains are not implicitly reactivated.
package chains
