// Package hypotheses stores explicit competing interpretations in memory.
//
// A HypothesisSet preserves ambiguity without selecting, applying, or
// resolving an alternative. Association and evidence conversions accept only
// ambiguous source results and retain detached scores, facts, revisions, and
// policy provenance. Registry owns mutable sets through defensive snapshots
// and optimistic source-revision checks.
//
// Assessment rebases and evidence supersessions are explicit and append-only.
// A supersession preserves the predecessor, opens one independent successor,
// and links both through a single lineage; it never selects or applies an
// alternative. The durable coordinator and journal adapter own the WAL
// representation. This package still has no snapshot-generation, ShadowEngine,
// Core, scheduler, action, or automation dependency. Resolution is only the
// explicit schema-1 transition described by ResolveCommand/MarkResolved; no
// selection or effect is automatic, and no runtime/Core integration is
// present.
//
// Pass 24 adds immutable resolution material to assessment version one. An
// association alternative carries a detached observation attachment or a
// deterministic candidate-creation effect; an evidence alternative carries
// a fixed contribution template or an explicit no-chain effect for
// insufficiency. Legacy assessments are schema zero and remain readable but
// are not resolvable. ResolutionReadiness and PlanResolution only prepare an
// explicitly named alternative with optimistic preconditions. They never
// select, apply, or write an effect, and add no WAL, coordinator, ShadowEngine,
// or Core integration.
package hypotheses
