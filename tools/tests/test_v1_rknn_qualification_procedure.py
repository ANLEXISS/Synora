"""Static guardrails for the physical-only M047 procedure."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
PROCEDURE = ROOT / "docs/v1/validation/M047_RKNN_ROCK5_ITX_PROCEDURE.md"


class RknnQualificationProcedureTests(unittest.TestCase):
    def test_procedure_never_claims_fixture_success(self):
        text = PROCEDURE.read_text(encoding="utf-8")
        self.assertIn("blocked_no_target_results", text)
        self.assertIn("Radxa ROCK 5 ITX", text)
        self.assertIn("biometric_payloads_stored", text)
        self.assertNotIn('"physical_qualification": "pass"', text)

    def test_required_measurements_are_explicit(self):
        text = PROCEDURE.read_text(encoding="utf-8")
        for marker in ("SCRFD", "ArcFace 512", "YOLOv8n", "p95", "NPU", "température", "trois sources", "redémarrage"):
            self.assertIn(marker, text)


if __name__ == "__main__":
    unittest.main()
