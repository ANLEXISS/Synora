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
| `camera_health` | stream/health metadata → Evidence V1 aggregate; deterministic producer may be model-free | Camera health not encoded | producer `not_configured`; no model selected or required |
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
registry entry; it declares 48 cases and is not silently folded into the 65
generic slots. No media or model has been copied into Git. The configured media root is
`SYNORA_VISION_MEDIA_ROOT`; files must be obtained and kept externally with
documented provenance, license, consent/privacy review, and independent labels.
Known-face test clips and any enrollment gallery must be disjoint and stored
separately. Plate clips require a privacy/legal review; the contract and report
must never retain plate text. Do not use a test clip to enroll a gallery.

Recommended external-only layout (the relative names must match the manifest):

```text
<vision-root>/
  face_known/face_known_01.mp4 ... face_known_10.mp4
  face_unknown/face_unknown_01.mp4 ... face_unknown_10.mp4
  face_ambiguous/face_ambiguous_01.mp4 ... face_ambiguous_05.mp4
  vehicle_presence/vehicle_presence_01.mp4 ... vehicle_presence_10.mp4
  plate_reading/plate_reading_01.mp4 ... plate_reading_10.mp4
  animal_presence/animal_presence_01.mp4 ... animal_presence_10.mp4
  camera_health/camera_health_01.mp4 ... camera_health_10.mp4
<restricted-gallery-root>/  # separate, consented enrollment set; face_known clips are holdout/test only
<restricted-model-root>/    # independently versioned model files
```

Never place these directories inside the repository. Keep the identity mapping
for opaque `resident_test_NN` references in a separately access-controlled
record; it must not be copied into manifests, reports, Store, or logs.

The closed `condition_tags` vocabulary is:
`indoor | outdoor`, `day | night | low_light`, `frontal | profile | occluded |
distant`, `single_subject | multi_subject | empty_scene`, `stationary | moving`,
`clear | degraded`, `resident_known | identity_unknown | identity_ambiguous`,
`vehicle_present | animal_present | no_target`, and
`healthy_camera | stream_missing | frozen_frame | tamper_suspected`. Tags are
selection/segmentation metadata, not model outputs. Every placeholder has
relevant tags; unknown, duplicate, missing, or suite-irrelevant tags fail
validation.

Camera-health fixture conditions are not invented Evidence V1 states. The
prepared aggregate expectations map a frozen frame or degradation to
`degraded`, suspected tamper to `tamper_suspected`, and missing stream or
invalid timestamps to `unavailable`; condition tags retain which controlled
condition was exercised. Core must treat degraded/unavailable health
conservatively, never as proof that the scene is safe. Face `unknown` means a
valid evaluation found no enrolled match; insufficient quality is unavailable
and is not converted to `unknown`. Face results remain only
`recognized | unknown | ambiguous | unavailable` at the report/decision layer;
the Evidence V1 semantic field keeps availability separate from result.

## Before adding media or a model

Media checklist: document rights and provenance; obtain explicit resident
consent for biometric test/enrollment data; keep gallery identities disjoint
from test clips; complete independent labels and condition tags; use the
declared relative filenames; calculate each clip SHA-256; record container,
codec, dimensions, frame count and positive duration; validate the manifest
against both schemas and the registry pin; scan reports for paths and forbidden
fields. Plate media requires a jurisdiction-specific legal/privacy review and
must never retain plate strings. Animal negative controls should include
human-only and empty scenes. Camera-health controls should include healthy,
missing, frozen, invalid-time, obscured, suspected-tamper, and progressive
degradation cases. No absence of evidence may be described as a successful
negative detection.

Model checklist: select no model until a real artifact is supplied; record its
version and SHA-256 in a reviewed registry change; check exact runtime/device
compatibility; implement only the corresponding module; reject model hash or
version mismatches; add hermetic plugin tests and a fail-closed path; pass only
validated Evidence V1 into an audited dry-run Discovery/Core/Store/Snapshot/MLP/
Safety Gate path. Never tune or qualify on the holdout clips.

Baseline means integrity-checked execution, per-condition diagnostics,
redacted Evidence V1, latency observations, and dry-run end-to-end transport.
It is not qualification. Qualification additionally requires adequate
independent labeled train/tune/holdout separation, preregistered acceptance
criteria, confidence calibration, slice metrics, privacy review, device
performance measurements, and an independent review. Face known (10), unknown
(10), ambiguous/degraded (5), vehicle (10), plate (10), animal (10), and camera
health (10) are minimum preparation slots, not sufficient qualification
corpora by themselves.

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

