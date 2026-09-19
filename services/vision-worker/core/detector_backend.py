"""Explicit replay adapter for the existing human detector.

The adapter is intentionally narrow: it exposes normalized human detections
and a non-sensitive diagnostic. Frames, crops and model outputs never leave
this process.
"""

from __future__ import annotations

from concurrent.futures import Future, ThreadPoolExecutor, TimeoutError
from dataclasses import dataclass
import math
import os
import time
from typing import Any, Optional


@dataclass(frozen=True)
class BackendDiagnostic:
    name: str
    model_version: str
    real_model: bool
    status: str
    frames_sampled: int
    detections_total: int
    latency_ms: float
    non_human_ignored: int = 0
    error_code: Optional[str] = None
    detector_compute_sum_ms: float = 0.0

    def as_dict(self) -> dict[str, Any]:
        result: dict[str, Any] = {
            "name": self.name,
            "model_version": self.model_version,
            "real_model": self.real_model,
            "status": self.status,
            "frames_sampled": self.frames_sampled,
            "detections_total": self.detections_total,
            "latency_ms": round(max(0.0, float(self.latency_ms)), 3),
            "detector_compute_sum_ms": round(max(0.0, float(self.detector_compute_sum_ms or self.latency_ms)), 3),
            "non_human_ignored": self.non_human_ignored,
        }
        if self.error_code:
            result["error_code"] = self.error_code
        return result


class DetectorBackend:
    """Small interface consumed by ``VisionClipPipelineV1.process_video``."""

    name = "detector_backend"

    def detect(self, frame: Any, frame_timestamp_ms: int = 0) -> list[dict[str, Any]]:
        raise NotImplementedError

    def diagnostic(self) -> dict[str, Any]:
        raise NotImplementedError


class ExistingDetectorBackend(DetectorBackend):
    """Normalize the already configured ``PersonDetector`` without replacing it."""

    name = "existing_detector"

    def __init__(self, detector: Any, timeout_seconds: float = 2.0):
        self.detector = detector
        self.timeout_seconds = max(0.05, float(timeout_seconds))
        self.model_path = str(getattr(detector, "model_path", ""))
        self.model_version = self._model_version(detector)
        self._executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="vision-detector")
        self._inflight: Optional[Future[Any]] = None
        self._frames_sampled = 0
        self._detections_total = 0
        self._non_human_ignored = 0
        self._latency_ms = 0.0
        self._real_model = False
        self._status = "ok" if self._available() else "unavailable"
        self._error_code = None if self._available() else "backend_unavailable"

    def _available(self) -> bool:
        return self.detector is not None and bool(getattr(self.detector, "available", False))

    @staticmethod
    def _model_version(detector: Any) -> str:
        explicit = getattr(detector, "model_version", None)
        if isinstance(explicit, str) and explicit.strip():
            return explicit.strip()
        path = getattr(detector, "model_path", None)
        if isinstance(path, str) and path.strip():
            return os.path.basename(path)
        return "unknown"

    def detect(self, frame: Any, frame_timestamp_ms: int = 0) -> list[dict[str, Any]]:
        self._frames_sampled += 1
        if not self._available():
            self._status = "unavailable"
            self._error_code = "backend_unavailable"
            return []
        if self._inflight is not None:
            if not self._inflight.done():
                self._status = "timeout"
                self._error_code = "inference_timeout"
                return []
            self._inflight = None

        started = time.monotonic()
        self._inflight = self._executor.submit(self.detector.detect, frame)
        try:
            raw = self._inflight.result(timeout=self.timeout_seconds)
        except TimeoutError:
            self._status = "timeout"
            self._error_code = "inference_timeout"
            return []
        except Exception:
            self._status = "failed"
            self._error_code = "inference_failed"
            return []
        finally:
            self._latency_ms += (time.monotonic() - started) * 1000.0
            if self._inflight is not None and self._inflight.done():
                self._inflight = None

        self._real_model = True
        self._status = "ok"
        self._error_code = None
        normalized = self._normalize(raw, frame_timestamp_ms)
        self._detections_total += len(normalized)
        return normalized

    def _normalize(self, raw: Any, frame_timestamp_ms: int) -> list[dict[str, Any]]:
        if raw is None:
            return []
        if isinstance(raw, dict):
            raw = [raw]
        if not isinstance(raw, (list, tuple)):
            self._status = "failed"
            self._error_code = "invalid_detector_output"
            return []
        normalized: list[dict[str, Any]] = []
        for item in raw:
            if not isinstance(item, dict):
                self._status = "failed"
                self._error_code = "invalid_detector_output"
                continue
            class_name = self._class_name(item)
            if class_name != "human":
                self._non_human_ignored += 1
                continue
            bbox = self._bbox(item.get("bbox"))
            confidence = self._confidence(item.get("confidence", item.get("score")))
            if bbox is None or confidence is None:
                self._status = "failed"
                self._error_code = "invalid_detector_output"
                continue
            normalized.append({
                "class": "human",
                "bbox": list(bbox),
                "confidence": confidence,
                "frame_timestamp_ms": int(frame_timestamp_ms),
                "backend": self.name,
                "model_version": self.model_version,
            })
        return normalized

    def _class_name(self, item: dict[str, Any]) -> str:
        if "class" in item:
            return str(item["class"]).strip().lower()
        if "class_id" in item:
            try:
                return "human" if int(item["class_id"]) == 0 else "other"
            except (TypeError, ValueError, OverflowError):
                return "other"
        # PersonDetector already enforces class_id == 0 and emits only bbox/score.
        return "human"

    @staticmethod
    def _bbox(value: Any) -> Optional[tuple[int, int, int, int]]:
        if not isinstance(value, (list, tuple)) or len(value) != 4:
            return None
        try:
            numbers = tuple(float(part) for part in value)
        except (TypeError, ValueError, OverflowError):
            return None
        if not all(math.isfinite(number) for number in numbers):
            return None
        x1, y1, x2, y2 = (int(part) for part in numbers)
        if x2 <= x1 or y2 <= y1:
            return None
        return x1, y1, x2, y2

    @staticmethod
    def _confidence(value: Any) -> Optional[float]:
        try:
            score = float(value)
        except (TypeError, ValueError, OverflowError):
            return None
        if not math.isfinite(score) or score < 0.0 or score > 1.0:
            return None
        return score

    def diagnostic(self) -> dict[str, Any]:
        status = self._status
        if status == "ok" and not self._real_model:
            status = "unavailable"
        return BackendDiagnostic(
            name=self.name,
            model_version=self.model_version,
            real_model=self._real_model,
            status=status,
            frames_sampled=self._frames_sampled,
            detections_total=self._detections_total,
            latency_ms=self._latency_ms,
            non_human_ignored=self._non_human_ignored,
            error_code=self._error_code,
            detector_compute_sum_ms=self._latency_ms,
        ).as_dict()

    def capability(self) -> dict[str, Any]:
        result = {
            "name": self.name,
            "status": "available" if self._available() else "unavailable",
            "backend": getattr(getattr(self.detector, "runner", None), "backend", "unknown"),
            "model_path": self.model_path,
            "model_version": self.model_version,
            "input_size": int(getattr(self.detector, "input_size", 640) or 640),
            "confidence_threshold": float(getattr(self.detector, "conf_threshold", 0.40) or 0.40),
            "nms_threshold": float(getattr(self.detector, "nms_threshold", 0.45) or 0.45),
            "real_model": self._real_model,
        }
        if getattr(self.detector, "error", None):
            result["error"] = str(self.detector.error)
        return result

    def close(self) -> None:
        self._executor.shutdown(wait=False, cancel_futures=True)


