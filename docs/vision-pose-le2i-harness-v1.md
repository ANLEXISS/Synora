# Vision Pose Le2i V1 harness

This is a visible, non-clinical regression pack. It contains 48 locally
provided Le2i-processed sequences: Fall 10, Lie 10, Likefall 8, Stand 10 and
Blank 10. Each clip is 16 frames at 25 FPS, H.264 and 320×240 or 320×180.
The external corpus remains outside Git under
`SYNORA_VISION_MEDIA_ROOT`; no video, image, model weight or generated output
is committed.

The versioned repository manifest is
`testdata/central-e2e-v1/media/le2i-v1-regression.json`. It contains only
relative paths, clip hashes, codec/shape/frame metadata and category
expectations. The external manifest hash is recorded in that file. The runner
resolves every path below `SYNORA_VISION_MEDIA_ROOT`, checks SHA-256 and
ffprobe metadata before reading it, and rejects traversal.

The real backend is `yolov8n_pose_rknn/v1` in the existing Vision worker. It
requires `SYNORA_POSE_RKNN_MODEL`, uses RKNNLite only, RGB NHWC uint8
`1×640×640×3`, letterbox padding 56, and the four RKNN outputs expected by the
validated RK3588 artifact. There is no RTMPose, CPU, ONNX Runtime or MediaPipe
fallback. The model is supplied outside Git, for example:

```bash
SYNORA_VISION_MEDIA_ROOT=/home/rock/Synora-test-media/le2i-v1 \
SYNORA_POSE_RKNN_MODEL=/home/rock/Synora-test-media/le2i-v1/models/yolov8n-pose-rk3588-toolkit22-fp.rknn \
make test-central-v1 MEDIA=le2i
```

The explicit media mode uploads each hash-verified clip to the production
Discovery ingress handler on an ephemeral loopback HTTP server (HTTP 202),
checks its redacted clip-lifecycle event, and hands the queued temporary file
only to the local test worker. The worker confirms a human with the existing
human detector before requesting pose; otherwise it returns
`pose_status=not_requested` with an explicit gate reason. Processing is capped
at 16 decoded frames and 16 pose requests/ROIs per clip, with at most one pose
request per confirmed-human frame. The local file path remains in the queue
and worker process only; the aggregate result is then injected through the
central Discovery/Core V3 bus fixture. This is a test ingress plus the central
aggregate pipeline, not a deployed Discovery service run.

The report keeps `mock_camera_e2e`, `central_static`,
`vision_media_le2i_real_pose` and `overall` counts distinct. It identifies the
real backend as `real_rknn_pose_test`, records the pose model SHA-256, RKNN
runtime/driver versions, model Toolkit version and output shapes, separates
model initialization from NPU inference p50/p95/max, and includes per-clip
gate refusals, valid pose results, Store revision, API-targeted V3 snapshot
observation, MLP V3 heads and Safety Gate/action status. Raw frames, bbox,
crops, keypoints, embeddings, identities, URLs and local paths are excluded
from bus payloads, Store, API state and reports. Audio, external network,
production Store writes and physical actions remain false.

The camera-simulator adapter invokes the existing local human detector first.
Pose is requested only for a confirmed human and the pose backend never
decides whether a human exists. A Blank clip therefore emits
`pose_status=not_requested` (or `low_quality`), `posture=unavailable` and
`fall_state=none`; it can never activate pose or a candidate. Detector boxes
and all intermediate tensors are process-local and released after the
aggregate is built. The detector model is read-only test input; it is not
copied into Git, `/opt/synora/models` or the Universal Store.

Each media case then follows the same central action lifecycle as the static
cases. Its redacted journey records ingress, Discovery, Core, Store, snapshot,
MLP, Safety Gate, Discovery action ingress, the internal dry-run
`TestActionExecutor`, the result recording and completion. Duplicate camera
messages cannot create a second action, duplicate results are idempotent, and
orphan results are rejected. No action status is interpreted as a physical
execution; `physical_action_executed` and `audio_rendered` remain false.

`fall_state=candidate` is only a temporal pose signal. It is not a confirmed
fall, emergency decision or clinical result. The report separates integrity,
runtime/safety gates and semantic observations. Semantic qualification is
always `not_qualified`; category mismatches are reported as debt rather than
used to invent a promotion threshold. Holdout material is not in this
manifest and is not executed by the visible regression command.

The corpus is short and processed into fixed 16-frame clips. It is useful for
runtime, redaction, latency and non-regression evidence only; it does not
qualify fall detection or promote V3.

The five categories have conservative semantics: Fall may emit only a
temporal upright-to-ground `candidate`; Lie, Likefall, Stand and Blank
prioritize absence of a candidate. `confirmed` is forbidden. Observed
category differences remain `semantic_status=observed_mismatch` and are
listed redacted; they do not become artificial successes or a promotion
threshold. The report includes category/posture and category/fall-state
matrices, non-fall false positives, observed Fall recall, and inference
latency p50/p95/max. `semantic_qualification` stays `not_qualified` until
independent acceptance metrics are defined and met.

The media mode requires both explicit media and model paths. The ordinary
mock-camera run remains a separate worker with zero pose-model loads and zero
inference. Neither mode promotes a bundle. Face, plate, sensitive-object,
aggression and inter-camera fusion capabilities are outside this milestone;
J2/J3/J4 are not promoted without defined metrics and reproducible results.
