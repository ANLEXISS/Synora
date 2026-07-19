# CGE chains domain

This package defines the cognitive-chain domain value for a chain of
observations. A cognitive chain is a revisable interpretation of an entity,
path, situation, or behavioral pattern. It contains only detached observation
references, an ordered node path, explicit confidence contributions, lifecycle
labels, and local revision provenance.

It is distinct from the runtime chain in `pkg/contract.EventChain`, managed by
`internal/event.ChainManager`. The runtime chain groups operational events and
has its own lifecycle, expiration, persistence, and transport/API projection.
A future CGE component may derive a cognitive chain from one or more runtime
chains, but this package does not depend on or import that runtime model.

It does not construct chains from Core events, expire or decay them, persist
them, maintain a global graph version, merge or split them, manage competing
hypotheses, make security decisions, or produce actions.

## Lifecycle

The cognitive lifecycle is explicit and controlled by `SetStatus`; no confidence
threshold, timer, absence interval, or new observation changes status
automatically.

| Status | Meaning |
| --- | --- |
| `candidate` | Newly formed from observations that are not yet sufficient for the reliable model. |
| `active` | Used by the active model, with limited stability. |
| `confirmed` | Sufficiently confirmed to represent a reliable relation or pattern in the active model. |
| `declining` | Still known, but its current relevance is explicitly decreasing; no temporal decay is calculated here. |
| `dormant` | Removed from active reasoning but retained as quickly reactivatable memory. |
| `archived` | Removed from the operational model and retained for audit or deeper historical search; archival is logical and does not delete data. |
| `reactivated` | A dormant or archived chain explicitly returned to temporary observation after reappearance. |
| `merged` | Reserved marker for a future chain replaced by a fusion result; not produced in this pass. |
| `split` | Reserved marker for a future chain replaced by several chains; not produced in this pass. |
| `invalidated` | Retained chain known to be incorrect, corrupt, or insufficiently founded; it has no lifecycle exit in this pass. |

Active-model statuses are `active`, `confirmed`, `declining`, and
`reactivated`. `candidate` is pre-model. `dormant` and `archived` are
historical memory statuses. `merged` and `split` are replacement statuses.
`invalidated`, `merged`, and `split` are terminal; `archived` is not terminal
because explicit reactivation is permitted.

The allowed transitions are:

```text
candidate   -> active, invalidated
active      -> confirmed, declining, dormant, invalidated
confirmed   -> declining, dormant, invalidated
declining   -> active, confirmed, dormant, archived, invalidated
dormant     -> reactivated, archived, invalidated
archived    -> reactivated, invalidated
reactivated -> active, confirmed, declining, dormant, invalidated
```

Same-state transitions, unknown states, transitions to `merged` or `split`,
and all exits from terminal states are rejected. An archival transition is
recorded as `chain.archived`, a reactivation as `chain.reactivated`, and other
allowed lifecycle transitions as `status.changed`.

## Lifetime policy

`EvaluateLifecycle(snapshot, evaluatedAt, policy)` is a pure evaluation. Its
three temporal anchors are derived from the defensive revision history:

```text
Age                = evaluatedAt - CreatedAt
InactiveFor        = evaluatedAt - LastSeenAt
InCurrentStatusFor = evaluatedAt - StatusChangedAt
```

`CreatedAt` is `chain.created`; `LastSeenAt` is the latest observation, or
`CreatedAt` for a chain without observations; `StatusChangedAt` is the latest
`status.changed`, `chain.archived`, or `chain.reactivated`, or `CreatedAt`
before the first lifecycle transition. `Snapshot.CreatedAt()`,
`Snapshot.StatusChangedAt()`, and `Snapshot.StatusSince()` expose these derived
anchors. Runtime-chain expiration in `internal/event.ChainManager` is a
separate operational mechanism.

The default policy is:

