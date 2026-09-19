import argparse
import json
import logging
import os
import signal
import socket
import sys
import threading
from datetime import datetime, timedelta, timezone

import cv2
import numpy as np

from core.events import ALLOWED_EVENT_TYPES, EventBuilder
from core.clip_pipeline_v1 import (
    ClipMetadata,
    Detection,
    EpisodeVisionContext,
    FrameObservation,
    IdentityResult,
    IdentityStatus,
    PlateResult,
    SensitiveObjectResult,
    SensitiveStatus,
    SubjectType,
    Topology,
    VisionClipPipelineV1,
    ConfiguredFaceEnricher,
    StaticFaceEnricher,
    StaticPlateEnricher,
    StaticSensitiveObjectEnricher,
    UnavailableFaceEnricher,
    UnavailablePlateEnricher,
    UnavailableSensitiveObjectEnricher,
)
from core.detector_backend import ExistingDetectorBackend, ThreePinnedDetectorBackend
from core.model_runner import model_status
from edge.worker import EdgeVisionWorkerV1
from face_dataset import FaceDatasetError, FaceDatasetManager, safe_component, _regular_file


SOCKET_PATH = os.getenv("SYNORA_VISION_SOCKET", "/run/synora/vision-worker.sock")
PROTOCOL_VERSION = "synora.vision.v1"
PROTOCOL_HELLO = "protocol.hello"
CLIP_PROCESS = "clip.process"
SEGMENT_PROCESS = "segment.process"
EDGE_PIPELINE = "edge-v1"
EPISODE_RELEASE = "episode.release"
ARCFACE_EMBEDDING_DIMENSION = 512
FACE_DATA_ROOT = os.path.abspath(os.path.realpath(os.getenv("SYNORA_FACE_DATA_ROOT", "/var/lib/synora/vision/face")))
MODEL_ROOT = os.getenv("SYNORA_MODEL_ROOT", "/var/lib/synora/models")
ARCFACE_MODEL = os.getenv("SYNORA_ARCFACE_MODEL", os.path.join(MODEL_ROOT, "arcface_w600k_r50.rknn"))
MAX_REQUEST_BYTES = 1 << 20
COMMAND_TIMEOUT_SECONDS = float(os.getenv("SYNORA_VISION_COMMAND_TIMEOUT", "30"))


logging.basicConfig(
    level=logging.INFO,
    format="[%(asctime)s] [%(levelname)s] [%(name)s] %(message)s"
)

log = logging.getLogger(
    "synora.vision"
)


def _parse_worker_time(value):
    if not value:
        return None
    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(float(value), tz=timezone.utc)
    if isinstance(value, str):
        try:
            return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(timezone.utc)
        except ValueError:
            return None
    return None


def _worker_float(name, fallback):
    try:
        value = float(os.getenv(name, str(fallback)))
        return value if value >= 0 else fallback
    except (TypeError, ValueError):
        return fallback


def _worker_enabled(name, default=False):
    return os.getenv(name, "1" if default else "0").strip() == "1"


