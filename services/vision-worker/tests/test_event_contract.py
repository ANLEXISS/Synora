import os
import sys
import tempfile
import unittest

import numpy as np


ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from core.events import ALLOWED_EVENT_TYPES, EventBuilder
from core.pipeline import VisionPipeline
from modules.face.FaceRecognizer import FaceRecognizer
from worker import build_dry_run_event


class DummyMetrics:

    def incr(self, *args, **kwargs):
        return None

    def identity(self, *args, **kwargs):
        return None

    def error(self, *args, **kwargs):
        return None

    def timing(self, *args, **kwargs):
        return None


class DummyDebugStore:

    def append_timeline(self, entry):
        return None


class MockRecognizer:

    match_threshold = 0.35
    uncertain_threshold = 0.20

    def __init__(self, status, identity, score):
        self.status = status
        self.identity = identity
        self.score = score

    def embed(self, face):
        return np.ones(512, dtype=np.float32)

    def identify_embedding(self, embedding):
        return self.status, self.identity, self.score


class MockModelRunner:

    backend = "mock-rknn"

    def infer(self, input_tensor):
        return [np.ones((1, 512), dtype=np.float32)]


def make_pipeline(status="match", identity="alexis", score=0.95, faces=True):
    pipeline = VisionPipeline.__new__(VisionPipeline)
    pipeline.face_recognizer = MockRecognizer(status, identity, score)
    pipeline.events = EventBuilder({
        "camera_id": "cam_01",
        "device_id": "device_01",
        "node_id": "node_01",
        "clip_id": "clip_01",
        "clip_path": "/tmp/clip_01.mp4",
    })
    pipeline.active_tracks_seen = {7}
    pipeline.track_faces = {}
    if faces:
        pipeline.track_faces[7] = [
            (
                1.0,
                np.zeros((112, 112, 3), dtype=np.uint8),
                "trace-1",
            )
        ]
    pipeline.track_identity_memory = {}
    pipeline.track_recognition_buffers = {}
    pipeline.track_last_arcface = {}
    pipeline.frame_id = 1
    pipeline.metrics = DummyMetrics()
    pipeline.debug_store = DummyDebugStore()
    pipeline.debug_step = lambda *args, **kwargs: None
    pipeline.save_debug_recognition = lambda *args, **kwargs: None
    return pipeline