| Setting | Default |
| --- | ---: |
| `CandidateTTL` | `1h` |
| `ActiveDeclineAfter` | `2h` |
| `ConfirmedDeclineAfter` | `6h` |
| `DecliningDormantAfter` | `24h` |
| `DormantArchiveAfter` | `168h` |
| `MinConfidenceToRemainActive` | `0.35` |
| `MinConfidenceToRemainConfirmed` | `0.55` |
| `MaxCandidateContradictions` | `3` |

All durations must be positive. The only cross-duration constraint is
`ConfirmedDeclineAfter > ActiveDeclineAfter`, because confirmed chains are
intentionally given a longer observation-inactivity tolerance. Candidate TTL,
declining retention, and dormant retention use different anchors and may be
ordered independently. Confidence thresholds are in `[0,1]`, with the
confirmed threshold strictly above the active threshold, and the contradiction
threshold must be positive. Invalid policies produce no evaluation.

Evaluation priority is deterministic: candidate contradictions, confidence
below the applicable threshold, then inactivity. Candidate, active, and
confirmed rules use `InactiveFor`. Declining and dormant rules use
`InCurrentStatusFor`, which prevents an old chain from cascading through
multiple statuses at one evaluation timestamp. A candidate becomes proposed
`invalidated`; active or confirmed becomes `declining`; declining becomes
`dormant`; dormant becomes `archived`. `archived`, `merged`, `split`, and
`invalidated` have no temporal proposal. A reactivated chain is never promoted
automatically; after `InCurrentStatusFor >= ActiveDeclineAfter` it may only be
proposed as `declining`, using that threshold conservatively.

Reason codes are stable (`candidate.ttl_expired`,
`candidate.too_many_contradictions`, `active.confidence_below_threshold`,
`active.inactive`, `confirmed.confidence_below_threshold`,
`confirmed.inactive`, `declining.inactive`, `dormant.retention_expired`, and
`reactivated.inactive`). Their time bases are respectively observation
inactivity for candidate/active/confirmed and current-status duration for
declining/dormant/reactivated. Proposals contain `Age`, `InactiveFor`,
`InCurrentStatusFor`, `StatusChangedAt`, thresholds, and deterministic facts;
they do not delete, archive, or otherwise mutate chain data. Durations are
initial domain defaults and require calibration through simulation and
real-world tests before any product policy is inferred.

`CurrentConfidence` is the bounded `[0,1]` confidence accumulated from explicit
support and contradiction contributions. `HistoricalReliability` is a separate
caller-supplied bounded value; no statistical or temporal update is inferred in
this pass. Every chain starts at revision 1 with `chain.created`. Each later
successful mutation requires a `MutationContext` and appends one delta-only
`RevisionRecord`; rejected mutations leave state and history unchanged.

Contributions are explicit `ConfidenceContribution` values with a stable ID,
source, kind (`support`, `contradiction`, or `neutral`), bounded value, reason,
creation time, and optional IDs of observations already present in the chain.
Support adds the value to current confidence and increments
`ConfirmationCount`; contradiction subtracts the value and increments
`ContradictionCount`; neutral changes neither confidence nor counters. Results
are clamped to `[0,1]`, and `MaxHistoricalConfidence` is updated only when the
new current confidence exceeds it. Each contribution creates exactly one
`contribution.added` local revision. Status and historical reliability remain
unchanged. Candidate, active, confirmed, declining, dormant, archived, and
reactivated chains accept explicit contributions; merged, split, and
invalidated chains do not.

Durable contribution records use the compact `chain.contribution_added` delta,
including confidence and support/contradiction counter values before and after
the domain operation. Replay reconstructs the mutation by calling
`Chain.AddContribution`; it never assigns confidence or counters directly.
The coordinator follows clone → domain mutation → validation → append+sync →
publication. Association and ShadowEngine flows do not create contributions
automatically in this pass.

