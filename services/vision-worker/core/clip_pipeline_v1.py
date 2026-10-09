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
import math
import os
import time
import uuid
from typing import Any, Callable, Iterable, Optional

from edge.tracker import ClipTrackerV1


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


class TrackEnrichmentState(str, Enum):
    UNSEEN = "unseen"
    CANDIDATE = "candidate"
    ENRICHING = "enriching"
    RECOGNIZED_STABLE = "recognized_stable"
    UNCERTAIN = "uncertain"
    UNKNOWN = "unknown"


class TopologyClass(str, Enum):
    PUBLIC_OUTDOOR = "public_outdoor"
    PRIVATE_PERIMETER = "private_perimeter"
    RESTRICTED_THRESHOLD = "restricted_threshold"
    PROTECTED_INTERIOR = "protected_interior"
    UNKNOWN = "unknown"


class VisionPriority(str, Enum):
    P0_SYSTEM_CRITICAL = "P0_system_critical"
    P1_URGENT_PRESENCE = "P1_urgent_presence"
    P2_CONTEXTUAL_ENRICHMENT = "P2_contextual_enrichment"
    P3_BEST_EFFORT_ENRICHMENT = "P3_best_effort_enrichment"
    P4_LOW_PRIORITY_CONTEXT = "P4_low_priority_context"


_PRIORITY_RANK = {
    VisionPriority.P0_SYSTEM_CRITICAL.value: 0,
    VisionPriority.P1_URGENT_PRESENCE.value: 1,
    VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value: 2,
    VisionPriority.P3_BEST_EFFORT_ENRICHMENT.value: 3,
    VisionPriority.P4_LOW_PRIORITY_CONTEXT.value: 4,
}


@dataclass(frozen=True)
class PriorityDecision:
    priority_hint: str
    reason_codes: tuple[str, ...]
    priority_state: str = "candidate"

    def as_dict(self) -> dict[str, Any]:
        return {"priority_hint": self.priority_hint, "priority_state": self.priority_state, "reason_codes": list(self.reason_codes)}


class VisionPriorityScheduler:
    """Deterministic Vision-only priority policy; it cannot create executable P0."""

    def __init__(self, human_confidence_threshold: float = 0.4, max_inflight: int = 3):
        self.human_confidence_threshold = _bounded(human_confidence_threshold)
        self.max_inflight = max(1, min(3, int(max_inflight)))
        self.priority_evictions = 0
        self.priority_starvation = 0

    def classify(self, subject_type: SubjectType | str, confidence: float,
                 topology_class: TopologyClass | str, *, trigger_reason: str = "",
                 track_state: TrackEnrichmentState | str = TrackEnrichmentState.CANDIDATE,
                 needs_enrichment: bool = False) -> PriorityDecision:
        try:
            subject = SubjectType(subject_type)
        except ValueError:
            subject = SubjectType.UNKNOWN
        try:
            topology = TopologyClass(topology_class)
        except ValueError:
            topology = TopologyClass.UNKNOWN
        try:
            state = TrackEnrichmentState(track_state)
        except ValueError:
            state = TrackEnrichmentState.UNKNOWN
        confidence = _bounded(confidence)
        explicit_trigger = bool(str(trigger_reason).strip()) and str(trigger_reason).strip().lower() not in {"none", "unknown"}
        topology_reason = topology.value
        if subject == SubjectType.HUMAN and confidence >= self.human_confidence_threshold:
            if topology in (TopologyClass.PROTECTED_INTERIOR, TopologyClass.RESTRICTED_THRESHOLD):
                return PriorityDecision(VisionPriority.P1_URGENT_PRESENCE.value,
                                        ("human_detected", topology_reason))
            if topology == TopologyClass.PRIVATE_PERIMETER:
                reasons = ["human_detected", topology_reason]
                if state in (TrackEnrichmentState.UNKNOWN, TrackEnrichmentState.UNCERTAIN):
                    reasons.append("identity_unknown")
                return PriorityDecision(VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value, tuple(reasons))
            if topology == TopologyClass.PUBLIC_OUTDOOR and explicit_trigger:
                return PriorityDecision(VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value,
                                        ("human_detected", topology_reason, "explicit_trigger"))
            if topology == TopologyClass.PUBLIC_OUTDOOR:
                return PriorityDecision(VisionPriority.P4_LOW_PRIORITY_CONTEXT.value,
                                        ("human_detected", topology_reason))
            return PriorityDecision(VisionPriority.P4_LOW_PRIORITY_CONTEXT.value,
                                    ("human_detected", "unknown_topology"))
        if subject == SubjectType.VEHICLE and topology in (TopologyClass.PRIVATE_PERIMETER, TopologyClass.RESTRICTED_THRESHOLD) and explicit_trigger:
            return PriorityDecision(VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value,
                                    ("vehicle_detected", topology_reason, "explicit_trigger"))
        if needs_enrichment:
            return PriorityDecision(VisionPriority.P3_BEST_EFFORT_ENRICHMENT.value,
                                    ("targeted_enrichment", topology_reason))
        return PriorityDecision(VisionPriority.P4_LOW_PRIORITY_CONTEXT.value,
                                (f"{subject.value}_detected", topology_reason))

    def merge_with_core(self, vision_decision: PriorityDecision, core_priority: Optional[str] = None) -> PriorityDecision:
        """Preserve a Core P0 and never let a Vision result downgrade it."""
        if core_priority == VisionPriority.P0_SYSTEM_CRITICAL.value:
            return PriorityDecision(core_priority, ("core_system_critical",), "confirmed")
        return vision_decision

    def order(self, items: Iterable[Any], priority_getter: Callable[[Any], str]) -> list[Any]:
        return sorted(items, key=lambda item: (_PRIORITY_RANK.get(priority_getter(item), 4), getattr(item, "frame_index", 0)))


