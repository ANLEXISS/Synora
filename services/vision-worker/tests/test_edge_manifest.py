import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from edge.manifest import (  # noqa: E402
    EdgeTrackManifest,
    SCHEMA,
    opaque_evidence_ref,
    validate_manifest,
)


class EdgeManifestTests(unittest.TestCase):
    def manifest(self):
        return EdgeTrackManifest(
            schema_version=SCHEMA,
            camera_id="cam-1",
            episode_id="episode-1",
            topology_class="private_perimeter",
            trigger_class="human",
            trigger_confidence=0.9,
            tracking_status="ok",
            started_at="2026-09-19T00:00:00+00:00",
            ended_at="2026-09-19T00:00:10+00:00",
            track_count=1,
            confirmed_track_count=1,
            observation_count=2,
            segment_count=2,
            gap_count=0,
            evidence_refs=(opaque_evidence_ref("episode-1", 0, "selected"),),
            priority_reason=("human_detected",),
            edge_emulated=True,
            metrics={"edge_frames_sampled": 2},
        )

    def test_manifest_is_deterministic_and_has_no_local_id(self):
        value = self.manifest().as_dict()
        self.assertEqual(value, json.loads(self.manifest().json_bytes()))
        self.assertNotIn("local_track_id", json.dumps(value))
        self.assertTrue(value["evidence_refs"][0].startswith("evidence://"))

    def test_forbidden_pixel_fields_are_rejected_recursively(self):
        value = self.manifest().as_dict()
        value["metrics"] = {"nested": {"bbox": [1, 2, 3, 4]}}
        with self.assertRaises(ValueError):
            validate_manifest(value)

    def test_invalid_status_and_non_opaque_ref_are_rejected(self):
        value = self.manifest().as_dict()
        value["tracking_status"] = "fallback"
        with self.assertRaises(ValueError):
            validate_manifest(value)
        value = self.manifest().as_dict()
        value["evidence_refs"] = ["/tmp/crop.jpg"]
        with self.assertRaises(ValueError):
            validate_manifest(value)


if __name__ == "__main__":
    unittest.main()
