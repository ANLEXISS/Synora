"""Bounded deterministic visual tracking for one camera episode.

This module is the sole owner of pixel-space tracking.  Its identifiers are
process-local and are never serialized by the edge manifest or a bus event.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
import math
from typing import Any, Iterable, Optional


def _utc(value: Optional[datetime]) -> datetime:
    if value is None:
        return datetime.now(timezone.utc)
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _bbox_iou(left: tuple[int, int, int, int], right: tuple[int, int, int, int]) -> float:
    x1 = max(left[0], right[0]); y1 = max(left[1], right[1])
    x2 = min(left[2], right[2]); y2 = min(left[3], right[3])
    intersection = max(0, x2 - x1) * max(0, y2 - y1)
    left_area = max(0, left[2] - left[0]) * max(0, left[3] - left[1])
    right_area = max(0, right[2] - right[0]) * max(0, right[3] - right[1])
    union = left_area + right_area - intersection
    return intersection / union if union else 0.0


@dataclass
class _TrackCandidate:
    track_id: str
    bbox: tuple[int, int, int, int]
    last_seen: datetime


class ClipTrackerV1:
    """Bounded local tracker; IDs never leave this module."""

    def __init__(self, iou_threshold: float = 0.30,
                 max_track_gap_seconds: float = 1.0,
                 max_active_tracks: int = 16,
                 min_bbox_width: int = 20,
                 min_bbox_height: int = 20,
                 track_id_prefix: str = "human"):
        if not 0.0 <= float(iou_threshold) <= 1.0:
            raise ValueError("iou threshold must be between 0 and 1")
        if max_track_gap_seconds < 0 or max_active_tracks <= 0:
            raise ValueError("tracker gap must be non-negative and track cap positive")
        if min_bbox_width <= 0 or min_bbox_height <= 0:
            raise ValueError("minimum bbox dimensions must be positive")
        self.iou_threshold = float(iou_threshold)
        self.max_track_gap = timedelta(seconds=float(max_track_gap_seconds))
        self.max_active_tracks = int(max_active_tracks)
        self.min_bbox_width = int(min_bbox_width)
        self.min_bbox_height = int(min_bbox_height)
        self.track_id_prefix = str(track_id_prefix).strip() or "human"
        self._next_track = 0
        self._active: dict[str, _TrackCandidate] = {}

    @property
    def active_track_ids(self) -> tuple[str, ...]:
        return tuple(sorted(self._active))

    def update(self, detections: Iterable[dict[str, Any]], at: datetime) -> list[dict[str, Any]]:
        observed_at = _utc(at)
        self._active = {track_id: candidate for track_id, candidate in self._active.items()
                        if observed_at >= candidate.last_seen and observed_at - candidate.last_seen <= self.max_track_gap}
        clean: list[tuple[int, tuple[int, int, int, int], float, dict[str, Any]]] = []
        for index, item in enumerate(detections):
            if not isinstance(item, dict):
                continue
            bbox = self._bbox(item.get("bbox"))
            if bbox is None:
                continue
            score = self._score(item.get("confidence", item.get("score", 0.0)))
            if bbox[2] - bbox[0] < self.min_bbox_width or bbox[3] - bbox[1] < self.min_bbox_height:
                continue
            clean.append((index, bbox, score, item))
        clean.sort(key=lambda item: (-item[2], item[1], item[0]))
        candidates = []
        for detection_order, (_, bbox, _, _) in enumerate(clean):
            for track_id in sorted(self._active):
                overlap = _bbox_iou(bbox, self._active[track_id].bbox)
                if overlap >= self.iou_threshold:
                    candidates.append((-overlap, track_id, detection_order))
        candidates.sort()
        matched_tracks: set[str] = set()
        matched_detections: set[int] = set()
        assigned: list[dict[str, Any]] = []
        for _, track_id, detection_order in candidates:
            if track_id in matched_tracks or detection_order in matched_detections:
                continue
            _, bbox, score, item = clean[detection_order]
            self._active[track_id] = _TrackCandidate(track_id, bbox, observed_at)
            matched_tracks.add(track_id); matched_detections.add(detection_order)
            assigned.append({"track_id": track_id, "bbox": bbox, "score": score, "item": item})
        for detection_order, (_, bbox, score, item) in enumerate(clean):
            if detection_order in matched_detections or len(self._active) >= self.max_active_tracks:
                continue
            track_id = f"{self.track_id_prefix}-{self._next_track}"
            self._next_track += 1
            self._active[track_id] = _TrackCandidate(track_id, bbox, observed_at)
            matched_detections.add(detection_order)
            assigned.append({"track_id": track_id, "bbox": bbox, "score": score, "item": item})
        assigned.sort(key=lambda item: item["track_id"])
        return assigned

    @staticmethod
    def _bbox(value: Any) -> Optional[tuple[int, int, int, int]]:
        if not isinstance(value, (list, tuple)) or len(value) != 4:
            return None
        try:
            values = tuple(int(value[index]) for index in range(4))
        except (TypeError, ValueError, OverflowError):
            return None
        return values if values[2] > values[0] and values[3] > values[1] else None

    @staticmethod
    def _score(value: Any) -> float:
        try:
            score = float(value)
        except (TypeError, ValueError):
            return 0.0
        return score if math.isfinite(score) else 0.0
