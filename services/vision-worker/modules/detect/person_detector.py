import json
import logging
import os
import time

import cv2
import numpy as np

from core.model_runner import create_model_runner, ModelUnavailableError, model_status


log = logging.getLogger("synora.vision.person_detector")


class PersonDetector:
    """Existing YOLOv8 human detector with explicit RKNN runtime layout.

    The converted RKNN model accepts an NHWC runtime buffer even though its
    source ONNX graph is NCHW.  Keeping that distinction explicit is critical:
    passing the old NCHW buffer without ``data_format`` produced plausible
    tensors but no usable class-0 detections.
    """

    MAX_PERSONS = 10
    def __init__(self, core_mask=None):
        log.info("PERSON DETECTOR INIT")
        cv2.setNumThreads(1)
        model_path = os.getenv("SYNORA_YOLO_MODEL_PATH", "/var/lib/synora/models/yolov8.rknn")
        self.model_path = model_path
        self.available = False
        self.error = None
        self.capability_status = model_status(model_path)
        self.runner = None
        self.core_mask = core_mask
        try:
            self.runner = create_model_runner(model_path, core_mask=core_mask, input_data_format="nhwc")
            self.available = True
        except ModelUnavailableError as exc:
            self.error = exc.message
            self.capability_status = exc.as_dict()
            log.error("YOLO unavailable code=%s model=%s error=%s", exc.code, model_path, exc.message)
        except Exception as exc:
            self.error = str(exc)
            self.capability_status = {
                "status": "unavailable", "code": "rknn_runtime_error", "path": model_path,
                "error": self.error,
            }
            log.exception("YOLO unavailable model=%s", model_path)

        self.input_size = 640
        self.conf_threshold = 0.40
        self.nms_threshold = 0.45
        self.canvas = np.zeros((self.input_size, self.input_size, 3), dtype=np.uint8)
        if self.available:
            log.info("PERSON DETECTOR READY backend=%s model=%s", self.runner.backend, model_path)

    def capability(self):
        status = dict(self.capability_status or {})
        status.setdefault("path", self.model_path)
        status["status"] = "available" if self.available else "unavailable"
        status["input_size"] = self.input_size
        status["input_layout"] = "nhwc"
        status["confidence_threshold"] = self.conf_threshold
        status["nms_threshold"] = self.nms_threshold
        if self.error:
            status["error"] = self.error
        return status

    def preprocess(self, frame):
        h, w = frame.shape[:2]
        scale = min(self.input_size / w, self.input_size / h)
        new_w = int(w * scale)
        new_h = int(h * scale)
        resized = cv2.resize(frame, (new_w, new_h), interpolation=cv2.INTER_LINEAR)
        self.canvas.fill(0)
        pad_x = (self.input_size - new_w) // 2
        pad_y = (self.input_size - new_h) // 2
        self.canvas[pad_y:pad_y + new_h, pad_x:pad_x + new_w] = resized
        # BGR camera frames become RGB float32, retaining NHWC for RKNNLite.
        image = self.canvas[:, :, ::-1].astype(np.float32) / 255.0
        meta = {
            "scale": scale, "pad_x": pad_x, "pad_y": pad_y,
            "orig_h": h, "orig_w": w,
        }
        return np.ascontiguousarray(image[None, ...]), meta

    def detect(self, frame):
        return self.detect_timed(frame)[0]

    def detect_timed(self, frame):
        started = time.perf_counter()
        timings = {
            "decode_ms": 0.0,
            "preprocess_ms": 0.0,
            "rknn_inference_ms": 0.0,
            "postprocess_ms": 0.0,
            "nms_ms": 0.0,
            "total_ms": 0.0,
        }
        if (
            not self.available or self.runner is None
            or not isinstance(frame, np.ndarray) or frame.ndim != 3
            or frame.shape[0] == 0 or frame.shape[1] == 0
        ):
            timings["total_ms"] = (time.perf_counter() - started) * 1000.0
            return [], timings
        stage = time.perf_counter()
        blob, meta = self.preprocess(frame)
        timings["preprocess_ms"] = (time.perf_counter() - stage) * 1000.0
        stage = time.perf_counter()
        try:
            outputs = self.runner.infer(blob)
        except Exception:
            log.exception("YOLO inference failed")
            timings["rknn_inference_ms"] = (time.perf_counter() - stage) * 1000.0
            timings["total_ms"] = (time.perf_counter() - started) * 1000.0
            return [], timings
        timings["rknn_inference_ms"] = (time.perf_counter() - stage) * 1000.0

        stage = time.perf_counter()
        try:
            rows = self._normalize_outputs(outputs)
            decoded = self._decode_candidates(rows, meta)
        except (TypeError, ValueError, IndexError):
            log.exception("YOLO output post-processing failed")
            timings["postprocess_ms"] = (time.perf_counter() - stage) * 1000.0
            timings["total_ms"] = (time.perf_counter() - started) * 1000.0
            return [], timings
        timings["postprocess_ms"] = (time.perf_counter() - stage) * 1000.0
        if not decoded:
            timings["total_ms"] = (time.perf_counter() - started) * 1000.0
            return [], timings
        stage = time.perf_counter()
        try:
            results = self._apply_nms(frame, decoded)
        except Exception:
            log.exception("YOLO NMS failed")
            timings["nms_ms"] = (time.perf_counter() - stage) * 1000.0
            timings["total_ms"] = (time.perf_counter() - started) * 1000.0
            return [], timings
        timings["nms_ms"] = (time.perf_counter() - stage) * 1000.0
        timings["total_ms"] = (time.perf_counter() - started) * 1000.0
        return results, timings

    def _apply_nms(self, frame, decoded):
        boxes = [item["bbox"] for item in decoded]
        scores = [item["score"] for item in decoded]
        # OpenCV NMSBoxes consumes [x, y, width, height], not corner points.
        nms_boxes = [[x1, y1, x2 - x1, y2 - y1] for x1, y1, x2, y2 in boxes]
        indices = np.asarray(
            cv2.dnn.NMSBoxes(nms_boxes, scores, self.conf_threshold, self.nms_threshold)
        ).reshape(-1)
        results = []
        for index in indices:
            index = int(index)
            x1, y1, x2, y2 = boxes[index]
            results.append({"bbox": (x1, y1, x2, y2), "score": float(scores[index])})
        results.sort(key=lambda item: (-item["score"], item["bbox"]))
        return results[: self.MAX_PERSONS]

    def _decode_candidates(self, rows, meta):
        if rows is None or rows.ndim != 2 or rows.shape[1] < 6:
            return []
        rows = np.asarray(rows, dtype=np.float32)
        rows = rows[np.all(np.isfinite(rows), axis=1)]
        if rows.size == 0:
            return []
        # Keep geometric arithmetic in float64 so de-letterboxing preserves
        # the historical truncation at image boundaries.
        boxes = rows[:, :4].astype(np.float64, copy=True)
        if np.nanmax(boxes) <= 2.0:
            boxes *= self.input_size
        if rows.shape[1] >= 85:
            objectness = self._score_values(rows[:, 4])
            class_values = self._score_values(rows[:, 5:])
        else:
            objectness = np.ones(rows.shape[0], dtype=np.float32)
            class_values = self._score_values(rows[:, 4:])
        class_ids = np.argmax(class_values, axis=1)
        confidence = objectness * class_values[np.arange(rows.shape[0]), class_ids]
        keep = (class_ids == 0) & (confidence >= self.conf_threshold) & np.isfinite(confidence)
        boxes = boxes[keep]
        confidence = confidence[keep]
        if boxes.size == 0:
            return []

        scale = meta["scale"]
        translated = np.empty_like(boxes)
        translated[:, 0] = (boxes[:, 0] - boxes[:, 2] / 2 - meta["pad_x"]) / scale
        translated[:, 1] = (boxes[:, 1] - boxes[:, 3] / 2 - meta["pad_y"]) / scale
        translated[:, 2] = (boxes[:, 0] + boxes[:, 2] / 2 - meta["pad_x"]) / scale
        translated[:, 3] = (boxes[:, 1] + boxes[:, 3] / 2 - meta["pad_y"]) / scale
        translated[:, [0, 2]] = np.clip(translated[:, [0, 2]], 0, meta["orig_w"])
        translated[:, [1, 3]] = np.clip(translated[:, [1, 3]], 0, meta["orig_h"])
        widths = translated[:, 2] - translated[:, 0]
        heights = translated[:, 3] - translated[:, 1]
        aspect = heights / np.maximum(widths, 1)
        keep = (
            (widths >= 40) & (heights >= 80)
            & (aspect >= 0.5) & (aspect <= 5.0)
            & (translated[:, 2] > translated[:, 0])
            & (translated[:, 3] > translated[:, 1])
        )
        return [
            {"bbox": tuple(int(value) for value in box), "score": float(score)}
            for box, score in zip(translated[keep], confidence[keep])
        ]

    @staticmethod
    def _normalize_outputs(outputs):
        """Normalize YOLO [1,84,8400] and row-major test outputs."""
        if isinstance(outputs, (list, tuple)):
            if not outputs:
                return None
            outputs = outputs[0]
        array = np.asarray(outputs, dtype=np.float32)
        if array.ndim == 3:
            if array.shape[0] != 1:
                return None
            array = array[0]
        if array.ndim != 2:
            return None
        if array.shape[0] in (84, 85) and array.shape[1] > array.shape[0]:
            return array.transpose()
        if array.shape[1] in (84, 85):
            return array
        if array.shape[1] < 6 <= array.shape[0]:
            return array.transpose()
        if array.shape[0] < 6 <= array.shape[1]:
            return array
        return array if array.shape[1] >= 6 else None

    @staticmethod
    def _score_value(value):
        value = float(value)
        if 0.0 <= value <= 1.0:
            return value
        return float(1.0 / (1.0 + np.exp(-np.clip(value, -60.0, 60.0))))

    @staticmethod
    def _score_values(values):
        values = np.asarray(values, dtype=np.float32)
        if values.size == 0:
            return values
        if np.nanmin(values) >= 0.0 and np.nanmax(values) <= 1.0:
            return values
        return 1.0 / (1.0 + np.exp(-np.clip(values, -60.0, 60.0)))

    def close(self):
        if self.runner is not None:
            self.runner.close()
