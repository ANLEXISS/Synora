// Package registry owns mutable cognitive chains for the in-memory CGE
// boundary. It exposes only defensive snapshots and explicit lifecycle
// application; it has no persistence, scheduler, runtime integration, or
// decision-making authority.
//
// EvaluateLifecycle captures one coherent snapshot view under the registry
// read lock and evaluates it after releasing the lock. EvaluationBatch is a
// value describing that view, so a later mutation can make an included
// proposal stale. ApplyLifecycleBatch and ApplyLifecycleProposals apply
// selected proposals explicitly, in ChainID order, and are transactional per
// chain but deliberately non-atomic across the batch. An error for one chain
// does not roll back successful applications for other chains.
package registry