class VisionWorker:

    def __init__(self, dry_run=False):

        log.info(
            "VISION WORKER INIT"
        )

        self.dry_run = dry_run
        self.pipeline_error = None
        self.detector_error = None
        self.face_error = None
        self.face_dataset = None
        self.face_dataset_startup_error = None
        self.face_enabled = _worker_enabled("SYNORA_VISION_FACE_ENABLED")
        self.plate_enabled = _worker_enabled("SYNORA_VISION_PLATE_ENABLED")
        self.sensitive_objects_enabled = _worker_enabled("SYNORA_VISION_SENSITIVE_OBJECTS_ENABLED")
        self.clip_v1_enabled = dry_run or os.getenv("SYNORA_VISION_CLIP_V1_ENABLED", "0") == "1"
        self.detector_mode = os.getenv("SYNORA_VISION_CLIP_V1_DETECTOR_MODE", "unavailable")
        self.detector_strategy = os.getenv(
            "SYNORA_VISION_DETECTOR_STRATEGY", "three_pinned_workers"
        )
        self.detector_backend = None
        self._episode_contexts = {}
        self._episode_contexts_lock = threading.RLock()

        if dry_run:
            self.face_recognizer = None
            self.person_detector = None
            self.pipeline = None
        else:
            self.face_recognizer = None
            self.person_detector = None
            self.pipeline = None
            try:
                from modules.detect.person_detector import PersonDetector
                detector_core_mask = None
                if self.detector_strategy == "three_pinned_workers":
                    try:
                        from rknnlite.api import RKNNLite
                        detector_core_mask = RKNNLite.NPU_CORE_0
                    except Exception:
                        detector_core_mask = None
                self.person_detector = PersonDetector(core_mask=detector_core_mask)
            except Exception as exc:
                self.detector_error = str(exc)
                log.exception("HUMAN DETECTOR degraded during initialization")
            if self.face_enabled:
                try:
                    try:
                        from modules.face.FaceRecognizer import FaceRecognizer
                        self.face_recognizer = FaceRecognizer(model_path=ARCFACE_MODEL)
                    except Exception as exc:
                        self.face_error = str(exc)
                        log.exception("FACE backend unavailable during initialization")
                    if self.face_recognizer is not None and self.person_detector is not None:
                        from core.pipeline import VisionPipeline
                        self.pipeline = VisionPipeline(self.face_recognizer, self.person_detector)
                        self.face_dataset = FaceDatasetManager(FACE_DATA_ROOT, self.face_recognizer)
                        try:
                            self.face_dataset.startup()
                        except FaceDatasetError as exc:
                            self.face_dataset_startup_error = exc.code
                except Exception as exc:
                    self.pipeline_error = str(exc)
                    log.exception("VISION PIPELINE degraded during initialization")
            else:
                self.face_error = "disabled_by_configuration"
            if self.detector_error:
                self.pipeline_error = self.detector_error
        if not dry_run and self.detector_backend is None:
            detector_timeout = _worker_float("SYNORA_VISION_V1_DETECTOR_TIMEOUT", 2.0)
            if self.detector_strategy == "three_pinned_workers" and self.person_detector is not None:
                try:
                    from rknnlite.api import RKNNLite
                    masks = [RKNNLite.NPU_CORE_0, RKNNLite.NPU_CORE_1, RKNNLite.NPU_CORE_2]
                    pinned = [self.person_detector]
                    pinned.extend(PersonDetector(core_mask=mask) for mask in masks[1:])
                    if not all(detector.available for detector in pinned):
                        raise RuntimeError("one or more pinned NPU cores are unavailable")
                    self.detector_backend = ThreePinnedDetectorBackend(pinned, detector_timeout)
                except Exception as exc:
                    log.warning("three pinned detector setup failed; falling back to single runner: %s", exc)
                    for detector in locals().get("pinned", [])[1:]:
                        try:
                            detector.close()
                        except Exception:
                            pass
            if self.detector_backend is None:
                self.detector_backend = ExistingDetectorBackend(self.person_detector, detector_timeout)

    def capabilities(self):
        if self.dry_run:
            available = {"status": "available", "mode": "dry_run"}
            return {
                "mode": "dry_run",
                "status": "normal",
                "backend": "dry_run",
                "embedding_dimension": ARCFACE_EMBEDDING_DIMENSION,
                "capabilities": {
                    "face_detection": dict(available),
                    "face_recognition": dict(available),
                    "object_detection": dict(available),
                    "weapon_detection": dict(available),
                    "fall_detection": dict(available),
                },
                "models": {},
                "face_dataset": {"status": "not_configured", "dimension": ARCFACE_EMBEDDING_DIMENSION},
                "detector_backend": {
                    "name": "existing_detector", "status": "unavailable", "backend": "not_run",
                    "model_path": "", "model_version": "not_run", "input_size": 640,
                    "confidence_threshold": 0.40, "nms_threshold": 0.45, "real_model": False,
                },
            }

        models = {
            "arcface": model_status(ARCFACE_MODEL),
            "scrfd": model_status(os.path.join(MODEL_ROOT, "det_10g.rknn")),
            "yolo": model_status(os.path.join(MODEL_ROOT, "yolov8.rknn")),
            "weapon": model_status(os.path.join(MODEL_ROOT, "weapon.rknn")),
        }
        weapon_capability = dict(models["weapon"])
        weapon_capability["optional"] = True
        if weapon_capability.get("status") == "missing":
            weapon_capability["status"] = "degraded"
            weapon_capability["error"] = "optional weapon model is unavailable"
        else:
            weapon_capability["status"] = "unavailable"
            weapon_capability["error"] = "weapon detector is not enabled in the clip pipeline"
        face_capability = self.face_recognizer.capability() if self.face_recognizer is not None else {
            "status": "disabled" if not getattr(self, "face_enabled", False) else "unavailable",
            "error": getattr(self, "face_error", None) or "face recognizer unavailable",
        }
        object_capability = self.person_detector.capability() if self.person_detector is not None else {"status": "unavailable", "error": getattr(self, "detector_error", None) or "person detector unavailable"}
        face_detection = {"status": "disabled", "error": "disabled_by_configuration"} if not getattr(self, "face_enabled", False) else {"status": "unavailable", "error": "face detector unavailable"}
        if self.pipeline is not None:
            face_detection = self.pipeline.face_detection_capability()
        backend = "unavailable"
        for component in (self.face_recognizer, self.person_detector):
            runner = getattr(component, "runner", None)
            if runner is not None and getattr(runner, "backend", None):
                backend = runner.backend
                break
        capability_ok = lambda item: item.get("status") in {"available", "disabled"}
        runtime_status = "degraded" if self.pipeline_error or not all(capability_ok(item) for item in (face_capability, object_capability, face_detection)) else "normal"
        result = {
            "mode": runtime_status,
            "status": runtime_status,
            "backend": backend,
            "embedding_dimension": getattr(self.face_recognizer, "embedding_dim", ARCFACE_EMBEDDING_DIMENSION),
            "capabilities": {
                "face_detection": face_detection,
                "face_recognition": face_capability,
                "object_detection": object_capability,
                "weapon_detection": weapon_capability,
                "fall_detection": {"status": "unavailable", "error": "fall detector is not enabled in the clip pipeline"},
            },
            "models": models,
            "error": self.pipeline_error,
            "enrichers": {
                "face": "enabled" if getattr(self, "face_enabled", False) else "disabled",
                "plate": "enabled" if getattr(self, "plate_enabled", False) else "disabled",
                "sensitive_objects": "enabled" if getattr(self, "sensitive_objects_enabled", False) else "disabled",
            },
            "detector_backend": self.detector_backend.capability() if getattr(self, "detector_backend", None) is not None else {
                "name": "existing_detector", "status": "unavailable", "model_version": "unknown", "real_model": False,
            },
        }
        if self.face_dataset is not None:
            result["face_dataset"] = self.face_dataset.snapshot()
        if self.face_dataset_startup_error:
            result["face_dataset_error"] = self.face_dataset_startup_error
        return result

    def protocol_hello(self, req):
        requested = req.get("protocol_version")
        if requested != PROTOCOL_VERSION:
            return self._with_request_id(req.get("request_id", ""), {
                "operation": PROTOCOL_HELLO,
                "protocol_version": PROTOCOL_VERSION,
                "status": "degraded",
                "backend": "unavailable",
                "embedding_dimension": ARCFACE_EMBEDDING_DIMENSION,
                "error": "unsupported protocol version",
                "failure_code": "protocol_version_unsupported",
            })
        details = self.capabilities()
        return self._with_request_id(req.get("request_id", ""), {
            "operation": PROTOCOL_HELLO,
            "protocol_version": PROTOCOL_VERSION,
            "status": details["status"],
            "backend": details["backend"],
            "embedding_dimension": details["embedding_dimension"],
            "models": details.get("models", {}),
            "capabilities": details.get("capabilities", {}),
            "face_dataset": details.get("face_dataset", {"status": "unavailable"}),
        })

    # ------------------------------------------------

    def process_request(
        self,
        req,
    ):

        request_id = req.get("request_id") or req.get("id") or ""
        operation = req.get("operation")
        if operation == PROTOCOL_HELLO:
            return self.protocol_hello(req)
        if operation == "face_dataset.embed":
            return self._with_request_id(request_id, self.process_face_embed(req))
        if operation == "face_dataset.reload":
            return self._with_request_id(request_id, self.process_face_reload(req))
        if operation == SEGMENT_PROCESS:
            if req.get("pipeline") not in {"clip-v1", EDGE_PIPELINE}:
                return self._with_request_id(request_id, {
                    "error": "segment processing requires clip-v1",
                    "failure_code": "invalid_request",
                })
            return self._with_request_id(request_id, self.process_segment_v1(req))
        if operation == EPISODE_RELEASE:
            self.release_episode_context(req.get("episode_id") or "")
            return self._with_request_id(request_id, {"status": "released", "events": []})
        if operation and operation != CLIP_PROCESS:
            return self._with_request_id(request_id, {
                "error": "unsupported worker operation",
                "failure_code": "unsupported_operation",
            })

        if req.get("pipeline") in {"clip-v1", EDGE_PIPELINE}:
            if not self.dry_run and (not getattr(self, "clip_v1_enabled", False) or getattr(self, "detector_mode", "") != "real_replay"):
                return self._with_request_id(request_id, {
                    "error": "real replay detector mode is not enabled",
                    "failure_code": "vision_clip_v1_disabled",
                })
            return self._with_request_id(request_id, self.process_clip_v1(req))

        clip_path = req.get(
            "clip_path"
        )

        camera_id = (
            req.get("camera_id") or
            req.get("camera") or
            "unknown"
        )

        clip_id = (
            req.get("clip_id") or
            req.get("id")
        )

        node_id = req.get(
            "node_id"
        )
        activation_id = req.get("activation_id")
        sequence_key = req.get("sequence_key")
        clip_index = req.get("clip_index")

        device_id = (
            req.get("device_id") or
            camera_id
        )

        if not clip_path:
            return self._with_request_id(request_id, {
                "error": "missing clip_path",
                "failure_code": "invalid_request",
            })

        details = self.capabilities()
        if not self.dry_run and details["status"] != "normal":
            return self._with_request_id(request_id, {
                "error": "no_models_available",
                "failure_code": "worker_degraded",
                "message": self.pipeline_error or "vision pipeline unavailable",
                "capabilities": details,
            })

        log.info(
            "PROCESS CLIP camera=%s clip_id=%s",
            camera_id,
            clip_id,
        )

        if self.dry_run:
            result = {
                "events": [
                    build_dry_run_event(
                        clip_path=clip_path,
                        camera_id=camera_id,
                        clip_id=clip_id,
                        node_id=node_id,
                        device_id=device_id,
                        activation_id=activation_id,
                        sequence_key=sequence_key,
                        clip_index=clip_index,
                        event_kind=req.get("debug_event", "unknown"),
                    )
                ]
            }
        else:
            result = self.pipeline.process_clip(
                clip_path,
                camera_id,
                clip_id=clip_id,
                node_id=node_id,
                device_id=device_id,
                activation_id=activation_id,
                sequence_key=sequence_key,
                clip_index=clip_index,
            )

        log.info(
            "PIPELINE RESULT keys=%s",
            list(result.keys()) if result else None,
        )

        events = result.get(
            "events",
            [],
        )

        log.info(
            "PIPELINE EVENTS RAW=%s",
            events,
        )

        if not result:

            return {
                "events": []
            }

        events = result.get(
            "events",
            [],
        )

        log.info(
            "WORKER RETURN events=%d",
            len(events),
        )

        return {"request_id": request_id, "events": events}

    def _clip_v1_config(self):
        return {
            "max_crops_per_track": int(os.getenv("SYNORA_VISION_V1_MAX_CROPS", "5")),
            "critical_alert_threshold": _worker_float("SYNORA_VISION_V1_CRITICAL_THRESHOLD", .90),
            "tracker_iou_threshold": _worker_float("SYNORA_VISION_V1_TRACKER_IOU", .20),
            "tracker_max_gap_seconds": _worker_float("SYNORA_VISION_V1_TRACKER_MAX_GAP", 2.5),
            "tracker_max_active_tracks": int(os.getenv("SYNORA_VISION_V1_TRACKER_MAX_ACTIVE", "16")),
            "tracker_min_bbox_width": int(os.getenv("SYNORA_VISION_V1_TRACKER_MIN_WIDTH", "20")),
            "tracker_min_bbox_height": int(os.getenv("SYNORA_VISION_V1_TRACKER_MIN_HEIGHT", "20")),
            "sampling_initial_fps": _worker_float("SYNORA_VISION_V1_INITIAL_FPS", 5.0),
            "sampling_active_fps": _worker_float("SYNORA_VISION_V1_ACTIVE_FPS", 5.0),
            "sampling_stable_fps": _worker_float("SYNORA_VISION_V1_STABLE_FPS", 1.0),
            "sampling_quiet_fps": _worker_float("SYNORA_VISION_V1_QUIET_FPS", 2.0),
            "sampling_quiet_after_clean_samples": int(os.getenv("SYNORA_VISION_V1_QUIET_AFTER_CLEAN_SAMPLES", "5")),
            "sampling_lost_track_recovery_fps": _worker_float("SYNORA_VISION_V1_LOST_TRACK_RECOVERY_FPS", 5.0),
            "sampling_minimum_detection_fps": _worker_float("SYNORA_VISION_V1_MINIMUM_DETECTION_FPS", 1.0),
            "enrichment_max_occlusion_samples": int(os.getenv("SYNORA_VISION_V1_ENRICHMENT_MAX_OCCLUSION_SAMPLES", "5")),
        }

    def _episode_context(self, episode_id, segment_index, *, continuity_reset=False):
        with self._episode_contexts_lock:
            existing = self._episode_contexts.get(episode_id)
            reset = bool(continuity_reset) or (existing is None and int(segment_index or 0) > 0)
            if existing is None or reset:
                existing = EpisodeVisionContext.from_config(
                    episode_id, self._clip_v1_config(), continuity_reset=reset,
                )
                self._episode_contexts[episode_id] = existing
            return existing

    def release_episode_context(self, episode_id):
        with self._episode_contexts_lock:
            self._episode_contexts.pop(episode_id, None)

    def process_clip_v1(self, req, episode_context=None):
        """Run the opt-in clip pipeline and return observations before final summaries."""
        if not req.get("clip_path") and not self.dry_run:
            return {"error": "missing clip_path"}
        clip_id = req.get("clip_id") or req.get("id") or "clip-v1"
        camera_id = req.get("camera_id") or req.get("camera") or "unknown"
        started = _parse_worker_time(req.get("started_at")) or datetime.now(timezone.utc)
        max_duration = _worker_float("SYNORA_VISION_V1_MAX_DURATION", 10.0)
        requested_end = _parse_worker_time(req.get("ends_at"))
        ends = min(requested_end or started + timedelta(seconds=max_duration),
                   started + timedelta(seconds=max_duration))
        clip = ClipMetadata(
            clip_id=clip_id,
            episode_id=req.get("episode_id") or f"episode-{clip_id}",
            camera_id=camera_id,
            topology=Topology(req.get("node_id") or "unknown", req.get("zone") or "unknown",
                              req.get("topology_class") or req.get("zone") or "unknown"),
            trigger_reason=req.get("trigger_reason") or "unknown",
            started_at=started,
            ends_at=ends,
            clip_ref=f"local://clips/{clip_id}",
        )

        # Discovery is the sole bus bridge. Keeping this sink disabled avoids
        # an unvalidated worker-side path around the Go contract boundary.
        preliminary_sink = None
        if self.dry_run:
            face_status = req.get("mock_identity_status", "uncertain")
            face = StaticFaceEnricher(IdentityResult(face_status, req.get("mock_identity_confidence", 0.0)))
            plate = StaticPlateEnricher(PlateResult("not_available"))
            sensitive = StaticSensitiveObjectEnricher(SensitiveObjectResult(SensitiveStatus.NOT_AVAILABLE))
            pipeline = VisionClipPipelineV1({}, face, plate, sensitive, preliminary_sink)
            frames = []
            for item in req.get("mock_frames", []):
                detections = [Detection(d.get("track_id", "track-0"), d.get("subject_type", "human"),
                                         d.get("confidence", 0.0), d.get("roi_ref"))
                              for d in item.get("detections", [])]
                frames.append(FrameObservation.from_values(_parse_worker_time(item.get("at")) or started, detections))
            events = pipeline.process_frames(clip, frames, {
                "name": "existing_detector", "model_version": "not_run", "real_model": False,
                "status": "unavailable", "frames_sampled": len(frames),
                "detections_total": sum(len(frame.detections) for frame in frames),
                "latency_ms": 0.0, "non_human_ignored": 0, "error_code": "dry_run",
            })
        else:
            if req.get("pipeline") == EDGE_PIPELINE:
                try:
                    result = EdgeVisionWorkerV1(self._clip_v1_config()).process_video(
                        clip, req["clip_path"], self.detector_backend,
                        segment_count=1, edge_emulated=False,
                    )
                    events = list(result.events)
                except Exception as exc:
                    return {"error": str(exc), "failure_code": "edge_clip_processing_failed"}
                return {"events": [{"type": event["type"], "track_id": event.get("track_id"), "payload": event["payload"]}
                                   for event in events]}
            face = ConfiguredFaceEnricher(self.pipeline,
                                          min_crops=int(os.getenv("SYNORA_VISION_V1_MIN_FACE_CROPS", "2")),
                                          stability_threshold=_worker_float("SYNORA_VISION_V1_IDENTITY_STABILITY", .67))
            face = face if self.face_enabled else UnavailableFaceEnricher()
            pipeline = VisionClipPipelineV1(self._clip_v1_config(), face, UnavailablePlateEnricher(), UnavailableSensitiveObjectEnricher(), preliminary_sink)
            try:
                events = pipeline.process_video(clip, req["clip_path"], self.detector_backend,
                                                _worker_float("SYNORA_VISION_V1_SAMPLE_PERIOD", .2),
                                                episode_context=episode_context)
            except Exception as exc:
                return {"error": str(exc), "failure_code": "clip_processing_failed"}

        return {"events": [{"type": event["type"], "track_id": event.get("track_id"), "payload": event["payload"]}
                           for event in events]}

    def process_segment_v1(self, req):
        """Process one closed segment using the authoritative segment metadata."""
        segment_req = dict(req)
        segment_req["clip_id"] = req.get("segment_id") or req.get("id") or "segment-v1"
        segment_req["id"] = segment_req["clip_id"]
        segment_req["episode_id"] = req.get("episode_id") or f"episode-{segment_req['clip_id']}"
        segment_req["ends_at"] = req.get("ends_at") or req.get("ended_at")
        segment_req["zone"] = req.get("zone") or req.get("topology_class") or "unknown"
        segment_req["trigger_reason"] = req.get("trigger_reason") or req.get("trigger") or "unknown"
        episode_id = segment_req["episode_id"]
        if req.get("pipeline") == EDGE_PIPELINE:
            # Edge owns pixel tracking for this path.  Do not allocate the
            # central EpisodeVisionContext or retain central track state.
            return self.process_clip_v1(segment_req, episode_context=None)
        context = self._episode_context(
            episode_id, req.get("segment_index", 0),
            continuity_reset=bool(req.get("continuity_reset", False)),
        )
        continuity_reset = context.continuity_reset_pending
        try:
            result = self.process_clip_v1(segment_req, episode_context=context)
            if continuity_reset and "error" not in result:
                result["continuity_reset"] = True
            return result
        finally:
            if bool(req.get("is_final", False)):
                self.release_episode_context(episode_id)

    @staticmethod
    def _with_request_id(request_id, response):
        result = dict(response or {})
        result["request_id"] = request_id
        return result

    @staticmethod
    def _run_bounded(fn, timeout=None):
        if timeout is None:
            timeout = COMMAND_TIMEOUT_SECONDS
        result = {}
        finished = threading.Event()

        def invoke():
            try:
                result["value"] = fn()
            except Exception as exc:
                result["error"] = exc
            finally:
                finished.set()

        threading.Thread(target=invoke, daemon=True).start()
        if not finished.wait(max(0.1, timeout)):
            raise FaceDatasetError("timeout", "vision command timed out")
        if "error" in result:
            raise result["error"]
        return result.get("value")

    def _source_for_request(self, req):
        resident_id = req.get("resident_id")
        photo_id = req.get("photo_id")
        storage_key = req.get("storage_key")
        if not safe_component(resident_id) or not safe_component(photo_id):
            raise FaceDatasetError("invalid_request", "resident_id and photo_id are required")
        if not isinstance(storage_key, str):
            raise FaceDatasetError("path_not_allowed", "a canonical storage key is required")
        parts = storage_key.split("/")
        if len(parts) != 2 or parts[0] != resident_id or not safe_component(parts[1]):
            raise FaceDatasetError("path_outside_root", "invalid face source key")
        source = os.path.join(FACE_DATA_ROOT, "sources", parts[0], parts[1])
        return source, _regular_file(source, FACE_DATA_ROOT)

    def process_face_embed(self, req):
        if self.face_recognizer is None or self.pipeline is None or self.face_dataset is None:
            return {"error": "face recognition unavailable", "failure_code": "backend_unavailable"}
        try:
            source, _ = self._source_for_request(req)
            image = cv2.imread(source, cv2.IMREAD_COLOR)
            if image is None:
                raise FaceDatasetError("source_invalid", "face source cannot be decoded")

            def infer():
                faces = self.pipeline.face_detector.detect(image)
                if len(faces) == 0:
                    raise FaceDatasetError("no_face", "exactly one face is required")
                if len(faces) != 1:
                    raise FaceDatasetError("multiple_faces", "exactly one face is required")
                face = faces[0]
                x1, y1, x2, y2 = face["bbox"]
                if min(x2 - x1, y2 - y1) < self.pipeline.FACE_MIN_SIZE:
                    raise FaceDatasetError("face_too_small", "face is below the existing minimum size")
                crop = self.pipeline.make_square_crop(image, x1, y1, x2, y2)
                quality = float(self.pipeline.face_quality(crop))
                if not np.isfinite(quality) or quality <= 0.0:
                    raise FaceDatasetError("face_too_blurry", "face quality is unusable")
                aligned = self.pipeline.align_face_arcface(image, face.get("landmarks"))
                if aligned is None:
                    raise FaceDatasetError("face_alignment_failed", "face alignment failed")
                embedding = self.face_recognizer.embed(aligned)
                backend_failure = getattr(self.face_recognizer, "last_embed_failure", "")
                if embedding is None and backend_failure:
                    raise FaceDatasetError(backend_failure, "ArcFace embedding is invalid")
                embedding, failure = self.face_recognizer.validate_embedding(
                    embedding, self.face_recognizer.embedding_dim
                )
                if failure:
                    raise FaceDatasetError(failure, "ArcFace embedding is invalid")
                return embedding

            embedding = self._run_bounded(infer)
            return {
                "embedding": [float(value) for value in embedding],
                "model_fingerprint": self.face_recognizer.model_fingerprint(),
            }
        except FaceDatasetError as exc:
            return {"error": exc.message, "failure_code": exc.code}
        except Exception:
            log.exception("face dataset embed failed")
            return {"error": "face embedding failed", "failure_code": "embedding_failed"}

    def process_face_reload(self, req):
        if self.face_dataset is None:
            return {"error": "face dataset unavailable", "failure_code": "backend_unavailable"}
        try:
            state = self._run_bounded(
                lambda: self.face_dataset.reload(req.get("version"), req.get("root"))
            )
            return {
                "version": state["loaded_version"],
                "loaded_version": state["loaded_version"],
                "active_revision": state["active_revision"],
                "dimension": state["dimension"],
                "model_fingerprint": state["fingerprint"],
            }
        except FaceDatasetError as exc:
            return {"error": exc.message, "failure_code": exc.code}
        except Exception:
            log.exception("face dataset reload failed")
            return {"error": "face dataset reload failed", "failure_code": "reload_failed"}

    # ------------------------------------------------

    def start(self):

        os.makedirs(os.path.dirname(SOCKET_PATH), exist_ok=True)

        if os.path.exists(SOCKET_PATH):
            os.remove(SOCKET_PATH)

        server = socket.socket(
            socket.AF_UNIX,
            socket.SOCK_STREAM,
        )

        server.bind(
            SOCKET_PATH
        )

        server.listen(4)

        log.info(
            "VISION IPC READY socket=%s",
            SOCKET_PATH,
        )

        while True:

            conn, _ = server.accept()

            log.info(
                "IPC CLIENT CONNECTED"
            )

            self.serve_connection(conn)

    def serve_connection(self, conn):
        """Serve the same bounded JSON transport used by the Unix socket."""
        with conn:
            reader = conn.makefile("r")
            writer = conn.makefile("w")
            hello_complete = False
            while True:
                line = reader.readline(MAX_REQUEST_BYTES + 1)
                if not line:
                    break
                if len(line) > MAX_REQUEST_BYTES:
                    writer.write(json.dumps({"error": "request too large", "failure_code": "request_too_large"}, separators=(",", ":")) + "\n")
                    writer.flush()
                    continue
                req = None
                try:
                    req = json.loads(line)
                    if not isinstance(req, dict):
                        raise ValueError("request must be an object")
                    if not hello_complete and req.get("operation") != PROTOCOL_HELLO:
                        resp = {
                            "request_id": req.get("request_id") or req.get("id") or "",
                            "error": "protocol hello required before worker requests",
                            "failure_code": "protocol_handshake_required",
                        }
                    else:
                        resp = self.process_request(req)
                        if req.get("operation") == PROTOCOL_HELLO and "error" not in resp:
                            hello_complete = resp.get("protocol_version") == PROTOCOL_VERSION
                except Exception:
                    log.exception("PROCESS ERROR")
                    resp = {
                        "request_id": req.get("request_id") if isinstance(req, dict) else "",
                        "error": "invalid worker request",
                        "failure_code": "invalid_request",
                    }
                writer.write(json.dumps(resp, separators=(",", ":"), allow_nan=False) + "\n")
                writer.flush()


