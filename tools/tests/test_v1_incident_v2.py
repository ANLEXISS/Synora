from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class IncidentV2Tests(unittest.TestCase):
    def test_policy_freezes_scope_and_independent_split(self):
        policy = (ROOT / "testdata/cognitive-v1/incident-v2-policy.json").read_text(encoding="utf-8")
        for marker in ("incident", "86", "independent-test", "categorical-one-hot-v2", "byte-identical"):
            self.assertIn(marker, policy)

    def test_pipeline_is_incident_only_and_keeps_other_heads(self):
        source = (ROOT / "tools/incident_v2_pipeline.py").read_text(encoding="utf-8")
        self.assertIn('HEAD = "incident"', source)
        self.assertIn('("danger", "task", "action")', source)
        self.assertIn("same_dimension", source)
        self.assertIn("forbidden_data_absent", source)


if __name__ == "__main__":
    unittest.main()
