import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[2]
SCRIPT = ROOT / "tools" / "v1_release_engineering.py"
SPEC = importlib.util.spec_from_file_location("v1_release_engineering", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class ReleaseEngineeringTests(unittest.TestCase):
    def test_source_manifest_is_deterministic_and_does_not_claim_signed_image(self):
        first = MODULE.release_manifest(ROOT)
        second = MODULE.release_manifest(ROOT)
        self.assertEqual(first, second)
        self.assertEqual(first["signing"]["status"], "blocked_external_key_required")
        self.assertFalse(first["signing"]["private_key_present"])
        self.assertTrue(first["files"])

    def test_sbom_and_provisioning_are_explicit_about_limits(self):
        inventory = MODULE.sbom(ROOT)
        self.assertTrue(inventory["components"])
        self.assertIn("unresolved", inventory["license_inventory_status"])
        plan = MODULE.provisioning(ROOT)
        self.assertFalse(plan["mutates_system"])
        self.assertTrue(plan["preflight_sources_present"])

    def test_check_keeps_regulatory_and_target_gates_external(self):
        report = MODULE.check(ROOT)
        self.assertEqual(report["status"], "release_engineering_ready_external_signing_and_target_validation_required")
        self.assertTrue(report["no_false_certification_claim"])
        self.assertEqual(report["regulatory"]["CE"], "external_evidence_required")


if __name__ == "__main__":
    unittest.main()
