import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[1] / "v1_camera_network_qualification.py"
FIXTURE = Path(__file__).parents[2] / "docs/v1/V1_CAMERA_NETWORK_FIXTURE.json"
SPEC = importlib.util.spec_from_file_location("v1_camera_network_qualification", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class CameraNetworkQualificationTests(unittest.TestCase):
    def test_fixture_reports_three_cameras_without_claiming_physical_pass(self):
        report = MODULE.evaluate(MODULE.read_json(FIXTURE))
        self.assertEqual(report["status"], "fixture_observed_physical_qualification_blocked")
        self.assertEqual(report["physical_qualification"], "blocked_no_target_confirmation")
        self.assertEqual(tuple(camera["id"] for camera in report["cameras"]), ("cam_01", "cam_02", "cam_03"))
        self.assertEqual(report["cameras"][0]["upload"]["lost"], 0)
        self.assertEqual(report["cameras"][1]["synoranet"]["unrecovered"], 0)

    def test_output_is_atomic_and_private(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "nested" / "report.json"
            report = MODULE.evaluate(MODULE.read_json(FIXTURE))
            MODULE.atomic_json(output, report)
            self.assertEqual(json.loads(output.read_text()), report)
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)

    def test_physical_source_is_rejected(self):
        document = MODULE.read_json(FIXTURE)
        document["source"] = "physical_measurement"
        with self.assertRaises(ValueError):
            MODULE.evaluate(document)


if __name__ == "__main__":
    unittest.main()
