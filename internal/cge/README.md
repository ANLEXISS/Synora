# CGE integration boundary

`internal/cge` already contains Synora's CGE-related profile, feedback, and
danger-runtime support. Pass 1 adds the separate passive `CognitiveEngine`
boundary plus `ShadowEngine` and `NoopEngine`; the existing decision engine
remains under `internal/engine` and retains sole authority over security
decisions.

The pass-2/3 cognitive-chain domain now lives in `internal/cge/chains`. It is
distinct from the runtime `pkg/contract.EventChain` managed by
`internal/event.ChainManager`: runtime chains group operational events and
own their lifecycle, expiration, persistence, and transport projection;
cognitive chains are revisable interpretations of entities, paths, situations,
or behavioral patterns. A future component may derive a cognitive chain from
runtime chains, but the cognitive package does not depend on that runtime
model.

Future CGE responsibilities remain reserved conceptually for graph, memory,
hypotheses, investigation, matching, persistence, and explain. They are not
duplicated here because the repository already has corresponding runtime
implementation boundaries under `internal/engine`.

Invariants:

- the injected engine is shadow-only and has no authority over actions;
- observation errors are non-blocking and are logged without event payloads;
- Core passes an immutable scalar representation captured before historical
  processing can enrich or mutate the source event;
- no product behavior is changed when the engine is absent or configured as
  `NoopEngine`.

## Pass 16: durable shadow association

The optional durable shadow is composed by `NewShadowEngineWithConfig` and is
disabled unless `SYNORA_CGE_SHADOW_ENABLED` is explicitly true. Its initial
allowlist is deliberately limited to `vision.identity`, `vision.unknown`, and
`vision.uncertain`; other event types are counted as skipped. The adapter
copies only the scalar event boundary into an `ObservationRef`, never the
contract payload, image, embedding, or biometric data.

Core finishes its historical processing first and then invokes the shadow
observer synchronously. The shadow plans one of `attach_existing`,
`create_candidate`, `ambiguous`, or `already_attached`. Only the first two are
durably applied, through the existing coordinator WAL APIs. Ambiguity never
creates a chain, and an already-attached observation is an idempotent no-op.
The association score is not a confidence value and does not change status,
actions, automations, or security decisions.

The shadow uses an injected clock for planning and mutation timestamps. At
startup it loads the active generation manifest when present, otherwise an
existing journal, and initializes a journal only when explicitly authorized
with a supplied journal ID. Corrupt manifests and journals fail shadow startup
without a silent fallback; `cmd/synora-core` logs a stable error code and
continues with `NoopEngine`. Shadow errors, including recovered panics, are
non-blocking for historical Core processing. There is no automatic recovery,
snapshot, scheduler, history search, reactivation, merge, or split.

In-memory metrics contain only counters, timestamps, and stable error codes.
They contain no event, chain, device, resident, or payload identifiers. The
configured journal is closed at Core shutdown; shutdown does not create a
snapshot.

## Pass 20: in-memory competing hypotheses

`internal/cge/hypotheses` is a pure, in-memory owner for explicit ambiguity.
It has two families: `association` retains plausible existing-chain
alternatives, and `evidence` retains the evidence directions represented by
an ambiguous evaluation. Set IDs and alternative IDs are SHA-256-derived from
stable, non-sensitive identifiers; clocks are never part of an identity.

Sets begin `open` and may only move through `under_review`, back to `open`, or
to `invalidated`. `resolved` and `superseded` are reserved. Every successful
open or status change appends one local immutable revision record, and all
reads, alternatives, facts, and histories are defensive copies. The registry
owns sets, lists them by SetID, and uses source revisions for optimistic
status mutations.

This pass does not choose an alternative, attach an observation, create a
chain, add a contribution, write the global journal, update generations, or
connect to durable/runtime/Core/ShadowEngine code. Persistence, re-evaluation,
and explicit reversible resolution remain future passes.

## Pass 22: append-only hypothesis rebases

Each hypothesis set now keeps an immutable, numbered assessment history. The
current `Alternatives` and `Provenance` fields are a view of the last
assessment; older versions remain available in defensive snapshots. Semantic
assessment fingerprints include the family, subject, policy identity,
alternatives, scores, ranks, and detached fact codes, but never mutation time
or actor. Assessment IDs are deterministic SHA-256 values derived from the
set, version, and fingerprint.

`ProposeAssociationRebase` and `ProposeEvidenceRebase` are pure and accept
only ambiguous inputs. They require the same association observation or the
same evidence subject and fingerprint. An unchanged fingerprint is an
idempotent no-op; returning to an older fingerprint creates a new version and
does not resurrect the old one. `HypothesisSet.Rebase` and
`Coordinator.RebaseHypothesis` append exactly one local revision and one
`hypothesis.rebased` WAL record, preserving the status and all chain state.

Legacy opened snapshots without assessment fields synthesize version one in
memory without creating a revision or journal record. Hypothesis replay starts
from the journal genesis, including rebases, while generation snapshots remain
chain-only. No assessment is selected or applied to a chain, and no automatic
rebase, resolution, supersession, scheduler, or runtime integration exists.

## Pass 21: durable hypotheses

The global journal accepts `hypothesis.opened` and
`hypothesis.status_changed` alongside chain records. Opening stores the
complete revision-one snapshot; status changes store only the validated local
delta. Both use the same sequence, previous hash, append+sync boundary, and
coordinator lock as chain mutations.

`durable.Coordinator` owns independent chain and hypothesis registries.
Journal-only, snapshot-plus-journal, and generation-manifest recovery rebuild
both registries before publishing a coordinator. Generation snapshots remain
chain-only: hypotheses are always replayed from the complete journal, so no
compaction or truncation is possible yet. Hypothesis records never cause an
observation attachment, contribution, alternative application, or status
resolution.