# ------------------------------------------------
# SIGNALS
# ------------------------------------------------

def shutdown(
    signum,
    frame,
):

    log.info(
        "Shutdown signal received"
    )

    try:

        os.remove(
            SOCKET_PATH
        )

    except:
        pass

    sys.exit(0)


signal.signal(
    signal.SIGTERM,
    shutdown,
)

signal.signal(
    signal.SIGINT,
    shutdown,
)


def build_dry_run_event(
    clip_path,
    camera_id,
    clip_id=None,
    node_id=None,
    device_id=None,
    activation_id=None,
    sequence_key=None,
    clip_index=None,
    event_kind="unknown",
):

    camera_id = camera_id or "unknown"
    clip_id = (
        clip_id or
        os.path.splitext(
            os.path.basename(clip_path)
        )[0] or
        "dry-run"
    )

    builder = EventBuilder({
        "camera_id": camera_id,
        "device_id": device_id or camera_id,
        "node_id": node_id,
        "clip_id": clip_id,
        "clip_path": clip_path,
        "activation_id": activation_id,
        "sequence_key": sequence_key,
        "clip_index": clip_index,
    })

    scene_id = (
        clip_id or
        "dry-run"
    )

    event_kind = (
        event_kind or
        "unknown"
    ).strip().lower()

    if event_kind == "identity":
        return builder.identity(
            camera_id,
            scene_id,
            "dry-run-track",
            "dry-run-resident",
            0.99,
            1,
        )

    if event_kind == "uncertain":
        return builder.uncertain(
            camera_id,
            scene_id,
            "dry-run-track",
            "dry-run-resident",
            0.42,
            1,
        )

    return builder.unknown(
        camera_id,
        scene_id,
        "dry-run-track",
        1,
    )


