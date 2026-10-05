"""RTMPose-s RKNN adapter.

The adapter is deliberately lower-level than the V3 cognitive contract.  It
keeps decoded keypoints inside one call, derives bounded posture/quality facts,
then releases the input, tensor and RKNN outputs before returning.
"""

from __future__ import annotations

from dataclasses import dataclass
import gc
import math
import os
import time
from typing import Any, Mapping, Optional

import cv2
import numpy as np

from .enrichment_v2 import ConfirmedHumanROI, PoseAggregateV2
from .model_runner import ModelUnavailableError, create_model_runner


RTMPOSE_WIDTH = 192
RTMPOSE_HEIGHT = 256
RTMPOSE_KEYPOINTS = 17
RTMPOSE_SPLIT_RATIO = 2.0
RTMPOSE_MEAN = np.asarray([123.675, 116.28, 103.53], dtype=np.float32)
RTMPOSE_STD = np.asarray([58.395, 57.12, 57.375], dtype=np.float32)


@dataclass(frozen=True)
class RTMPoseStatus:
    status: str
    model_path: str
    backend: str = "rknn"
    target: str = "rk3588"
    reason: str = ""


class RTMPoseBackend:
    """One RKNN RTMPose-s runner; no YOLO-pose or CPU fallback is allowed."""

    def __init__(self, model_path: Optional[str] = None, *, max_rate_hz: float = 5.0, max_rois: int = 4):
        self.model_path = str(model_path or os.getenv("SYNORA_RTMPOSE_MODEL_PATH", ""))
        self.minimum_interval_seconds = 1.0 / max(0.1, float(max_rate_hz))
        self.max_rois = max(1, int(max_rois))
        self.calls = 0
        self.latency_ms = 0.0
        self.buffers_released = 0
        self._last_call_at: dict[str, float] = {}
        self._last_result: dict[str, dict[str, Any]] = {}
        self._immobility_started_at: dict[str, float] = {}
        self._last_posture: dict[str, str] = {}
        self._last_keypoints: dict[str, np.ndarray] = {}
        self.runner = None
        self.status = RTMPoseStatus("unavailable", self.model_path, reason="model path is not configured")
        if not self.model_path:
            return
        try:
            self.runner = create_model_runner(self.model_path)
            self.status = RTMPoseStatus("available", self.model_path)
        except ModelUnavailableError as exc:
            self.status = RTMPoseStatus("unavailable", self.model_path, reason=exc.message)
        except Exception as exc:  # pragma: no cover - defensive hardware boundary
            self.status = RTMPoseStatus("unavailable", self.model_path, reason=str(exc))

    @property
    def available(self) -> bool:
        return self.runner is not None and self.status.status == "available"

    def capability(self) -> dict[str, Any]:
        value = {"status": self.status.status, "model_path": self.model_path, "backend": self.status.backend, "target": self.status.target, "calls": self.calls, "latency_ms": round(self.latency_ms, 3), "buffers_released": self.buffers_released}
        if self.status.reason:
            value["reason"] = self.status.reason
        return value

    def close(self) -> None:
        if self.runner is not None:
            self.runner.close()
            self.runner = None
        self._last_call_at.clear()
        self._last_result.clear()
        self._immobility_started_at.clear()
        self._last_posture.clear()
        self._last_keypoints.clear()
        self.status = RTMPoseStatus("unavailable", self.model_path, reason="runner closed")

    def _prepare(self, image: Any) -> np.ndarray:
        array = np.asarray(image)
        if array.ndim != 3 or array.shape[2] not in (3, 4):
            raise ValueError("RTMPose requires an HWC BGR/RGBA ROI image")
        if array.shape[2] == 4:
            array = array[:, :, :3]
        resized = cv2.resize(array, (RTMPOSE_WIDTH, RTMPOSE_HEIGHT), interpolation=cv2.INTER_LINEAR)
        rgb = cv2.cvtColor(resized, cv2.COLOR_BGR2RGB)
        normalized = (rgb.astype(np.float32) - RTMPOSE_MEAN) / RTMPOSE_STD
        return np.ascontiguousarray(np.transpose(normalized, (2, 0, 1))[None, ...], dtype=np.float32)

    @staticmethod
    def _simcc_array(value: Any) -> np.ndarray:
        array = np.asarray(value, dtype=np.float32)
        while array.ndim > 2 and array.shape[0] == 1:
            array = array[0]
        if array.ndim != 2 or array.shape[0] != RTMPOSE_KEYPOINTS:
            raise ValueError(f"unexpected SimCC output shape: {array.shape}")
        return array

    @staticmethod
    def decode(outputs: Any) -> tuple[np.ndarray, float]:
        if not isinstance(outputs, (list, tuple)) or len(outputs) < 2:
            raise ValueError("RTMPose RKNN output must contain simcc_x and simcc_y")
        arrays = [RTMPoseBackend._simcc_array(item) for item in outputs[:2]]
        x, y = sorted(arrays, key=lambda item: item.shape[1])
        x_indices = np.argmax(x, axis=1).astype(np.float32) / RTMPOSE_SPLIT_RATIO
        y_indices = np.argmax(y, axis=1).astype(np.float32) / RTMPOSE_SPLIT_RATIO
        values = np.stack((x_indices, y_indices), axis=1)
        maxima = np.concatenate((np.max(x, axis=1), np.max(y, axis=1)))
        maxima = np.clip(maxima, -30.0, 30.0)
        confidence = float(np.mean(1.0 / (1.0 + np.exp(-maxima))))
        return values, max(0.0, min(1.0, confidence))

    @staticmethod
    def _posture(keypoints: np.ndarray, quality: float) -> str:
        if quality < 0.60:
            return "unknown"
        # COCO body keypoint indices: shoulders 5/6, hips 11/12, knees 13/14,
        # ankles 15/16.  This is a posture aggregate only, not fall detection.
        shoulder = float(np.mean(keypoints[[5, 6], 1]))
        hip = float(np.mean(keypoints[[11, 12], 1]))
        knee = float(np.mean(keypoints[[13, 14], 1]))
        ankle = float(np.mean(keypoints[[15, 16], 1]))
        vertical_span = max(ankle, knee, hip) - min(shoulder, hip, knee)
        if vertical_span < 45.0:
            return "ground"
        if hip >= knee - 18.0:
            return "seated"
        return "upright"

    def infer(self, image: Any, *, local_key: str, observed_at: float) -> dict[str, Any]:
        if not self.available:
            raise ModelUnavailableError("backend_unavailable", self.model_path, self.status.reason or "RTMPose runner unavailable")
        previous = self._last_call_at.get(local_key)
        if previous is not None and observed_at - previous < self.minimum_interval_seconds:
            return dict(self._last_result.get(local_key, {"status": "unavailable"}))
        started = time.perf_counter()
        tensor = outputs = keypoints = None
        self._last_call_at[local_key] = observed_at
        self.calls += 1
        try:
            tensor = self._prepare(image)
            outputs = self.runner.infer(tensor)
            keypoints, quality = self.decode(outputs)
            posture = self._posture(keypoints, quality)
            previous_posture = self._last_posture.get(local_key, "unknown")
            # The aggregate contract intentionally does not expose keypoints.
            # A stable, quality-qualified posture across bounded observations
            # is the conservative immobility signal at this boundary.
            if posture != "unknown" and previous_posture == posture:
                self._immobility_started_at.setdefault(local_key, observed_at)
                immobility_seconds = max(0.0, observed_at - self._immobility_started_at[local_key])
            elif posture != "unknown":
                self._immobility_started_at[local_key] = observed_at
                immobility_seconds = 0.0
            else:
                self._immobility_started_at.pop(local_key, None)
                immobility_seconds = 0.0
            recovery_observed = posture in {"upright", "seated"} and previous_posture == "ground"
            self._last_posture[local_key] = posture
            self._last_keypoints[local_key] = keypoints.copy()
            result = {"status": "available", "quality": quality, "posture": posture, "transition_to_ground": previous_posture in {"upright", "seated"} and posture == "ground", "immobility_seconds": immobility_seconds, "recovery_observed": recovery_observed}
            self._last_result[local_key] = result
            return dict(result)
        finally:
            self.latency_ms += (time.perf_counter() - started) * 1000.0
            del tensor, outputs, keypoints
            del image
            self.buffers_released += 1
            gc.collect()