## Pass 23: explicit evidence supersession

Evidence ambiguity dossiers now have an explicit append-only lineage. A pure
`ProposeEvidenceSupersession` accepts only an ambiguous evaluation for the same
chain and target observation with a different evidence fingerprint. It prepares
one deterministic successor dossier with the same root and the next generation;
the proposal does not touch either registry.

`Coordinator.SupersedeHypothesis` applies the proposal through one WAL record,
`hypothesis.superseded`. That record contains the predecessor's exact terminal
revision and the complete initial successor snapshot. After append and sync,
the coordinator publishes the superseded predecessor and open successor
together. The operation is idempotent for the exact already-applied pair;
stale revisions, occupied successors, divergent lineage, and cycles are
rejected. A chain of supersessions is therefore auditable as A -> B -> C,
while no alternative is selected or applied.

Legacy hypothesis snapshots without lineage synthesize a root generation in
memory. Generation snapshots remain chain-only, so hypotheses—including all
supersessions—are replayed from the complete journal and the journal cannot yet
be compacted or truncated. Association dossiers continue to use rebase; the
ShadowEngine and Core do not create, propose, or apply supersessions.

## Pass 24: immutable resolution effects

Assessment schema version `0` represents legacy alternatives without
resolution material. New assessments use schema version `1` and retain an
immutable `ResolutionEffect` on every alternative. Association alternatives
carry detached observation-attachment or candidate-creation parameters;
evidence alternatives carry fixed contribution templates, while
`insufficient` carries an explicit no-chain effect. The fixed values captured
by the evidence evaluator are never recomputed from scores or current chain
confidence.

`Snapshot.ResolutionReadiness` reports whether the current assessment can
support a future explicit resolution. `PlanResolution` requires the caller
to name the alternative and returns only a defensive plan containing the
assessment, revision, and effect preconditions. It performs no ranking,
selection, mutation, WAL append, coordinator call, chain change, observation
attachment, chain creation, contribution, confidence update, or runtime/Core
integration. Legacy assessments remain valid and replayable but require an
explicit modern rebase before they can become resolvable.

## Pass 25: durable explicit resolution

`ResolutionPlan.Command` converts one explicitly named schema-1 alternative
into an optimistic `ResolveCommand`. The caller supplies the alternative ID;
the resolver never ranks, compares, chooses, or reinterprets policy. The
immutable effect has a deterministic fingerprint and its real application
produces one typed outcome: attach an observation, create a candidate, add a
fixed contribution, or perform an explicit no-chain effect.

`Coordinator.ResolveHypothesis` clones both registries, applies the effect
through existing chain-domain operations, marks the hypothesis `resolved`
through its specialized operation, validates both candidates, and appends
exactly one `hypothesis.resolved` WAL record before publishing both together.
The dossier retains every assessment and alternative, is terminal, and
cannot be rebased or superseded. `SetStatus` cannot enter `resolved`.

The record contains the hypothesis revision, selected assessment and
alternative, effect fingerprint, actual outcome, and one exact chain-delta
union. Chain and hypothesis replay validate that common payload independently;
neither publishes partial state. Generation snapshots remain chain-only:
pre-checkpoint effects are already in the snapshot and post-checkpoint
effects replay once. Identical resolved commands are explicit idempotent
reads; divergent commands and stale revisions are rejected. Resolution never
applies lifecycle, confirmation, invalidation, actions, automation,
ShadowEngine/Core behavior, or HTTP exposure.

## Pass 26: qualification bench

`internal/cge/validation` is a development-only behavioral and transactional
bench. Its typed scenarios use the durable coordinator, the real association
and evidence evaluators, hypothesis planning, the global journal, replay, and
generation manifests. Alternatives are always named by the scenario; the
runner never chooses by score or rank.

The catalogue covers explicit attach, create-candidate planner behavior,
support, contradiction, neutral, insufficient/no-chain, stale-plan behavior,
and the supersession qualification entry. `CheckpointMatrix` and
`JournalReplayScenarios` exercise manifest and journal-only recovery. State
digests sort aggregates by stable IDs and compare detached snapshots without
exposing raw payloads. `ValidateCoordinatorState` checks domain and
cross-domain invariants after each mutating step.

Run it from the repository root with:

```text
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-validation list
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-validation run-all --json
```

The tool uses a temporary directory by default, supports `--output` and
`--keep-data`, and is not connected to ShadowEngine, Core, HTTP, or any
runtime startup path.

## Pass 27: transactional qualification

The qualification matrix is separate from the behavioral catalogue. It audits
the public association planner before claiming `create_candidate`: the current
planner produces that decision when no eligible candidate remains, while an
ambiguous plan carries existing candidates only. Therefore
`create_candidate` is transactionally implemented and directly qualified, but
its ambiguous cognitive path is currently dormant under the public policy.
The stable reason is `association_create_candidate_not_reachable`; the bench
does not add that alternative to ambiguous plans.

The matrix exercises rejected and uncertain appends, sync uncertainty,
external journal changes, context cancellation, publication failure and
explicit recovery. It also covers same/different alternatives, rebase,
supersession, status and chain races, distinct dossiers, checkpoints,
collisions, and exact idempotent repeats. Fault seams are installed only by
validation tests and are not production configuration.

Run the qualification report with:

```text
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-validation qualify
GOCACHE=/tmp/synora-gocache go run ./tools/dev/synora-cge-validation qualify --json
```

`qualify` reports cognitive reachability separately from transactional,
WAL-failure, concurrency, checkpoint, collision, and idempotence status. It
returns a non-zero exit code when a critical capability is not qualified.
No scenario is started by ShadowEngine or Core; no lifecycle transition,
action, automation, or policy recalculation is performed.