def validate_event_contract(event):

    if event.get("type") not in ALLOWED_EVENT_TYPES:
        raise AssertionError(f"unexpected event type: {event.get('type')}")

    for key in (
        "type",
        "source",
        "timestamp",
        "payload",
    ):
        if key not in event:
            raise AssertionError(f"missing event key: {key}")

    payload = event["payload"]

    for key in (
        "camera_id",
        "device_id",
        "clip_id",
        "clip_path",
        "timestamp",
        "confidence",
    ):
        if key not in payload:
            raise AssertionError(f"missing payload key: {key}")


def run_self_test():

    for event_kind in (
        "unknown",
        "identity",
        "uncertain",
    ):
        event = build_dry_run_event(
            clip_path="/tmp/synora-self-test.mp4",
            camera_id="cam_01",
            clip_id="clip_self_test",
            node_id="node_01",
            device_id="device_01",
            event_kind=event_kind,
        )

        validate_event_contract(
            event
        )

        if event["clip_id"] != "clip_self_test":
            raise AssertionError("clip_id not propagated top-level")

        if event["payload"]["clip_id"] != "clip_self_test":
            raise AssertionError("clip_id not propagated in payload")

        expected_type = f"vision.{event_kind}"

        if event["type"] != expected_type:
            raise AssertionError(
                f"expected {expected_type}, got {event['type']}"
            )

    print("vision-worker self-test ok")


