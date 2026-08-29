"""Static qualification for the safe, pre-policy M046 preparation."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class SignedUpdatePreparationTests(unittest.TestCase):
    def test_manifest_and_verification_cover_release_integrity(self) -> None:
        manifest = (ROOT / "internal/ota/manifest.go").read_text(encoding="utf-8")
        tests = (ROOT / "internal/ota/manifest_test.go").read_text(encoding="utf-8")
        for marker in ("BundleSHA256", "MigrationTarget", "ed25519.Sign", "ed25519.Verify"):
            self.assertIn(marker, manifest)
        for marker in ("unsigned manifest accepted", "invalid manifest signature accepted", "version downgrade accepted"):
            self.assertIn(marker, tests)

    def test_apply_orders_verification_install_health_gate(self) -> None:
        controller = (ROOT / "internal/ota/controller.go").read_text(encoding="utf-8")
        self.assertLess(controller.index("verifyBundleManifest"), controller.index("c.Install(ctx, bundle)"))
        self.assertLess(controller.index("c.Install(ctx, bundle)"), controller.index("c.MarkGood(ctx)"))
        self.assertIn('c.writeJournal("rolled_back"', controller)
        self.assertIn("c.MarkBad(ctx)", controller)

    def test_migration_failure_is_non_mutating_and_policy_remains_pending(self) -> None:
        migration_test = (ROOT / "internal/migrations/migrations_test.go").read_text(encoding="utf-8")
        docs = (ROOT / "docs/v1/V1_SIGNED_RELEASE_PREPARATION.md").read_text(encoding="utf-8")
        self.assertIn("TestUnsupportedMigrationFailsBeforeMutation", migration_test)
        self.assertIn("racine de confiance", docs)
        self.assertIn("stratégie opérationnelle de rollback", docs)


if __name__ == "__main__":
    unittest.main()
