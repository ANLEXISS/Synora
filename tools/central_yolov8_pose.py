#!/usr/bin/env python3
"""Local YOLOv8n-pose RKNN adapter for the central media harness.

The process boundary is deliberately aggregate-only: frames, crops, boxes and
keypoints are kept in this process and are never written to stdout, the bus or
the Universal Store.  This is an optional P3 backend, not a second test
runner, and it has no CPU or YOLO-pose fallback.
"""

import argparse
import json
import os
import sys
import time

import cv2
import numpy as np

from core.model_runner import ModelUnavailableError, create_model_runner


INPUT_SIZE = 640
CONFIDENCE_THRESHOLD = 0.25
KEYPOINT_COUNT = 17


def emit(value):
    sys.stdout.write(json.dumps(value, sort_keys=True) + "\n")


def load_backend(model_path):
    if not model_path or not model_path.lower().endswith(".rknn"):
        raise ModelUnavailableError("invalid_model", model_path, "YOLOv8n-pose RKNN model is required")
    return create_model_runner(model_path, core_mask=None, input_data_format="nhwc")


def preprocess(frame):
    height, width = frame.shape[:2]
    scale = min(INPUT_SIZE / width, INPUT_SIZE / height)
    resized_width = max(1, int(round(width * scale)))
    resized_height = max(1, int(round(height * scale)))
    resized = cv2.resize(frame, (resized_width, resized_height), interpolation=cv2.INTER_LINEAR)
    canvas = np.zeros((INPUT_SIZE, INPUT_SIZE, 3), dtype=np.uint8)
    pad_x = (INPUT_SIZE - resized_width) // 2
    pad_y = (INPUT_SIZE - resized_height) // 2
    canvas[pad_y:pad_y + resized_height, pad_x:pad_x + resized_width] = resized
    tensor = canvas[:, :, ::-1].astype(np.float32) / 255.0
    return np.ascontiguousarray(tensor[None, ...]), (scale, pad_x, pad_y, width, height)


def normalize_output(outputs):
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
    # YOLOv8 pose is [1, 56, N] or [1, N, 56] for one class and 17 x 3
    # keypoint values.  Refuse unknown layouts instead of guessing.
    if array.shape[0] == 56:
        array = array.transpose()
    if array.shape[1] != 56:
        return None
    return array


def as_probability(values):
    values = np.asarray(values, dtype=np.float32)
    if values.size and np.nanmin(values) >= 0.0 and np.nanmax(values) <= 1.0:
        return values
    return 1.0 / (1.0 + np.exp(-np.clip(values, -60.0, 60.0)))


def decode_best(outputs, meta):
    rows = normalize_output(outputs)
    if rows is None:
        return None
    rows = rows[np.all(np.isfinite(rows), axis=1)]
    if rows.size == 0:
        return None
    scores = as_probability(rows[:, 4])
    index = int(np.argmax(scores))
    if float(scores[index]) < CONFIDENCE_THRESHOLD:
        return None
    row = rows[index].astype(np.float64, copy=True)
    scale, pad_x, pad_y, width, height = meta
    box = row[:4]
    if np.max(np.abs(box)) <= 2.0:
        box *= INPUT_SIZE
    x1 = (box[0] - box[2] / 2.0 - pad_x) / scale
    y1 = (box[1] - box[3] / 2.0 - pad_y) / scale
    x2 = (box[0] + box[2] / 2.0 - pad_x) / scale
    y2 = (box[1] + box[3] / 2.0 - pad_y) / scale
    x1, x2 = np.clip((x1, x2), 0.0, width)
    y1, y2 = np.clip((y1, y2), 0.0, height)
    keypoints = row[5:].reshape(KEYPOINT_COUNT, 3)
    if np.max(np.abs(keypoints[:, :2])) <= 2.0:
        keypoints[:, :2] *= INPUT_SIZE
    keypoints[:, 0] = (keypoints[:, 0] - pad_x) / scale
    keypoints[:, 1] = (keypoints[:, 1] - pad_y) / scale
    keypoints[:, 0] = np.clip(keypoints[:, 0], 0.0, width)
    keypoints[:, 1] = np.clip(keypoints[:, 1], 0.0, height)
    keypoints[:, 2] = as_probability(keypoints[:, 2])
    return {
        "bbox": (float(x1), float(y1), float(x2), float(y2)),
        "keypoints": keypoints,
        "score": float(scores[index]),
    }