class ThreePinnedDetectorBackend(DetectorBackend):
    """Three independent RKNN runners with a bounded, ordered batch API."""

    name = "existing_detector"

    def __init__(self, detectors: list[Any], timeout_seconds: float = 2.0):
        if len(detectors) != 3:
            raise ValueError("three pinned detector backend requires exactly three detectors")
        self.backends = [ExistingDetectorBackend(detector, timeout_seconds) for detector in detectors]
        self._executor = ThreadPoolExecutor(max_workers=3, thread_name_prefix="vision-detector")
        self.model_version = self.backends[0].model_version

    def detect(self, frame: Any, frame_timestamp_ms: int = 0) -> list[dict[str, Any]]:
        return self.backends[0].detect(frame, frame_timestamp_ms)

    def detect_many(self, frames: list[Any], frame_timestamps_ms: list[int]) -> list[list[dict[str, Any]]]:
        if len(frames) != len(frame_timestamps_ms) or len(frames) > 3:
            raise ValueError("three pinned detector batch must contain one to three frames")
        futures = [
            self._executor.submit(self.backends[index].detect, frame, timestamp)
            for index, (frame, timestamp) in enumerate(zip(frames, frame_timestamps_ms))
        ]
        return [future.result() for future in futures]

    def diagnostic(self) -> dict[str, Any]:
        diagnostics = [backend.diagnostic() for backend in self.backends]
        statuses = {item["status"] for item in diagnostics}
        if "failed" in statuses:
            status = "failed"
        elif "timeout" in statuses:
            status = "timeout"
        elif "unavailable" in statuses:
            status = "unavailable"
        else:
            status = "ok"
        return BackendDiagnostic(
            name=self.name,
            model_version=self.model_version,
            real_model=any(item["real_model"] for item in diagnostics),
            status=status,
            frames_sampled=sum(item["frames_sampled"] for item in diagnostics),
            detections_total=sum(item["detections_total"] for item in diagnostics),
            latency_ms=sum(item["latency_ms"] for item in diagnostics),
            non_human_ignored=sum(item["non_human_ignored"] for item in diagnostics),
            error_code=next((item.get("error_code") for item in diagnostics if item.get("error_code")), None),
            detector_compute_sum_ms=sum(item.get("detector_compute_sum_ms", item.get("latency_ms", 0.0)) for item in diagnostics),
        ).as_dict()

    def capability(self) -> dict[str, Any]:
        capability = dict(self.backends[0].capability())
        capability["strategy"] = "three_pinned_workers"
        capability["workers"] = 3
        return capability

    def close(self) -> None:
        self._executor.shutdown(wait=True, cancel_futures=True)
        for backend in self.backends:
            backend.close()
