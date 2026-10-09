# Face gallery enrichment V1: status and safety boundary

## Current contract

`synora.vision.evidence/v1` admits `face.result=candidate` only when face
availability is `evaluated`. The value is an aggregate semantic result: it
does not carry a resident identifier, image, crop, embedding, local track ID,
or file reference. The plate vocabulary remains unchanged. Candidate face
facts have no feature in the fixed CognitiveSnapshot V3 encoder; they must not
be mapped onto another feature or change the 86D layout.

The Python and Go validators share the versioned JSON Schema and golden fixture.
The extension is a contract capability only; it does not mean that the
recognizer emits candidates or that a candidate is persisted/promoted.

## Existing local storage and limits

The current local face source/dataset code is separate from the Universal
Store. It validates image type, dimensions, hashes, storage-key components and
immutable dataset manifests, and switches dataset versions through a
rollback-capable pointer. Dataset manifests include embeddings and source
references. Filesystem permissions are used; there is no application-level
encryption or managed-key integration. Therefore this storage is not claimed
to be a hardened biometric vault. Do not enable an adaptive candidate gallery
or import private media until the host's ownership/permissions, explicit
consent, retention, and recovery procedure have been reviewed.

The face worker is opt-in by configuration and its current similarity model
and thresholds have not been independently qualified. A model file being
present, a unit test passing, or a local image matching is not qualification.
No recognition status from this implementation may be treated as a reliable
identity decision or used to weaken the deterministic Safety Gate.

## Provisional policy target (not yet active)

The proposed thresholds are configuration defaults for future controlled
evaluation, not calibrated operating points:

| Score | Intended result | Persistence |
|---|---|---|
| `< 0.65` | `unknown` | none |
| `0.65–<0.90` | `candidate` | local-only, bounded TTL, only after all quality/track/episode/quota checks |
| `>= 0.90` | `recognized` | only with a qualified backend and independent, consistent observations |

Candidate-only self-bootstrap is forbidden. A promotion requires a distinct,
high-confidence anchor for the same resident in the same continuous episode
and local track, or a separately audited manual approval. Multiple faces,
ambiguous identity, low quality, stale/restarted episodes, duplicate samples,
quota exhaustion, invalid source gallery, simulated/replay provenance, or an
unqualified backend must fail closed. This policy is not implemented as a
candidate persistence/promotions workflow yet; no such workflow is claimed.

## Privacy and qualification status

The central harness does not open a facial model or private gallery. Its face
signals are synthetic aggregate inputs only. The local consent smoke test is
not implemented and must remain unrun until an explicit, verifiable consent
manifest and an approved local workflow exist. Private media must never be
copied into Git, reports, logs, bus messages, Core, Universal Store, or the
general API. Any unexpected identity/path/embedding field is a rejection, not
a field to redact after publication.

Current classification: `prepared_blocked` for adaptive gallery persistence
and real facial recognition. Contract candidate support is implemented;
facial backend qualification, consented independent evaluation, a hardened
vault/retention workflow, and a local consent smoke test remain open.

## V1 milestone snapshot

States use only the project vocabulary. This is a source audit, not a claim of
field qualification; rerun the central and full validation suites for the
current commit before treating software tests as current evidence.

| Function V1 | State | Evidence | Remaining blocker | Next milestone |
|---|---|---|---|---|
| Camera ingress / Discovery | `implemented` | Discovery ingress and central transport/resilience cases | Hardware installation/recovery qualification | Complete controlled J2/J3/J4 evidence |
| Evidence V1 / Core / Store | `implemented` | Strict V1 validator and central contract journeys | Full runtime deployment audit | Validate all producers and deployed ACLs |
| Pose and fall | `prepared_blocked` | Aggregate contracts and optional replay harness | No qualified real fall corpus/backend evidence | Label independent clips and qualify metrics |
| Face / resident gallery | `prepared_blocked` | Local source/dataset primitives; candidate contract extension | Consent, vault hardening, model qualification and independent evaluation absent | Build approved vault/policy workflow before local smoke |
| Vehicles | `not_configured` | Suite slots/contracts only | No qualified backend or labeled corpus | Register and qualify a backend |
| Plates | `not_configured` | Aggregate contract only | No qualified backend or labeled corpus | Register and qualify a backend |
| Animals | `not_configured` | Suite slots/contracts only | No qualified backend or labeled corpus | Register and qualify a backend |
| Camera health | `simulated_only` | Deterministic central scenarios | Device-level recovery/tamper evidence | Qualify against controlled hardware faults |
| Topology / non-biometric tracking | `simulated_only` | Synthetic Edge/Discovery contract tests | Independent device/replay evidence | Validate on approved non-sensitive clips |
| MLP V3 / five heads | `simulated_only` | Candidate bundle tests in `active_dry_run` | No promotion requested or authorized | Keep dry-run pending independent qualification |
| Abstract actions / Safety Gate | `implemented` | Deterministic gate and dry-run executor tests | Hardware action qualification intentionally absent | Preserve dry-run; separately review any future actuator work |
| Communication / TTS | `not_configured` | Gate contracts only | No real TTS/render validation requested | Keep all real audio disabled |
| Redacted state API | `implemented` | Authenticated API projection tests | Deployed identity/ACL audit | Verify deployment configuration without exposing private fields |
| Central regression harness | `implemented` | `cmd/synora-central-test`, static/mock/media suites | Real media inputs/backends are optional and separately qualified | Continue central regression on each change |
