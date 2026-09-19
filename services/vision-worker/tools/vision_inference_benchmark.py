#!/usr/bin/env python3
"""Reproducible local ONNX/RKNN parity and NPU strategy benchmark."""

from __future__ import annotations

import argparse
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import queue
import statistics
import threading
import time
from typing import Any

import cv2
import numpy as np

from core.clip_pipeline_v1 import ClipTrackerV1
from modules.detect.person_detector import PersonDetector


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_RKNN = Path("/var/lib/synora/models/yolov8.rknn")
DEFAULT_ONNX = Path("/home/rock/SynoraBis/models/yolov8n.onnx")


@dataclass
class SampledFrame:
    index: int
    frame: np.ndarray
    decode_ms: float


def sample_frames(path: Path, count: int) -> tuple[list[SampledFrame], dict[str, Any]]:
    capture = cv2.VideoCapture(str(path))
    if not capture.isOpened():
        raise RuntimeError(f"clip could not be opened: {path}")
    total = int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    fps = float(capture.get(cv2.CAP_PROP_FPS) or 0.0)
    if total <= 0 or fps <= 0:
        capture.release()
        raise RuntimeError("clip must have positive frame count and FPS")
    targets = set(np.linspace(0, total - 1, min(count, total), dtype=int).tolist())
    samples: list[SampledFrame] = []
    index = 0
    try:
        while targets:
            started = time.perf_counter()
            ok, frame = capture.read()
            elapsed = (time.perf_counter() - started) * 1000.0
            if not ok:
                break
            if index in targets:
                samples.append(SampledFrame(index, frame, elapsed))
                targets.remove(index)
            index += 1
    finally:
        capture.release()
    if len(samples) < min(count, total):
        raise RuntimeError(f"only decoded {len(samples)} of {min(count, total)} requested frames")
    return samples, {"fps": fps, "frame_count": total, "duration_seconds": total / fps}


def _core_masks() -> dict[str, Any]:
    from rknnlite.api import RKNNLite

    return {
        "single_all": getattr(RKNNLite, "NPU_CORE_0_1_2", RKNNLite.NPU_CORE_ALL),
        "single_core_0": RKNNLite.NPU_CORE_0,
        "single_core_1": RKNNLite.NPU_CORE_1,
        "single_core_2": RKNNLite.NPU_CORE_2,
    }


def _track_count(results: list[tuple[int, list[dict[str, Any]], dict[str, float]]]) -> int:
    tracker = ClipTrackerV1()
    start = datetime(2026, 1, 1, tzinfo=timezone.utc)
    for position, (_, detections, _) in enumerate(sorted(results)):
        tracker.update(detections, start + timedelta(milliseconds=200 * position))
    return len(tracker.active_track_ids)


def _metrics(name: str, samples: list[SampledFrame], results, wall_ms: float, errors: int = 0) -> dict[str, Any]:
    timings = [timing for _, _, timing in results]
    inference = [item["rknn_inference_ms"] for item in timings]
    total = [item["total_ms"] for item in timings]
    sums = {key: sum(item[key] for item in timings) for key in (
        "preprocess_ms", "rknn_inference_ms", "postprocess_ms", "nms_ms", "debug_io_ms", "total_ms"
    )}
    return {
        "strategy": name,
        "frames": len(samples),
        "median_inference_ms": round(statistics.median(inference), 3),
        "p95_inference_ms": round(float(np.percentile(inference, 95)), 3),
        "median_total_frame_ms": round(statistics.median(total), 3),
        "clip_total_ms": round(wall_ms, 3),
        "decode_ms": round(sum(sample.decode_ms for sample in samples), 3),
        "preprocess_ms": round(sums["preprocess_ms"], 3),
        "rknn_inference_ms": round(sums["rknn_inference_ms"], 3),
        "postprocess_ms": round(sums["postprocess_ms"], 3),
        "nms_ms": round(sums["nms_ms"], 3),
        "debug_io_ms": round(sums["debug_io_ms"], 3),
        "detections": sum(len(detections) for _, detections, _ in results),
        "tracks": _track_count(results),
        "errors": errors,
    }


def run_single(samples: list[SampledFrame], strategy: str, core_mask: Any) -> dict[str, Any]:
    detector = PersonDetector(core_mask=core_mask, debug_enabled=False)
    if not detector.available:
        raise RuntimeError(detector.error or f"{strategy} unavailable")
    results = []
    errors = 0
    started = time.perf_counter()
    try:
        for sample in samples:
            try:
                detections, timing = detector.detect_timed(sample.frame)
            except Exception:
                detections, timing = [], {key: 0.0 for key in (
                    "preprocess_ms", "rknn_inference_ms", "postprocess_ms", "nms_ms", "debug_io_ms", "total_ms"
                )}
                errors += 1
            results.append((sample.index, detections, timing))
    finally:
        detector.close()
    return _metrics(strategy, samples, results, (time.perf_counter() - started) * 1000.0, errors)


