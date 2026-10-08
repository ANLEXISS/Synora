# Vision V1 external-suite foundation

This is an inventory and plugin foundation only. It does not include media,
biometric data, a gallery, model weights, model installation or inference.
All five declared modules currently report `not_configured`; every committed
media slot is a placeholder and cannot be counted as passed or qualified.

Every slot has non-empty `condition_tags` from the closed vocabulary:
`indoor`, `outdoor`, `day`, `night`, `low_light`, `frontal`, `profile`,
`occluded`, `distant`, `single_subject`, `multi_subject`, `empty_scene`,
`stationary`, `moving`, `clear`, `degraded`, `resident_known`,
`identity_unknown`, `identity_ambiguous`, `vehicle_present`, `animal_present`,
`no_target`, `healthy_camera`, `stream_missing`, `frozen_frame`, and
`tamper_suspected`. The validator checks suite relevance and rejects unknown,
duplicate, or missing tags. Tags are test-selection/segmentation metadata only;
they are not expected outputs and are never supplied to a Vision module.

The slot manifest is `testdata/vision-v1/suites/manifest.json`. Its schema is
`pkg/contract/vision-media-suite-manifest-v1.schema.json`; the inactive module
registry is `testdata/vision-v1/modules.json`. Manifests contain only opaque
test references, pending hashes, expected redacted semantics and relative
paths. The expected values are not passed to a module: the plugin input
contains only its media/model paths and opaque case metadata. Reports omit
both media paths and `subject_ref`.

## External asset layout

The default external root is `/home/rock/Synora-test-media/vision-v1/`,
selected with `SYNORA_VISION_MEDIA_ROOT` or `--vision-suite-root`. Place
consented, licensed material only outside Git, under these directories:

| Suite | Slots | External directory | Expected semantic outcomes |
| --- | ---: | --- | --- |
| `face_known` | 10 | `face_known/` | `recognized`; opaque `resident_test_01` reference exists only in the external fixture expectation. |
| `face_unknown` | 10 | `face_unknown/` | `unknown`, with no forced identity. |
| `face_ambiguous` | 5 | `face_ambiguous/` | `ambiguous` or `unavailable`. |
| `vehicle_presence` | 10 | `vehicle_presence/` | present, absent or ambiguous with confidence; no plate text. |
| `plate_reading` | 10 | `plate_reading/` | recognized, unknown, ambiguous or unavailable; plate text is never part of Evidence V1. |
| `animal_presence` | 10 | `animal_presence/` | present, absent or ambiguous with confidence. |
| `camera_health` | 10 | `camera_health/` | healthy, unavailable, frozen, obscured, degraded or invalid timestamp. |

Each slot currently names a planned relative `.mp4` location, uses a zero
SHA-256, has `asset_status=placeholder`, and includes a reason it is not run.
Replacing a placeholder requires a reviewed source, consent/licence, technical
metadata and the actual file hash. The harness never downloads or decrypts an
asset. Path traversal, symlinks, URLs and unknown/raw Vision fields are
rejected.

## Plugin contract and execution

`internal/visionsuite.Module` is the module extension point. A module reports
state (`not_configured`, `unavailable`, `available` or `failed`), model version
and hash, input/output compatibility, a structured error, latency, an
Evidence V1 aggregate and whether inference executed. Results must pass the
Evidence V1 validator before the injected pipeline callback may traverse
Discovery → Core → Store → CognitiveSnapshot V3 → MLP → Safety Gate → dry-run
result. The callback must attest that the whole path completed with no audio
rendering or physical action. Semantic mismatches are distinct from
infrastructure failures. Qualification remains a separate future process.

No module plugin is registered today. Supplying a path or filling a manifest
does not activate a model or turn a placeholder into an output. Add a reviewed
module implementation and register it through the `visionsuite.Execute`
extension point before a populated suite can run. Until then `run` reports
`media_absent` or `model_absent` and exits unsuccessfully; it never calls a
fallback model. Le2i continues to use its existing, separate report.

Commands from the repository root:

```bash
go run ./cmd/synora-central-test --vision-suites list
go run ./cmd/synora-central-test --vision-suites verify \
  --vision-suite-root /home/rock/Synora-test-media/vision-v1 \
  --vision-suites-out /tmp/synora-vision-v1-verify.json
go run ./cmd/synora-central-test --vision-suites run \
  --vision-suite face_known \
  --vision-suite-root /home/rock/Synora-test-media/vision-v1 \
  --vision-suites-out /tmp/synora-vision-v1-face-known.json
```

The generic report is written independently from `/tmp/synora-central-e2e-v1.json`
and contains slot status, module state, latency, redacted semantic match,
pipeline dry-run facts and qualification status. `qualified_count` remains
zero unless a future, independently reviewed qualification flow is added.