`Chain` is intentionally not concurrency-safe. Its owner must serialize access;
the `Snapshot` and `History` methods return defensive copies so readers cannot
mutate the aggregate through returned slices. The audit trail is local and
in-memory only; it is not persistence or a global graph journal.

The `chains/registry` subpackage is the owner boundary for mutable chain
instances. It stores deep clones, serializes access with an in-memory
`RWMutex`, returns only snapshots, and applies lifecycle proposals through
optimistic source-revision checks. It is not connected to `ShadowEngine` or to
the runtime chain manager.

## Pure evidence evaluation

`chains/evidence` evaluates one explicitly selected observation already present
in one defensive `chains.Snapshot`. It selects a bounded context from the same
chain using the target timestamp plus/minus `Policy.ContextWindow`, ordering
ties by distance, timestamp, and observation ID. It never searches another
chain, the historical registry, or a physical graph.

The evaluator calculates integer `SupportScore` and `ContradictionScore`
separately from explainable, stable-code `EvidenceFact` values. Current
confidence, maximum historical confidence, lifecycle status, and historical
reliability are not evidence and do not participate in either score. An
uncertain observation exposes an explicit penalty fact that reduces both
directional scores; it cannot create a strong contradiction by absence of
identity. Unknown observations may produce a fixed neutral proposal when
continuity is non-discriminating, while missing or non-conclusive evidence is
reported as `insufficient_evidence`.

The six decisions are `propose_support`, `propose_contradiction`,
`propose_neutral`, `insufficient_evidence`, `ambiguous`, and
`already_evaluated`. A support or contradiction decision requires its
configured threshold and decision margin, and mixed or close evidence remains
ambiguous. No decision is applied by this package.

The default policy is `synora.cge.evidence/evidence-v1`, with a five-minute
context window, at most sixteen context observations, thresholds 80/90, and a
minimum margin of 25. Its fixed contribution values are 0.10 for support,
0.15 for contradiction, and exactly 0 for neutral. These values are contribution
magnitudes, not conversions of evidence scores into confidence.

Proposal IDs use `cge-evidence-` followed by SHA-256 of the policy namespace,
chain ID, target ID, and selected context IDs. The policy version is retained
in provenance but is intentionally not part of the identity, so changing only
the version cannot silently count the same evidence twice. A same-ID semantic
mismatch is an explicit `evidence_contribution_collision`; a matching existing
contribution yields `already_evaluated`.

`ContributionProposal.Command` is the only bridge to durable mutation. It
returns a detached `chains.AddContributionCommand` and does not call the
coordinator, append a journal record, create a snapshot, alter lifecycle, or
produce a contribution automatically. Applying it remains an explicit caller
operation in a later integration boundary. The evaluator also has no
ShadowEngine, Core, HTTP, scheduler, action, automation, reactivation, merge,
split, or ambiguity-resolution dependency.

## Explicit evidence batches

`evidence.EvaluateBatch` is a pure bounded pass over defensive snapshots. It
sorts chains by `ChainID` and observations by timestamp then ID, filters active
and historical statuses through `BatchOptions`, and retains at most one
applicable proposal per chain. Once the first proposal for a chain is selected,
later selected observations are represented as
`deferred_after_selected_proposal`; they are not claimed to have been
evaluated. Non-contributive results (`already_evaluated`, `ambiguous`, and
`insufficient_evidence`) remain visible in the batch.

`durable.Coordinator.EvaluateEvidenceBatch` captures a coherent defensive view
and delegates to the pure package. It is readable while degraded, but
`ApplyEvidenceProposals` requires `ready`. Application sorts proposals by
`ChainID`, validates all duplicate and malformed inputs before the first write,
then runs the existing contribution WAL independently per proposal. The batch
is therefore atomic per proposal, not globally: successful applications remain
when a later proposal is stale, missing, rejected, or uncertain. Repeating an
identical already durable contribution is an idempotent no-op; the same ID with
different semantics is a collision.