def run_three_pinned(samples: list[SampledFrame], masks: dict[str, Any]) -> dict[str, Any]:
    detectors = []
    try:
        for index in range(3):
            detector = PersonDetector(core_mask=masks[f"single_core_{index}"], debug_enabled=False)
            if not detector.available:
                raise RuntimeError(detector.error or f"core {index} unavailable")
            detectors.append(detector)
    except Exception:
        for detector in detectors:
            detector.close()
        raise

    tasks: queue.Queue = queue.Queue(maxsize=3)
    completed: queue.Queue = queue.Queue()

    def worker(detector):
        while True:
            item = tasks.get()
            if item is None:
                tasks.task_done()
                return
            index, frame = item
            try:
                completed.put((index, *detector.detect_timed(frame), None))
            except Exception as exc:
                completed.put((index, [], {}, exc))
            finally:
                tasks.task_done()

    threads = [threading.Thread(target=worker, args=(detector,), daemon=True) for detector in detectors]
    for thread in threads:
        thread.start()
    results = []
    errors = 0
    next_index = 0
    in_flight = 0
    started = time.perf_counter()
    try:
        while next_index < len(samples) or in_flight:
            while next_index < len(samples) and in_flight < 3:
                sample = samples[next_index]
                tasks.put((sample.index, sample.frame))
                next_index += 1
                in_flight += 1
            index, detections, timing, error = completed.get()
            in_flight -= 1
            if error is not None:
                errors += 1
                timing = {key: 0.0 for key in (
                    "preprocess_ms", "rknn_inference_ms", "postprocess_ms", "nms_ms", "debug_io_ms", "total_ms"
                )}
            results.append((index, detections, timing))
    finally:
        for _ in threads:
            tasks.put(None)
        for thread in threads:
            thread.join(timeout=5.0)
        for detector in detectors:
            detector.close()
    return _metrics("three_pinned_workers", samples, results, (time.perf_counter() - started) * 1000.0, errors)


def choose_strategy(results: list[dict[str, Any]]) -> str:
    baseline = next(item for item in results if item["strategy"] == "single_all")
    candidates = [
        item for item in results
        if item["errors"] == 0
        and item["detections"] >= baseline["detections"]
        and item["tracks"] >= baseline["tracks"]
        and item["clip_total_ms"] < baseline["clip_total_ms"] * 0.90
    ]
    if not candidates:
        return "single_all"
    return min(candidates, key=lambda item: item["clip_total_ms"])["strategy"]


def run_onnx_parity(samples: list[SampledFrame], onnx_path: Path) -> dict[str, Any]:
    try:
        import onnxruntime as ort
    except ImportError as exc:
        return {"status": "unavailable", "error": f"onnxruntime is required: {exc}"}
    try:
        session = ort.InferenceSession(str(onnx_path), providers=["CPUExecutionProvider"])
    except Exception as exc:
        return {"status": "failed", "error": str(exc)}
    decoder = PersonDetector.__new__(PersonDetector)
    decoder.input_size = 640
    decoder.conf_threshold = 0.40
    decoder.nms_threshold = 0.45
    decoder.canvas = np.zeros((640, 640, 3), dtype=np.uint8)
    decoder.debug_enabled = False
    decoder.debug_frames = 0
    decoder.debug_max_frames = 0
    decoder.debug_counter = 0
    rknn = PersonDetector(debug_enabled=False)
    if not rknn.available:
        return {"status": "unavailable", "error": rknn.error or "RKNN unavailable"}
    overlaps = []
    frame_rows = []
    input_name = session.get_inputs()[0].name
    try:
        for sample in samples:
            image, meta = decoder.preprocess(sample.frame)
            onnx_input = np.transpose(image, (0, 3, 1, 2))
            started = time.perf_counter()
            output = session.run(None, {input_name: onnx_input})[0]
            onnx_ms = (time.perf_counter() - started) * 1000.0
            rows = decoder._normalize_outputs([output])
            onnx_detections = decoder._decode_candidates(rows, meta)
            onnx_detections = decoder._apply_nms(sample.frame, onnx_detections)
            rknn_detections = rknn.detect(sample.frame)
            pair_ious = [
                max((_iou(item["bbox"], other["bbox"]) for other in onnx_detections), default=0.0)
                for item in rknn_detections
            ]
            overlaps.extend(pair_ious)
            frame_rows.append({
                "frame": sample.index,
                "onnx_detections": len(onnx_detections),
                "rknn_detections": len(rknn_detections),
                "best_iou": round(max(pair_ious, default=0.0), 4),
                "onnx_ms": round(onnx_ms, 3),
            })
    finally:
        rknn.close()
    return {
        "status": "ok",
        "sampled_frames": len(samples),
        "frames_with_humans": sum(item["onnx_detections"] > 0 for item in frame_rows),
        "mean_best_iou": round(statistics.mean(overlaps), 4) if overlaps else 0.0,
        "frames_iou_over_0_2": sum(item["best_iou"] >= 0.2 for item in frame_rows),
        "frame_results": frame_rows,
    }


