import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.camera_health_v3 import aggregate_camera_health


class CameraHealthV3Tests(unittest.TestCase):
  def test_camera_health_states_are_aggregate_only(self):
    cases = [
        ({"online": True}, "healthy"),
        ({"online": False}, "offline"),
        ({"online": True, "frozen": True}, "degraded"),
        ({"online": True, "masked": True}, "degraded"),
        ({"online": True, "clock_skew_seconds": 3}, "degraded"),
        ({"online": True, "dropped_segments": 2}, "degraded"),
        ({"online": True, "manifest_valid": False}, "uncertain"),
        ({"online": True, "quota_exceeded": True}, "degraded"),
    ]
    for kwargs, expected in cases:
        value = aggregate_camera_health(**kwargs).as_bus_payload()
        self.assertEqual(value["status"], expected)
        self.assertNotIn("frame", value)
        self.assertNotIn("image", value)
        self.assertNotIn("media", value)
        self.assertNotIn("bbox", value)
        self.assertNotIn("crop", value)


  def test_duplicate_is_idempotent_fact(self):
    value = aggregate_camera_health(online=True, duplicate=True).as_bus_payload()
    self.assertEqual(value["status"], "healthy")
    self.assertIs(value["duplicate_ignored"], True)
