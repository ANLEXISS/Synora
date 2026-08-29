"""Static guardrails for the hermetic V1 end-to-end scenario."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
CORE = ROOT / "cmd/synora-core/v1_hermetic_e2e_test.go"
API = ROOT / "cmd/synora-api/v1_hermetic_e2e_test.go"


class HermeticE2EQualificationTests(unittest.TestCase):
    def test_core_scenario_covers_all_local_boundaries_and_recovery(self) -> None:
        content = CORE.read_text(encoding="utf-8")
        for marker in (
            "httptest.NewServer",
            "mediamtx.Reconcile",
            "EventDiscoveryCameraOnline",
            "RunClipWorker",
            "RunClipWorkerAttempt",
            "actions.Service",
            "AcknowledgeIncident",
            "restarted.state.LoadPersisted",
            "fake queue saturated",
        ):
            self.assertIn(marker, content)

    def test_api_scenario_uses_real_http_routes_and_durable_store(self) -> None:
        content = API.read_text(encoding="utf-8")
        for marker in (
            "httptest.NewServer",
            "handleIncidentCollection",
            "handleIncidentRoute",
            "RecordIncident",
            "LoadPersisted",
            "IncidentStatusAcknowledged",
        ):
            self.assertIn(marker, content)

    def test_scenarios_are_local_and_do_not_start_system_services(self) -> None:
        for path in (CORE, API):
            content = path.read_text(encoding="utf-8")
            for forbidden in ("exec.Command", "systemctl", "/etc/synora", "/var/lib/synora"):
                self.assertNotIn(forbidden, content, f"{path}: {forbidden}")
            self.assertIn("t.TempDir()", content)


if __name__ == "__main__":
    unittest.main()
