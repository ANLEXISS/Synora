"""Edge-owned visual tracking and semantic evidence selection.

The replay uses the same deterministic decoder and detector interface as the
production worker, but owns the pixel-space tracker here.  Only aggregate
semantic events leave this boundary.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import time
from typing import Any, Mapping

from edge.manifest import EdgeTrackManifest, opaque_evidence_ref
from core.clip_pipeline_v1 import ClipMetadata, VisionClipPipelineV1


@dataclass(frozen=True)
class EdgeReplayResult:
    events: tuple[dict[str, Any], ...]
    manifest: dict[str, Any]
    metrics: dict[str, Any]


def _replace_track_ids(value: Any, mapping: Mapping[str, str], key: str = "") -> Any:
    if isinstance(value, dict):
        return {name: _replace_track_ids(child, mapping, name) for name, child in value.items()}
    if isinstance(value, list):
        return [_replace_track_ids(child, mapping, key) for child in value]
    if key in {"track_id", "id"} and isinstance(value, str):
        return mapping.get(value, value)
    return value


class EdgeVisionWorkerV1:
    """Run one bounded clip with edge tracking and semantic-only output."""

    def __init__(self, config: dict[str, Any] | None = None):
        self.config = {
            "sampling_initial_fps": 5.0,
            "sampling_active_fps": 5.0,
            "sampling_stable_fps": 2.0,
            "sampling_quiet_fps": 2.0,
            "sampling_minimum_detection_fps": 1.0,
            "max_crops_per_track": 0,
            **dict(config or {}),
        }

    def process_video(self, clip: ClipMetadata, video_path: str, detector: Any,
                      *, segment_count: int = 1, edge_emulated: bool = True) -> EdgeReplayResult:
        started = time.perf_counter()
        # VisionClipPipelineV1 is retained as the semantic event assembler.  Its
        # tracker dependency is edge.tracker; it never serializes pixel state.
        pipeline = VisionClipPipelineV1(self.config)
        events = pipeline.process_video(clip, video_path, detector, sample_period_seconds=0.2)
        diagnostic = dict(detector.diagnostic()) if hasattr(detector, "diagnostic") else {}
        raw_ids = sorted({
            str(track.get("track_id"))
            for event in events
            for track in (event.get("payload", {}).get("tracks", []) if isinstance(event, dict) else [])
            if isinstance(track, dict) and track.get("track_id")
        })
        if not raw_ids:
            raw_ids = sorted({str(event.get("track_id")) for event in events if event.get("track_id")})
        id_map = {old: f"aggregate-track-{index + 1}" for index, old in enumerate(raw_ids)}
        safe_events: list[dict[str, Any]] = []
        for event in events:
            if event.get("type") not in {"synora.vision.clip-observation/v1", "synora.vision.clip-summary/v1"}:
                continue
            safe = _replace_track_ids(event, id_map)
            payload = safe.setdefault("payload", {})
            payload.pop("media", None)
            payload.pop("identity", None)
            payload.pop("plate", None)
            payload.pop("sensitive_objects", None)
            payload["edge_tracking_status"] = "ok"
            payload["edge_emulated"] = bool(edge_emulated)
            payload["central_visual_tracking_invocations"] = 0
            safe_events.append(safe)

        summaries = [item for item in safe_events if item["type"] == "synora.vision.clip-summary/v1"]
        observations = [item for item in safe_events if item["type"] == "synora.vision.clip-observation/v1"]
        metric_values: dict[str, Any] = {}
        for event in summaries:
            metric_values.update(event.get("payload", {}).get("metrics", {}))
        metric_values.update({
            "edge_tracking_wall_ms": round(float(metric_values.get("tracking_wall_ms", 0.0)), 3),
            "edge_detection_wall_ms": round(float(metric_values.get("detector_wall_ms", 0.0)), 3),
            "edge_frames_read": int(metric_values.get("frames_read", 0)),
            "edge_frames_sampled": int(metric_values.get("frames_sampled", 0)),
            "edge_frames_ignored": int(metric_values.get("frames_skipped_by_policy", 0)),
            "edge_peak_frames_in_flight": int(metric_values.get("peak_frames_in_flight", 0)),
            "central_visual_tracking_invocations": 0,
            "edge_emulated": bool(edge_emulated),
            "edge_wall_ms": round((time.perf_counter() - started) * 1000.0, 3),
        })
        for event in safe_events:
            event.setdefault("payload", {}).setdefault("metrics", {}).update(metric_values)

        priorities = [
            event.get("payload", {}).get("priority_hint")
            for event in safe_events
            if event.get("payload", {}).get("priority_hint")
        ]
        reasons = sorted({
            reason
            for event in safe_events
            for reason in event.get("payload", {}).get("reason_codes", [])
        })
        topology = clip.topology.topology_class.value
        trigger_class = "human" if any(
            track.get("subject_type") == "human"
            for event in observations
            for track in event.get("payload", {}).get("tracks", [])
        ) else "unknown"
        refs = tuple(opaque_evidence_ref(clip.episode_id, index, "selected") for index in range(min(3, len(observations))))
        manifest = EdgeTrackManifest(
            schema_version="synora.vision.edge-track-manifest/v1",
            camera_id=clip.camera_id,
            episode_id=clip.episode_id,
            topology_class=topology,
            trigger_class=trigger_class,
            trigger_confidence=1.0 if trigger_class == "human" else 0.0,
            tracking_status="ok" if diagnostic.get("status", "ok") == "ok" else "unavailable",
            started_at=clip.started_at.isoformat(),
            ended_at=clip.ends_at.isoformat(),
            track_count=len(id_map),
            confirmed_track_count=sum(
                1 for event in observations
                for track in event.get("payload", {}).get("tracks", [])
                if track.get("state") == "confirmed"
            ),
            observation_count=len(observations),
            segment_count=int(segment_count),
            gap_count=int(metric_values.get("frames_skipped_by_policy", 0) > 0),
            evidence_refs=refs,
            priority_reason=tuple(reasons or priorities[:1] or ("no_observation",)),
            edge_emulated=bool(edge_emulated),
            metrics=metric_values,
        ).as_dict()
        return EdgeReplayResult(tuple(safe_events), manifest, metric_values)
