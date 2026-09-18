import sys
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.clip_pipeline_v1 import ClipTrackerV1  # noqa: E402,F401
from tools.vision_inference_benchmark import (  # noqa: E402
    SampledFrame,
    choose_strategy,
    run_single,
    run_three_pinned,
)


class FakeBenchmarkDetector:
    def __init__(self, core_mask=None, debug_enabled=False):
        self.available = True
        self.error = None
        self.core_mask = core_mask

    def detect_timed(self, frame):
        height, width = frame.shape[:2]
        detections = [{"bbox": [10, 10, max(50, width // 3), max(100, height // 2)], "score": .8}]
        timing = {
            "preprocess_ms": .1, "rknn_inference_ms": 2.0,
            "postprocess_ms": .2, "nms_ms": .01, "debug_io_ms": 0.0, "total_ms": 2.31,
        }
        return detections, timing

    def close(self):
        pass


class InferenceBenchmarkTests(unittest.TestCase):
    def samples(self, count=6):
        return [SampledFrame(index, np.zeros((240, 320, 3), dtype=np.uint8), .1) for index in range(count)]

    def test_strategy_selection_requires_fidelity_and_measured_gain(self):
        baseline = {"strategy": "single_all", "detections": 5, "tracks": 2, "clip_total_ms": 100.0, "errors": 0}
        equal = {"strategy": "three_pinned_workers", "detections": 5, "tracks": 2, "clip_total_ms": 95.0, "errors": 0}
        self.assertEqual(choose_strategy([baseline, equal]), "single_all")
        faster = dict(equal, clip_total_ms=80.0)
        self.assertEqual(choose_strategy([baseline, faster]), "three_pinned_workers")

    def test_mocked_single_and_three_worker_reports_preserve_tracks(self):
        frames = self.samples()
        with patch("tools.vision_inference_benchmark.PersonDetector", FakeBenchmarkDetector):
            single = run_single(frames, "single_all", "all")
            parallel = run_three_pinned(frames, {"single_core_0": 0, "single_core_1": 1, "single_core_2": 2})
        self.assertEqual(single["detections"], len(frames))
        self.assertEqual(parallel["detections"], len(frames))
        self.assertEqual(single["tracks"], 1)
        self.assertEqual(parallel["tracks"], 1)
        self.assertEqual(parallel["errors"], 0)


if __name__ == "__main__":
    unittest.main()
