#!/usr/bin/env python3
"""Aggregate-only YOLOv8n-Pose RKNN adapter for the central harness.

The four-output decoder follows Rockchip's public RKNN Model Zoo
``examples/yolov8_pose/python/yolov8_pose.py`` post-process: DFL over the
three 65-channel detection heads, sigmoid objectness, and the separate
``[1, 17, 3, 8400]`` keypoint head. Raw frames and keypoints remain local to
this process and stdout contains only the bounded aggregate contract.
"""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
import re
import sys
import time

import cv2
import numpy as np

from modules.detect.person_detector import PersonDetector

from core.model_runner import ModelUnavailableError, create_model_runner


INPUT_SIZE = 640
PADDING = 56
OBJECT_THRESHOLD = 0.25
NMS_THRESHOLD = 0.40
DFL_BINS = 16
KEYPOINT_COUNT = 17
BRANCH_SHAPES = ((80, 80), (40, 40), (20, 20))


@contextmanager
def silence_native_stdout():
    """Keep RKNN C-runtime diagnostics out of the aggregate JSON protocol."""
    saved = os.dup(1)
    sink = os.open(os.devnull, os.O_WRONLY)
    try:
        os.dup2(sink, 1)
        yield
    finally:
        os.dup2(saved, 1)
        os.close(sink)
        os.close(saved)


def emit(value):
    sys.stdout.write(json.dumps(value, sort_keys=True) + "\n")


def load_backend(model_path):
    if not model_path or not model_path.lower().endswith(".rknn"):
        raise ModelUnavailableError("invalid_model", model_path, "YOLOv8n-pose RKNN model is required")
    return create_model_runner(model_path, core_mask=None, input_data_format="nhwc")


def letterbox(frame):
    height, width = frame.shape[:2]
    scale = min(INPUT_SIZE / width, INPUT_SIZE / height)
    resized_width = max(1, int(width * scale))
    resized_height = max(1, int(height * scale))
    resized = cv2.resize(frame, (resized_width, resized_height), interpolation=cv2.INTER_AREA)
    canvas = np.full((INPUT_SIZE, INPUT_SIZE, 3), PADDING, dtype=np.uint8)
    pad_x = (INPUT_SIZE - resized_width) // 2
    pad_y = (INPUT_SIZE - resized_height) // 2
    canvas[pad_y:pad_y + resized_height, pad_x:pad_x + resized_width] = resized
    # The validated RKNN artifact expects RGB NHWC uint8, not normalized float.
    return np.ascontiguousarray(canvas[:, :, ::-1][None, ...]), (scale, pad_x, pad_y, width, height)


def validate_outputs(outputs):
    if not isinstance(outputs, (list, tuple)) or len(outputs) != 4:
        raise RuntimeError("RKNN YOLOv8 pose output must contain four tensors")
    for index, expected in enumerate((*BRANCH_SHAPES, (17, 3, 8400))):
        actual = tuple(np.asarray(outputs[index]).shape)
        expected_shape = (1, 65, *expected) if index < 3 else (1, *expected)
        if actual != expected_shape:
            raise RuntimeError(f"invalid RKNN output shape at index {index}: {actual}")
        array = np.asarray(outputs[index], dtype=np.float32)
        if not np.all(np.isfinite(array)):
            raise RuntimeError(f"non-finite RKNN output at index {index}")
        if not np.any(array):
            raise RuntimeError(f"null RKNN output at index {index}")
    return [np.asarray(value, dtype=np.float32) for value in outputs]


def sigmoid(values):
    return 1.0 / (1.0 + np.exp(-np.clip(values, -60.0, 60.0)))


def softmax(values, axis=-1):
    shifted = values - np.max(values, axis=axis, keepdims=True)
    exponent = np.exp(shifted)
    return exponent / np.sum(exponent, axis=axis, keepdims=True)