class PriorityFrameQueue:
    """Bounded priority queue used for scheduling metadata, never unbounded work."""

    def __init__(self, scheduler: VisionPriorityScheduler):
        self.scheduler = scheduler
        self._items: list[Any] = []

    def enqueue(self, item: Any, priority_hint: str) -> bool:
        setattr(item, "priority_hint", priority_hint) if hasattr(item, "__dict__") else None
        if len(self._items) < self.scheduler.max_inflight:
            self._items.append(item)
            return True
        lowest = max(range(len(self._items)), key=lambda index: _PRIORITY_RANK.get(getattr(self._items[index], "priority_hint", VisionPriority.P4_LOW_PRIORITY_CONTEXT.value), 4))
        current = _PRIORITY_RANK.get(getattr(self._items[lowest], "priority_hint", VisionPriority.P4_LOW_PRIORITY_CONTEXT.value), 4)
        incoming = _PRIORITY_RANK.get(priority_hint, 4)
        if incoming < current:
            self._items.pop(lowest)
            self._items.append(item)
            self.scheduler.priority_evictions += 1
            return True
        self.scheduler.priority_starvation += 1
        return False

    def drain_temporal(self) -> list[Any]:
        items = sorted(self._items, key=lambda item: getattr(item, "frame_index", 0))
        self._items.clear()
        return items


@dataclass
class _PendingPriorityFrame:
    frame_index: int
    at: datetime
    frame: Any
    raw_detections: list[dict[str, Any]]
    priority_hint: str = VisionPriority.P4_LOW_PRIORITY_CONTEXT.value


@dataclass
class EpisodeEvidenceLedger:
    max_entries: int = 256
    entries: list[dict[str, Any]] = field(default_factory=list)
    _keys: set[str] = field(default_factory=set, init=False, repr=False)
    dropped_entries: int = 0

    def append(self, sequence: int, priority_hint: str, reason_codes: Iterable[str],
               track_id: str, observed_at: datetime) -> bool:
        item = {
            "sequence": int(sequence), "priority_hint": str(priority_hint),
            "reason_codes": sorted({str(code) for code in reason_codes if str(code)}),
            "track_id": str(track_id), "observed_at": _utc(observed_at).isoformat(),
        }
        key = repr(item)
        if key in self._keys:
            return False
        if len(self.entries) >= max(1, int(self.max_entries)):
            self.dropped_entries += 1
            return False
        self._keys.add(key)
        self.entries.append(item)
        return True

    def snapshot(self) -> list[dict[str, Any]]:
        return [dict(item, reason_codes=list(item["reason_codes"])) for item in self.entries]


@dataclass
class _TrackEnrichment:
    state: TrackEnrichmentState = TrackEnrichmentState.UNSEEN
    identity: Optional[IdentityResult] = None
    last_seen: Optional[datetime] = None
    occluded_frames: int = 0


class TrackEnrichmentPolicy:
    """Per-track face-enrichment gate; it never gates human detection."""

    def __init__(self, max_occlusion_frames: int = 5, max_requests_per_track: int = 5):
        if max_occlusion_frames < 1:
            raise ValueError("max_occlusion_frames must be positive")
        self.max_occlusion_frames = int(max_occlusion_frames)
        self.max_requests_per_track = max(1, int(max_requests_per_track))
        self._tracks: dict[str, _TrackEnrichment] = {}
        self._requests: dict[str, int] = {}
        self.enrichment_requests = 0
        self.enrichment_skipped_recognized = 0
        self.enrichment_skipped_budget = 0
        self.enrichment_reopened = 0

    def observe(self, track_id: str, at: datetime, *, visible: bool = True,
                ambiguous: bool = False, strong_change: bool = False) -> TrackEnrichmentState:
        entry = self._tracks.setdefault(track_id, _TrackEnrichment())
        if not visible:
            entry.occluded_frames += 1
            if entry.occluded_frames >= self.max_occlusion_frames:
                self._reopen(entry)
            return entry.state
        entry.last_seen = _utc(at)
        entry.occluded_frames = 0
        if ambiguous or strong_change:
            self._reopen(entry)
        if entry.state == TrackEnrichmentState.UNSEEN:
            entry.state = TrackEnrichmentState.CANDIDATE
        return entry.state

    def begin_enrichment(self, track_id: str) -> bool:
        entry = self._tracks.setdefault(track_id, _TrackEnrichment())
        if entry.state == TrackEnrichmentState.RECOGNIZED_STABLE:
            self.enrichment_skipped_recognized += 1
            return False
        if self._requests.get(track_id, 0) >= self.max_requests_per_track:
            self.enrichment_skipped_budget += 1
            return False
        entry.state = TrackEnrichmentState.ENRICHING
        self._requests[track_id] = self._requests.get(track_id, 0) + 1
        self.enrichment_requests += 1
        return True

    def complete(self, track_id: str, result: IdentityResult, *, stable: bool = False) -> TrackEnrichmentState:
        entry = self._tracks.setdefault(track_id, _TrackEnrichment())
        entry.identity = result
        if result.status == IdentityStatus.RECOGNIZED and stable:
            entry.state = TrackEnrichmentState.RECOGNIZED_STABLE
        elif result.status == IdentityStatus.UNKNOWN:
            entry.state = TrackEnrichmentState.UNKNOWN
        else:
            entry.state = TrackEnrichmentState.UNCERTAIN
        return entry.state

    def should_enrich(self, track_id: str) -> bool:
        return self._tracks.get(track_id, _TrackEnrichment()).state != TrackEnrichmentState.RECOGNIZED_STABLE

    def identity(self, track_id: str) -> Optional[IdentityResult]:
        entry = self._tracks.get(track_id)
        return entry.identity if entry else None

    def expire(self, track_id: str) -> None:
        self._tracks.pop(track_id, None)
        self._requests.pop(track_id, None)

    def state(self, track_id: str) -> TrackEnrichmentState:
        return self._tracks.get(track_id, _TrackEnrichment()).state

    def _reopen(self, entry: _TrackEnrichment) -> None:
        entry.state = TrackEnrichmentState.CANDIDATE
        entry.identity = None
        entry.occluded_frames = 0
        self.enrichment_reopened += 1

    def counters(self) -> dict[str, int]:
        return {
            "enrichment_requests": self.enrichment_requests,
            "enrichment_skipped_recognized": self.enrichment_skipped_recognized,
            "enrichment_skipped_budget": self.enrichment_skipped_budget,
            "enrichment_reopened": self.enrichment_reopened,
        }


