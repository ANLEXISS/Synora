"""Shared low-level ROI adapters used by the V3 candidate.

The executor boundary is intentionally semantic.  The local track key is
accepted only inside this process and is never present in ``as_bus_payload``.
No default path invents a pose, risk score or fall confidence when a model is
missing.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import math
import os
import time
from typing import Any, Callable, Mapping, Optional


POSE_NOT_AVAILABLE = "not_available"
POSE_NOT_REQUESTED = "not_requested"
POSE_AVAILABLE = "available"
RISK_STATUSES = {"not_available", "not_requested", "uncertain", "suspected", "confirmed"}
POSTURES = {"unknown", "standing", "sitting", "lying"}


def _utc(value: Optional[datetime]) -> datetime:
    if value is None:
        return datetime.now(timezone.utc)
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _bounded(value: Any, fallback: float = 0.0) -> float:
    try:
        number = float(value)
    except (TypeError, ValueError, OverflowError):
        return fallback
    return number if math.isfinite(number) else fallback


def _safe_status(value: Any, allowed: set[str], fallback: str) -> str:
    value = str(value or "").strip().lower()
    return value if value in allowed else fallback


@dataclass(frozen=True)
class ConfirmedHumanROI:
    """Process-local input supplied by Edge/Discovery, not a bus contract."""

    local_track_key: str
    confirmed: bool
    topology: str
    priority: str
    observed_at: datetime
    pose_sample: Optional[Mapping[str, Any]] = None
    risk_sample: Optional[Mapping[str, Any]] = None


@dataclass(frozen=True)
class PoseAggregateV2:
    status: str = POSE_NOT_AVAILABLE
    quality: float = 0.0
    posture: str = "unknown"
    transition_to_ground: bool = False
    immobility_seconds: float = 0.0
    recovery_observed: bool = False

    def as_dict(self) -> dict[str, Any]:
        return {
            "status": self.status,
            "quality": round(self.quality, 4),
            "posture": self.posture,
            "transition_to_ground": self.transition_to_ground,
            "immobility_seconds": round(max(0.0, self.immobility_seconds), 3),
            "recovery_observed": self.recovery_observed,
        }


@dataclass(frozen=True)
class RiskAggregateV2:
    status: str = "not_available"
    kind: str = "none"
    confidence: float = 0.0

    def as_dict(self) -> dict[str, Any]:
        return {"status": self.status, "kind": self.kind, "confidence": round(self.confidence, 4)}


class PoseEnricherV2:
    """Bounded pose adapter.  A missing RKNN model yields not_available."""

    def __init__(self, model_path: Optional[str] = None, executor: Optional[Callable[[Mapping[str, Any]], Mapping[str, Any]]] = None, max_rate_hz: float = 5.0):
        self.model_path = str(model_path or os.getenv("SYNORA_POSE_MODEL", ""))
        self.executor = executor
        self.minimum_interval_seconds = 1.0 / max(0.1, float(max_rate_hz))
        self.calls = 0
        self.latency_ms = 0.0
        self._last_call_at: dict[str, datetime] = {}
        self._last_result: dict[str, PoseAggregateV2] = {}

    @property
    def available(self) -> bool:
        return bool(self.executor) or bool(self.model_path and os.path.isfile(self.model_path))

    def enrich(self, roi: ConfirmedHumanROI) -> PoseAggregateV2:
        if not roi.confirmed:
            return PoseAggregateV2(status=POSE_NOT_REQUESTED)
        if roi.priority not in {"P1_urgent_presence", "P2_contextual_enrichment"} or roi.topology not in {"protected_interior", "restricted_threshold", "private_perimeter"}:
            return PoseAggregateV2(status=POSE_NOT_REQUESTED)
        if self.executor is None:
            return PoseAggregateV2(status=POSE_NOT_AVAILABLE)
        now = _utc(roi.observed_at)
        previous_at = self._last_call_at.get(roi.local_track_key)
        if previous_at is not None and (now - previous_at).total_seconds() < self.minimum_interval_seconds:
            return self._last_result.get(roi.local_track_key, PoseAggregateV2(status=POSE_NOT_REQUESTED))
        started = time.perf_counter()
        self.calls += 1
        self._last_call_at[roi.local_track_key] = now
        try:
            raw = self.executor(roi.pose_sample or {}) or {}
        except Exception:
            # A backend error is different from a low-quality pose.  Keep the
            # absence explicit so the cognitive layer cannot turn an
            # unavailable model into an invented ``available`` observation.
            result = PoseAggregateV2(status=POSE_NOT_AVAILABLE)
            self._last_result[roi.local_track_key] = result
            return result
        finally:
            self.latency_ms += (time.perf_counter() - started) * 1000.0
        result = PoseAggregateV2(
            status=POSE_AVAILABLE,
            quality=max(0.0, min(1.0, _bounded(raw.get("quality")))),
            posture=_safe_status(raw.get("posture"), POSTURES, "unknown"),
            transition_to_ground=bool(raw.get("transition_to_ground", False)),
            immobility_seconds=max(0.0, _bounded(raw.get("immobility_seconds"))),
            recovery_observed=bool(raw.get("recovery_observed", False)),
        )
        self._last_result[roi.local_track_key] = result
        return result


class RiskObjectEnricherV2:
    """Optional risk adapter; it cannot fabricate confidence without a model."""

    def __init__(self, model_path: Optional[str] = None, executor: Optional[Callable[[Mapping[str, Any]], Mapping[str, Any]]] = None):
        self.model_path = str(model_path or os.getenv("SYNORA_RISK_MODEL", ""))
        self.executor = executor
        self.calls = 0
        self.latency_ms = 0.0

    def enrich(self, roi: ConfirmedHumanROI) -> RiskAggregateV2:
        if not roi.confirmed:
            return RiskAggregateV2(status="not_requested")
        if roi.priority not in {"P1_urgent_presence", "P2_contextual_enrichment"} or roi.topology not in {"protected_interior", "restricted_threshold", "private_perimeter"}:
            return RiskAggregateV2(status="not_requested")
        if self.executor is None:
            return RiskAggregateV2(status="not_available")
        started = time.perf_counter()
        self.calls += 1
        try:
            raw = self.executor(roi.risk_sample or {}) or {}
        except Exception:
            # Do not downgrade an execution failure to ``uncertain``.  That
            # state is reserved for a successful, deliberately inconclusive
            # model observation.
            return RiskAggregateV2(status="not_available")
        finally:
            self.latency_ms += (time.perf_counter() - started) * 1000.0
        status = _safe_status(raw.get("status"), RISK_STATUSES, "uncertain")
        kind = str(raw.get("kind", "unknown") or "unknown").strip().lower()
        if kind not in {"none", "firearm", "other", "unknown"}:
            kind = "unknown"
        return RiskAggregateV2(status=status, kind=kind, confidence=max(0.0, min(1.0, _bounded(raw.get("confidence")))))
