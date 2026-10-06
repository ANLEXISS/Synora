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
track ID crosses that boundary. Audio, network access, persistent Store
changes and physical actions remain disabled.

`fall_state=candidate` is only a temporal pose signal. It is not a confirmed
fall, emergency decision or clinical result. The report separates integrity,
runtime/safety gates and semantic observations. Semantic qualification is
always `not_qualified`; category mismatches are reported as debt rather than
used to invent a promotion threshold. Holdout material is not in this
manifest and is not executed by the visible regression command.

The corpus is short and processed into fixed 16-frame clips. It is useful for
runtime, redaction, latency and non-regression evidence only; it does not
qualify fall detection or promote V3.