def _iou(left, right):
    x1, y1 = max(left[0], right[0]), max(left[1], right[1])
    x2, y2 = min(left[2], right[2]), min(left[3], right[3])
    intersection = max(0, x2 - x1) * max(0, y2 - y1)
    union = ((left[2] - left[0]) * (left[3] - left[1])
             + (right[2] - right[0]) * (right[3] - right[1]) - intersection)
    return intersection / union if union else 0.0


def write_fixture(path: Path, sample: SampledFrame, detections: list[dict[str, Any]]):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({
        "source": "/home/rock/test3.mp4",
        "frame_index": sample.index,
        "minimum_humans": 2,
        "minimum_iou": 0.20,
        "expected_boxes": [list(item["bbox"]) for item in detections[:2]],
    }, indent=2) + "\n", encoding="utf-8")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--clip", type=Path, default=Path("/home/rock/test3.mp4"))
    parser.add_argument("--onnx", type=Path, default=DEFAULT_ONNX)
    parser.add_argument("--out", type=Path, default=ROOT / "out" / "vision-inference-performance-v1")
    parser.add_argument("--frames", type=int, default=50)
    parser.add_argument("--fixture", type=Path, default=ROOT / "testdata" / "vision" / "test3.fixture.json")
    parser.add_argument("--write-fixture", action="store_true")
    parser.add_argument("--skip-onnx", action="store_true")
    args = parser.parse_args(argv)
    samples, clip = sample_frames(args.clip, args.frames)
    masks = _core_masks()
    results = [
        run_single(samples, "single_all", masks["single_all"]),
        run_single(samples, "single_core_0", masks["single_core_0"]),
        run_single(samples, "single_core_1", masks["single_core_1"]),
        run_single(samples, "single_core_2", masks["single_core_2"]),
    ]
    try:
        results.append(run_three_pinned(samples, masks))
    except Exception as exc:
        results.append({"strategy": "three_pinned_workers", "frames": len(samples), "errors": 1, "error": str(exc)})
    chosen = choose_strategy([item for item in results if "clip_total_ms" in item])

    fixture_expected = None
    if args.fixture.is_file():
        fixture_expected = json.loads(args.fixture.read_text(encoding="utf-8"))
    fixture_index = int(fixture_expected.get("frame_index", 150)) if fixture_expected else 150
    fixture_sample = min(samples, key=lambda item: abs(item.index - fixture_index))
    if fixture_expected and not args.write_fixture and fixture_sample.index != fixture_index:
        raise SystemExit(f"fixture frame {fixture_index} was not sampled; increase --frames")
    fixture_detector = PersonDetector(core_mask=masks["single_all"], debug_enabled=False)
    fixture_detections = fixture_detector.detect(fixture_sample.frame)
    fixture_detector.close()
    if args.write_fixture:
        write_fixture(args.fixture, fixture_sample, fixture_detections)
    fixture_status = {"status": "not_requested"}
    if args.fixture.is_file():
        expected = fixture_expected
        fixture_status = {
            "status": "ok" if len(fixture_detections) >= int(expected["minimum_humans"]) else "failed",
            "frame_index": fixture_sample.index,
            "detections": len(fixture_detections),
            "expected_boxes": expected.get("expected_boxes", []),
        }
        if fixture_status["status"] != "ok":
            raise SystemExit("fixture expectation failed: fewer than two humans")

    report = {
        "clip": {"path": str(args.clip), **clip},
        "preprocessing": {"source_layout": "NCHW", "rknn_runtime_layout": "NHWC", "color": "BGR->RGB", "normalization": "float32 / 255", "letterbox": "centered"},
        "parity": None if args.skip_onnx else run_onnx_parity(samples, args.onnx),
        "strategies": results,
        "selected_strategy": chosen,
        "fixture": fixture_status,
		"runtime_boundary": {"cognitive_mode": "active_dry_run", "physical_action_executed": False},
    }
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "benchmark.json").write_text(json.dumps(report, indent=2, default=str) + "\n", encoding="utf-8")
    (args.out / "benchmark.md").write_text(
        "# Vision inference benchmark\n\n"
        f"- selected strategy: `{chosen}`\n"
        f"- frames: `{len(samples)}`\n"
        f"- fixture: `{fixture_status['status']}`\n"
		"- cognitive mode: `active_dry_run`\n"
        "- physical action executed: `false`\n",
        encoding="utf-8",
    )
    print(json.dumps(report, indent=2, default=str))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
