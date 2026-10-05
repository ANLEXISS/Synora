"""Local-only facial evidence aggregation for the V3 boundary.

The component accepts backend observations inside the Vision process and emits
only a bounded aggregate.  Names, resident identifiers, embeddings, crops and
images are deliberately not representable in the public result.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
import math
from typing import Any, Iterable, Optional


FACE_STATUSES = {"not_requested", "unavailable", "low_quality", "uncertain", "recognized", "unknown"}
QUALIFICATION_PROVENANCE = {"not_qualified", "controlled_test", "labeled_consent_manifest"}


def _bounded(value: Any) -> float:
    try:
        number = float(value)
    except (TypeError, ValueError, OverflowError):
        return 0.0
    return number if math.isfinite(number) else 0.0


def _utc(value: Optional[datetime]) -> datetime:
    if value is None:
        return datetime.now(timezone.utc)
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


@dataclass(frozen=True)
class FaceObservationV3:
    status: str
    quality: float = 0.0
    confidence: float = 0.0
    expires_at: Optional[datetime] = None
    provenance: str = "not_qualified"


@dataclass(frozen=True)
class FaceAggregateV3:
    status: str = "unavailable"
    consensus_frames: int = 0
    quality: float = 0.0
    confidence: float = 0.0
    expires_at: Optional[datetime] = None
    qualification_provenance: str = "not_qualified"

    def as_bus_payload(self) -> dict[str, Any]:
        """Return the only facial shape allowed beyond this local component."""
        return {
            "status": self.status,
            "consensus_frames": max(0, int(self.consensus_frames)),
            "quality": round(max(0.0, min(1.0, self.quality)), 4),
            "confidence": round(max(0.0, min(1.0, self.confidence)), 4),
            "expires_at": _utc(self.expires_at).isoformat() if self.expires_at else None,
            "qualification_provenance": self.qualification_provenance,
        }


class FaceConsensusV3:
    """Conservatively aggregate bounded status observations, never identity."""

    def __init__(self, *, minimum_consensus: int = 2, ttl_seconds: float = 10.0):
        self.minimum_consensus = max(1, int(minimum_consensus))
        self.ttl = timedelta(seconds=max(0.1, float(ttl_seconds)))

    def aggregate(self, observations: Iterable[FaceObservationV3], *, now: Optional[datetime] = None) -> FaceAggregateV3:
        current = _utc(now)
        clean: list[FaceObservationV3] = []
        for item in observations:
            status = str(item.status or "").strip().lower()
            if status not in FACE_STATUSES:
                status = "uncertain"
            provenance = str(item.provenance or "not_qualified").strip().lower()
            if provenance not in QUALIFICATION_PROVENANCE:
                provenance = "not_qualified"
            expires_at = _utc(item.expires_at) if item.expires_at else current + self.ttl
            if expires_at < current:
                continue
            clean.append(FaceObservationV3(status, max(0.0, min(1.0, _bounded(item.quality))), max(0.0, min(1.0, _bounded(item.confidence))), expires_at, provenance))
        if not clean:
            return FaceAggregateV3(status="unavailable", qualification_provenance="not_qualified")
        statuses = {item.status for item in clean}
        if "unavailable" in statuses and len(statuses) == 1:
            status = "unavailable"
        elif "recognized" in statuses and statuses <= {"recognized"} and len(clean) >= self.minimum_consensus:
            status = "recognized"
        elif statuses <= {"unknown"} and len(clean) >= self.minimum_consensus:
            status = "unknown"
        elif any(item.status == "low_quality" for item in clean) and not {"recognized", "unknown"} & statuses:
            status = "low_quality"
        else:
            status = "uncertain"
        provenance = "labeled_consent_manifest" if all(item.provenance == "labeled_consent_manifest" for item in clean) else "not_qualified"
        return FaceAggregateV3(
            status=status,
            consensus_frames=len(clean),
            quality=sum(item.quality for item in clean) / len(clean),
            confidence=sum(item.confidence for item in clean) / len(clean),
            expires_at=min(item.expires_at for item in clean if item.expires_at),
            qualification_provenance=provenance,
        )
