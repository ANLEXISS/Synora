import unittest
from pathlib import Path


ROOT = Path(__file__).parents[2]


class ReproducibleDeployTests(unittest.TestCase):
    def test_build_inputs_are_pinned_and_do_not_fallback_to_install(self):
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("GO_BUILD_FLAGS ?= -trimpath -buildvcs=false", makefile)
        self.assertGreaterEqual(makefile.count("$(GO_BUILD_FLAGS)"), 3)
        self.assertIn("npm ci", makefile)
        self.assertNotIn("npm install", makefile)
        requirements = (ROOT / "services/vision-worker/requirements.txt").read_text(encoding="utf-8")
        packages = [line.strip() for line in requirements.splitlines() if line.strip() and not line.startswith("#")]
        self.assertTrue(packages)
        self.assertTrue(all("==" in package for package in packages))

    def test_version_manifest_has_no_host_clock_fallback(self):
        source = (ROOT / "cmd/synora-version/main.go").read_text(encoding="utf-8")
        self.assertIn("SOURCE_DATE_EPOCH", source)
        self.assertIn('return "unknown"', source)
        self.assertNotIn("time.Now().UTC().Format", source)

    def test_runtime_units_have_bounded_stop_and_file_resources(self):
        names = (
            "synora-bus.service",
            "synora-runtime-manager.service",
            "synora-core.service",
            "synora-discovery.service",
            "synora-actions.service",
            "synora-api.service",
            "synora-connect.service",
            "mediamtx.service",
        )
        for name in names:
            unit = (ROOT / "deployments/systemd" / name).read_text(encoding="utf-8")
            self.assertIn("TimeoutStopSec=15s", unit, name)
            self.assertIn("KillMode=mixed", unit, name)
            self.assertIn("UMask=0077", unit, name)
            self.assertIn("LimitNOFILE=4096", unit, name)
            self.assertIn("TasksMax=128", unit, name)

    def test_start_order_is_explicit_and_migrations_are_contiguous(self):
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn(
            "START_ORDER := synora-bus synora-runtime-manager synora-core synora-discovery synora-actions synora-api synora-connect mediamtx",
            makefile,
        )
        migrations = sorted((ROOT / "migrations").glob("*.yaml"))
        self.assertEqual([path.name[:4] for path in migrations], ["0001", "0002", "0003"])


if __name__ == "__main__":
    unittest.main()
