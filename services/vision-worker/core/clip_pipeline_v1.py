"""Foundations for the fixed-duration Vision Clip V1 pipeline.

This module deliberately keeps model-specific work behind small interfaces.
The default backends report unavailable/inconclusive; they never manufacture a
classification, identity, plate or embedding.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from enum import Enum
import hashlib
import os
import time
import uuid
from typing import Any, Callable, Iterable, Optional


class SubjectType(str, Enum):
    HUMAN = "human"
    ANIMAL = "animal"
    VEHICLE = "vehicle"
    UNKNOWN = "unknown"


class IdentityStatus(str, Enum):
    RECOGNIZED = "recognized"
    UNCERTAIN = "uncertain"
    UNKNOWN = "unknown"
    NOT_AVAILABLE = "not_available"


class SensitiveStatus(str, Enum):
    CLEAR = "clear"
    DETECTED = "detected"
    INCONCLUSIVE = "inconclusive"
    NOT_AVAILABLE = "not_available"


def _utc(value: Optional[datetime]) -> datetime:
    if value is None:
        return datetime.now(timezone.utc)
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _bounded(value: float) -> float:
    return max(0.0, min(1.0, float(value)))


def _local_ref(value: Optional[str]) -> Optional[str]:
    """Accept only opaque local references, never URLs or inline data."""
    if not value or not isinstance(value, str):
        return None
    value = value.strip()
    if not value or value.startswith(("http://", "https://", "data:", "file://")):
        return None
    if value.startswith("local://") or value.startswith("/var/lib/synora/"):
        return value
    return None


@dataclass(frozen=True)
class Topology:
    node_id: str
    zone: str

    def as_dict(self) -> dict[str, str]:
        return {"node_id": self.node_id, "zone": self.zone}


@dataclass(frozen=True)
class ClipMetadata:
    clip_id: str
    episode_id: str
    camera_id: str
    topology: Topology
    trigger_reason: str
    started_at: datetime
    ends_at: datetime
    clip_ref: Optional[str] = None

    def __post_init__(self) -> None:
        if not self.clip_id or not self.episode_id or not self.camera_id:
            raise ValueError("clip_id, episode_id and camera_id are required")
        if _utc(self.ends_at) < _utc(self.started_at):
            raise ValueError("clip ends_at precedes started_at")


@dataclass(frozen=True)
class Detection:
    track_id: str
    subject_type: SubjectType | str
    confidence: float
    roi_ref: Optional[str] = None
    roi: Any = None

    def __post_init__(self) -> None:
        if not self.track_id:
            raise ValueError("track_id is required")
        try:
            object.__setattr__(self, "subject_type", SubjectType(self.subject_type))
        except ValueError:
            object.__setattr__(self, "subject_type", SubjectType.UNKNOWN)
        object.__setattr__(self, "confidence", _bounded(self.confidence))


@dataclass(frozen=True)
class FrameObservation:
    at: datetime
    detections: tuple[Detection, ...]

    @classmethod
    def from_values(cls, at: datetime, detections: Iterable[Detection]) -> "FrameObservation":
        return cls(_utc(at), tuple(detections))


@dataclass
class TrackState:
    track_id: str
    subject_type: SubjectType
    first_seen_at: datetime
    last_seen_at: datetime
    confidences: list[float] = field(default_factory=list)
    crops: list[dict[str, Any]] = field(default_factory=list)
    sensitive: list["SensitiveObjectResult"] = field(default_factory=list)

    def add(self, detection: Detection, at: datetime, max_crops: int) -> None:
        at = _utc(at)
        self.first_seen_at = min(self.first_seen_at, at)
        self.last_seen_at = max(self.last_seen_at, at)
        self.confidences.append(_bounded(detection.confidence))
        ref = _local_ref(detection.roi_ref)
        if ref:
            self.crops.append({"ref": ref, "score": detection.confidence, "roi": detection.roi})
            self.crops.sort(key=lambda item: item["score"], reverse=True)
            del self.crops[max_crops:]


@dataclass(frozen=True)
class IdentityResult:
    status: IdentityStatus | str
    confidence: float = 0.0
    embedding_ref: Optional[str] = None
    reason: Optional[str] = None

    def as_dict(self) -> dict[str, Any]:
        try:
            status = IdentityStatus(self.status).value
        except ValueError:
            status = IdentityStatus.UNCERTAIN.value
        return {
            "status": status,
            "confidence": _bounded(self.confidence),
            "embedding_ref": _local_ref(self.embedding_ref),
        }


@dataclass(frozen=True)
class PlateResult:
    status: IdentityStatus | str
    confidence: float = 0.0
    value_ref: Optional[str] = None
    reason: Optional[str] = None

    def as_dict(self) -> dict[str, Any]:
        try:
            status = IdentityStatus(self.status).value
        except ValueError:
            status = IdentityStatus.NOT_AVAILABLE.value
        return {
            "status": status,
            "confidence": _bounded(self.confidence),
            "value_ref": _local_ref(self.value_ref),
        }


@dataclass(frozen=True)
class SensitiveObjectResult:
    status: SensitiveStatus | str
    detections: tuple[dict[str, Any], ...] = ()
    critical: bool = False
    confidence: float = 0.0
    reason: Optional[str] = None

    def safe_detections(self) -> list[dict[str, Any]]:
        safe: list[dict[str, Any]] = []
        for detection in self.detections:
            if not isinstance(detection, dict):
                continue
            item: dict[str, Any] = {}
            if isinstance(detection.get("kind"), str):
                item["kind"] = detection["kind"]
            item["confidence"] = _bounded(detection.get("confidence", self.confidence))
            ref = _local_ref(detection.get("roi_ref"))
            if ref:
                item["roi_ref"] = ref
            safe.append(item)
        return safe

    def as_dict(self) -> dict[str, Any]:
        try:
            status = SensitiveStatus(self.status).value
        except ValueError:
            status = SensitiveStatus.INCONCLUSIVE.value
        return {"status": status, "detections": self.safe_detections()}


class FaceEnricher:
    def enrich(self, track: TrackState) -> IdentityResult:
        raise NotImplementedError


class PlateEnricher:
    def enrich(self, track: TrackState) -> PlateResult:
        raise NotImplementedError


class SensitiveObjectEnricher:
    available = False

    def inspect(self, detection: Detection) -> SensitiveObjectResult:
        return SensitiveObjectResult(SensitiveStatus.NOT_AVAILABLE, reason="backend_unavailable")


class UnavailableFaceEnricher(FaceEnricher):
    def enrich(self, track: TrackState) -> IdentityResult:
        return IdentityResult(IdentityStatus.UNCERTAIN, reason="face_backend_unavailable")


class UnavailablePlateEnricher(PlateEnricher):
    def enrich(self, track: TrackState) -> PlateResult:
        return PlateResult(IdentityStatus.NOT_AVAILABLE, reason="plate_backend_unavailable")


class UnavailableSensitiveObjectEnricher(SensitiveObjectEnricher):
    available = False


class ConfiguredFaceEnricher(FaceEnricher):
    """Adapter around the already configured detector/recognizer pair.

    It only reports ``recognized`` after multiple quality observations agree.
    A single good crop is intentionally still ``uncertain``.
    """

    def __init__(self, vision_pipeline: Any, min_crops: int = 2,
                 stability_threshold: float = 0.67):
        self.pipeline = vision_pipeline
        self.min_crops = max(2, int(min_crops))
        self.stability_threshold = _bounded(stability_threshold)

    def enrich(self, track: TrackState) -> IdentityResult:
        recognizer = getattr(self.pipeline, "face_recognizer", None)
        detector = getattr(self.pipeline, "face_detector", None)
        if recognizer is None or detector is None or not getattr(recognizer, "available", False):
            return IdentityResult(IdentityStatus.UNCERTAIN, reason="face_backend_unavailable")
        observations: list[tuple[str, float]] = []
        for index, crop in enumerate(track.crops):
            image = crop.get("roi")
            if image is None:
                continue
            try:
                faces = detector.detect(image)
                if len(faces) != 1:
                    continue
                face = faces[0]
                x1, y1, x2, y2 = map(int, face["bbox"])
                face_crop = self.pipeline.make_square_crop(image, x1, y1, x2, y2)
                if face_crop.size == 0 or float(self.pipeline.face_quality(face_crop)) <= 0:
                    continue
                aligned = self.pipeline.align_face_arcface(image, face.get("landmarks"))
                if aligned is None:
                    continue
                embedding = recognizer.embed(aligned)
                status, identity, score = recognizer.identify_embedding(embedding)
                if status in ("match", "uncertain", "unknown"):
                    observations.append((identity or "", _bounded(score)))
            except Exception:
                continue
        if len(observations) < self.min_crops:
            return IdentityResult(IdentityStatus.UNCERTAIN, max((score for _, score in observations), default=0.0),
                                  reason="insufficient_quality_crops")
        grouped: dict[str, list[float]] = {}
        for identity, score in observations:
            grouped.setdefault(identity, []).append(score)
        identity, scores = max(grouped.items(), key=lambda item: (len(item[1]), max(item[1])))
        confidence = sum(scores) / len(scores)
        consistency = len(scores) / len(observations)
        match_threshold = float(getattr(recognizer, "match_threshold", 1.0))
        if confidence >= match_threshold and consistency >= self.stability_threshold:
            return IdentityResult(IdentityStatus.RECOGNIZED, confidence,
                                  make_embedding_ref(track.track_id, 0))
        if not identity and consistency >= self.stability_threshold and confidence < float(getattr(recognizer, "uncertain_threshold", match_threshold)):
            return IdentityResult(IdentityStatus.UNKNOWN, confidence, reason="gallery_rejected")
        return IdentityResult(IdentityStatus.UNCERTAIN, confidence, reason="ambiguous_match")


class StaticFaceEnricher(FaceEnricher):
    """Test/demo backend; production code must inject a real configured backend."""

    def __init__(self, result: IdentityResult):
        self.result = result

    def enrich(self, track: TrackState) -> IdentityResult:
        return self.result


class StaticPlateEnricher(PlateEnricher):
    """Test/demo backend; never used by the worker unless explicitly injected."""

    def __init__(self, result: PlateResult):
        self.result = result

    def enrich(self, track: TrackState) -> PlateResult:
        return self.result


class StaticSensitiveObjectEnricher(SensitiveObjectEnricher):
    def __init__(self, result: SensitiveObjectResult):
        self.result = result
        self.available = True

    def inspect(self, detection: Detection) -> SensitiveObjectResult:
        return self.result


class FixedClipManager:
    """Lifecycle-only manager for fixed clips and episode continuity."""

    def __init__(self, max_duration_seconds: float = 10.0, continuity_window_seconds: float = 5.0,
                 id_factory: Optional[Callable[[str], str]] = None):
        if max_duration_seconds <= 0 or continuity_window_seconds < 0:
            raise ValueError("clip duration must be positive and continuity window non-negative")
        self.max_duration = timedelta(seconds=max_duration_seconds)
        self.continuity_window = timedelta(seconds=continuity_window_seconds)
        self._id_factory = id_factory or (lambda prefix: f"{prefix}-{uuid.uuid4().hex[:12]}")
        self._active: Optional[ClipMetadata] = None
        self._last_closed: Optional[ClipMetadata] = None
        self._last_track_ids: set[str] = set()

    @property
    def active(self) -> Optional[ClipMetadata]:
        return self._active

    def trigger(self, camera_id: str, topology: Topology, trigger_reason: str,
                at: Optional[datetime] = None, track_ids: Iterable[str] = ()) -> ClipMetadata:
        started = _utc(at)
        if self._active is not None and started < self._active.ends_at:
            return self._active
        previous = self._last_closed
        same_context = previous is not None and previous.camera_id == camera_id and previous.topology == topology
        near = same_context and started - previous.ends_at <= self.continuity_window
        continuing_track = same_context and bool(set(track_ids) & self._last_track_ids)
        episode_id = previous.episode_id if (near or continuing_track) else self._id_factory("episode")
        clip = ClipMetadata(
            clip_id=self._id_factory("clip"), episode_id=episode_id, camera_id=camera_id,
            topology=topology, trigger_reason=trigger_reason, started_at=started,
            ends_at=started + self.max_duration,
        )
        self._active = clip
        return clip

    def close(self, at: Optional[datetime] = None, track_ids: Iterable[str] = ()) -> ClipMetadata:
        if self._active is None:
            raise RuntimeError("no active clip")
        closed_at = _utc(at)
        if closed_at < self._active.started_at:
            raise ValueError("close time precedes clip start")
        clip = ClipMetadata(**{**self._active.__dict__, "ends_at": min(closed_at, self._active.ends_at)})
        self._last_closed = clip
        self._last_track_ids = set(track_ids)
        self._active = None
        return clip


class VisionClipPipelineV1:
    def __init__(self, config: Optional[dict[str, Any]] = None,
                 face_enricher: Optional[FaceEnricher] = None,
                 plate_enricher: Optional[PlateEnricher] = None,
                 sensitive_enricher: Optional[SensitiveObjectEnricher] = None,
                 preliminary_sink: Optional[Callable[[dict[str, Any]], None]] = None):
        cfg = dict(config or {})
        self.max_crops = max(1, int(cfg.get("max_crops_per_track", 5)))
        self.critical_threshold = _bounded(cfg.get("critical_alert_threshold", 0.90))
        self.face_enricher = face_enricher or UnavailableFaceEnricher()
        self.plate_enricher = plate_enricher or UnavailablePlateEnricher()
        self.sensitive_enricher = sensitive_enricher or UnavailableSensitiveObjectEnricher()
        self.preliminary_sink = preliminary_sink

    def process_frames(self, clip: ClipMetadata, frames: Iterable[FrameObservation]) -> list[dict[str, Any]]:
        tracks: dict[str, TrackState] = {}
        emitted_alerts: set[str] = set()
        events: list[dict[str, Any]] = []
        for frame in frames:
            at = _utc(frame.at)
            if at < clip.started_at or at > clip.ends_at:
                continue
            for detection in frame.detections:
                state = tracks.get(detection.track_id)
                if state is None:
                    state = TrackState(detection.track_id, detection.subject_type, at, at)
                    tracks[detection.track_id] = state
                elif state.subject_type != detection.subject_type:
                    state.subject_type = SubjectType.UNKNOWN
                state.add(detection, at, self.max_crops)
                sensitive = self.sensitive_enricher.inspect(detection)
                state.sensitive.append(sensitive)
                if (getattr(self.sensitive_enricher, "available", False)
                        and sensitive.critical and sensitive.confidence >= self.critical_threshold):
                    if detection.track_id not in emitted_alerts:
                        alert = self._preliminary_alert(clip, state, sensitive)
                        emitted_alerts.add(detection.track_id)
                        events.append(alert)
                        if self.preliminary_sink is not None:
                            self.preliminary_sink(alert)
        for state in sorted(tracks.values(), key=lambda item: item.track_id):
            events.append(self._summary(clip, state))
        return events

    def _preliminary_alert(self, clip: ClipMetadata, track: TrackState,
                           result: SensitiveObjectResult) -> dict[str, Any]:
        return {
            "type": "synora.vision.preliminary-alert/v1",
            "track_id": track.track_id,
            "payload": {
                "schema": "synora.vision.preliminary-alert/v1",
                "episode_id": clip.episode_id,
                "clip_id": clip.clip_id,
                "camera_id": clip.camera_id,
                "topology": clip.topology.as_dict(),
                "track": {"id": track.track_id, "subject_type": track.subject_type.value},
                "alert": {"kind": "critical_sensitive_object", "confidence": _bounded(result.confidence),
                           "detections": result.safe_detections()},
            },
        }

    def _summary(self, clip: ClipMetadata, track: TrackState) -> dict[str, Any]:
        identity = IdentityResult(IdentityStatus.NOT_AVAILABLE)
        plate = PlateResult(IdentityStatus.NOT_AVAILABLE)
        if track.subject_type == SubjectType.HUMAN:
            identity = self.face_enricher.enrich(track)
        elif track.subject_type == SubjectType.VEHICLE:
            plate = self.plate_enricher.enrich(track)

        sensitive = self._merge_sensitive(track.sensitive)
        confidence = sum(track.confidences) / max(1, len(track.confidences))
        refs = [item["ref"] for item in track.crops if _local_ref(item.get("ref"))]
        payload = {
            "schema": "synora.vision.clip-summary/v1",
            "episode_id": clip.episode_id,
            "clip_id": clip.clip_id,
            "camera_id": clip.camera_id,
            "topology": clip.topology.as_dict(),
            "trigger": {"reason": clip.trigger_reason, "started_at": _utc(clip.started_at).isoformat()},
            "track": {"id": track.track_id, "subject_type": track.subject_type.value,
                      "first_seen_at": _utc(track.first_seen_at).isoformat(),
                      "last_seen_at": _utc(track.last_seen_at).isoformat(),
                      "confidence": _bounded(confidence)},
            "identity": identity.as_dict(),
            "plate": plate.as_dict(),
            "sensitive_objects": sensitive.as_dict(),
            "media": {"clip_ref": _local_ref(clip.clip_ref), "best_roi_refs": refs},
        }
        return {"type": "synora.vision.clip-summary/v1", "track_id": track.track_id, "payload": payload}

    @staticmethod
    def _merge_sensitive(results: list[SensitiveObjectResult]) -> SensitiveObjectResult:
        available = [item for item in results if item.status != SensitiveStatus.NOT_AVAILABLE]
        if not available:
            return SensitiveObjectResult(SensitiveStatus.NOT_AVAILABLE, reason="backend_unavailable")
        detections: list[dict[str, Any]] = []
        strongest = max(available, key=lambda item: item.confidence)
        for item in available:
            detections.extend(item.safe_detections())
        if any(item.status == SensitiveStatus.DETECTED for item in available):
            status = SensitiveStatus.DETECTED
        elif any(item.status == SensitiveStatus.INCONCLUSIVE for item in available):
            status = SensitiveStatus.INCONCLUSIVE
        else:
            status = SensitiveStatus.CLEAR
        return SensitiveObjectResult(status, tuple(detections), any(item.critical for item in available),
                                     strongest.confidence)

    def process_video(self, clip: ClipMetadata, video_path: str, detector: Any,
                      sample_period_seconds: float = 0.2) -> list[dict[str, Any]]:
        """Adapt the existing person detector without claiming unsupported classes."""
        import cv2
        cap = cv2.VideoCapture(video_path)
        if not cap.isOpened():
            cap.release()
            raise RuntimeError("video cannot be opened")
        fps = float(cap.get(cv2.CAP_PROP_FPS) or 0.0)
        stride = max(1, int(round((fps or 5.0) * sample_period_seconds)))
        frames: list[FrameObservation] = []
        index = 0
        next_track = 0
        active_boxes: dict[str, tuple[int, int, int, int]] = {}
        try:
            while True:
                ok, frame = cap.read()
                if not ok:
                    break
                if index % stride == 0:
                    at = clip.started_at + timedelta(seconds=index / (fps or 5.0))
                    detections: list[Detection] = []
                    raw_detections = list(detector.detect(frame))
                    assigned: set[str] = set()
                    for item in raw_detections:
                        x1, y1, x2, y2 = map(int, item["bbox"])
                        roi = frame[max(0, y1):max(0, y2), max(0, x1):max(0, x2)]
                        ref = f"local://clips/{clip.clip_id}/roi/{index}-{len(detections)}"
                        best_id = None
                        best_iou = 0.0
                        for track_id, old_box in active_boxes.items():
                            if track_id in assigned:
                                continue
                            overlap = self._iou((x1, y1, x2, y2), old_box)
                            if overlap > best_iou:
                                best_id, best_iou = track_id, overlap
                        if best_id is None or best_iou < 0.30:
                            best_id = f"human-{next_track}"
                            next_track += 1
                        assigned.add(best_id)
                        active_boxes[best_id] = (x1, y1, x2, y2)
                        detections.append(Detection(best_id, SubjectType.HUMAN,
                                                    item.get("score", 0.0), ref, roi))
                    frames.append(FrameObservation.from_values(at, detections))
                index += 1
        finally:
            cap.release()
        return self.process_frames(clip, frames)

    @staticmethod
    def _iou(left: tuple[int, int, int, int], right: tuple[int, int, int, int]) -> float:
        x1 = max(left[0], right[0]); y1 = max(left[1], right[1])
        x2 = min(left[2], right[2]); y2 = min(left[3], right[3])
        intersection = max(0, x2 - x1) * max(0, y2 - y1)
        left_area = max(0, left[2] - left[0]) * max(0, left[3] - left[1])
        right_area = max(0, right[2] - right[0]) * max(0, right[3] - right[1])
        union = left_area + right_area - intersection
        return intersection / union if union else 0.0


def make_embedding_ref(track_id: str, crop_index: int) -> str:
    digest = hashlib.sha256(f"{track_id}:{crop_index}".encode()).hexdigest()[:16]
    return f"local://vision/embeddings/{digest}"