Batch results are in-memory only. No batch is journaled, no snapshot is created,
no scheduler or persistent goroutine exists, and neither ShadowEngine nor the
Core invokes this orchestration layer.

## Explicit registry evaluation batches

`registry.Registry.EvaluateLifecycle` takes an explicit evaluation timestamp and
policy. It captures all snapshots under one read lock, releases that lock, and
then delegates every rule to `chains.EvaluateLifecycle`. The resulting
`EvaluationBatch` is sorted by `ChainID` and contains one `ChainEvaluationResult`
per captured chain, including a stable error code if one snapshot cannot be
evaluated. A globally invalid policy or timestamp rejects the operation before
capture. No evaluation mutates a chain or applies a proposal.

`ApplyLifecycleBatch` applies all proposals in a batch, while
`ApplyLifecycleProposals` supports explicit partial selection. Proposals are
sorted by `ChainID`; duplicate chain IDs are rejected before any duplicate is
applied. Each proposal is revalidated through the existing single-chain
application path, including its source revision, source status, anchors, and
allowed transition. Concurrent applications therefore use optimistic
concurrency: one succeeds and later applications of the same source revision
return `stale_proposal`.

Batch application is transactional per chain, not globally. Successful chains
remain committed when another proposal is stale, missing, invalid, or fails
validation. Correlations supplied to the batch are deterministically extended
with `/<ChainID>` for each mutation. No scheduler, ticker, background worker,
persistence, or runtime consumer exists in this pass.

## Local snapshot persistence

The `chains/persistence` subpackage adds an explicit `FileStore`; it is not
called by the registry, Core, or `ShadowEngine`. A save captures the registry's
defensive `List()` view, validates every chain, sorts by `ChainID`, and writes a
schema-versioned JSON envelope. The envelope contains `schema_version: 1`, an
explicit `created_at`, the JSON `payload`, and a `sha256:<hex>` checksum over the
payload bytes only. The default complete-file limit is 64 MiB and the default
mode is `0640`; globally writable modes are rejected.

The write uses a temporary file in the target directory, applies the mode,
writes and syncs the complete envelope, closes it, then renames it over the
destination and best-effort syncs the parent directory. There is no `.bak`,
rotation, append-only journal, or automatic save. Load verifies the size,
single-object JSON shape, schema, checksum, count, duplicate IDs, and every
chain through `chains.Restore` before adding anything to a new registry. Any
failure returns no partial registry. Only detached observation references,
contributions, confidence values, lifecycle state, counters, anchors, and
local revision history are stored; raw event payloads, images, secrets,
tokens, and logs are excluded. Future schema migration is intentionally
reserved for a later pass.

## Global append-only journal

The `chains/journal` package is separate from each chain's local revision
history. Local history explains mutations within one chain; the global journal
orders registry-level events across chains. Schema v1 is strict NDJSON: one
complete UTF-8 JSON object and final newline per physical record. Records start
with `journal.genesis`, use sequences beginning at 1, and form a SHA-256 chain
from the fixed `GenesisPreviousHash` through `PreviousHash` and `RecordHash`.
`PayloadSHA256` covers the exact payload bytes; `RecordHash` covers version,
sequence, kind, canonical UTC timestamp, actor, correlation, previous hash,
exact payload, and payload checksum.

Implemented records are `chain.added`, `chain.lifecycle_transitioned`, and
`snapshot.checkpointed`. A checkpoint identifies snapshot metadata and the
journal head immediately before the checkpoint record; it does not create or
verify a snapshot file. `FileJournal` serializes writers with one
process-local mutex, but does not provide a cross-process lock. It revalidates
the file before each append and rejects an external head or size change. A
truncated last line is reported as corruption and is never repaired
automatically.

The journal has no replay, registry mutation, startup loading, automatic
append, scheduler, rotation, recovery, or runtime consumer. A future replay
will combine a complete registry snapshot with valid records after its
checkpoint sequence.