def nms_indices(boxes, scores):
    order = np.argsort(scores)[::-1]
    keep = []
    while order.size:
        current = int(order[0])
        keep.append(current)
        if order.size == 1:
            break
        rest = order[1:]
        x1 = np.maximum(boxes[current, 0], boxes[rest, 0])
        y1 = np.maximum(boxes[current, 1], boxes[rest, 1])
        x2 = np.minimum(boxes[current, 2], boxes[rest, 2])
        y2 = np.minimum(boxes[current, 3], boxes[rest, 3])
        intersection = np.maximum(0.0, x2 - x1) * np.maximum(0.0, y2 - y1)
        area_current = max(0.0, boxes[current, 2] - boxes[current, 0]) * max(0.0, boxes[current, 3] - boxes[current, 1])
        area_rest = np.maximum(0.0, boxes[rest, 2] - boxes[rest, 0]) * np.maximum(0.0, boxes[rest, 3] - boxes[rest, 1])
        overlap = intersection / np.maximum(area_current + area_rest - intersection, 1e-6)
        order = rest[overlap <= NMS_THRESHOLD]
    return keep


def decode_detections(outputs, meta):
    outputs = validate_outputs(outputs)
    scale, pad_x, pad_y, width, height = meta
    keypoint_head = outputs[3][0]
    detections = []
    keypoint_offset = 0
    for branch_index, (grid_h, grid_w) in enumerate(BRANCH_SHAPES):
        feature = outputs[branch_index].reshape(1, 65, -1)
        score = sigmoid(feature[0, 64, :])
        selected = score >= OBJECT_THRESHOLD
        count = grid_h * grid_w
        if not np.any(selected):
            keypoint_offset += count
            continue
        regression = feature[0, :64, :].reshape(4, DFL_BINS, count).transpose(2, 0, 1)
        distances = np.sum(softmax(regression, axis=2) * np.arange(DFL_BINS, dtype=np.float32), axis=2)
        columns, rows = np.meshgrid(np.arange(grid_w, dtype=np.float32), np.arange(grid_h, dtype=np.float32))
        centers = np.stack((columns.reshape(-1), rows.reshape(-1)), axis=1)
        top_left = centers + 0.5 - distances[:, :2]
        bottom_right = centers + 0.5 + distances[:, 2:]
        boxes = np.concatenate((top_left, bottom_right), axis=1) * (INPUT_SIZE // grid_w)
        points = keypoint_head[:, :, keypoint_offset:keypoint_offset + count].transpose(2, 0, 1)
        keypoint_offset += count
        for box, point, confidence in zip(boxes[selected], points[selected], score[selected]):
            box = box.astype(np.float64)
            box[[0, 2]] = (box[[0, 2]] - pad_x) / scale
            box[[1, 3]] = (box[[1, 3]] - pad_y) / scale
            box[[0, 2]] = np.clip(box[[0, 2]], 0.0, width)
            box[[1, 3]] = np.clip(box[[1, 3]], 0.0, height)
            point = point.astype(np.float64, copy=True)
            point[:, 0] = np.clip((point[:, 0] - pad_x) / scale, 0.0, width)
            point[:, 1] = np.clip((point[:, 1] - pad_y) / scale, 0.0, height)
            point[:, 2] = np.clip(point[:, 2], 0.0, 1.0)
            detections.append({"bbox": box, "keypoints": point, "score": float(confidence)})
    if not detections:
        return []
    boxes = np.asarray([item["bbox"] for item in detections], dtype=np.float64)
    scores = np.asarray([item["score"] for item in detections], dtype=np.float64)
    return [detections[index] for index in nms_indices(boxes, scores)]


def decode_best(outputs, meta):
    detections = decode_detections(outputs, meta)
    return detections[0] if detections else None


def posture_from_pose(pose):
    if pose is None:
        return "unavailable"
    x1, y1, x2, y2 = pose["bbox"]
    width = max(1.0, x2 - x1)
    height = max(1.0, y2 - y1)
    points = pose["keypoints"]
    visible = points[:, 2] >= 0.25
    if int(np.count_nonzero(visible)) < 5:
        return "ambiguous"
    if height / width < 1.05:
        return "ground"
    hip_y = float(np.mean(points[[11, 12], 1]))
    knee_y = float(np.mean(points[[13, 14], 1]))
    shoulder_y = float(np.mean(points[[5, 6], 1]))
    if knee_y - hip_y > height * 0.12 and height / width < 1.7:
        return "ambiguous"
    if shoulder_y <= hip_y and height / width >= 1.25:
        return "upright"
    return "ground" if height / width < 1.25 else "ambiguous"


def percentile(values, fraction):
    if not values:
        return 0.0
    return float(np.percentile(np.asarray(values, dtype=np.float64), fraction))


def rknn_versions(runner):
    raw = runner.rknn.get_sdk_version() or ""
    match = re.search(r"API:\s*([^\n]+).*?DRV:\s*([^\n]+)", str(raw), re.S)
    if not match:
        return "unreported", "unreported"
    return match.group(1).strip(), match.group(2).strip()


def pose_gate_allows(humans, roi_count, max_rois):
    return bool(humans) and roi_count < max_rois


def run_video(video_path, model_path, max_frames, max_rois):
    pose_init_started = time.perf_counter()
    runner = load_backend(model_path)
    pose_initialization_ms = (time.perf_counter() - pose_init_started) * 1000.0
    human_gate_init_started = time.perf_counter()
    person_detector = PersonDetector()
    human_gate_initialization_ms = (time.perf_counter() - human_gate_init_started) * 1000.0
    capture = cv2.VideoCapture(video_path)
    if not capture.isOpened():
        runner.close()
        if person_detector.runner is not None:
            person_detector.runner.close()
        raise RuntimeError("video could not be decoded")
    frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    if frame_count <= 0:
        capture.release()
        runner.close()
        if person_detector.runner is not None:
            person_detector.runner.close()
        raise RuntimeError("video has no readable frames")
    stride = max(1, int(np.ceil(frame_count / max_frames)))
    previous_center = None
    previous_posture = None
    previous_time = None
    postures = []
    confidences = []
    latency_samples = []
    inference_latency_samples = []
    immobility = 0.0
    rapid = False
    fall_candidate = False
    processed = 0
    human_confirmed = 0
    pose_requests = 0
    human_detector_available = person_detector.available
    pose_request_reason = "human_not_confirmed"
    index = 0
    try:
        while processed < max_frames:
            ok, frame = capture.read()
            if not ok:
                break
            if index % stride != 0:
                index += 1
                continue
            index += 1
            processed += 1
            humans = person_detector.detect(frame)
            if not humans:
                continue
            human_confirmed += 1
            if not pose_gate_allows(humans, pose_requests, max_rois):
                pose_request_reason = "pose_roi_budget_exhausted"
                continue
            pose_requests += 1
            pose_request_reason = "confirmed_human"
            started = time.perf_counter()
            tensor, meta = letterbox(frame)
            inference_started = time.perf_counter()
            outputs = runner.infer(tensor)
            inference_latency_samples.append((time.perf_counter() - inference_started) * 1000.0)
            pose = decode_best(outputs, meta)
            latency_samples.append((time.perf_counter() - started) * 1000.0)
            if pose is None:
                continue
            posture = posture_from_pose(pose)
            postures.append(posture)
            confidences.append(float(min(1.0, max(0.0, pose["score"]))))
            x1, y1, x2, y2 = pose["bbox"]
            center = np.asarray(((x1 + x2) / 2.0, (y1 + y2) / 2.0), dtype=np.float64)
            now = index / max(float(capture.get(cv2.CAP_PROP_FPS) or 1.0), 1.0)
            if previous_center is not None:
                displacement = float(np.linalg.norm(center - previous_center)) / max(frame.shape[1], frame.shape[0], 1)
                rapid = rapid or displacement > 0.20
                if displacement < 0.02:
                    immobility += max(0.0, now - (previous_time or now))
                else:
                    immobility = 0.0
            if previous_posture == "upright" and posture == "ground":
                fall_candidate = True
            previous_center = center
            previous_posture = posture
            previous_time = now
    finally:
        capture.release()
        runner.close()
        if person_detector.runner is not None:
            person_detector.runner.close()
    common = {
        "latency_ms": round(float(np.mean(latency_samples)) if latency_samples else 0.0, 3),
        "latency_p50_ms": round(percentile(latency_samples, 50), 3),
        "latency_p95_ms": round(percentile(latency_samples, 95), 3),
        "latency_max_ms": round(max(latency_samples) if latency_samples else 0.0, 3),
        "inference_latency_p50_ms": round(percentile(inference_latency_samples, 50), 3),
        "inference_latency_p95_ms": round(percentile(inference_latency_samples, 95), 3),
        "inference_latency_max_ms": round(max(inference_latency_samples) if inference_latency_samples else 0.0, 3),
        "inference_latency_samples_ms": [round(value, 3) for value in inference_latency_samples],
        "pose_initialization_ms": round(pose_initialization_ms, 3),
        "human_gate_initialization_ms": round(human_gate_initialization_ms, 3),
        "max_frames": max_frames,
        "max_pose_rois": max_rois,
        "human_gate_rejected_frames": max(0, processed - human_confirmed),
        "frame_count": processed,
        "pose_frame_count": len(postures),
        "human_detector_status": "available" if human_detector_available else "unavailable",
        "human_confirmed_frame_count": human_confirmed,
        "pose_request_count": pose_requests,
        "pose_request_reason": pose_request_reason if human_detector_available else "human_detector_unavailable",
        "valid_pose_results": len(postures),
    }
    if not postures:
        return {
            **common, "pose_status": "not_requested" if human_confirmed == 0 else "low_quality", "posture": "unavailable", "immobility_seconds": 0.0,
            "fall_state": "none", "rapid_motion_state": "unknown", "physical_interaction_candidate": False, "confidence": 0.0,
        }
    counts = {value: postures.count(value) for value in set(postures)}
    posture = sorted(counts, key=lambda value: (-counts[value], value))[0]
    return {
        **common, "pose_status": "available", "posture": posture, "immobility_seconds": round(min(120.0, max(0.0, immobility)), 3),
        "fall_state": "candidate" if fall_candidate else "none", "rapid_motion_state": "rapid" if rapid else "none",
        "physical_interaction_candidate": False, "confidence": round(float(np.mean(confidences)), 4),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--diagnostic", action="store_true")
    parser.add_argument("--model", required=True)
    parser.add_argument("--video")
    parser.add_argument("--max-frames", type=int, default=32)
    parser.add_argument("--max-rois", type=int, default=32)
    args = parser.parse_args()
    if args.diagnostic:
        runner = None
        try:
            with silence_native_stdout():
                initialized_at = time.perf_counter()
                runner = load_backend(args.model)
                initialization_ms = (time.perf_counter() - initialized_at) * 1000.0
                runtime_version, driver_version = rknn_versions(runner)
                outputs = validate_outputs(runner.infer(np.zeros((1, INPUT_SIZE, INPUT_SIZE, 3), dtype=np.uint8)))
                output_shapes = [str(tuple(np.asarray(value).shape)) for value in outputs]
                if sum(int(np.count_nonzero(value)) for value in outputs) == 0:
                    raise RuntimeError("RKNN YOLOv8 pose outputs are entirely null")
                runner.close()
                runner = None
            with open(args.model, "rb") as model_file:
                model_sha256 = hashlib.sha256(model_file.read()).hexdigest()
            emit({"status": "available", "reason": "YOLOv8n-pose RKNN model loaded and four-output format verified on RK3588",
                  "runtime_version": runtime_version, "driver_version": driver_version,
                  "initialization_ms": round(initialization_ms, 3), "output_shapes": output_shapes,
                  "model_sha256": model_sha256})
            return 0
        except Exception as exc:
            emit({"status": "unavailable", "reason": str(exc)})
            return 2
        finally:
            if runner is not None:
                with silence_native_stdout():
                    runner.close()
    if not args.video:
        emit({"status": "unavailable", "reason": "video path is required"})
        return 2
    try:
        with silence_native_stdout():
            result = run_video(args.video, args.model, max(1, min(args.max_frames, 64)), max(1, min(args.max_rois, 64)))
        emit(result)
        return 0
    except Exception as exc:
        emit({"status": "unavailable", "reason": str(exc)})
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