class EventContractTests(unittest.TestCase):

    def assert_contract_event(self, event, event_type):
        self.assertIn(event["type"], ALLOWED_EVENT_TYPES)
        self.assertEqual(event["type"], event_type)
        self.assertEqual(event["camera_id"], "cam_01")
        self.assertEqual(event["device_id"], "device_01")
        self.assertEqual(event["clip_id"], "clip_01")
        self.assertEqual(event["payload"]["camera_id"], "cam_01")
        self.assertEqual(event["payload"]["device_id"], "device_01")
        self.assertEqual(event["payload"]["node_id"], "node_01")
        self.assertEqual(event["payload"]["clip_id"], "clip_01")
        self.assertEqual(event["payload"]["clip_path"], "/tmp/clip_01.mp4")
        self.assertIn("timestamp", event)
        self.assertIn("timestamp", event["payload"])
        self.assertIn("confidence", event["payload"])

    def test_builder_identity_unknown_uncertain_contract_shape(self):
        builder = EventBuilder({
            "camera_id": "cam_01",
            "device_id": "device_01",
            "node_id": "node_01",
            "clip_id": "clip_01",
            "clip_path": "/tmp/clip_01.mp4",
        })

        events = [
            builder.identity("cam_01", "scene_01", 1, "alexis", 0.95, 1),
            builder.unknown("cam_01", "scene_01", 2, 1),
            builder.uncertain("cam_01", "scene_01", 3, "alexis", 0.42, 1),
        ]

        self.assert_contract_event(events[0], "vision.identity")
        self.assert_contract_event(events[1], "vision.unknown")
        self.assert_contract_event(events[2], "vision.uncertain")
        self.assertEqual(events[0]["identity"], "alexis")
        self.assertEqual(events[1]["identity"], "unknown")
        self.assertEqual(events[2]["identity"], "uncertain")

    def test_builder_scene_event_contract_shape(self):
        builder = EventBuilder({
            "camera_id": "cam_01",
            "device_id": "device_01",
            "node_id": "node_01",
            "clip_id": "clip_01",
            "clip_path": "/tmp/clip_01.mp4",
        })

        events = [
            builder.motion("cam_01", "scene_01"),
            builder.weapon("cam_01", "scene_01", 4, 0.77, "firearm"),
            builder.fall("cam_01", "scene_01", 5, 0.88),
            builder.tamper("cam_01", "scene_01"),
        ]

        self.assert_contract_event(events[0], "vision.motion")
        self.assert_contract_event(events[1], "vision.weapon")
        self.assert_contract_event(events[2], "vision.fall")
        self.assert_contract_event(events[3], "vision.tamper")
        self.assertEqual(events[1]["payload"]["weapon_type"], "firearm")
        self.assertTrue(events[2]["payload"]["fall"])
        self.assertTrue(events[3]["payload"]["tamper"])

    def test_dry_run_propagates_clip_id(self):
        event = build_dry_run_event(
            clip_path="/tmp/clip_01.mp4",
            camera_id="cam_01",
            clip_id="clip_01",
            node_id="node_01",
            device_id="device_01",
            event_kind="unknown",
        )

        self.assert_contract_event(event, "vision.unknown")
        self.assertEqual(event["track_id"], "dry-run-track")
        self.assertEqual(event["payload"]["track_id"], "dry-run-track")

    def test_pipeline_run_recognition_identity_with_mock_runner_path(self):
        pipeline = make_pipeline("match", "alexis", 0.95, faces=True)

        events = pipeline.run_recognition("cam_01", scene_id="clip_01")

        self.assertEqual(len(events), 1)
        self.assert_contract_event(events[0], "vision.identity")
        self.assertEqual(events[0]["payload"]["identity"], "alexis")

    def test_pipeline_run_recognition_uncertain(self):
        pipeline = make_pipeline("uncertain", "alexis", 0.30, faces=True)

        events = pipeline.run_recognition("cam_01", scene_id="clip_01")

        self.assertEqual(len(events), 1)
        self.assert_contract_event(events[0], "vision.uncertain")
        self.assertEqual(events[0]["payload"]["best_match"], "alexis")

    def test_pipeline_run_recognition_unknown_without_face(self):
        pipeline = make_pipeline("unknown", None, 0.0, faces=False)

        events = pipeline.run_recognition("cam_01", scene_id="clip_01")

        self.assertEqual(len(events), 1)
        self.assert_contract_event(events[0], "vision.unknown")

    def test_face_recognizer_embed_uses_mock_model_runner(self):
        recognizer = FaceRecognizer.__new__(FaceRecognizer)
        recognizer.runner = MockModelRunner()
        recognizer.embedding_dim = 512

        face = np.zeros((112, 112, 3), dtype=np.uint8)
        embedding = recognizer._embed_raw(face)

        self.assertEqual(embedding.shape, (512,))
        self.assertTrue(np.all(embedding == 1.0))

    def test_missing_arcface_is_unavailable_without_raising(self):
        with tempfile.TemporaryDirectory() as directory:
            recognizer = FaceRecognizer(
                model_path=os.path.join(directory, "missing.rknn"),
                faces_dir=os.path.join(directory, "faces"),
                debug_dir=os.path.join(directory, "debug"),
            )

            self.assertFalse(recognizer.available)
            self.assertEqual(recognizer.capability()["status"], "unavailable")
            self.assertIsNone(recognizer.embed(np.zeros((112, 112, 3), dtype=np.uint8)))


if __name__ == "__main__":
    unittest.main()
