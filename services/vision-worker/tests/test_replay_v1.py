import importlib.util
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np


MODULE_PATH = Path(__file__).parents[1] / "replay_v1.py"


class FakeCapture:
    def __init__(self, _path, opened=True, fps=5.0, frames=5):
        self.opened = opened
        self.fps = fps
        self.frames = frames
        self.released = False

    def isOpened(self):
        return self.opened

    def get(self, prop):
        return self.fps if prop == 5 else self.frames

    def release(self):
        self.released = True


class ReplayValidationTests(unittest.TestCase):
    def load_module(self, capture_factory):
        fake_cv2 = types.SimpleNamespace(
            CAP_PROP_FPS=5,
            CAP_PROP_FRAME_COUNT=7,
            VideoCapture=capture_factory,
        )
        spec = importlib.util.spec_from_file_location("replay_v1_test_module", MODULE_PATH)
        module = importlib.util.module_from_spec(spec)
        with patch.dict(sys.modules, {"cv2": fake_cv2}):
            spec.loader.exec_module(module)
        return module

    def test_valid_fixture_metadata(self):
        module = self.load_module(lambda path: FakeCapture(path))
        with tempfile.NamedTemporaryFile(suffix=".mp4") as fixture:
            metadata = module.inspect_clip(Path(fixture.name), 10.0)
        self.assertEqual(metadata["frames"], 5)
        self.assertEqual(metadata["duration_seconds"], 1.0)

    def test_invalid_fixture_is_rejected(self):
        module = self.load_module(lambda path: FakeCapture(path, fps=0.0))
        with tempfile.NamedTemporaryFile(suffix=".mp4") as fixture:
            with self.assertRaisesRegex(ValueError, "positive FPS"):
                module.inspect_clip(Path(fixture.name), 10.0)

    def test_unsupported_format_is_rejected_before_capture(self):
        module = self.load_module(lambda path: self.fail("capture should not start"))
        with tempfile.NamedTemporaryFile(suffix=".txt") as fixture:
            with self.assertRaisesRegex(ValueError, "unsupported clip format"):
                module.inspect_clip(Path(fixture.name), 10.0)

    def test_minimal_encoded_fixture_is_accepted(self):
        import cv2

        with tempfile.NamedTemporaryFile(suffix=".mp4") as fixture:
            writer = cv2.VideoWriter(fixture.name, cv2.VideoWriter_fourcc(*"mp4v"), 5.0, (32, 32))
            if not writer.isOpened():
                self.skipTest("mp4 encoder unavailable")
            for _ in range(5):
                writer.write(np.zeros((32, 32, 3), dtype=np.uint8))
            writer.release()
            spec = importlib.util.spec_from_file_location("replay_v1_real_test_module", MODULE_PATH)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            metadata = module.inspect_clip(Path(fixture.name), 10.0)
        self.assertEqual(metadata["frames"], 5)
        self.assertGreater(metadata["duration_seconds"], 0.0)


if __name__ == "__main__":
    unittest.main()
