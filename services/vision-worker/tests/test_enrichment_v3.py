import json
import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))

from core.enrichment_v2 import ConfirmedHumanROI, PoseEnricherV2, RiskObjectEnricherV2  # noqa: E402
from core.enrichment_v3 import RiskPersistenceV3, VisionEnrichmentPipelineV3  # noqa: E402


class EnrichmentV3Test(unittest.TestCase):
    def setUp(self):
        self.start = datetime(2026, 9, 21, 12, 0, tzinfo=timezone.utc)

    def roi(self, at, *, pose=None, risk=None):
        return ConfirmedHumanROI("process-local", True, "protected_interior", "P1_urgent_presence", at, pose_sample=pose, risk_sample=risk)

    def test_unavailable_backend_is_explicit_and_no_raw_fields_cross_boundary(self):
        result = VisionEnrichmentPipelineV3().process([self.roi(self.start)])
        self.assertEqual(result.pose.status, "unavailable")
        self.assertEqual(result.risk.status, "not_available")
        body = json.dumps(result.as_bus_payload()).lower()
        for forbidden in ("bbox", "crop", "embedding", "local_track_id", "local_track_key", "media"):
            self.assertNotIn(forbidden, body)

    def test_pose_states_and_strict_fall_transition(self):
        pose = PoseEnricherV2(executor=lambda _: {"quality": .9, "posture": "standing"}, max_rate_hz=100)
        pipeline = VisionEnrichmentPipelineV3(pose=pose, risk=RiskObjectEnricherV2(executor=lambda _: {"status": "uncertain", "kind": "unknown"}))
        self.assertEqual(pipeline.process([self.roi(self.start)]).pose.posture, "upright")
        ground = PoseEnricherV2(executor=lambda _: {"quality": .9, "posture": "lying", "transition_to_ground": True}, max_rate_hz=100)
        pipeline = VisionEnrichmentPipelineV3(pose=ground)
        self.assertEqual(pipeline.process([self.roi(self.start + timedelta(seconds=1))]).fall_state, "none")

    def test_risk_persistence_requires_repeated_temporal_observations(self):
        risk = RiskObjectEnricherV2(executor=lambda _: {"status": "suspected", "kind": "other", "confidence": .8})
        pipeline = VisionEnrichmentPipelineV3(risk=risk, persistence=RiskPersistenceV3(repeated_count=2, persistent_seconds=3))
        first = pipeline.process([self.roi(self.start)])
        second = pipeline.process([self.roi(self.start + timedelta(seconds=1))])
        third = pipeline.process([self.roi(self.start + timedelta(seconds=4))])
        self.assertEqual(first.risk.persistence, "isolated")
        self.assertEqual(second.risk.persistence, "repeated")
        self.assertEqual(third.risk.persistence, "persistent")


if __name__ == "__main__":
    unittest.main()
