import json
import tempfile
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np


ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))
from core import yolov8_pose_backend as MODULE  # noqa: E402
from core import model_runner as MODEL_RUNNER  # noqa: E402


class CentralYOLOPoseTests(unittest.TestCase):
    def test_synthetic_output_decodes_to_aggregate_only(self):
        outputs = []
        for height, width in MODULE.BRANCH_SHAPES:
            head = np.full((1, 65, height, width), -10.0, dtype=np.float32)
            head[0, 64, height // 2, width // 2] = 10.0
            outputs.append(head)
        keypoints = np.ones((1, 17, 3, 8400), dtype=np.float32)
        keypoints[:, :, 0, :] = 320.0
        keypoints[:, :, 1, :] = 320.0
        keypoints[:, :, 2, :] = 0.95
        outputs.append(keypoints)
        pose = MODULE.decode_best(outputs, (1.0, 0, 0, 640, 640))
        self.assertIsNotNone(pose)
        aggregate = {
            "pose_status": "available",
            "posture": MODULE.posture_from_pose(pose),
            "fall_state": "candidate",
            "physical_interaction_candidate": False,
        }
        encoded = json.dumps(aggregate, sort_keys=True).lower()
        for forbidden in ("keypoint", "bbox", "image", "crop", "embedding", "identity", "local_track_id"):
            self.assertNotIn(forbidden, encoded)

    def test_unknown_output_layout_is_rejected(self):
        with self.assertRaises(RuntimeError):
            MODULE.validate_outputs([np.zeros((1, 84, 10), dtype=np.float32)])

    def test_preprocess_matches_validated_rknn_input(self):
        tensor, _ = MODULE.letterbox(np.zeros((180, 320, 3), dtype=np.uint8))
        self.assertEqual(tensor.shape, (1, 640, 640, 3))
        self.assertEqual(tensor.dtype, np.uint8)
        self.assertEqual(int(tensor[0, 0, 0, 0]), MODULE.PADDING)

    def test_model_path_is_explicit_and_unavailable(self):
        with self.assertRaises(MODULE.ModelUnavailableError):
            MODULE.load_backend("/tmp/does-not-exist-yolov8n-pose.rknn")

    def test_rknnlite_unavailable_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory) / "pose.rknn"
            model.write_bytes(b"not-a-model")
            with patch.object(MODEL_RUNNER, "RKNNLite", None):
                with self.assertRaises(MODULE.ModelUnavailableError) as raised:
                    MODULE.load_backend(str(model))
            self.assertEqual(raised.exception.code, "backend_unavailable")

    def test_pose_gate_requires_confirmed_human_and_respects_roi_budget(self):
        self.assertFalse(MODULE.pose_gate_allows([], 0, 1))
        self.assertTrue(MODULE.pose_gate_allows([{"score": 0.9}], 0, 1))
        self.assertFalse(MODULE.pose_gate_allows([{"score": 0.9}], 1, 1))


if __name__ == "__main__":
    unittest.main()
