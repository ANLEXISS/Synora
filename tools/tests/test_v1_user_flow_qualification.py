import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[2]
SCRIPT = ROOT / "tools" / "v1_user_flow_qualification.py"
MANIFEST = ROOT / "docs/v1/V1_USER_FLOW_QUALIFICATION.json"
SPEC = importlib.util.spec_from_file_location("v1_user_flow_qualification", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class UserFlowQualificationTests(unittest.TestCase):
    def test_manifest_covers_local_flow_and_marks_remote_external(self):
        report = MODULE.qualify(ROOT, MODULE.load(MANIFEST))
        self.assertEqual(report["status"], "local_user_flow_qualified_remote_access_blocked")
        statuses = {flow["id"]: flow["status"] for flow in report["flows"]}
        self.assertEqual(statuses["remote_access"], "blocked_external_adapter")
        self.assertEqual(statuses["onboarding_pairing"], "covered")
        self.assertEqual(statuses["incidents_clips_ack_resolve"], "covered")

    def test_manifest_rejects_missing_marker(self):
        manifest = MODULE.load(MANIFEST)
        manifest["flows"][0]["artifacts"][0]["markers"].append("not-present")
        with self.assertRaises(ValueError):
            MODULE.qualify(ROOT, manifest)


if __name__ == "__main__":
    unittest.main()