The registry pins the exact generic manifest SHA-256. Any edited manifest must
be reviewed and repinned in the registry; an arbitrary valid-looking manifest
is rejected before media probing. The standard report records both manifest
and registry hashes and remains redacted.
The generic external-media executor labels every selected case and report
`execution_mode=replay`; it rejects live `real` mode and requires
`simulated_test` Evidence V1 to carry `simulated_camera=true` in isolated
synthetic tests. The mode is checked before the Evidence V1 pipeline is called.
The executor creates no copied/transcoded media; only the explicitly selected
redacted report output is written, and its retention is the caller's
responsibility.

After those gates and the reviewed plugin/pipeline have been implemented, set
the external paths and select exactly one suite and case:

```bash
export SYNORA_VISION_MEDIA_ROOT=/external/vision-v1
export SYNORA_VISION_FACE_MODEL=/external/models/face-model
export SYNORA_VISION_FACE_GALLERY=/restricted/gallery-v1
export SYNORA_VISION_VEHICLE_MODEL=/external/models/vehicle-model
export SYNORA_VISION_PLATE_MODEL=/external/models/plate-model
export SYNORA_VISION_ANIMAL_MODEL=/external/models/animal-model
export SYNORA_POSE_RKNN_MODEL=/external/models/pose-model
VISION_MANIFEST=testdata/vision-v1/suites/manifest.json
MODULE_REGISTRY=testdata/vision-v1/modules.json
SUITE=face_known
CASE_ID=face_known_01
REPORT=/tmp/vision-face-known.json

# Reusable shape; SUITE and CASE_ID must be declared slots in the pinned manifest.
go run ./cmd/synora-central-test --vision-suites verify \
  --vision-suite-manifest "$VISION_MANIFEST" \
  --vision-module-registry "$MODULE_REGISTRY" \
  --vision-suite-root "$SYNORA_VISION_MEDIA_ROOT" \
  --vision-suite "$SUITE" --vision-case "$CASE_ID" \
  --vision-suites-out "$REPORT"

go run ./cmd/synora-central-test --vision-suites run \
  --vision-suite-manifest "$VISION_MANIFEST" \
  --vision-module-registry "$MODULE_REGISTRY" \
  --vision-suite-root "$SYNORA_VISION_MEDIA_ROOT" \
  --vision-suite "$SUITE" --vision-case "$CASE_ID" \
  --vision-model-face "$SYNORA_VISION_FACE_MODEL" \
  --vision-face-gallery "$SYNORA_VISION_FACE_GALLERY" \
  --vision-model-vehicle "$SYNORA_VISION_VEHICLE_MODEL" \
  --vision-model-plate "$SYNORA_VISION_PLATE_MODEL" \
  --vision-model-animal "$SYNORA_VISION_ANIMAL_MODEL" \
  --vision-model-camera "" \
  --vision-model-pose "$SYNORA_POSE_RKNN_MODEL" \
  --vision-suites-out "$REPORT"
```

These are instructions for a future reviewed plugin, not a claim that the
commands currently perform inference. Current CLI has no production generic
Vision plugin or Discovery pipeline attached: `run` therefore fails closed as
unavailable even when paths are supplied. `verify` and `list` are non-inference
operations. An absent file, absent model, unconfigured module, unavailable
runtime, metadata mismatch, or missing plugin remains `not_run`, `not_configured`,
`unavailable`, or `blocked`; never a pass. Face `run` additionally requires
the external gallery directory, passed only to the face plugin and never
written to a report. The Le2i pose suite uses its existing separate command,
only after external prerequisites are available:

```bash
SYNORA_VISION_MEDIA_ROOT=/external/le2i-v1 \
SYNORA_POSE_RKNN_MODEL=/external/models/pose-model \
go run ./cmd/synora-central-test --media \
  --media-manifest testdata/central-e2e-v1/media/le2i-v1-regression.json \
  --out /tmp/synora-le2i-report.json
```

This command is not run as part of suite preparation. A Le2i replay is not
fall qualification unless individual clips are independently labeled and the
qualification protocol says so. All generic reports are separate from the
central E2E report and retain `qualification_status=not_qualified`.

## Current V1 readiness

| Function | Prepared in V1 | Still outside V1 |
| --- | --- | --- |
| Face aggregate | slots, opaque test refs, registry, gallery path contract, Evidence V1 validation | consented gallery/clips, reviewed plugin, audited pipeline, metrics |
| Vehicle presence | slots, presence-only output/contract | labeled clips, plugin/model choice, metrics |
| Plate readability | slots and no-text output boundary | legal review, labeled clips, plugin/model, non-retention proof |
| Animal presence | slots, condition tags and aggregate contract | negative controls, plugin/model and metrics |
| Camera health | condition slots and conservative aggregate labels | controlled operational fault suite and reviewed health producer |
| Human pose | separate 48-case Le2i manifest and existing harness | proven model/runtime and labeled independent pose/fall evaluation |

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
