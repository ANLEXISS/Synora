"""Qualification matrix for the V1 hostile security boundary suite."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class HostileSecurityQualificationTests(unittest.TestCase):
    def test_each_hostile_boundary_has_a_regression_oracle(self) -> None:
        required = {
            "spoof_camera_service": ("internal/security/device_auth_test.go", "RejectsInvalidSignature"),
            "replay_pairing": ("cmd/synora-api/synora_camera_pairing_security_test.go", "replayed claim"),
            "bruteforce_login": ("internal/api/hostile_security_test.go", "Bruteforce"),
            "enumeration_and_idor": ("cmd/synora-api/hostile_security_regression_test.go", "ResourceIDs"),
            "csrf": ("cmd/synora-api/main_test.go", "SameOriginRejectsWildcard"),
            "websocket_origin": ("cmd/synora-api/ws_realtime_test.go", "RejectsDisallowedOrigin"),
            "session_fixation": ("internal/api/auth_test.go", "refresh did not rotate"),
            "json_limits": ("cmd/synora-api/hostile_security_regression_test.go", "JSONDepth"),
            "archive_hostile": ("internal/backup/manager_test.go", "RejectsTraversalSymlink"),
            "range_abuse": ("cmd/synora-api/hostile_security_regression_test.go", "MediaRanges"),
            "slow_client": ("cmd/synora-api/ws_realtime_test.go", "SlowClient"),
            "biometric_logs": ("internal/security/support_bundle_test.go", "Biometrics"),
            "last_admin": ("internal/api/auth_test.go", "ProtectsLastAdmin"),
        }
        for boundary, (relative, marker) in required.items():
            content = (ROOT / relative).read_text(encoding="utf-8")
            self.assertIn(marker, content, boundary)

    def test_hostile_suite_is_offline_and_does_not_disable_tests(self) -> None:
        content = (ROOT / "cmd/synora-api/hostile_security_regression_test.go").read_text(encoding="utf-8")
        self.assertNotIn("t.Skip", content)
        self.assertNotIn("http://", content)
        self.assertNotIn("https://", content)


if __name__ == "__main__":
    unittest.main()
