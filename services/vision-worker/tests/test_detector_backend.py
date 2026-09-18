import sys
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.detector_backend import ExistingDetectorBackend, ThreePinnedDetectorBackend


class FakeDetector:
    available = True
    model_path = "/var/lib/synora/models/yolov8.rknn"
    input_size = 640
    conf_threshold = 0.40
    nms_threshold = 0.45

    class Runner:
        backend = "rknn"

    runner = Runner()

    def __init__(self, result=None, delay=0.0):
        self.result = result if result is not None else []
        self.delay = delay

    def detect(self, _frame):
        if self.delay:
            time.sleep(self.delay)
        if callable(self.result):
            return self.result(_frame)
        return self.result


class DetectorBackendTests(unittest.TestCase):
    def test_available_detector_normalizes_humans_and_counts_other_classes(self):
        backend = ExistingDetectorBackend(FakeDetector([
            {"bbox": [1, 2, 41, 82], "score": .91},
            {"bbox": [3, 4, 43, 84], "class_id": 1, "confidence": .8},
        ]))
        try:
            detections = backend.detect(object(), 120)
            self.assertEqual(detections, [{
                "class": "human", "bbox": [1, 2, 41, 82], "confidence": .91,
                "frame_timestamp_ms": 120, "backend": "existing_detector", "model_version": "yolov8.rknn",
            }])
            diagnostic = backend.diagnostic()
            self.assertTrue(diagnostic["real_model"])
            self.assertEqual(diagnostic["status"], "ok")
            self.assertEqual(diagnostic["non_human_ignored"], 1)
            self.assertNotIn("frame", diagnostic)
            self.assertNotIn("crop", detections[0])
        finally:
            backend.close()

    def test_unavailable_detector_is_explicit(self):
        detector = FakeDetector()
        detector.available = False
        backend = ExistingDetectorBackend(detector)
        try:
            self.assertEqual(backend.detect(object(), 0), [])
            diagnostic = backend.diagnostic()
            self.assertFalse(diagnostic["real_model"])
            self.assertEqual(diagnostic["status"], "unavailable")
            self.assertEqual(diagnostic["error_code"], "backend_unavailable")
        finally:
            backend.close()

    def test_timeout_is_explicit_and_does_not_fabricate_detection(self):
        backend = ExistingDetectorBackend(FakeDetector([
            {"bbox": [1, 2, 41, 82], "score": .91},
        ], delay=.08), timeout_seconds=.01)
        try:
            self.assertEqual(backend.detect(object(), 0), [])
            diagnostic = backend.diagnostic()
            self.assertFalse(diagnostic["real_model"])
            self.assertEqual(diagnostic["status"], "timeout")
            self.assertEqual(diagnostic["error_code"], "inference_timeout")
        finally:
            backend.close()

    def test_invalid_class_is_not_relabelled_as_human(self):
        backend = ExistingDetectorBackend(FakeDetector([
            {"class": "vehicle", "bbox": [1, 2, 41, 82], "confidence": .91},
        ]))
        try:
            self.assertEqual(backend.detect(object(), 0), [])
            self.assertEqual(backend.diagnostic()["non_human_ignored"], 1)
        finally:
            backend.close()

    def test_three_pinned_backend_keeps_batch_order_and_aggregates_diagnostics(self):
        detector = FakeDetector(lambda frame: [{"bbox": [frame, 0, frame + 30, 30], "score": .9}])
        backend = ThreePinnedDetectorBackend([detector, detector, detector])
        try:
            batches = backend.detect_many([10, 20, 30], [100, 200, 300])
            self.assertEqual([item[0]["bbox"] for item in batches], [[10, 0, 40, 30], [20, 0, 50, 30], [30, 0, 60, 30]])
            diagnostic = backend.diagnostic()
            self.assertEqual(diagnostic["frames_sampled"], 3)
            self.assertEqual(diagnostic["detections_total"], 3)
            self.assertTrue(diagnostic["real_model"])
            self.assertEqual(diagnostic["status"], "ok")
        finally:
            backend.close()


if __name__ == "__main__":
    unittest.main()