def posture_from_pose(pose):
    if pose is None:
        return "unknown"
    x1, y1, x2, y2 = pose["bbox"]
    width = max(1.0, x2 - x1)
    height = max(1.0, y2 - y1)
    if height / width < 1.05:
        return "ground"
    points = pose["keypoints"]
    visible = points[:, 2] >= 0.25
    if int(np.count_nonzero(visible)) < 5:
        return "unknown"
    hip_y = float(np.mean(points[[11, 12], 1]))
    knee_y = float(np.mean(points[[13, 14], 1]))
    shoulder_y = float(np.mean(points[[5, 6], 1]))
    if knee_y - hip_y > height * 0.12 and height / width < 1.7:
        return "seated"
    if shoulder_y <= hip_y and height / width >= 1.25:
        return "upright"
    return "ground" if height / width < 1.25 else "unknown"


def run_video(video_path, model_path, max_frames):
    runner = load_backend(model_path)
    capture = cv2.VideoCapture(video_path)
    if not capture.isOpened():
        runner.close()
        raise RuntimeError("video could not be decoded")
    frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    if frame_count <= 0:
        capture.release()
        runner.close()
        raise RuntimeError("video has no readable frames")
    stride = max(1, int(np.ceil(frame_count / max_frames)))
    previous_center = None
    previous_posture = None
    previous_time = None
    postures = []
    confidences = []
    immobility = 0.0
    rapid = False
    fall_candidate = False
    total_latency = 0.0
    processed = 0
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
            started = time.perf_counter()
            tensor, meta = preprocess(frame)
            outputs = runner.infer(tensor)
            pose = decode_best(outputs, meta)
            total_latency += (time.perf_counter() - started) * 1000.0
            processed += 1
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
            if previous_posture in {"upright", "seated"} and posture == "ground":
                fall_candidate = True
            previous_center = center
            previous_posture = posture
            previous_time = now
    finally:
        capture.release()
        runner.close()
    if not postures:
        return {
            "pose_status": "low_quality",
            "posture": "unknown",
            "immobility_seconds": 0.0,
            "fall_state": "uncertain",
            "rapid_motion_state": "unknown",
            "physical_interaction_candidate": False,
            "confidence": 0.0,
            "latency_ms": round(total_latency / max(processed, 1), 3),
        }
    counts = {value: postures.count(value) for value in set(postures)}
    posture = sorted(counts, key=lambda value: (-counts[value], value))[0]
    return {
        "pose_status": "available",
        "posture": posture,
        "immobility_seconds": round(min(120.0, max(0.0, immobility)), 3),
        "fall_state": "candidate" if fall_candidate else "none",
        "rapid_motion_state": "rapid" if rapid else "none",
        "physical_interaction_candidate": False,
        "confidence": round(float(np.mean(confidences)), 4),
        "latency_ms": round(total_latency / max(processed, 1), 3),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--diagnostic", action="store_true")
    parser.add_argument("--model", required=True)
    parser.add_argument("--video")
    parser.add_argument("--max-frames", type=int, default=32)
    args = parser.parse_args()
    if args.diagnostic:
        runner = None
        try:
            runner = load_backend(args.model)
            probe = np.zeros((1, INPUT_SIZE, INPUT_SIZE, 3), dtype=np.float32)
            outputs = runner.infer(probe)
            if normalize_output(outputs) is None:
                raise RuntimeError("RKNN output is not YOLOv8n-pose with 17 body keypoints")
            emit({"status": "available", "reason": "YOLOv8n-pose RKNN model loaded and output format verified on the available RKNN runtime"})
            return 0
        except Exception as exc:  # no raw model outputs are emitted
            emit({"status": "unavailable", "reason": str(exc)})
            return 2
        finally:
            if runner is not None:
                runner.close()
    if not args.video:
        emit({"status": "unavailable", "reason": "video path is required"})
        return 2
    try:
        emit(run_video(args.video, args.model, max(1, min(args.max_frames, 64))))
        return 0
    except Exception as exc:
        emit({"status": "unavailable", "reason": str(exc)})
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
