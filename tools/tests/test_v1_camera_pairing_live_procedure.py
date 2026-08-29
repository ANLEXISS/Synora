"""Static guardrails for the physical-only M048 procedure."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
PROCEDURE = ROOT / "docs/v1/validation/M048_CAMERA_PAIRING_LIVE_PROCEDURE.md"


class CameraPairingLiveProcedureTests(unittest.TestCase):
    def test_procedure_requires_three_real_cameras(self):
        text = PROCEDURE.read_text(encoding="utf-8")
        self.assertIn("trois caméras Synora réelles", text)
        self.assertIn("blocked_no_target_results", text)
        self.assertIn('"camera_count": 3', text)
        self.assertNotIn('"physical_qualification": "pass"', text)

    def test_procedure_covers_security_network_media_and_sensors(self):
        text = PROCEDURE.read_text(encoding="utf-8")
        for marker in ("clé fausse", "révoquer", "changement d’adresse IP", "checksum", "clip_index", "MediaMTX", "PIR", "Doppler", "secrets_stored_in_report"):
            self.assertIn(marker, text)


if __name__ == "__main__":
    unittest.main()
