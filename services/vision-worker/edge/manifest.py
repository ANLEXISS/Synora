"""Versioned, privacy-safe manifest emitted by the camera/edge replay.

The manifest is deliberately semantic.  Pixel coordinates, crops, embeddings,
raw media paths and process-local track identifiers are rejected recursively.
"""

from __future__ import annotations

from dataclasses import dataclass, asdict
import hashlib
import json
import re
from typing import Any, Mapping


SCHEMA = "synora.vision.edge-track-manifest/v1"
TRACKING_STATUSES = {"ok", "unavailable", "invalid", "contradictory"}
TRIGGER_CLASSES = {"human", "vehicle", "animal", "unknown"}
FORBIDDEN_TOKENS = {
    "bbox", "bboxes", "crop", "crops", "embedding", "embeddings", "identity",
    "hardware_id", "serial", "mac", "local_track_id", "raw_media", "frame", "frames",
}
_OPAQUE_REF = re.compile(r"^evidence://[a-f0-9]{32}$")


def _walk_forbidden(value: Any, path: str = "") -> str | None:
    if isinstance(value, Mapping):
        for key, child in value.items():
            normalized = str(key).lower()
            if normalized in FORBIDDEN_TOKENS:
                return f"forbidden field at {path}/{key}"
            found = _walk_forbidden(child, f"{path}/{key}")
            if found:
                return found
    elif isinstance(value, (list, tuple)):
        for index, child in enumerate(value):
            found = _walk_forbidden(child, f"{path}/{index}")
            if found:
                return found
    return None


def opaque_evidence_ref(episode_id: str, ordinal: int, kind: str) -> str:
    """Return a deterministic opaque reference, never a filesystem path."""
    material = f"synora-edge-v1|{episode_id}|{int(ordinal)}|{kind}".encode()
    return f"evidence://{hashlib.sha256(material).hexdigest()[:32]}"


@dataclass(frozen=True)
class EdgeTrackManifest:
    schema_version: str
    camera_id: str
    episode_id: str
    topology_class: str
    trigger_class: str
    trigger_confidence: float
    tracking_status: str
    started_at: str
    ended_at: str
    track_count: int
    confirmed_track_count: int
    observation_count: int
    segment_count: int
    gap_count: int
    evidence_refs: tuple[str, ...]
    priority_reason: tuple[str, ...]
    edge_emulated: bool
    metrics: Mapping[str, Any]

    def as_dict(self) -> dict[str, Any]:
        value = asdict(self)
        value["evidence_refs"] = list(self.evidence_refs)
        value["priority_reason"] = list(self.priority_reason)
        value["metrics"] = dict(self.metrics)
        validate_manifest(value)
        return value

    def json_bytes(self) -> bytes:
        return (json.dumps(self.as_dict(), sort_keys=True, separators=(",", ":")) + "\n").encode()


def validate_manifest(value: Mapping[str, Any]) -> None:
    if not isinstance(value, Mapping):
        raise ValueError("edge manifest must be an object")
    forbidden = _walk_forbidden(value)
    if forbidden:
        raise ValueError(forbidden)
    required = {
        "schema_version", "camera_id", "episode_id", "topology_class", "trigger_class",
        "trigger_confidence", "tracking_status", "started_at", "ended_at", "track_count",
        "confirmed_track_count", "observation_count", "segment_count", "gap_count",
        "evidence_refs", "priority_reason", "edge_emulated", "metrics",
    }
    missing = sorted(required - set(value))
    if missing:
        raise ValueError(f"missing manifest fields: {','.join(missing)}")
    if value["schema_version"] != SCHEMA:
        raise ValueError("unsupported edge manifest schema")
    if value["trigger_class"] not in TRIGGER_CLASSES:
        raise ValueError("invalid trigger class")
    if value["tracking_status"] not in TRACKING_STATUSES:
        raise ValueError("invalid tracking status")
    confidence = float(value["trigger_confidence"])
    if not 0.0 <= confidence <= 1.0:
        raise ValueError("trigger confidence must be normalized")
    for field in ("track_count", "confirmed_track_count", "observation_count", "segment_count", "gap_count"):
        if not isinstance(value[field], int) or value[field] < 0:
            raise ValueError(f"{field} must be a non-negative integer")
    refs = value["evidence_refs"]
    if not isinstance(refs, list) or any(not isinstance(item, str) or not _OPAQUE_REF.fullmatch(item) for item in refs):
        raise ValueError("evidence refs must be opaque evidence:// references")
    if not isinstance(value["metrics"], Mapping):
        raise ValueError("manifest metrics must be an object")
