import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.clip_pipeline_v1 import ClipMetadata, Topology, TopologyClass  # noqa: E402
from edge.worker import EdgeVisionWorkerV1  # noqa: E402


class _Detector:
    def diagnostic(self):
        return {"status": "ok", "real_model": False, "detections_total": 1}


class EdgeNoDoubleTrackingTests(unittest.TestCase):
    def test_edge_events_are_aggregate_and_report_no_central_tracking(self):
        start = datetime(2026, 9, 19, tzinfo=timezone.utc)
        clip = ClipMetadata(
            clip_id="clip-1", episode_id="episode-1", camera_id="camera-1",
            topology=Topology("node-1", "private_perimeter", TopologyClass.PRIVATE_PERIMETER),
            trigger_reason="motion", started_at=start, ends_at=start + timedelta(seconds=1),
        )
        fake_events = [{
            "type": "synora.vision.clip-observation/v1",
            "track_id": "human-0",
            "payload": {"tracks": [{"track_id": "human-0", "subject_type": "human", "state": "candidate"}],
                        "priority_hint": "P2_contextual_enrichment", "reason_codes": ["human_detected"]},
        }]
        with patch("edge.worker.VisionClipPipelineV1") as pipeline_type:
            pipeline_type.return_value.process_video.return_value = fake_events
            result = EdgeVisionWorkerV1().process_video(clip, "/tmp/not-opened.mp4", _Detector())
        self.assertEqual(result.metrics["central_visual_tracking_invocations"], 0)
        self.assertEqual(result.events[0]["payload"]["tracks"][0]["track_id"], "aggregate-track-1")
        self.assertTrue(result.manifest["edge_emulated"])


if __name__ == "__main__":
    unittest.main()
