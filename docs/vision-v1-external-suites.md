# Vision V1 external-suite readiness

This repository contains preparation metadata only: no new media, model,
gallery, plugin, or inference implementation is included. The six canonical
module records live in `testdata/vision-v1/modules.json`; every model is
`not_configured` except `human_pose`, which is explicitly `unavailable` until
its RKNN runtime/model and labeled data are independently demonstrated.
All 65 committed media slots are placeholders. A placeholder is `not_run`,
never a passing example, and cannot invoke a plugin.

## Modules and V3 scope

| Canonical module | Input → output | V3 projection | Current readiness |
| --- | --- | --- | --- |
| `face_recognition` | external media slot → `synora.vision.evidence/v1` aggregate; no identity | No face fields encoded | `not_configured`; separate consented gallery required |
| `vehicle_presence` | external media slot → Evidence V1 aggregate | Vehicle presence not encoded | `not_configured` |
| `plate_reading` | external media slot → readability/result aggregate, never plate text | Plate result/quality not encoded | `not_configured` |
| `animal_presence` | external media slot → Evidence V1 aggregate | Animal presence not encoded | `not_configured` |
| `camera_health` | stream/health metadata → Evidence V1 aggregate | Camera health not encoded | `not_configured` |
| `human_pose` | existing Le2i media manifest → Evidence V1 pose aggregate | Existing pose availability/posture/support mappings only; no change to 86D order | `unavailable`; existing separate Le2i runner, no generic duplicate slots |

Each registry record declares the role, Evidence V1 contracts, preconditions,
forbidden data, model and missing-input status, external environment-variable
names (never resolved paths), quality measures, latency target when known,
qualification criteria, and V3 encoded/not-encoded scope. V3 projection is
descriptive only; this work does not change the snapshot, labels, weights, or
dimension. The common suite executor accepts suite and case filters, checks
path containment and SHA-256, and probes container/codec/dimensions/frame count
and duration with local `ffprobe` before any future plugin could run. Probe
failure or mismatch quarantines the slot. Reports contain only opaque case
references and technical metadata, not media paths or subject references.

## Slot inventory and media layout

The manifest is `testdata/vision-v1/suites/manifest.json`, validated against
`pkg/contract/vision-media-suite-manifest-v1.schema.json`:

| Suite | Slots | External subdirectory |
| --- | ---: | --- |
| `face_known` | 10 | `face_known/` |
| `face_unknown` | 10 | `face_unknown/` |
| `face_ambiguous` | 5 | `face_ambiguous/` |
| `vehicle_presence` | 10 | `vehicle_presence/` |
| `plate_reading` | 10 | `plate_reading/` |
| `animal_presence` | 10 | `animal_presence/` |
| `camera_health` | 10 | `camera_health/` |

The Le2i pose suite remains separate and is referenced by the `human_pose`
registry entry; it is not silently folded into the 65 generic slots. No media
or model has been copied into Git. The configured media root is
`SYNORA_VISION_MEDIA_ROOT`; files must be obtained and kept externally with
documented provenance, license, consent/privacy review, and independent labels.
Known-face test clips and any enrollment gallery must be disjoint and stored
separately. Plate clips require a privacy/legal review; the contract and report
must never retain plate text. Do not use a test clip to enroll a gallery.

The closed `condition_tags` vocabulary is:
`indoor | outdoor`, `day | night | low_light`, `frontal | profile | occluded |
distant`, `single_subject | multi_subject | empty_scene`, `stationary | moving`,
`clear | degraded`, `resident_known | identity_unknown | identity_ambiguous`,
`vehicle_present | animal_present | no_target`, and
`healthy_camera | stream_missing | frozen_frame | tamper_suspected`. Tags are
selection/segmentation metadata, not model outputs. Every placeholder has
relevant tags; unknown, duplicate, missing, or suite-irrelevant tags fail
validation.

## Readiness gates and future commands

Before a suite can run, all of the following must exist outside Git: enough
consented/licensed, independently labeled clips for every declared slot;
verified media hashes and technical metadata; a reviewed module implementation
that accepts only the declared inputs and emits validated Evidence V1; an
independently hashed model whose version/hash/path agree with the registry;
and an audited dry-run Discovery → Core → Store → CognitiveSnapshot → MLP →
Safety Gate callback. No plugin is currently registered, so setting an
environment variable or filling a slot does not activate inference. Model
absence, absent media, metadata mismatch, and unavailable runtimes remain
explicit non-success states. No fallback model exists.

After those gates have been implemented and reviewed, the generic CLI is:

```bash
SYNORA_VISION_MEDIA_ROOT=/external/vision-v1 \
SYNORA_VISION_FACE_MODEL=/external/models/face-model \
go run ./cmd/synora-central-test --vision-suites verify \
  --vision-suite-root "$SYNORA_VISION_MEDIA_ROOT" \
  --vision-suites-out /tmp/vision-verify.json

go run ./cmd/synora-central-test --vision-suites run \
  --vision-suite face_known --vision-case face_known_01 \
  --vision-suite-root "$SYNORA_VISION_MEDIA_ROOT" \
  --vision-model-face "$SYNORA_VISION_FACE_MODEL" \
  --vision-suites-out /tmp/vision-face-known.json
```

These are instructions for a future reviewed plugin, not a claim that the
commands currently perform inference. At present `verify` reports media
absence/quarantine and `run` fails closed when slots, a model, or a plugin are
missing. `list` is safe with placeholders. Reports are separate from the
central E2E report and retain `qualification_status=not_qualified`; execution
counts are not qualification. The existing Le2i command remains the central
media harness and must be run only when its external corpus/runtime is
available. No result from an unlabelled clip may be used to claim pose or fall
performance.

## Qualification requirements

No module is qualified. Future acceptance needs module-specific labeled
positive/negative and stress slices, independent holdout sets, condition-wise
precision/recall and false-accept/false-reject analysis as appropriate,
confidence calibration, temporal stability where applicable, device p95
latency, privacy/redaction review, and Safety Gate dry-run evidence. Face
recognition additionally requires explicit consent and identity-disjoint
holdouts; plate reading requires a separate legal/privacy decision and tests
that prove text is not retained. Human-pose/fall claims require genuinely
labeled data and independent metrics. J2/J3/J4 or other system qualification
status is not changed by these preparation artifacts.
