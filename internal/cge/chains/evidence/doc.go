// Package evidence provides a pure, explainable evaluator for one observation
// already present in one cognitive-chain snapshot.
//
// The evaluator reads only detached chains.Snapshot values and explicit policy
// input. It computes separate support and contradiction evidence, returns a
// deterministic decision, and may construct a contribution proposal. It does
// not mutate a chain, select another chain, write a journal, call a
// coordinator, or apply a contribution. The caller must perform any durable
// application explicitly through the existing contribution command API.
//
// Evidence scores are not confidence values. The evaluator retains the fixed
// support, contradiction, or neutral values configured by Policy in
// ResolutionValues so a later explicit resolution cannot recalculate an old
// assessment with a newer policy. Current
// confidence, lifecycle status, and historical reliability are deliberately
// excluded from scoring.
//
// EvaluateBatch is the bounded pure orchestration layer. It orders chains and
// observations deterministically, keeps at most one applicable proposal per
// chain, and records later observations as deferred after that proposal. It
// does not persist the batch or apply any contribution.
package evidence
