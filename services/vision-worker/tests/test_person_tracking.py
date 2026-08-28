import os
import sys
import unittest
from unittest import mock

import numpy as np

ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from core.pipeline import VisionPipeline
from modules.detect.person_detector import PersonDetector
from modules.tracking.tracker import Tracker


class FakeRunner:
    backend = "fake-rknn"

    def __init__(self, outputs):
        self.outputs = outputs

    def infer(self, _tensor):
        return self.outputs


def detector_with_outputs(outputs):
    detector = PersonDetector.__new__(PersonDetector)
    detector.available = True
    detector.runner = FakeRunner(outputs)
    detector.input_size = 640
    detector.canvas = np.zeros((640, 640, 3), dtype=np.uint8)
    detector.conf_threshold = 0.40
    detector.nms_threshold = 0.45
    detector.debug_counter = 0
    detector.save_person_roi = mock.Mock()
    detector.save_detection_frame = mock.Mock()
    return detector


class PersonDetectorTests(unittest.TestCase):

    def test_single_numpy_detection_and_transposed_output(self):
        row = np.array([[0.50, 0.50, 0.40, 0.80, 0.95, 0.05]], dtype=np.float32)
        for outputs in (row, row.transpose()):
            detector = detector_with_outputs(outputs)
            detections = detector.detect(np.zeros((240, 320, 3), dtype=np.uint8))
            self.assertEqual(len(detections), 1)
            self.assertGreaterEqual(detections[0]["score"], 0.94)
            x1, y1, x2, y2 = detections[0]["bbox"]
            self.assertEqual((x1, y1), (95, 0))
            self.assertEqual((x2, y2), (224, 240))

    def test_multiple_persons_nms_and_non_person_are_deterministic(self):
        outputs = np.array([
            [320, 320, 256, 512, 0.95, 0.05],
            [322, 322, 256, 512, 0.90, 0.10],
            [520, 320, 120, 240, 0.92, 0.08],
            [100, 100, 100, 200, 0.05, 0.95],
        ], dtype=np.float32)
        detector = detector_with_outputs(outputs)
        detections = detector.detect(np.zeros((240, 640, 3), dtype=np.uint8))
        self.assertEqual(len(detections), 2)
        self.assertGreater(detections[0]["score"], detections[1]["score"])
        self.assertEqual(detections[0]["bbox"], (192, 0, 448, 240))

    def test_empty_and_malformed_model_outputs_fail_closed(self):
        for outputs in ([], 1.0, np.empty((2, 3), dtype=np.float32), np.array([[np.nan] * 6])):
            detector = detector_with_outputs(outputs)
            self.assertEqual(detector.detect(np.zeros((240, 320, 3), dtype=np.uint8)), [])


class TrackerTests(unittest.TestCase):

    def test_track_survives_short_loss_and_is_visible_only_when_observed(self):
        tracker = Tracker()
        first = tracker.update([[10, 10, 100, 200]])
        self.assertEqual([track.id for track in first], [0])
        self.assertEqual([track.id for track in tracker.visible_tracks()], [0])

        lost = tracker.update([])
        self.assertEqual([track.id for track in lost], [0])
        self.assertEqual(tracker.visible_tracks(), [])

        recovered = tracker.update([[12, 12, 102, 202]])
        self.assertEqual([track.id for track in tracker.visible_tracks()], [0])
        self.assertEqual(recovered[0].missed, 0)

    def test_invalid_boxes_are_ignored_without_creating_tracks(self):
        tracker = Tracker()
        tracks = tracker.update([
            [10, 10, 10, 20],
            [10, 10, 100, 200, 1],
            [np.nan, 0, 10, 10],
        ])
        self.assertEqual(tracks, [])


class FrameNormalizationTests(unittest.TestCase):

    def test_grayscale_alpha_and_float_frames_are_normalized(self):
        gray = VisionPipeline.normalize_frame(np.zeros((20, 30), dtype=np.uint8))
        alpha = VisionPipeline.normalize_frame(np.zeros((20, 30, 4), dtype=np.uint8))
        floating = VisionPipeline.normalize_frame(np.full((20, 30, 3), 300.0, dtype=np.float32))
        self.assertEqual(gray.shape, (20, 30, 3))
        self.assertEqual(alpha.shape, (20, 30, 3))
        self.assertEqual(floating.dtype, np.uint8)
        self.assertEqual(int(floating[0, 0, 0]), 255)
        with self.assertRaises(ValueError):
            VisionPipeline.normalize_frame(np.zeros((0, 20, 3), dtype=np.uint8))


if __name__ == "__main__":
    unittest.main()
