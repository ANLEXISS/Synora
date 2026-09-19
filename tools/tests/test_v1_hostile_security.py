"""Qualification matrix for the V1 hostile security boundary suite."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class HostileSecurityQualificationTests(unittest.TestCase):
    def test_each_hostile_boundary_has_a_regression_oracle(self) -> None:
        required = {
            "spoof_camera_service": ("internal/security/device_auth_test.go", "RejectsInvalidSignature"),
			"discovery_auth": ("internal/discovery/api_server_test.go", "Unauthorized"),
            "bruteforce_login": ("internal/api/hostile_security_test.go", "Bruteforce"),
			"boundary_raw_data": ("internal/discovery/boundary_v1_test.go", "TestBoundaryRejectsRawAndBiometricData"),
            "session_fixation": ("internal/api/auth_test.go", "refresh did not rotate"),
			"json_limits": ("internal/discovery/web_v1.go", "MaxBoundaryPayload"),
            "archive_hostile": ("internal/backup/manager_test.go", "RejectsTraversalSymlink"),
			"bounded_history": ("internal/discovery/snapshot_cache_test.go", "snapshotHistoryLimit"),
            "biometric_logs": ("internal/security/support_bundle_test.go", "Biometrics"),
            "last_admin": ("internal/api/auth_test.go", "ProtectsLastAdmin"),
        }
        for boundary, (relative, marker) in required.items():
            content = (ROOT / relative).read_text(encoding="utf-8")
            self.assertIn(marker, content, boundary)

    def test_hostile_suite_is_offline_and_does_not_disable_tests(self) -> None:
		content = (ROOT / "internal/discovery/api_server_test.go").read_text(encoding="utf-8")
        self.assertNotIn("t.Skip", content)
        self.assertNotIn("http://", content)
        self.assertNotIn("https://", content)


if __name__ == "__main__":
    unittest.main()
