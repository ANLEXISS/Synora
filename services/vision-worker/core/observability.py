import json
import logging
import os
import threading
import time
import uuid
from collections import defaultdict, deque
from contextlib import contextmanager
from datetime import datetime

import cv2
import numpy as np


log = logging.getLogger("synora.vision.observability")


DEBUG_ROOT = "/var/lib/synora/debug"
DEBUG_STEPS = (
    "raw",
    "yolo",
    "scrfd",
    "align",
    "arcface",
    "tracks",
    "events",
    "timeline",
)


def _shape(value):
    if value is None:
        return []
    return list(getattr(value, "shape", []))


def _json_default(value):
    if isinstance(value, np.generic):
        return value.item()
    if isinstance(value, np.ndarray):
        return value.tolist()
    if isinstance(value, (deque, set)):
        return list(value)
    return str(value)


class PipelineMetrics:

    def __init__(self):
        self.lock = threading.Lock()
        self.counters = defaultdict(int)
        self.timings = defaultdict(lambda: deque(maxlen=120))
        self.queue_sizes = {}
        self.latencies = deque(maxlen=120)
        self.input_frames = deque(maxlen=300)
        self.processed_frames = deque(maxlen=300)
        self.errors = deque(maxlen=100)
        self.identities = {}

    def mark_input(self):
        with self.lock:
            self.input_frames.append(time.time())

    def mark_processed(self):
        with self.lock:
            self.processed_frames.append(time.time())

    def incr(self, name, value=1):
        with self.lock:
            self.counters[name] += value

    def set_counter(self, name, value):
        with self.lock:
            self.counters[name] = value

    def timing(self, name, duration_ms):
        with self.lock:
            self.timings[name].append(float(duration_ms))

    def latency(self, duration_ms):
        with self.lock:
            self.latencies.append(float(duration_ms))

    def queue(self, name, size, capacity=None):
        with self.lock:
            self.queue_sizes[name] = {
                "size": int(size),
                "capacity": int(capacity) if capacity is not None else None,
                "occupancy": (
                    float(size) / float(capacity)
                    if capacity
                    else 0.0
                ),
            }

    def identity(self, track_id, payload):
        with self.lock:
            self.identities[str(track_id)] = payload

    def error(self, step, message, trace_id=None):
        with self.lock:
            self.errors.append({
                "time": datetime.utcnow().isoformat() + "Z",
                "trace_id": trace_id,
                "step": step,
                "message": message,
            })

    def snapshot(self):
        now = time.time()

        def fps(samples):
            recent = [t for t in samples if now - t <= 10.0]
            if len(recent) < 2:
                return float(len(recent))
            span = max(recent[-1] - recent[0], 1e-6)
            return float((len(recent) - 1) / span)

        def avg(values):
            return float(sum(values) / len(values)) if values else 0.0

        with self.lock:
            timings = {
                name: avg(list(values))
                for name, values in self.timings.items()
            }
            counters = dict(self.counters)
            queues = dict(self.queue_sizes)
            identities = dict(self.identities)
            errors = list(self.errors)
            latencies = list(self.latencies)
            input_frames = list(self.input_frames)
            processed_frames = list(self.processed_frames)

        return {
            "fps": {
                "input": fps(input_frames),
                "processed": fps(processed_frames),
            },
            "timings_ms": {
                "yolo": timings.get("yolo", 0.0),
                "scrfd": timings.get("scrfd", 0.0),
                "arcface": timings.get("arcface", 0.0),
                "total_latency": avg(latencies),
            },
            "queues": queues,
            "counters": {
                "queue_size": sum(q["size"] for q in queues.values()),
                "frames_dropped": counters.get("frames_dropped", 0),
                "faces_detected": counters.get("faces_detected", 0),
                "persons_detected": counters.get("persons_detected", 0),
                "known_faces": counters.get("known_faces", 0),
                "unknown_faces": counters.get("unknown_faces", 0),
                "uncertain_faces": counters.get("uncertain_faces", 0),
                "events_generated": counters.get("events_generated", 0),
                "events_dropped": counters.get("events_dropped", 0),
            },
            "identities": identities,
            "errors": errors,
        }


