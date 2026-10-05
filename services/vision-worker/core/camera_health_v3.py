"""Bounded camera and ingress health facts for the V3 aggregate boundary."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any


HEALTH_STATUSES = {"unknown", "healthy", "degraded", "offline", "uncertain"}
INTEGRITY_STATUSES = HEALTH_STATUSES


@dataclass(frozen=True)
class CameraHealthAggregateV3:
    status: str = "unknown"
    integrity_status: str = "unknown"
    uncertainty: bool = True
    reason: str = "not_requested"
    dropped_segments: int = 0
    clock_skew_seconds: float = 0.0
    duplicate_ignored: bool = False

    def as_bus_payload(self) -> dict[str, Any]:
        status = self.status if self.status in HEALTH_STATUSES else "unknown"
        integrity = self.integrity_status if self.integrity_status in INTEGRITY_STATUSES else "unknown"
        return {
            "status": status,
            "integrity_status": integrity,
            "uncertainty": bool(self.uncertainty or status != "healthy" or integrity != "healthy"),
            "reason": str(self.reason or "unknown")[:64],
            "dropped_segments": max(0, int(self.dropped_segments)),
            "clock_skew_seconds": max(0.0, min(300.0, float(self.clock_skew_seconds))),
            "duplicate_ignored": bool(self.duplicate_ignored),
        }


def aggregate_camera_health(*, online: bool, frozen: bool = False, masked: bool = False,
                            clock_skew_seconds: float = 0.0, dropped_segments: int = 0,
                            manifest_valid: bool = True, duplicate: bool = False,
                            quota_exceeded: bool = False) -> CameraHealthAggregateV3:
    if not manifest_valid:
        return CameraHealthAggregateV3("uncertain", "uncertain", True, "manifest_invalid", dropped_segments, clock_skew_seconds, duplicate)
    if quota_exceeded:
        return CameraHealthAggregateV3("degraded", "degraded", True, "quota_exceeded", dropped_segments, clock_skew_seconds, duplicate)
    if not online:
        return CameraHealthAggregateV3("offline", "uncertain", True, "camera_offline", dropped_segments, clock_skew_seconds, duplicate)
    if frozen or masked:
        return CameraHealthAggregateV3("degraded", "degraded", True, "stream_integrity_uncertain", dropped_segments, clock_skew_seconds, duplicate)
    if abs(float(clock_skew_seconds)) > 2 or dropped_segments > 0:
        return CameraHealthAggregateV3("degraded", "degraded", True, "ingress_integrity_degraded", dropped_segments, clock_skew_seconds, duplicate)
    return CameraHealthAggregateV3("healthy", "healthy", False, "ok", 0, 0.0, duplicate)
