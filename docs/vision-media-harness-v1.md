# Vision media harness V1

The central harness has one optional, explicit media mode. The default
`make test-central-v1` path remains the hermetic aggregate-fixture run and
does not read or create media. Media mode is enabled only with both
`--media` and `SYNORA_VISION_MEDIA_ROOT` (the Make interface is
`MEDIA_ROOT=/path/to/root`).

The versioned inventory is
`testdata/central-e2e-v1/vision-media-v1/manifest.json`. It contains no video,
only relative paths, expected hashes, duration metadata, camera/topology
metadata, an opaque Edge fixture reference, consent/licence reference and
three-level expectations. The committed zero hash and zero duration mean
“pending local deposit”; they cannot pass integrity validation. Local clips
must be deposited under the root named by `SYNORA_VISION_MEDIA_ROOT`, with
the manifest entry completed from the real file. Do not commit the clips,
models or derived crops.

The inventory is intentionally declarative and currently has 80 entries:

| Family | Count |
| --- | ---: |
| fall | 10 |
| lie_down | 10 |
| sit_down | 10 |
| face_known | 10 |
| face_unknown | 10 |
| near_fall_or_bend | 10 |
| no_person_or_occluded | 10 |
| multi_person | 5 |
| delivery_failure | 5 |

Recommended local layout is `<family>/<id>.mp4`. Names are stable and do not
encode identity. Every clip must have a real SHA-256, duration and a valid
video stream before processing. The central process validates path containment,
presence, SHA-256, `ffprobe` decode and duration. It never downloads files.

The only pose backend named by this harness is YOLOv8n-pose as an RKNN model
on RK3588, selected with `SYNORA_POSE_RKNN_MODEL=/path/model.rknn`. The
diagnostic loads the model through RKNNLite, initializes the RK3588 runtime
and verifies the YOLOv8n-pose output shape;
if the model, runtime or format is unavailable, pose cases are reported as
`blocked_model_unavailable`. There is no CPU or YOLO-pose fallback. The
adapter retains keypoints only in process memory and emits only aggregate
signals: pose status, posture, immobility, fall candidate/none/uncertain,
rapid motion, physical interaction candidate, confidence and latency. A
`confirmed` fall is rejected by the adapter.

The four-output decoder is based on the public Rockchip implementation in
[`airockchip/rknn_model_zoo/examples/yolov8_pose/python/yolov8_pose.py`](https://github.com/airockchip/rknn_model_zoo/blob/main/examples/yolov8_pose/python/yolov8_pose.py),
with its drawing and raw-output publication removed for this harness.

Face families remain `blocked_model_unavailable`; no face recognizer or
identity path is added here. Delivery failures are classified as
`blocked_media_missing` or `blocked_integrity_failure`. The report uses the
same runner and stores media results under `vision_media`; no image, crop,
box, keypoint, embedding, identity, local track identifier or raw media is
sent to the bus, Store, trace or report.

Example commands after local deposit:

```bash
make test-central-v1 MEDIA_ROOT=/srv/synora/vision-test-media \
  POSE_MODEL=/srv/synora/vision-test-models/yolov8n-pose.rknn \
  OUT=/tmp/synora-central-media-v1.json
```

The output is a local dry-run report. It is not a corpus qualification, fall
qualification or bundle promotion. Semantic validation requires independently
labelled clips and metrics; an unlabelled clip such as `test3.mp4` is only a
runtime smoke test and cannot prove fall detection.