def parse_args(argv):

    parser = argparse.ArgumentParser(
        description="Synora Vision Worker"
    )

    parser.add_argument(
        "--self-test",
        action="store_true",
        help="run local event-format checks without RKNN inference",
    )

    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="produce a simulated vision event without RKNN inference",
    )

    parser.add_argument(
        "--clip",
        help="clip path for dry-run one-shot mode",
    )

    parser.add_argument(
        "--camera",
        default="cam_01",
        help="camera id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--clip-id",
        help="clip id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--node-id",
        help="node id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--device-id",
        help="device id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--debug-event",
        choices=("unknown", "identity", "uncertain"),
        default="unknown",
        help="simulated event kind for dry-run",
    )

    return parser.parse_args(
        argv
    )


# ------------------------------------------------
# MAIN
# ------------------------------------------------

if __name__ == "__main__":

    args = parse_args(
        sys.argv[1:]
    )

    if args.self_test:
        run_self_test()
        sys.exit(0)

    if args.dry_run and args.clip:
        event = build_dry_run_event(
            clip_path=args.clip,
            camera_id=args.camera,
            clip_id=args.clip_id,
            node_id=args.node_id,
            device_id=args.device_id,
            event_kind=args.debug_event,
        )
        print(json.dumps({
            "events": [
                event
            ]
        }))
        sys.exit(0)

    worker = VisionWorker(
        dry_run=args.dry_run,
    )

    worker.start()
