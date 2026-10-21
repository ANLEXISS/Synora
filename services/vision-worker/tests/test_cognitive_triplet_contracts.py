import json
import hashlib
import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
FIXTURES = ROOT / "pkg" / "contract" / "testdata" / "v1"
FORBIDDEN = {
    "media", "frame", "crop", "bbox", "keypoints", "biometric_embedding",
    "identity", "name", "local_path", "url", "camera_id", "hardware_id",
    "local_track_id", "logits", "tensor",
}


def walk_keys(value):
    if isinstance(value, dict):
        for key, nested in value.items():
            yield key
            yield from walk_keys(nested)
    elif isinstance(value, list):
        for nested in value:
            yield from walk_keys(nested)


class CognitiveTripletFixtureTests(unittest.TestCase):
    def test_three_frame_goldens_are_versioned_redacted_and_bounded(self):
        expected = {
            "cognitive-event-frame-v1.json": ("synora.cognitive.event-frame/v1", "cognitive-event-frame-v1.schema.json"),
            "cognitive-context-frame-v1.json": ("synora.cognitive.context-frame/v1", "cognitive-context-frame-v1.schema.json"),
            "cognitive-arbitration-frame-v1.json": ("synora.cognitive.arbitration-frame/v1", "cognitive-arbitration-frame-v1.schema.json"),
            "cognitive-event-frame-degraded-v1.json": ("synora.cognitive.event-frame/v1", "cognitive-event-frame-v1.schema.json"),
        }
        for filename, (version, schema_name) in expected.items():
            with self.subTest(filename=filename):
                data = json.loads((FIXTURES / filename).read_text())
                self.assertEqual(data["schema_version"], version)
                self.assertFalse(FORBIDDEN.intersection(walk_keys(data)))
                self.assertLessEqual(len(json.dumps(data)), 1 << 20)
                schema = json.loads((ROOT / "pkg" / "contract" / schema_name).read_text())
                self.assertEqual(schema["$id"], version)
                self.assertFalse(schema["additionalProperties"])

    def test_degraded_blocked_and_unknown_states_have_fixtures(self):
        degraded = json.loads((FIXTURES / "cognitive-event-frame-degraded-v1.json").read_text())
        blocked = json.loads((FIXTURES / "cognitive-arbiter-blocked-output-v1.json").read_text())
        unknown = json.loads((FIXTURES / "cognitive-event-frame-v1.json").read_text())
        self.assertEqual(degraded["envelope"]["availability"], "degraded")
        self.assertEqual(blocked["decision_availability"], "blocked")
        self.assertEqual(unknown["presence"]["human"], "unknown")

    def test_bundle_hashes_and_compatibility_version_are_closed(self):
        data = json.loads((FIXTURES / "cognitive-triplet-bundle-v1.json").read_text())
        self.assertEqual(data["schema_version"], "synora.cognitive.triplet-bundle/v1")
        self.assertEqual(data["compatibility_version"], "synora.cognitive.triplet-compatibility/v1")
        for key in (
            "event_model_manifest_hash", "context_model_manifest_hash",
            "arbiter_model_manifest_hash", "event_frame_schema_hash",
            "context_frame_schema_hash", "arbitration_frame_schema_hash",
            "label_taxonomy_hash", "calibration_manifest_hash",
        ):
            self.assertRegex(data[key], re.compile(r"^[a-f0-9]{64}$"))
        schema_files = {
            "event_frame_schema_hash": "cognitive-event-frame-v1.schema.json",
            "context_frame_schema_hash": "cognitive-context-frame-v1.schema.json",
            "arbitration_frame_schema_hash": "cognitive-arbitration-frame-v1.schema.json",
            "label_taxonomy_hash": "cognitive-triplet-label-taxonomy-v1.json",
        }
        for hash_key, filename in schema_files.items():
            raw = (ROOT / "pkg" / "contract" / filename).read_bytes()
            self.assertEqual(data[hash_key], hashlib.sha256(raw).hexdigest())


if __name__ == "__main__":
    unittest.main()
