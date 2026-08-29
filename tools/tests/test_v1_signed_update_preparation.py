"""Static qualification for the signed release and rollback M046 boundary."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class SignedUpdatePreparationTests(unittest.TestCase):
    def test_manifest_and_verification_cover_release_integrity(self) -> None:
        manifest = (ROOT / "internal/ota/manifest.go").read_text(encoding="utf-8")
        tests = (ROOT / "internal/ota/manifest_test.go").read_text(encoding="utf-8")
        for marker in ("BundleSHA256", "SecurityGeneration", "ReleaseSignature", "rsa.SignPKCS1v15", "x509"):
            self.assertIn(marker, manifest)
        for marker in ("unsigned manifest accepted", "invalid manifest signature accepted", "version downgrade accepted"):
            self.assertIn(marker, tests)
        pki = (ROOT / "internal/ota/pki.go").read_text(encoding="utf-8")
        self.assertIn("ExtKeyUsageCodeSigning", pki)
        self.assertIn("OTA release signer is revoked", pki)
        central_rauc = (ROOT / "deployments/rauc/synora-central-system.conf.example").read_text(encoding="utf-8")
        camera_rauc = (ROOT / "deployments/rauc/synora-camera-system.conf.example").read_text(encoding="utf-8")
        for config in (central_rauc, camera_rauc):
            self.assertIn("boot-attempts=3", config)
            self.assertIn("check-purpose=codesign", config)
            self.assertIn("check-crl=true", config)
        self.assertNotIn("BEGIN PRIVATE KEY", central_rauc + camera_rauc)

    def test_apply_orders_verification_install_health_gate(self) -> None:
        controller = (ROOT / "internal/ota/controller.go").read_text(encoding="utf-8")
        self.assertLess(controller.index("verifyBundleManifest"), controller.index("c.Install(ctx, bundle)"))
        self.assertLess(controller.index("c.Install(ctx, bundle)"), controller.index("c.MarkGood(ctx)"))
        self.assertIn('c.writeJournal("rolled_back"', controller)
        self.assertIn("c.MarkBad(ctx)", controller)
        self.assertIn("stabilityWindow", controller)

    def test_migration_failure_is_non_mutating_and_policy_remains_pending(self) -> None:
        migration_test = (ROOT / "internal/migrations/migrations_test.go").read_text(encoding="utf-8")
        docs = (ROOT / "docs/v1/V1_SIGNED_RELEASE_PREPARATION.md").read_text(encoding="utf-8")
        self.assertIn("TestUnsupportedMigrationFailsBeforeMutation", migration_test)
        self.assertIn("PKI RAUC X.509", docs)
        self.assertIn("120 secondes", docs)


if __name__ == "__main__":
    unittest.main()