@dataclass
class AdaptiveSamplingPolicy:
    initial_fps: float = 5.0
    active_fps: float = 5.0
    stable_fps: float = 1.0
    quiet_fps: float = 2.0
    quiet_after_clean_samples: int = 5
    lost_track_recovery_fps: float = 5.0
    minimum_detection_fps: float = 1.0
    state: str = "initial"
    clean_samples: int = 0
    transitions: list[dict[str, Any]] = field(default_factory=list)

    def __post_init__(self) -> None:
        for name in ("initial_fps", "active_fps", "stable_fps", "quiet_fps", "lost_track_recovery_fps", "minimum_detection_fps"):
            value = float(getattr(self, name))
            if value <= 0:
                raise ValueError(f"{name} must be positive")
            setattr(self, name, value)
        self.quiet_after_clean_samples = max(1, int(self.quiet_after_clean_samples))

    def fps(self) -> float:
        values = {"initial": self.initial_fps, "active": self.active_fps, "stable": self.stable_fps, "quiet": self.quiet_fps, "recovery": self.lost_track_recovery_fps}
        return max(self.minimum_detection_fps, values.get(self.state, self.active_fps))

    def observe(self, *, has_human: bool, all_stable: bool = False, recently_lost: bool = False) -> str:
        previous = self.state
        if recently_lost:
            self.state = "recovery"
            self.clean_samples = 0
        elif has_human:
            self.clean_samples = 0
            self.state = "stable" if all_stable else "active"
        else:
            self.clean_samples += 1
            if self.clean_samples >= self.quiet_after_clean_samples:
                self.state = "quiet"
            elif self.state == "initial":
                self.state = "active"
        if self.state != previous:
            self.transitions.append({"from": previous, "to": self.state, "clean_samples": self.clean_samples})
        return self.state


@dataclass
class ClipProcessingMetrics:
    capture_open_wall_ms: float = 0.0
    first_frame_decode_wall_ms: float = 0.0
    first_detector_batch_wall_ms: float = 0.0
    first_tracking_wall_ms: float = 0.0
    queue_wait_ms: float = 0.0
    clip_decode_wall_ms: float = 0.0
    detector_wall_ms: float = 0.0
    tracking_wall_ms: float = 0.0
    enrichment_wall_ms: float = 0.0
    summary_wall_ms: float = 0.0
    vision_wall_latency_ms: float = 0.0
    first_observation_wall_ms: float = 0.0
    detector_compute_sum_ms: float = 0.0
    frames_read: int = 0
    frames_skipped_by_policy: int = 0
    frames_sampled: int = 0
    sampling_state_transitions: list[dict[str, Any]] = field(default_factory=list)
    peak_frames_in_flight: int = 0
    enrichment: dict[str, int] = field(default_factory=dict)
    frames_by_priority: dict[str, int] = field(default_factory=dict)
    enrichments_executed: int = 0
    enrichments_avoided: int = 0
    priority_evictions: int = 0
    priority_starvation: int = 0

    def as_dict(self) -> dict[str, Any]:
        return {
            "capture_open_wall_ms": round(max(0.0, self.capture_open_wall_ms), 3),
            "first_frame_decode_wall_ms": round(max(0.0, self.first_frame_decode_wall_ms), 3),
            "first_detector_batch_wall_ms": round(max(0.0, self.first_detector_batch_wall_ms), 3),
            "first_tracking_wall_ms": round(max(0.0, self.first_tracking_wall_ms), 3),
            "queue_wait_ms": round(max(0.0, self.queue_wait_ms), 3),
            "clip_decode_wall_ms": round(max(0.0, self.clip_decode_wall_ms), 3),
            "detector_wall_ms": round(max(0.0, self.detector_wall_ms), 3),
            "tracking_wall_ms": round(max(0.0, self.tracking_wall_ms), 3),
            "enrichment_wall_ms": round(max(0.0, self.enrichment_wall_ms), 3),
            "summary_wall_ms": round(max(0.0, self.summary_wall_ms), 3),
            "vision_wall_latency_ms": round(max(0.0, self.vision_wall_latency_ms), 3),
            "first_observation_wall_ms": round(max(0.0, self.first_observation_wall_ms), 3),
            "detector_compute_sum_ms": round(max(0.0, self.detector_compute_sum_ms), 3),
            "frames_read": self.frames_read,
            "frames_skipped_by_policy": self.frames_skipped_by_policy,
            "frames_sampled": self.frames_sampled,
            "sampling_state_transitions": list(self.sampling_state_transitions),
            "peak_frames_in_flight": self.peak_frames_in_flight,
            "frames_by_priority": dict(self.frames_by_priority),
            "enrichments_executed": self.enrichments_executed,
            "enrichments_avoided": self.enrichments_avoided,
            "priority_evictions": self.priority_evictions,
            "priority_starvation": self.priority_starvation,
            **self.enrichment,
        }


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


