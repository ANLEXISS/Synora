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

Face processing is forced unavailable until a qualification artifact is
implemented and reviewed. The worker's dry-run capabilities also report face
inference as unavailable. Face recognition logs contain neither identity nor
similarity score; worker debug timelines are disabled by default, trace detail
payloads are redacted, and aggregate worker metrics do not expose an identity
map.

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

The face worker is fail-closed: no qualification artifact exists, so an
explicit enable flag still reports the backend as `unavailable` and does not
construct the face recognizer. Its current similarity model and thresholds
have not been independently qualified. A model file being present, a unit
test passing, or a local image matching is not qualification.
No recognition status from this implementation may be treated as a reliable
identity decision or used to weaken the deterministic Safety Gate.

## Provisional score thresholds and policy status

The recognizer defaults are now 0.65 for the uncertainty floor and 0.90 for
match (`SYNORA_VISION_FACE_CANDIDATE_THRESHOLD` and
`SYNORA_VISION_FACE_MATCH_THRESHOLD`), with a 0.90 temporal-consistency
default. These are provisional
configuration thresholds for future controlled evaluation, not calibrated
operating points. The current worker maps the middle band to its conservative
`uncertain` result; it does not persist a `candidate` sample.

| Score | Intended result | Persistence |
|---|---|---|
| `< 0.65` | `unknown` | none |
| `0.65–<0.90` | `uncertain` today; target is `candidate` | no candidate persistence is active |
| `>= 0.90` | `recognized` by the unqualified opt-in worker path | not qualified; do not enable as a resident identity decision |

Candidate-only self-bootstrap is forbidden. A promotion requires a distinct,
high-confidence anchor for the same resident in the same continuous episode
and local track, or a separately audited manual approval. Multiple faces,
ambiguous identity, low quality, stale/restarted episodes, duplicate samples,
quota exhaustion, invalid source gallery, simulated/replay provenance, or an
unqualified backend must fail closed. The candidate persistence/promotions
workflow is not implemented; no such workflow is claimed.

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

## Inactive resident-gallery foundations

The repository now contains an inactive software foundation; this does not
enable enrollment or facial recognition. Discovery exposes the following
redacted administration surface on its existing HTTP server:

| Route | Authorization | Effect |
|---|---|---|
| `POST /api/residents` | `resident:write` and `face_gallery:manage` | Creates only an opaque logical resident record; accepts an idempotency key and no identity fields |
| `GET /api/residents/{resident_ref}/face-gallery/status` | Existing authenticated API token | Returns only opaque reference, gallery state, generation and policy version |
| `POST /api/residents/{resident_ref}/face-gallery/rollback` | Both administration scopes | Resets the logical gallery state; no image or model operation |
| `DELETE /api/residents/{resident_ref}` | Both administration scopes | Logically disables the resident record |

Scoped API credentials are configured by SHA-256 token hash under
`api_token_scopes`; raw tokens are never stored there. The legacy API token
does not gain administrative scopes implicitly. The administrator must
provision both exact scopes out of band. HTTP errors do not disclose which
scope was missing. The public status projection contains no name, image,
embedding, score, path, or identity.

Core owns the persisted logical resident registry in the Universal Store.
Records contain an opaque `resident_ref`, one of `not_enrolled`, `prepared`,
`unavailable`, `disabled`, or `error`, a gallery generation, the policy
version, redacted error category, timestamps and logical deletion state.
Idempotency keys are hashed before persistence. Store restart validates the
registry and fails closed on corrupt records. No resident name or biometric
material is written there.

The local consent reader accepts only a server-configured private root and a
server-derived filename from the opaque reference. It validates the strict
versioned fields, allowlisted source-set ID and digest, time bounds, revocation
state and policy version. A `simulated_test` attestation is always rejected by
the real validator. This is a technical audit record only, not proof of legal
consent; no real attestation was created during this work.

The separate vault abstraction supports an empty versioned manifest, atomic
generation pointer, digest verification and rollback. It rejects symlinks,
traversal, noncanonical roots and directories with broad permissions. The
vault was exercised only in temporary test directories and remains empty.
Protection is local filesystem permissions and separation; application-level
encryption and managed keys are explicitly out of scope. The test-only
candidate lifecycle is memory-only, uses synthetic opaque case identifiers,
enforces TTL, deduplication and quotas, and cannot write to the local vault.
Thresholds `.65`/`.90`, minimum quality `.70`, 24-hour candidate TTL, 16
pending candidates, four per episode and 40 per day are provisional test
policy values, not calibrated recognition operating points. The test policy
limits pending samples to 16 per resident, four per episode, and 12 per day.

`FACE_BACKEND_QUALIFIED` remains `false`; the production worker reports face
recognition unavailable and does not instantiate the face recognizer. The
deterministic test backend accepts only named synthetic cases, no images or
model files. Therefore these foundations are not a private-media smoke test,
not real replay verification, and not facial-recognition qualification. No
photos were opened, copied, hashed, moved or enrolled by these foundations.
The CognitiveSnapshot V3 86D layout and its offsets are unchanged, and face
signals remain unencoded unless an existing mapping is explicitly added in a
separate reviewed task.

## V1 milestone snapshot

States use only the project vocabulary. This is a source audit, not a claim of
field qualification; rerun the central and full validation suites for the
current commit before treating software tests as current evidence.

| Function V1 | State | Evidence | Remaining blocker | Next milestone |
|---|---|---|---|---|
| Camera ingress / Discovery | `implemented` | Discovery ingress and central transport/resilience cases | Hardware installation/recovery qualification | Complete controlled J2/J3/J4 evidence |
| Evidence V1 / Core / Store | `implemented` | Strict V1 validator and central contract journeys | Full runtime deployment audit | Validate all producers and deployed ACLs |
| Pose and fall | `prepared_blocked` | Aggregate contracts and optional replay harness | No qualified real fall corpus/backend evidence | Label independent clips and qualify metrics |
| Face / resident gallery | `prepared_blocked` | Opaque resident API/Store, scope checks, technical attestation reader, empty vault and synthetic policy tests | Real consent, qualified backend, independent evaluation, encryption/key review and private smoke absent | Obtain verifiable consent out of band; separately qualify backend before any approved smoke |
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
