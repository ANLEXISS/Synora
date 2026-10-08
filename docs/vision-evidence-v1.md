# Vision Evidence V1

`synora.vision.evidence/v1` is the closed, aggregate-only semantic vocabulary
for camera observations. The canonical schema is
[`pkg/contract/vision-evidence-v1.schema.json`](../pkg/contract/vision-evidence-v1.schema.json);
Go consumers use `DecodeVisionEvidenceV1`, and the Vision worker's Python
consumer validates against that same checked-in schema. Unknown fields are
rejected. The contract contains no media reference, local track ID, identity,
plate text, biometric data, or execution instruction.

Every family has independent availability (`not_requested`, `unavailable`,
`evaluated`). Non-evaluated data has zero confidence/quality and empty temporal
support; evaluated data has at least one valid evaluation. Opaque event and
episode IDs are `ev_`/`ep_` plus 24–64 lowercase hexadecimal characters.
Windows use UTC timestamps, duration must match, scores are finite `[0,1]`,
and immobility is bounded by the window and requires human presence.

| Family | Closed states/categories | Existing V3 mapping |
|---|---|---|
| Camera health | `healthy`, `degraded`, `unavailable`, `tamper_suspected` | None; preserved in Store/API evidence |
| Trigger | `motion`, `human_probable`, `vehicle_probable`, `animal_probable`, `tamper`, `unknown` | None |
| Presence | `absent`, `present`, `ambiguous`, `unknown` for human/vehicle/animal | None; no semantically equivalent reserved feature |
| Activity | `still`, `normal`, `rapid`, `very_rapid`, `unknown` | No dedicated activity feature |
| Pose | `upright`, `seated`, `reclined`, `ground`, `ambiguous`, `unknown` | Availability → existing V3 pose-status slots; `upright`/`seated`/`ground` use existing posture slots; `reclined` remains distinct in Evidence/Store and is conservatively projected to the frozen V3 `unknown` slot because 86D has no reclined slot; quality → existing V2 pose-quality slot; support count/sample flag → existing V3 slots; immobility seconds → existing V2 slot |
| Face | `recognized`, `unknown`, `ambiguous` plus availability | None; only aggregate semantic outcome is retained |
| Vehicle/plate | presence above; plate result `recognized`, `unknown`, `ambiguous` plus availability | None |
| Sensitive object | generic category `none`, `generic`, `unknown` plus availability | None; no active model is implied |
| Media continuity | `continuous`, `gapped`, `recovered`, `unknown` plus availability | None |

`runtime_aggregate` carries the prior V2/V3 aggregate facts that lack a dedicated
Evidence family: fall state (never `confirmed`), recovery, ground duration, risk
status/confidence/persistence, motion tier, interaction candidate, aggregate
face quality/status, camera integrity and provenance/tracking quality. These
facts remain attached to Evidence/Store; only semantics with an existing,
equivalent encoder offset are projected into the frozen 86D snapshot. The
projection records which Evidence facts are not encoded. In particular,
`reclined` stays distinct in Evidence while the fixed encoder uses its existing
unknown posture value; it is not rewritten to unknown in the source evidence.
A ground posture or candidate fall state is not a qualified fall detection.
The 86D feature order and weights are unchanged.

After Discovery, the only admitted Vision semantic bus event is
`synora.vision.evidence/v1`. Discovery is the single validated publisher to
Core. Edge manifests are mapped there without forwarding camera/episode IDs;
unknown metrics, media references and unrepresentable status are rejected or
quarantined. The legacy `vision.enrichment/v3` ingress accepts only a complete
Evidence V1 body (or a wrapper containing only that body); other fields are
quarantined. The bus ACL denies legacy segment/observation/summary and
`clip.*` events from Discovery to Core. Historical worker outputs are never
republished: Discovery receives a redacted `unavailable` Evidence V1 result
with a structured quarantine reason. Clip paths and lifecycle stay inside the
private Discovery/worker queue. Migration counters track converted, rejected
and quarantined ingress.

Core V1/V3 reject legacy Vision events and process only Evidence V1. V3
snapshots retain the complete validated Evidence plus an explicit list of
unmapped facts; the fixed 86D projection is not reinterpreted. The Universal
Store persists the resulting snapshot and the API state projection exposes
only validated Evidence V1, never the historical parallel Vision object or
unredacted input. The harness converts static, simulated-mock and Le2i inputs
before Discovery; mock inputs remain explicitly simulated.

Harness-derived observations are synthetic/replay, not new Vision inference.
Mock camera remains `simulated_camera=true`; Le2i remains
`semantic_qualification=not_qualified`. No qualification, fall confirmation,
bundle promotion, audio, network access, or physical action is authorized by
this contract.
