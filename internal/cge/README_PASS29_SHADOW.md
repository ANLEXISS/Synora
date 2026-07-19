# Pass 29 — cognitive Shadow orchestration

Pass 29 adds an opt-in orchestration layer behind `CognitiveShadowConfig`.
The existing Shadow path remains association-only when the cognitive flag is
disabled, and the production default is disabled.

## Configuration

Environment variables:

* `SYNORA_CGE_SHADOW_COGNITIVE_ENABLED`
* `SYNORA_CGE_SHADOW_AUTO_EVIDENCE_ENABLED`
* `SYNORA_CGE_SHADOW_MAX_EVIDENCE_REEVALUATIONS`

The safe defaults are `false`, `false`, and `8`; the reevaluation bound is
limited to 64. A disabled cognitive layer creates no additional coordinator,
journal, replay, digest, or validation work.

## Runtime order

An admissible observation is planned once. An `attach_existing` or
`create_candidate` plan is applied through the existing WAL boundary, then
only the resulting chain and observation are sent to
`evidence.EvaluateObservation`. Open evidence dossiers on that chain are
reevaluated in a bounded, deterministic temporal order.

`ambiguous` association opens or rebases an association hypothesis and does
not evaluate evidence. An ambiguous evidence result opens, rebases, or
supersedes an evidence dossier. An evidence decision with an existing
non-terminal dossier is reported as `resolution_candidate_only`.

`association_create_candidate` remains a non-ambiguous public planner result
when no eligible chain remains. It is not converted into an alternative.

Every durable operation remains its own existing WAL record. An interruption
may leave a durable association before later evidence work; that is a valid
state and can be completed by a later observation. No automatic replay is
started per observation.

## Safety boundary

The orchestrator never calls `ResolveHypothesis`, never selects an
alternative, never applies lifecycle, and never emits a security decision,
action, automation, or historical-engine input. The Core historical result is
already determined before the isolated Shadow call; Shadow failures and
panics are contained and counted.

The actor is `synora-core/cge-shadow`. Mutation reasons are stable
`shadow.*` categories and correlations are deterministic SHA-256-derived
values with bounded length. Results and metrics contain aggregate diagnostics
only; no payloads, identities, SetIDs, ChainIDs, or observation IDs are
exposed by Explain/Snapshot diagnostics.

## Indexes and cost budgets

Hypothesis subject indexes are rebuilt from dossiers after recovery, clone,
add, rebase, supersession, status change, and resolution. They are not part
of snapshots or WAL. Targeted lookup is bounded by the returned dossier;
`ListOpenEvidenceForChain` is bounded by the active chain's open dossiers and
uses stable observation/generation/SetID ordering.

The runtime budget is structural rather than machine-time based:

* no full hypothesis scan for a subject lookup;
* no full journal read, replay, global digest, or global coordinator validation
  per observation;
* reevaluations are capped by `MaxEvidenceReevaluationsPerObservation`;
* mutation copies are targeted to the changed chain/hypothesis entries;
* all WAL append and publication guarantees remain those of the existing
  coordinator.

Benchmarks are in `internal/cge/shadow_orchestrator_benchmark_test.go`,
`internal/cge/hypotheses/registry_index_benchmark_test.go`, and the existing
pass-28 validation benchmark suite. Profiles, when requested, belong in
`/tmp` and are not versioned. A reproducible focused profile is:

```bash
GOCACHE=/tmp/synora-gocache go test ./internal/cge -run '^$' \
  -bench 'BenchmarkShadowProcessObservation' -benchmem \
  -cpuprofile=/tmp/cge-pass29-cpu.pprof \
  -memprofile=/tmp/cge-pass29-mem.pprof
```

The standard qualification path is `go run ./tools/dev/synora-cge-validation
qualify`; `qualify --full` selects the 500-item workload. Neither mode is
started by the runtime.
