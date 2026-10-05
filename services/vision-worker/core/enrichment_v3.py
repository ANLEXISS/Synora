"""V3 aggregate adapters for the signals that do not fit the immutable V2 vector.

The adapter accepts only confirmed, priority human ROIs and emits semantic
states.  It never forwards media, boxes, crops, embeddings or local tracking
keys beyond this process.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Mapping, Optional

from .enrichment_v2 import ConfirmedHumanROI, PoseEnricherV2, RiskObjectEnricherV2, _utc


POSE_STATES = {"unavailable", "not_requested", "low_quality", "available"}
POSTURES = {"unknown", "upright", "seated", "ground"}
FALL_STATES = {"none", "candidate", "unknown"}
RISK_STATES = {"not_available", "not_requested", "uncertain", "suspected", "confirmed"}
PERSISTENCE_STATES = {"none", "isolated", "repeated", "persistent", "confirmed"}


@dataclass(frozen=True)
class PoseAggregateV3:
    status: str = "unavailable"
    quality: float = 0.0
    posture: str = "unknown"
    transition_to_ground: bool = False
    immobility_seconds: float = 0.0
    recovery_observed: bool = False
    observation_count: int = 0
    sampled: bool = False


@dataclass(frozen=True)
class RiskAggregateV3:
    status: str = "not_available"
    kind: str = "none"
    confidence: float = 0.0
    persistence: str = "none"
    persistence_seconds: float = 0.0
    observation_count: int = 0
    quality_sufficient: bool = False


@dataclass
class _RiskTrack:
    first_at: Optional[datetime] = None
    last_at: Optional[datetime] = None
    observations: int = 0
    last_status: str = "not_available"


class RiskPersistenceV3:
    """Persistence is temporal evidence, never a reason to upgrade a score."""

    def __init__(self, repeated_count: int = 2, persistent_seconds: float = 3.0):
        self.repeated_count = max(2, int(repeated_count))
        self.persistent_seconds = max(0.5, float(persistent_seconds))
        self._tracks: dict[str, _RiskTrack] = {}

    def observe(self, key: str, at: datetime, risk: RiskAggregateV3) -> RiskAggregateV3:
        now = _utc(at)
        state = self._tracks.setdefault(key, _RiskTrack())
        qualifying = risk.status in {"suspected", "confirmed"}
        if not qualifying:
            return risk
        if state.first_at is None or (state.last_status not in {"suspected", "confirmed"}):
            state.first_at = now
            state.observations = 0
        state.observations += 1
        state.last_at = now
        state.last_status = risk.status
        duration = max(0.0, (now - state.first_at).total_seconds())
        if risk.status == "confirmed":
            persistence = "confirmed"
        elif state.observations >= self.repeated_count and duration >= self.persistent_seconds:
            persistence = "persistent"
        elif state.observations >= self.repeated_count:
            persistence = "repeated"
        else:
            persistence = "isolated"
        return RiskAggregateV3(risk.status, risk.kind, risk.confidence, persistence, duration, state.observations, risk.quality_sufficient)

    def expire(self, key: str) -> None:
        self._tracks.pop(key, None)


class FallLogicV3:
    """A fall requires upright/seated -> ground, quality, and no recovery."""

    def __init__(self, minimum_quality: float = 0.60, confirmation_seconds: float = 3.0):
        self.minimum_quality = max(0.0, min(1.0, float(minimum_quality)))
        self.confirmation_seconds = max(0.5, float(confirmation_seconds))
        self._tracks: dict[str, dict[str, Any]] = {}

    def observe(self, key: str, at: datetime, pose: PoseAggregateV3) -> str:
        now = _utc(at)
        state = self._tracks.setdefault(key, {"upright": False, "transition_at": None, "recovered": False, "last": "unknown"})
        if pose.posture in {"upright", "seated"}:
            state["upright"] = True
        transition = pose.transition_to_ground or (state["last"] in {"upright", "seated"} and pose.posture == "ground")
        if transition and state["upright"] and pose.status == "available" and pose.quality >= self.minimum_quality:
            state["transition_at"] = now
            state["recovered"] = False
        if state["transition_at"] and pose.posture in {"upright", "seated"} and now > state["transition_at"]:
            state["recovered"] = True
        state["last"] = pose.posture
        if not state["transition_at"] or state["recovered"] or pose.posture != "ground" or pose.status != "available" or pose.quality < self.minimum_quality:
            return "none"
        # This task only emits a candidate.  Confirmation is intentionally not
        # representable until an annotated fall corpus and independent
        # qualification exist.
        return "candidate"

    def expire(self, key: str) -> None:
        self._tracks.pop(key, None)


@dataclass
class AggregatedVisionV3:
    human_confirmed: bool = False
    pose: PoseAggregateV3 = field(default_factory=PoseAggregateV3)
    risk: RiskAggregateV3 = field(default_factory=RiskAggregateV3)
    fall_state: str = "unknown"
    real_detection: bool = False
    replay_simulation: bool = False
    central_retracking_invocations: int = 0
    pose_model_status: str = "unavailable"
    pose_calls: int = 0
    pose_latency_ms: float = 0.0

    def as_bus_payload(self) -> dict[str, Any]:
        return {
            "schema_version": "synora.vision.enrichment/v3",
            "human_confirmed": self.human_confirmed,
            "pose": {"status": self.pose.status, "quality": round(self.pose.quality, 4), "posture": self.pose.posture, "immobility_seconds": round(min(120.0, max(0.0, self.pose.immobility_seconds)), 3), "recovery_observed": self.pose.recovery_observed, "observation_count": self.pose.observation_count, "sampled": self.pose.sampled},
            "fall": {"state": self.fall_state},
            "risk_object": {"status": self.risk.status, "kind": self.risk.kind, "confidence": round(self.risk.confidence, 4), "persistence": self.risk.persistence, "persistence_seconds": round(self.risk.persistence_seconds, 3), "observation_count": self.risk.observation_count, "quality_sufficient": self.risk.quality_sufficient},
            "real_detection": self.real_detection,
            "replay_simulation": self.replay_simulation,
            "central_retracking_invocations": 0,
            "pose_model": {"status": self.pose_model_status, "calls": self.pose_calls, "latency_ms": round(self.pose_latency_ms, 3)},
        }


class VisionEnrichmentPipelineV3:
    def __init__(self, pose: Optional[PoseEnricherV2] = None, risk: Optional[RiskObjectEnricherV2] = None, fall: Optional[FallLogicV3] = None, persistence: Optional[RiskPersistenceV3] = None, max_rois_per_cycle: int = 4):
        self.pose_backend = pose
        self.risk_backend = risk
        self.fall = fall or FallLogicV3()
        self.persistence = persistence or RiskPersistenceV3()
        self.max_rois_per_cycle = max(1, int(max_rois_per_cycle))

    def _pose(self, roi: ConfirmedHumanROI) -> PoseAggregateV3:
        if not roi.confirmed:
            return PoseAggregateV3(status="not_requested")
        if roi.priority not in {"P1_urgent_presence", "P2_contextual_enrichment"} or roi.topology not in {"protected_interior", "restricted_threshold", "private_perimeter"}:
            return PoseAggregateV3(status="not_requested")
        if self.pose_backend is None:
            return PoseAggregateV3(status="unavailable")
        value = self.pose_backend.enrich(roi)
        quality = max(0.0, min(1.0, value.quality))
        status = "unavailable" if value.status == "not_available" else "not_requested" if value.status == "not_requested" else "available" if quality >= 0.60 else "low_quality"
        posture = {"standing": "upright", "upright": "upright", "sitting": "seated", "seated": "seated", "lying": "ground", "ground": "ground"}.get(value.posture, "unknown")
        immobility_seconds = max(0.0, float(value.immobility_seconds))
        return PoseAggregateV3(status, quality, posture, value.transition_to_ground, immobility_seconds, value.recovery_observed, 1, value.status == "available")

    def _risk(self, roi: ConfirmedHumanROI) -> RiskAggregateV3:
        if not roi.confirmed:
            return RiskAggregateV3(status="not_requested")
        if roi.priority not in {"P1_urgent_presence", "P2_contextual_enrichment"} or roi.topology not in {"protected_interior", "restricted_threshold", "private_perimeter"}:
            return RiskAggregateV3(status="not_requested")
        if self.risk_backend is None:
            return RiskAggregateV3(status="not_available")
        value = self.risk_backend.enrich(roi)
        return RiskAggregateV3(value.status if value.status in RISK_STATES else "not_available", value.kind, value.confidence, "none", 0.0, 0, value.status in {"suspected", "confirmed"})

    def process(self, rois: list[ConfirmedHumanROI], *, real_detection: bool = False, replay_simulation: bool = False) -> AggregatedVisionV3:
        result = AggregatedVisionV3(real_detection=real_detection, replay_simulation=replay_simulation)
        confirmed = sorted((roi for roi in rois if roi.confirmed), key=lambda roi: _utc(roi.observed_at))[: self.max_rois_per_cycle]
        if not confirmed:
            result.pose = PoseAggregateV3(status="not_requested")
            result.risk = RiskAggregateV3(status="not_requested")
            result.fall_state = "unknown"
            return result
        result.human_confirmed = True
        result.pose_model_status = str(getattr(self.pose_backend, "model_status", "available" if self.pose_backend is not None else "unavailable"))
        for roi in confirmed:
            result.pose = self._pose(roi)
            result.risk = self.persistence.observe(roi.local_track_key, roi.observed_at, self._risk(roi))
            state = self.fall.observe(roi.local_track_key, roi.observed_at, result.pose)
            if {"none": 0, "candidate": 1}.get(state, 0) >= {"none": 0, "candidate": 1}.get(result.fall_state, -1):
                result.fall_state = state
        result.pose_calls = int(getattr(self.pose_backend, "calls", 0))
        result.pose_latency_ms = float(getattr(self.pose_backend, "latency_ms", 0.0))
        return result