def _p1_confirmed(confidences: list[float], first_seen: datetime, last_seen: datetime) -> bool:
    if max(confidences or [0.0]) >= .65:
        return True
    return len(confidences) >= 2 and min(confidences) >= .40 and (_utc(last_seen) - _utc(first_seen)).total_seconds() <= 1.2


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
    topology_class: TopologyClass | str = TopologyClass.UNKNOWN

    def __post_init__(self) -> None:
        try:
            object.__setattr__(self, "topology_class", TopologyClass(self.topology_class))
        except ValueError:
            object.__setattr__(self, "topology_class", TopologyClass.UNKNOWN)

    def as_dict(self) -> dict[str, str]:
        return {"node_id": self.node_id, "zone": self.zone}

    def topology_dict(self) -> dict[str, str]:
        return {"node_id": self.node_id, "zone": self.zone, "topology_class": self.topology_class.value}


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
    requires_roi = False

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

    requires_roi = True

    def __init__(self, vision_pipeline: Any, min_crops: int = 2,
                 stability_threshold: float = 0.90):
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
        elapsed = started - previous.ends_at if same_context else timedelta.max
        near = same_context and timedelta(0) <= elapsed <= self.continuity_window
        continuing_track = near and bool(set(track_ids) & self._last_track_ids)
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


@dataclass
class EpisodeVisionContext:
    """In-memory Vision state that survives finalized segment boundaries."""

    episode_id: str
    tracker: ClipTrackerV1
    priority_scheduler: VisionPriorityScheduler
    enrichment_policy: TrackEnrichmentPolicy
    evidence_ledger: EpisodeEvidenceLedger
    priority_by_track: dict[str, PriorityDecision] = field(default_factory=dict)
    track_records: dict[str, dict[str, Any]] = field(default_factory=dict)
    observation_sequence: int = 0
    last_observation_signature: Optional[str] = None
    last_observation_at: Optional[datetime] = None
    last_priority_state: str = ""
    continuity_reset_pending: bool = False
    last_seen: Optional[datetime] = None

    @classmethod
    def from_config(cls, episode_id: str, config: Optional[dict[str, Any]] = None,
                    *, continuity_reset: bool = False,
                    track_id_prefix: Optional[str] = None) -> "EpisodeVisionContext":
        cfg = dict(config or {})
        token = track_id_prefix or f"human-{uuid.uuid4().hex[:10]}"
        scheduler = VisionPriorityScheduler(
            human_confidence_threshold=float(cfg.get("priority_human_confidence_threshold", .4)),
            max_inflight=3,
        )
        tracker = ClipTrackerV1(
            iou_threshold=float(cfg.get("tracker_iou_threshold", .20)),
            max_track_gap_seconds=float(cfg.get("tracker_max_gap_seconds", 2.5)),
            max_active_tracks=int(cfg.get("tracker_max_active_tracks", 16)),
            min_bbox_width=int(cfg.get("tracker_min_bbox_width", 20)),
            min_bbox_height=int(cfg.get("tracker_min_bbox_height", 20)),
            track_id_prefix=token,
        )
        return cls(
            episode_id=episode_id,
            tracker=tracker,
            priority_scheduler=scheduler,
            enrichment_policy=TrackEnrichmentPolicy(
                max_occlusion_frames=int(cfg.get("enrichment_max_occlusion_samples", 5)),
                max_requests_per_track=int(cfg.get("max_crops_per_track", 5)),
            ),
            evidence_ledger=EpisodeEvidenceLedger(
                max_entries=int(cfg.get("priority_ledger_max_entries", 256)),
            ),
            continuity_reset_pending=continuity_reset,
        )


