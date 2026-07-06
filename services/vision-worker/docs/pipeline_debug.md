# Synora Vision Pipeline Debug Architecture

## Debug Storage

The worker always creates:

```text
/var/lib/synora/debug/
  raw/
  yolo/
  scrfd/
  align/
  arcface/
  tracks/
  events/
  timeline/
```

Each instrumented stage writes an annotated image, a raw image when available,
and a JSON document named with UTC timestamp, camera id, stage, and a unique id:

```text
20260506_183200_123_cam01_scrfd_a1b2c3d4.jpg
20260506_183200_123_cam01_scrfd_a1b2c3d4_raw.jpg
20260506_183200_123_cam01_scrfd_a1b2c3d4.json
```

The JSON schema is:

```json
{
  "camera_id": "",
  "frame_id": "",
  "timestamp": "",
  "trace_id": "",
  "step": "",
  "duration_ms": 0,
  "success": true,
  "error": null,
  "input_shape": [],
  "output_count": 0,
  "details": {}
}
```

## Trace Timeline

Every processed frame gets a `trace_id`. Stage transitions are appended to:

```text
/var/lib/synora/debug/timeline/timeline.jsonl
```

Each line records `START`, `SUCCESS`, `FAILURE`, or `SKIPPED` state through the
`status` field, plus start/end timestamps and duration. This makes it possible
to reconstruct a complete path:

```text
raw -> yolo -> tracks -> scrfd -> align -> arcface -> match -> events
```

## Metrics And Dashboard

The worker exposes:

```text
GET http://127.0.0.1:8094/debug/pipeline
```

The response contains:

- `fps.input` and `fps.processed`
- average `yolo`, `scrfd`, `arcface`, and total latency in ms
- queue sizes and occupancy
- `frames_dropped`, `faces_detected`, `persons_detected`
- `known_faces`, `unknown_faces`, `uncertain_faces`
- `events_generated`, `events_dropped`
- current per-track identity state
- recent errors

## Event Loss Diagnosis

The previous event generation loop only iterated over `self.track_faces`.
That means a YOLO/tracker person with no SCRFD face candidate did not produce a
per-track event. In some cases a fallback `vision.unknown` was emitted with
`track_id=0`, but the actual track id was lost and the branch was not observable.

Other silent paths were:

- raw frame debug write happened before checking `ret`/`frame is None`, so a clip
  ending could raise before a clean pipeline result;
- SCRFD failures returned an empty list without a pipeline trace entry;
- face crops rejected for size or empty content were skipped without structured
  JSON;
- ArcFace returning `None` did not force an error/debug artifact per face;
- matching with no identity vote did not write a trace explaining the unknown.

The current pipeline emits warnings/errors for these transitions:

- person detected but no face found: `WARNING`
- face found but no embedding: `ERROR`
- embedding created but no match: `WARNING`
- matching reached but no event: `ERROR` and `events_dropped += 1`

Unknown and uncertain identities are explicit first-class events:

- `vision.unknown`
- `vision.uncertain`
- `vision.identity`

## Performance Strategy

The implementation keeps model calls bounded:

- YOLO is sampled by `PERSON_DETECT_INTERVAL` and reused between samples.
- SCRFD runs only on tracked person ROIs.
- ArcFace runs at a higher cadence for unknown tracks and a lower cadence for
  confirmed known tracks.
- Recognition uses a 10-observation buffer per track instead of deciding from a
  single embedding.

`core/async_pipeline.py` provides bounded stages for RTSP, YOLO, SCRFD, ArcFace,
and event emission. A full RTSP integration can feed those stages with handlers
that wrap the existing synchronous stage methods while preserving queue
occupancy, drop rate, and latency metrics.
