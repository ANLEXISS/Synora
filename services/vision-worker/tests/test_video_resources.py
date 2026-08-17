import os
import sys
import unittest
from unittest import mock

import numpy as np


ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from core.pipeline import VisionPipeline
from video.clip_reader import ClipReader


class FakeCapture:

    def __init__(self, reads, opened=True):
        self.reads = iter(reads)
        self.opened = opened
        self.released = False

    def isOpened(self):
        return self.opened

    def get(self, _property):
        return 25.0

    def read(self):
        value = next(self.reads)
        if isinstance(value, BaseException):
            raise value
        return value

    def release(self):
        self.released = True


class VideoResourceTests(unittest.TestCase):

    def test_clip_reader_releases_capture_when_read_fails(self):
        capture = FakeCapture([RuntimeError("decode failed")])

        with mock.patch("video.clip_reader.cv2.VideoCapture", return_value=capture):
            with self.assertRaisesRegex(RuntimeError, "decode failed"):
                ClipReader().read("clip.mp4")

        self.assertTrue(capture.released)

    def test_pipeline_releases_capture_when_frame_processing_fails(self):
        capture = FakeCapture([(True, np.zeros((2, 2, 3), dtype=np.uint8))])
        pipeline = VisionPipeline.__new__(VisionPipeline)
        pipeline.events = mock.Mock()
        pipeline.metrics = mock.Mock()
        pipeline.tracker = mock.Mock()
        pipeline.track_faces = {}
        pipeline.track_identity_memory = {}
        pipeline.track_recognition_buffers = {}
        pipeline.track_last_arcface = {}
        pipeline.last_detections = []
        pipeline.active_tracks_seen = set()
        pipeline.person_seen = False
        pipeline.last_yolo_frame = -999999
        pipeline.process_person_frame = mock.Mock(
            side_effect=RuntimeError("frame processing failed")
        )

        with mock.patch("core.pipeline.cv2.VideoCapture", return_value=capture):
            with self.assertRaisesRegex(RuntimeError, "frame processing failed"):
                pipeline.process_clip("clip.mp4", "camera-1")

        self.assertTrue(capture.released)


if __name__ == "__main__":
    unittest.main()