class VisionClipPipelineV1:
    def __init__(self, config: Optional[dict[str, Any]] = None,
                 face_enricher: Optional[FaceEnricher] = None,
                 plate_enricher: Optional[PlateEnricher] = None,
                 sensitive_enricher: Optional[SensitiveObjectEnricher] = None,
                 preliminary_sink: Optional[Callable[[dict[str, Any]], None]] = None):
        cfg = dict(config or {})
        self._config = cfg
        self.max_crops = max(1, int(cfg.get("max_crops_per_track", 5)))
        self.critical_threshold = _bounded(cfg.get("critical_alert_threshold", 0.90))
        self.face_enricher = face_enricher or UnavailableFaceEnricher()
        self.plate_enricher = plate_enricher or UnavailablePlateEnricher()
        self.sensitive_enricher = sensitive_enricher or UnavailableSensitiveObjectEnricher()
        self._retain_detection_roi = bool(getattr(self.face_enricher, "requires_roi", False))
        self.preliminary_sink = preliminary_sink

    def new_episode_context(self, episode_id: str, *, continuity_reset: bool = False) -> EpisodeVisionContext:
        return EpisodeVisionContext.from_config(episode_id, self._config, continuity_reset=continuity_reset)

    def process_frames(self, clip: ClipMetadata, frames: Iterable[FrameObservation],
                       backend_diagnostic: Optional[dict[str, Any]] = None,
                       metrics: Optional[dict[str, Any]] = None,
                       evidence_ledger: Optional[EpisodeEvidenceLedger] = None,
                       priority_by_track: Optional[dict[str, PriorityDecision]] = None,
                       confirmed_tracks: Optional[set[str]] = None) -> list[dict[str, Any]]:
        tracks: dict[str, TrackState] = {}
        emitted_alerts: set[str] = set()
        events: list[dict[str, Any]] = []
        record_evidence = evidence_ledger is None
        ledger = evidence_ledger or EpisodeEvidenceLedger(
            max_entries=int(self._config.get("priority_ledger_max_entries", 256)))
        decisions = priority_by_track if priority_by_track is not None else {}
        scheduler = VisionPriorityScheduler(
            human_confidence_threshold=float(self._config.get("priority_human_confidence_threshold", .4)))
        next_sequence = len(ledger.entries) + 1
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
                decision = scheduler.classify(detection.subject_type, detection.confidence,
                                              clip.topology.topology_class,
                                              trigger_reason=clip.trigger_reason)
                previous = decisions.get(detection.track_id)
                if previous is None or _PRIORITY_RANK.get(decision.priority_hint, 4) < _PRIORITY_RANK.get(previous.priority_hint, 4):
                    decisions[detection.track_id] = decision
                if record_evidence and ledger.append(next_sequence, decision.priority_hint, decision.reason_codes,
                                                     detection.track_id, at):
                    next_sequence += 1
                if getattr(self.sensitive_enricher, "available", False):
                    sensitive = self.sensitive_enricher.inspect(detection)
                else:
                    sensitive = SensitiveObjectResult(SensitiveStatus.NOT_AVAILABLE, reason="backend_unavailable")
                state.sensitive.append(sensitive)
                if (getattr(self.sensitive_enricher, "available", False)
                        and sensitive.critical and sensitive.confidence >= self.critical_threshold):
                    if detection.track_id not in emitted_alerts:
                        alert = self._preliminary_alert(clip, state, sensitive)
                        emitted_alerts.add(detection.track_id)
                        events.append(alert)
                        if self.preliminary_sink is not None:
                            self.preliminary_sink(alert)
        if not tracks and backend_diagnostic is not None:
            empty = TrackState("clip-no-human", SubjectType.UNKNOWN, _utc(clip.started_at), _utc(clip.started_at))
            decision = scheduler.classify(SubjectType.UNKNOWN, 0.0, clip.topology.topology_class)
            events.append(self._summary(clip, empty, backend_diagnostic, metrics, decision, ledger.snapshot()))
        else:
            for state in sorted(tracks.values(), key=lambda item: item.track_id):
                enrichment_started = time.perf_counter()
                decision = decisions.get(state.track_id) or scheduler.classify(
                    state.subject_type, max(state.confidences, default=0.0), clip.topology.topology_class,
                    trigger_reason=clip.trigger_reason)
                if decision.priority_hint == VisionPriority.P1_URGENT_PRESENCE.value:
                    decision = PriorityDecision(decision.priority_hint, decision.reason_codes,
                                                "confirmed" if (state.track_id in (confirmed_tracks or set()) or
                                                                 _p1_confirmed(state.confidences, state.first_seen_at, state.last_seen_at)) else "candidate")
                events.append(self._summary(clip, state, backend_diagnostic, metrics, decision, ledger.snapshot()))
                if metrics is not None:
                    metrics["enrichment_wall_ms"] = metrics.get("enrichment_wall_ms", 0.0) + (time.perf_counter() - enrichment_started) * 1000.0
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

    def _summary(self, clip: ClipMetadata, track: TrackState,
                 backend_diagnostic: Optional[dict[str, Any]] = None,
                 metrics: Optional[dict[str, Any]] = None,
                 priority: Optional[PriorityDecision] = None,
                 priority_timeline: Optional[list[dict[str, Any]]] = None) -> dict[str, Any]:
        priority = priority or VisionPriorityScheduler().classify(
            track.subject_type, max(track.confidences, default=0.0), clip.topology.topology_class,
            trigger_reason=clip.trigger_reason)
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
            "topology_class": clip.topology.topology_class.value,
            "trigger": {"reason": clip.trigger_reason, "started_at": _utc(clip.started_at).isoformat()},
            "track": {"id": track.track_id, "subject_type": track.subject_type.value,
                      "first_seen_at": _utc(track.first_seen_at).isoformat(),
                      "last_seen_at": _utc(track.last_seen_at).isoformat(),
                      "confidence": _bounded(confidence)},
            "identity": identity.as_dict(),
            "plate": plate.as_dict(),
            "sensitive_objects": sensitive.as_dict(),
            "media": {"clip_ref": _local_ref(clip.clip_ref), "best_roi_refs": refs},
            "metrics": dict(metrics or {}),
            "priority_hint": priority.priority_hint,
            "priority_state": priority.priority_state,
            "reason_codes": list(priority.reason_codes),
            "priority_timeline": list(priority_timeline or []),
        }
        if backend_diagnostic is not None:
            payload["backend"] = dict(backend_diagnostic)
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
                      sample_period_seconds: float = 0.2,
                      episode_context: Optional[EpisodeVisionContext] = None) -> list[dict[str, Any]]:
        """Run bounded progressive observation, adaptive sampling and final summaries."""
        import cv2
        vision_started = time.perf_counter()
        capture_started = time.perf_counter()
        cap = cv2.VideoCapture(video_path)
        if not cap.isOpened():
            cap.release()
            raise RuntimeError("video cannot be opened")
        fps = float(cap.get(cv2.CAP_PROP_FPS) or 0.0)
        frames: list[FrameObservation] = []
        index = 0
        metrics = ClipProcessingMetrics()
        metrics.capture_open_wall_ms = (time.perf_counter() - capture_started) * 1000.0
        context = episode_context or EpisodeVisionContext.from_config(clip.episode_id, self._config, track_id_prefix="human")
        if context.episode_id != clip.episode_id:
            raise ValueError("episode context does not match clip episode")
        sampling = AdaptiveSamplingPolicy(
            initial_fps=float(self._config.get("sampling_initial_fps", 5.0)),
            active_fps=float(self._config.get("sampling_active_fps", 5.0)),
            stable_fps=float(self._config.get("sampling_stable_fps", 1.0)),
            quiet_fps=float(self._config.get("sampling_quiet_fps", 2.0)),
            quiet_after_clean_samples=int(self._config.get("sampling_quiet_after_clean_samples", 5)),
            lost_track_recovery_fps=float(self._config.get("sampling_lost_track_recovery_fps", 5.0)),
            minimum_detection_fps=float(self._config.get("sampling_minimum_detection_fps", 1.0)),
        )
        tracker = context.tracker
        frame_period = fps or 5.0
        batch: list[tuple[int, datetime, Any, int]] = []
        next_sample_index = 0
        enrichment_policy = context.enrichment_policy
        track_records = context.track_records
        priority_scheduler = context.priority_scheduler
        evidence_ledger = context.evidence_ledger
        priority_by_track = context.priority_by_track
        priority_queue = PriorityFrameQueue(priority_scheduler)
        observation_events: list[dict[str, Any]] = []
        observation_sequence = context.observation_sequence
        last_observation_signature = context.last_observation_signature
        last_observation_at = context.last_observation_at
        last_priority_state = context.last_priority_state

        def backend_diagnostic() -> dict[str, Any]:
            if hasattr(detector, "diagnostic"):
                return dict(detector.diagnostic())
            return {"name": "existing_detector", "model_version": "unknown", "real_model": False,
                    "status": "unavailable", "frames_sampled": metrics.frames_sampled,
                    "detections_total": sum(len(frame.detections) for frame in frames),
                    "latency_ms": 0.0, "detector_compute_sum_ms": 0.0, "non_human_ignored": 0}

        def maybe_observe(at: datetime) -> None:
            nonlocal last_observation_signature, last_observation_at, observation_sequence, last_priority_state
            diagnostic = backend_diagnostic()
            active_ids = tracker.active_track_ids
            if not active_ids and diagnostic.get("status") == "ok":
                return
            active_decisions = [priority_by_track[track_id] for track_id in active_ids if track_id in priority_by_track]
            if active_decisions:
                decision = min(active_decisions, key=lambda item: _PRIORITY_RANK.get(item.priority_hint, 4))
            else:
                decision = PriorityDecision(VisionPriority.P4_LOW_PRIORITY_CONTEXT.value, ("backend_error",))
            if decision.priority_hint == VisionPriority.P1_URGENT_PRESENCE.value and active_ids:
                confirmed = any(_p1_confirmed(track_records[track_id].get("scores", []),
                                              track_records[track_id]["first_at"], track_records[track_id]["last_at"])
                                for track_id in active_ids if track_id in track_records)
                decision = PriorityDecision(decision.priority_hint, decision.reason_codes,
                                            "confirmed" if confirmed else "candidate")
            tracks = []
            for track_id in active_ids:
                record = track_records.get(track_id)
                if not record:
                    continue
                tracks.append({
                    "track_id": track_id,
                    "subject_type": "human",
                    "confidence": record["score_sum"] / max(1, record["detection_count"]),
                    "state": enrichment_policy.state(track_id).value,
                    "detection_count": record["detection_count"],
                })
            payload = {
                "schema_version": "synora.vision.clip-observation/v1",
                "clip_id": clip.clip_id, "episode_id": clip.episode_id, "camera_id": clip.camera_id,
                "node_id": clip.topology.node_id, "zone": clip.topology.zone,
                "topology_class": clip.topology.topology_class.value,
                "trigger": clip.trigger_reason, "observed_at": _utc(at).isoformat(),
                "sequence": observation_sequence + 1, "tracks": tracks,
                "backend": {"status": diagnostic.get("status", "unavailable"), "real_model": bool(diagnostic.get("real_model", False))},
                "priority_hint": decision.priority_hint,
                "priority_state": decision.priority_state,
                "reason_codes": list(decision.reason_codes),
            }
            signature = repr(([(item["track_id"], item["state"]) for item in tracks],
                              decision.priority_hint, decision.priority_state, decision.reason_codes,
                              payload["topology_class"], payload["backend"].get("status")))
            if signature == last_observation_signature:
                return
            heartbeat_window = 2.0 if decision.priority_hint == VisionPriority.P1_URGENT_PRESENCE.value else .5
            if last_observation_at is not None and decision.priority_state == last_priority_state and (_utc(at) - last_observation_at).total_seconds() < heartbeat_window:
                return
            observation_sequence += 1
            payload["sequence"] = observation_sequence
            observation_events.append({"type": "synora.vision.clip-observation/v1", "payload": payload})
            last_observation_signature = signature
            last_observation_at = _utc(at)
            last_priority_state = decision.priority_state
            primary_track = active_ids[0] if active_ids else "clip-backend"
            evidence_ledger.append(observation_sequence, decision.priority_hint, decision.reason_codes,
                                   primary_track, at)
            payload["priority_timeline"] = evidence_ledger.snapshot()
            if metrics.first_observation_wall_ms == 0.0:
                metrics.first_observation_wall_ms = (time.perf_counter() - vision_started) * 1000.0

        def process_batch() -> None:
            nonlocal next_sample_index
            if not batch:
                return
            metrics.peak_frames_in_flight = max(metrics.peak_frames_in_flight, len(batch))
            detector_started = time.perf_counter()
            if hasattr(detector, "detect_many"):
                raw_batches = detector.detect_many(
                    [item[2] for item in batch], [item[3] for item in batch]
                )
            else:
                raw_batches = [
                    detector.detect(item[2], item[3]) if hasattr(detector, "diagnostic")
                    else detector.detect(item[2])
                    for item in batch
                ]
            metrics.detector_wall_ms += (time.perf_counter() - detector_started) * 1000.0
            if metrics.first_detector_batch_wall_ms == 0.0:
                metrics.first_detector_batch_wall_ms = (time.perf_counter() - detector_started) * 1000.0
            for (sample_index, at, frame, _), raw_detections in zip(batch, raw_batches):
                raw_detections = list(raw_detections or [])
                raw_decisions = []
                for raw in raw_detections:
                    if isinstance(raw, dict):
                        raw_decisions.append(priority_scheduler.classify(
                            SubjectType.HUMAN, raw.get("confidence", raw.get("score", 0.0)),
                            clip.topology.topology_class, trigger_reason=clip.trigger_reason))
                raw_priority = min(raw_decisions, key=lambda item: _PRIORITY_RANK.get(item.priority_hint, 4)).priority_hint if raw_decisions else VisionPriority.P4_LOW_PRIORITY_CONTEXT.value
                pending = _PendingPriorityFrame(sample_index, at, frame, raw_detections, raw_priority)
                if not priority_queue.enqueue(pending, raw_priority):
                    metrics.frames_skipped_by_policy += 1
            for pending in priority_queue.drain_temporal():
                sample_index, at, frame, raw_detections = pending.frame_index, pending.at, pending.frame, pending.raw_detections
                detections: list[Detection] = []
                tracking_started = time.perf_counter()
                previous_active = set(tracker.active_track_ids)
                for assigned in tracker.update(list(raw_detections), at):
                    x1, y1, x2, y2 = assigned["bbox"]
                    roi = frame[max(0, y1):max(0, y2), max(0, x1):max(0, x2)] if self._retain_detection_roi else None
                    ref = f"local://clips/{clip.clip_id}/roi/{sample_index}-{len(detections)}"
                    track_id = assigned["track_id"]
                    record = track_records.setdefault(track_id, {"detection_count": 0, "score_sum": 0.0, "scores": [], "first_at": at, "last_at": at})
                    record["detection_count"] += 1
                    record["score_sum"] += float(assigned["score"])
                    record["scores"].append(float(assigned["score"]))
                    record["first_at"] = min(record["first_at"], at)
                    record["last_at"] = max(record["last_at"], at)
                    enrichment_policy.observe(track_id, at)
                    decision = priority_scheduler.classify(
                        SubjectType.HUMAN, assigned["score"], clip.topology.topology_class,
                        trigger_reason=clip.trigger_reason,
                        track_state=enrichment_policy.state(track_id))
                    previous_decision = priority_by_track.get(track_id)
                    if previous_decision is None or _PRIORITY_RANK.get(decision.priority_hint, 4) < _PRIORITY_RANK.get(previous_decision.priority_hint, 4):
                        priority_by_track[track_id] = decision
                    record["decision"] = priority_by_track[track_id]
                    if enrichment_policy.state(track_id) == TrackEnrichmentState.CANDIDATE:
                        occupied = any(_PRIORITY_RANK.get(item.priority_hint, 4) <= 2 for item in priority_by_track.values())
                        if decision.priority_hint == VisionPriority.P4_LOW_PRIORITY_CONTEXT.value or (decision.priority_hint == VisionPriority.P3_BEST_EFFORT_ENRICHMENT.value and occupied):
                            metrics.enrichments_avoided += 1
                        elif enrichment_policy.begin_enrichment(track_id):
                            metrics.enrichments_executed += 1
                    detections.append(Detection(assigned["track_id"], SubjectType.HUMAN,
                                                assigned["score"], ref, roi))
                current_active = set(tracker.active_track_ids)
                for track_id in previous_active - current_active:
                    enrichment_policy.observe(track_id, at, visible=False)
                metrics.tracking_wall_ms += (time.perf_counter() - tracking_started) * 1000.0
                if metrics.first_tracking_wall_ms == 0.0:
                    metrics.first_tracking_wall_ms = (time.perf_counter() - tracking_started) * 1000.0
                metrics.frames_sampled += 1
                frame_decisions = [priority_by_track[item.track_id] for item in detections if item.track_id in priority_by_track]
                frame_decision = min(frame_decisions, key=lambda item: _PRIORITY_RANK.get(item.priority_hint, 4)) if frame_decisions else PriorityDecision(VisionPriority.P4_LOW_PRIORITY_CONTEXT.value, ("no_human_detected",))
                metrics.frames_by_priority[frame_decision.priority_hint] = metrics.frames_by_priority.get(frame_decision.priority_hint, 0) + 1
                sampling.observe(has_human=bool(detections), all_stable=bool(detections) and all(
                    enrichment_policy.state(item.track_id) == TrackEnrichmentState.RECOGNIZED_STABLE for item in detections),
                    recently_lost=bool(previous_active - current_active))
                maybe_observe(at)
                frames.append(FrameObservation.from_values(at, detections))
            batch.clear()

        try:
            while True:
                read_started = time.perf_counter()
                ok, frame = cap.read()
                decode_wall_ms = (time.perf_counter() - read_started) * 1000.0
                metrics.clip_decode_wall_ms += decode_wall_ms
                if not ok:
                    break
                metrics.frames_read += 1
                if metrics.first_frame_decode_wall_ms == 0.0:
                    metrics.first_frame_decode_wall_ms = decode_wall_ms
                if index >= next_sample_index:
                    at = clip.started_at + timedelta(seconds=index / frame_period)
                    if at > clip.ends_at:
                        break
                    timestamp_ms = int(round((at - clip.started_at).total_seconds() * 1000.0))
                    batch.append((index, at, frame, timestamp_ms))
                    sample_stride = max(1, int(round(frame_period / sampling.fps())))
                    next_sample_index = index + sample_stride
                    if len(batch) >= (3 if hasattr(detector, "detect_many") else 1):
                        process_batch()
                else:
                    metrics.frames_skipped_by_policy += 1
                index += 1
            process_batch()
        finally:
            cap.release()
        diagnostic = backend_diagnostic()
        metrics.detector_compute_sum_ms = float(diagnostic.get("detector_compute_sum_ms", diagnostic.get("latency_ms", 0.0)) or 0.0)
        metrics.sampling_state_transitions = sampling.transitions
        metrics.enrichment = enrichment_policy.counters()
        metric_values = metrics.as_dict()
        summary_started = time.perf_counter()
        summary_events = self.process_frames(clip, frames, diagnostic, metric_values,
                                             evidence_ledger=evidence_ledger,
                                             priority_by_track=priority_by_track,
                                             confirmed_tracks={track_id for track_id, record in track_records.items()
                                                              if _p1_confirmed(record.get("scores", []), record["first_at"], record["last_at"])} )
        metrics.summary_wall_ms = (time.perf_counter() - summary_started) * 1000.0
        metrics.enrichment_wall_ms = float(metric_values.get("enrichment_wall_ms", 0.0) or 0.0)
        metrics.vision_wall_latency_ms = (time.perf_counter() - vision_started) * 1000.0
        metric_values = metrics.as_dict()
        metrics.priority_evictions = priority_scheduler.priority_evictions
        metrics.priority_starvation = priority_scheduler.priority_starvation
        metric_values = metrics.as_dict()
        for event in summary_events:
            event["payload"]["metrics"] = metric_values

        output_events = observation_events + summary_events
        if context.continuity_reset_pending:
            for event in output_events:
                payload = event.get("payload", {})
                reasons = list(payload.get("reason_codes", []))
                if "continuity_reset" not in reasons:
                    reasons.append("continuity_reset")
                payload["reason_codes"] = reasons
            context.continuity_reset_pending = False
        context.observation_sequence = observation_sequence
        context.last_observation_signature = last_observation_signature
        context.last_observation_at = last_observation_at
        context.last_priority_state = last_priority_state
        context.last_seen = clip.ends_at
        return output_events

    @staticmethod
    def _iou(left: tuple[int, int, int, int], right: tuple[int, int, int, int]) -> float:
        return _bbox_iou(left, right)


def make_embedding_ref(track_id: str, crop_index: int) -> str:
    digest = hashlib.sha256(f"{track_id}:{crop_index}".encode()).hexdigest()[:16]
    return f"local://vision/embeddings/{digest}"
