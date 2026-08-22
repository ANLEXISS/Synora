import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[2]
SCRIPT = ROOT / "tools" / "v1_rc_audit.py"
SPEC = importlib.util.spec_from_file_location("v1_rc_audit", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class RCAuditTests(unittest.TestCase):
    def test_audit_has_no_p0_but_keeps_external_gates_open(self):
        report = MODULE.audit(ROOT)
        self.assertEqual(report["status"], "software_rc_audited_external_gates_open")
        self.assertEqual(report["p0_open"], [])
        self.assertEqual(report["vision"]["false_positive_measurement"], "not_available")
        self.assertEqual(report["release_decision"], "tag_v1_rc1_blocked_until_mandatory_external_evidence; tag_local_candidate_allowed")

    def test_required_commands_are_present(self):
        report = MODULE.audit(ROOT)
        self.assertTrue(any("go test ./..." in command for command in report["required_final_commands"]))
        self.assertTrue(any("-race ./..." in command for command in report["required_final_commands"]))


if __name__ == "__main__":
    unittest.main()
