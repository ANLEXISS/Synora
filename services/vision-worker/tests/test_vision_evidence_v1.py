import copy
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))

from core.vision_evidence_v1 import VisionEvidenceContractError, encode_vision_evidence_v1, validate_vision_evidence_v1  # noqa: E402


class VisionEvidenceV1Test(unittest.TestCase):
    def setUp(self):
        self.fixture = json.loads((ROOT / "pkg/contract/testdata/v1/vision-evidence-v1.json").read_text())

    def test_shared_golden_fixture_is_accepted(self):
        self.assertEqual(validate_vision_evidence_v1(self.fixture)["schema_version"], "synora.vision.evidence/v1")
        encoded = encode_vision_evidence_v1(self.fixture)
        self.assertEqual(json.loads(encoded), self.fixture)

    def test_provenance_values_and_simulated_marker(self):
        for provenance in ("real", "replay"):
            value = copy.deepcopy(self.fixture)
            value["provenance"] = provenance
            value["simulated_camera"] = False
            validate_vision_evidence_v1(value)
        value = copy.deepcopy(self.fixture)
        value["provenance"] = "simulated_test"
        value["simulated_camera"] = True
        validate_vision_evidence_v1(value)
        value["simulated_camera"] = False
        with self.assertRaises(VisionEvidenceContractError):
            validate_vision_evidence_v1(value)

    def test_rejects_unknown_raw_fields_invalid_ranges_and_time(self):
        invalid = []
        value = copy.deepcopy(self.fixture); value["bbox"] = [1, 2, 3, 4]; invalid.append(value)
        value = copy.deepcopy(self.fixture); value["pose"]["keypoints"] = [1]; invalid.append(value)
        value = copy.deepcopy(self.fixture); value["trigger"]["confidence"] = 1.01; invalid.append(value)
        value = copy.deepcopy(self.fixture); value["window_seconds"] = 2; invalid.append(value)
        value = copy.deepcopy(self.fixture); value["window_start"] = "2026-01-01T12:00:00+02:00"; invalid.append(value)
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(VisionEvidenceContractError):
                    validate_vision_evidence_v1(value)

    def test_availability_is_distinct_and_empty_support_is_required_when_not_evaluated(self):
        for availability in ("not_requested", "unavailable"):
            value = copy.deepcopy(self.fixture)
            value["face"]["availability"] = availability
            value["face"]["result"] = "unknown" if availability == "unavailable" else "unknown"
            value["face"]["confidence"] = value["face"]["quality"] = 0
            value["face"]["support"] = {"valid_evaluations": 0, "continuity": "unknown", "supported_seconds": 0, "gap_count": 0}
            validate_vision_evidence_v1(value)

    def test_pose_vocab_preserves_seated_reclined_and_ground(self):
        for posture in ("upright", "seated", "reclined", "ground", "ambiguous", "unknown"):
            value = copy.deepcopy(self.fixture)
            value["pose"].update({"availability": "evaluated", "posture": posture,
                                  "posture_confidence": .8, "quality": .75,
                                  "support": {"valid_evaluations": 2, "continuity": "continuous",
                                              "supported_seconds": 1, "gap_count": 0}})
            validate_vision_evidence_v1(value)
            self.assertEqual(encode_vision_evidence_v1(value), encode_vision_evidence_v1(value))


if __name__ == "__main__":
    unittest.main()
