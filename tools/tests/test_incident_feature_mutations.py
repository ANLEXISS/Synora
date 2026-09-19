import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def load():
    path = ROOT / "tools/incident_v2_pipeline.py"
    spec = importlib.util.spec_from_file_location("incident_v2_pipeline", path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class IncidentFeatureMutationTests(unittest.TestCase):
    def test_single_causal_fact_changes_only_its_feature_group(self):
        incident = load()
        base = {
            "security_armed": True,
            "topology": "private_perimeter",
            "phase": "candidate",
            "priority": "P2",
            "enrichment": "uncertain",
            "human_present": True,
            "track_count": 1,
            "track_confirmed": False,
            "segment_count": 2,
            "gap_count": 0,
            "seconds_since_first": 3,
            "seconds_since_last": 1,
            "calm_seconds": 0,
            "real_detection": True,
            "replay_simulation": False,
            "observation_count": 2,
            "confidence": 0.75,
            "access_state": "closed",
            "movement": True,
            "sensor_evidence": True,
            "alarm_state": "armed",
        }
        cases = {
            "topology": ("protected_interior", {9, 11, 49, 51}),
            "phase": ("confirmed", {14, 15, 59, 60}),
            "priority": ("P1", {54, 55}),
            "enrichment": ("recognized", {64, 65}),
            "access_state": ("forced", {26, 28, 77, 79}),
            "alarm_state": ("triggered", {31, 33, 83, 85}),
            "human_present": (False, {3, 45}),
            "track_count": (3, {6, 46}),
            "track_confirmed": (True, {7, 47}),
            "segment_count": (4, {17, 69}),
            "gap_count": (2, {18, 70}),
            "calm_seconds": (30, {21, 71}),
            "observation_count": (4, {34, 74}),
            "confidence": (0.9, {35, 75}),
            "movement": (False, {24, 80}),
            "sensor_evidence": (False, {29, 81}),
            "action_result": ("simulated_failure", {38, 40}),
            "capabilities": (["notify", "record"], {36, 37}),
        }
        for field, (changed, expected_indexes) in cases.items():
            left = dict(base)
            right = dict(base)
            if field == "action_result":
                left_result, right_result = "not_requested", changed
                left_vector = incident.encode_fixed(left, ["notify"], left_result)
                right_vector = incident.encode_fixed(right, ["notify"], right_result)
            elif field == "capabilities":
                left_vector = incident.encode_fixed(left, ["notify"], "not_requested")
                right_vector = incident.encode_fixed(right, changed, "not_requested")
            else:
                right[field] = changed
                left_vector = incident.encode_fixed(left, ["notify"], "not_requested")
                right_vector = incident.encode_fixed(right, ["notify"], "not_requested")
            actual = {index for index, (before, after) in enumerate(zip(left_vector, right_vector)) if before != after}
            self.assertEqual(actual, expected_indexes, field)


if __name__ == "__main__":
    unittest.main()
