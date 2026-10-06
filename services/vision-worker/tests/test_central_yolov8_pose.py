import importlib.util
import json
import sys
import unittest
from pathlib import Path

import numpy as np


ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))
SPEC = importlib.util.spec_from_file_location("central_yolov8_pose", ROOT / "tools" / "central_yolov8_pose.py")
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class CentralYOLOPoseTests(unittest.TestCase):
    def test_synthetic_output_decodes_to_aggregate_only(self):
        row = np.zeros((1, 56, 1), dtype=np.float32)
        row[0, 0:4, 0] = (320.0, 320.0, 180.0, 520.0)
        row[0, 4, 0] = 0.95
        for index in range(17):
            row[0, 5 + index * 3, 0] = 320.0
            row[0, 6 + index * 3, 0] = 320.0 + index
            row[0, 7 + index * 3, 0] = 0.95
        pose = MODULE.decode_best([row], (1.0, 0, 0, 640, 640))
        self.assertIsNotNone(pose)
        self.assertEqual(MODULE.posture_from_pose(pose), "upright")
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
        self.assertIsNone(MODULE.normalize_output(np.zeros((1, 84, 10), dtype=np.float32)))

    def test_model_path_is_explicit_and_unavailable(self):
        with self.assertRaises(MODULE.ModelUnavailableError):
            MODULE.load_backend("/tmp/does-not-exist-yolov8n-pose.rknn")


if __name__ == "__main__":
    unittest.main()