class RTMPosePoseEnricher:
    """V3 semantic adapter around one RTMPoseBackend."""

    def __init__(self, model_path: Optional[str] = None, *, max_rate_hz: float = 5.0, max_rois: int = 4):
        self.backend = RTMPoseBackend(model_path, max_rate_hz=max_rate_hz, max_rois=max_rois)
        self.max_rois = max(1, int(max_rois))

    @property
    def calls(self) -> int:
        return self.backend.calls

    @property
    def latency_ms(self) -> float:
        return self.backend.latency_ms

    @property
    def model_status(self) -> str:
        return self.backend.status.status

    def capability(self) -> dict[str, Any]:
        return self.backend.capability()

    def enrich(self, roi: ConfirmedHumanROI) -> PoseAggregateV2:
        if not roi.confirmed:
            return PoseAggregateV2(status="not_requested")
        if roi.priority not in {"P1_urgent_presence", "P2_contextual_enrichment"} or roi.topology not in {"protected_interior", "restricted_threshold", "private_perimeter"}:
            return PoseAggregateV2(status="not_requested")
        sample = roi.pose_sample or {}
        image = sample.get("image") if isinstance(sample, Mapping) else None
        if image is None:
            return PoseAggregateV2(status="not_available")
        try:
            value = self.backend.infer(image, local_key=roi.local_track_key, observed_at=roi.observed_at.timestamp())
        except ModelUnavailableError:
            return PoseAggregateV2(status="not_available")
        except Exception:
            return PoseAggregateV2(status="not_available")
        posture = str(value.get("posture", "unknown"))
        # This low-level adapter keeps the existing process-local vocabulary;
        # V3 maps ``lying`` to its explicit ``ground`` state.
        posture = "lying" if posture == "ground" else posture
        return PoseAggregateV2(status="available", quality=float(value.get("quality", 0.0)), posture=posture, transition_to_ground=bool(value.get("transition_to_ground", False)), immobility_seconds=max(0.0, float(value.get("immobility_seconds", 0.0))), recovery_observed=bool(value.get("recovery_observed", False)))

    def close(self) -> None:
        self.backend.close()
