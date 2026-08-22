import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("v1_hardware_qualification", ROOT / "tools/v1_hardware_qualification.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class HardwareQualificationTests(unittest.TestCase):
    def setUp(self):
        self.output = Path(tempfile.mkdtemp())
        self.fixture = self.output / "fixture.json"
        self.fixture.write_text(json.dumps({
            "thermal_c": {"zone0": 47.5},
            "loadavg": [0.4, 0.3, 0.2],
            "storage": {"free_bytes": 123},
            "network": {"eth0": {"rx_bytes": 10, "tx_bytes": 20}},
            "ssd": {"nvme0n1": {"sectors_written": 42, "rotational": False}},
        }), encoding="utf-8")

    def test_fixture_sample_and_report_never_claim_physical_pass(self):
        sample = MODULE.run_once(self.output, ROOT / "docs/v1/V1_HARDWARE_BOM_REFERENCE.json", self.fixture)
        self.assertEqual(sample["source"], "fixture")
        self.assertEqual(sample["physical_qualification"], "blocked_no_target_confirmation")
        report = MODULE.build_report(self.output)
        self.assertTrue(report["checks"]["no_physical_result_invented"])
        self.assertEqual(report["physical_qualification"], "blocked_until_target_unit_and_bom_are_attached")

    def test_soak_and_power_cut_recovery_are_deterministic(self):
        result = MODULE.run_soak(self.output, ROOT / "docs/v1/V1_HARDWARE_BOM_REFERENCE.json", self.fixture, 0, 1)
        self.assertEqual(result["samples"], 1)
        journal = MODULE.power_cut(self.output, "after_sample_write")
        self.assertEqual(journal["status"], "recovered_without_claiming_physical_power_cycle")

    def test_invalid_power_phase_is_rejected(self):
        with self.assertRaises(ValueError):
            MODULE.power_cut(self.output, "unsafe")


if __name__ == "__main__":
    unittest.main()
