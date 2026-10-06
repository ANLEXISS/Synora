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

The same central runner validates the clip, executes the local Vision worker
backend, injects only its aggregate result into the hermetic Unix-bus/Core/V3
fixture, and records MLP V3 candidate and Safety Gate test-only results. No
raw frame, keypoint, coordinate, box, crop, embedding, identity or local
track ID crosses that boundary. Audio, network access, production Store
changes and physical actions remain disabled; the temporary harness Store is
used to prove the Core commit path.

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

The five categories have conservative semantics: Stand cannot emit a fall
candidate, Lie cannot emit one without an upright-to-ground transition,
Likefall remains ambiguous/non-confirmed, and Fall may emit only
`candidate`. `confirmed` is forbidden. The central report includes human
confirmation counts, pose request reason, action lifecycle, MLP head labels,
Safety Gate outcomes and the p50/p95/max pose latency. It never turns these
clips into a fall qualification set.