class DebugStore:

    def __init__(self, root=DEBUG_ROOT, enabled=True):
        self.root = root
        self.enabled = enabled
        self.lock = threading.Lock()
        for step in DEBUG_STEPS:
            os.makedirs(os.path.join(self.root, step), exist_ok=True)

    def _name(self, camera_id, frame_id, step, item_id=None):
        ts = datetime.utcnow().strftime("%Y%m%d_%H%M%S_%f")[:-3]
        safe_camera = str(camera_id or "unknown").replace("/", "_")
        suffix = item_id or uuid.uuid4().hex[:10]
        return f"{ts}_{safe_camera}_{step}_{suffix}"

    def save_step(
        self,
        camera_id,
        frame_id,
        step,
        raw=None,
        annotated=None,
        duration_ms=0.0,
        success=True,
        error=None,
        input_shape=None,
        output_count=0,
        details=None,
        trace_id=None,
        item_id=None,
    ):
        if not self.enabled:
            return None

        directory = os.path.join(self.root, step)
        os.makedirs(directory, exist_ok=True)
        base = self._name(camera_id, frame_id, step, item_id)

        annotated_path = None
        raw_path = None

        with self.lock:
            if annotated is not None and getattr(annotated, "size", 0) > 0:
                annotated_path = os.path.join(directory, f"{base}.jpg")
                cv2.imwrite(annotated_path, annotated)

            if raw is not None and getattr(raw, "size", 0) > 0:
                raw_path = os.path.join(directory, f"{base}_raw.jpg")
                cv2.imwrite(raw_path, raw)

            payload = {
                "camera_id": camera_id or "",
                "frame_id": str(frame_id),
                "timestamp": datetime.utcnow().isoformat() + "Z",
                "trace_id": trace_id,
                "step": step,
                "duration_ms": float(duration_ms),
                "success": bool(success),
                "error": error,
                "input_shape": input_shape if input_shape is not None else _shape(raw),
                "output_count": int(output_count),
                "details": details or {},
                "files": {
                    "annotated": annotated_path,
                    "raw": raw_path,
                },
            }

            json_path = os.path.join(directory, f"{base}.json")
            with open(json_path, "w", encoding="utf-8") as fh:
                json.dump(payload, fh, default=_json_default)

        return {
            "json": json_path,
            "annotated": annotated_path,
            "raw": raw_path,
        }

    def append_timeline(self, entry):
        path = os.path.join(self.root, "timeline", "timeline.jsonl")
        with self.lock:
            with open(path, "a", encoding="utf-8") as fh:
                fh.write(json.dumps(entry, default=_json_default) + "\n")


class PipelineTrace:

    def __init__(self, camera_id, frame_id, store, metrics=None, trace_id=None):
        self.camera_id = camera_id
        self.frame_id = frame_id
        self.trace_id = trace_id or uuid.uuid4().hex
        self.store = store
        self.metrics = metrics

    @contextmanager
    def step(self, name, details=None, input_shape=None):
        start_monotonic = time.perf_counter()
        start = datetime.utcnow().isoformat() + "Z"
        log.info(
            "TRACE %s frame=%s step=%s START details=%s",
            self.trace_id,
            self.frame_id,
            name,
            details or {},
        )
        status = "SUCCESS"
        error = None
        try:
            yield
        except Exception as exc:
            status = "FAILURE"
            error = str(exc)
            if self.metrics:
                self.metrics.error(name, error, self.trace_id)
            raise
        finally:
            end = datetime.utcnow().isoformat() + "Z"
            duration_ms = (time.perf_counter() - start_monotonic) * 1000.0
            success = status == "SUCCESS"
            if self.metrics:
                self.metrics.timing(name, duration_ms)
            entry = {
                "trace_id": self.trace_id,
                "camera_id": self.camera_id,
                "frame_id": str(self.frame_id),
                "step": name,
                "status": status,
                "start": start,
                "end": end,
                "duration_ms": duration_ms,
                "success": success,
                "error": error,
                "input_shape": input_shape or [],
                "details": details or {},
            }
            self.store.append_timeline(entry)
            log.info(
                "TRACE %s frame=%s step=%s %s duration_ms=%.2f",
                self.trace_id,
                self.frame_id,
                name,
                status,
                duration_ms,
            )

    def skipped(self, name, reason, details=None):
        now = datetime.utcnow().isoformat() + "Z"
        entry = {
            "trace_id": self.trace_id,
            "camera_id": self.camera_id,
            "frame_id": str(self.frame_id),
            "step": name,
            "status": "SKIPPED",
            "start": now,
            "end": now,
            "duration_ms": 0.0,
            "success": True,
            "error": None,
            "details": {
                **(details or {}),
                "reason": reason,
            },
        }
        self.store.append_timeline(entry)
        log.info(
            "TRACE %s frame=%s step=%s SKIPPED reason=%s",
            self.trace_id,
            self.frame_id,
            name,
            reason,
        )
