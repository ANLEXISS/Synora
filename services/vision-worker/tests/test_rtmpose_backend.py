import json
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest import mock

import numpy as np

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))

from core.enrichment_v2 import ConfirmedHumanROI  # noqa: E402
from core.enrichment_v3 import VisionEnrichmentPipelineV3  # noqa: E402
from core.rtmpose_backend import RTMPoseBackend, RTMPosePoseEnricher  # noqa: E402


class FakeRunner:
    def __init__(self, outputs):
        self.outputs = outputs
        self.inputs = []
        self.closed = False

    def infer(self, tensor):
        self.inputs.append(tensor)
        return self.outputs

    def close(self):
        self.closed = True


def simcc_outputs(posture="upright"):
    x = np.full((1, 17, 384), -8.0, dtype=np.float32)
    y = np.full((1, 17, 512), -8.0, dtype=np.float32)
    x[:, :, 80] = 8.0
    y[:, :, 200] = 0.0
    y_positions = {"upright": (40, 100, 170, 230), "seated": (40, 140, 145, 210), "ground": (100, 110, 115, 120)}[posture]
    for indices, position in zip(((5, 6), (11, 12), (13, 14), (15, 16)), y_positions):
        for index in indices:
            y[:, index, position * 2] = 8.0
    return [x, y]


class RTMPoseBackendTests(unittest.TestCase):
    def roi(self, image, key="local-a"):
        return ConfirmedHumanROI(key, True, "protected_interior", "P1_urgent_presence", datetime.now(timezone.utc), pose_sample={"image": image})

    def available_enricher(self, posture="upright"):
        runner = FakeRunner(simcc_outputs(posture))
        with mock.patch("core.rtmpose_backend.create_model_runner", return_value=runner):
            enricher = RTMPosePoseEnricher("/tmp/rtmpose-s.rknn", max_rate_hz=5)
        return enricher, runner

    def test_model_unavailable_is_explicit_without_yolo_fallback(self):
        enricher = RTMPosePoseEnricher("/tmp/does-not-exist.rknn")
        result = enricher.enrich(self.roi(np.zeros((32, 24, 3), dtype=np.uint8)))
        self.assertEqual(result.status, "not_available")
        self.assertEqual(enricher.model_status, "unavailable")

    def test_static_preprocess_simcc_decode_and_memory_cleanup(self):
        enricher, runner = self.available_enricher("upright")
        result = enricher.enrich(self.roi(np.zeros((32, 24, 3), dtype=np.uint8)))
        self.assertEqual(result.status, "available")
        self.assertEqual(result.posture, "upright")
        self.assertEqual(runner.inputs[0].shape, (1, 3, 256, 192))
        self.assertEqual(enricher.backend.buffers_released, 1)
        pipeline = VisionEnrichmentPipelineV3(pose=enricher)
        aggregate = pipeline.process([self.roi(np.zeros((32, 24, 3), dtype=np.uint8), "local-b")], replay_simulation=True)
        encoded = json.dumps(aggregate.as_bus_payload(), sort_keys=True).lower()
        for forbidden in ("keypoint", "image", "bbox", "crop", "embedding", "local_track_id", "local_track_key"):
            self.assertNotIn(forbidden, encoded)

    def test_cadence_is_bounded_per_local_roi(self):
        enricher, runner = self.available_enricher("seated")
        roi = self.roi(np.zeros((32, 24, 3), dtype=np.uint8))
        first = enricher.enrich(roi)
        second = enricher.enrich(roi)
        self.assertEqual(first.posture, "seated")
        self.assertEqual(second.posture, "seated")
        self.assertEqual(len(runner.inputs), 1)

    def test_ground_is_an_aggregate_and_never_confirmed_fall(self):
        enricher, _ = self.available_enricher("ground")
        pipeline = VisionEnrichmentPipelineV3(pose=enricher)
        roi = self.roi(np.zeros((32, 24, 3), dtype=np.uint8))
        aggregate = pipeline.process([roi], replay_simulation=True)
        self.assertEqual(aggregate.pose.posture, "ground")
        self.assertIn(aggregate.fall_state, {"none", "candidate", "unknown"})
        self.assertNotEqual(aggregate.fall_state, "confirmed")

    def test_immobility_is_posture_independent_and_recovery_is_explicit(self):
        enricher, runner = self.available_enricher("ground")
        pipeline = VisionEnrichmentPipelineV3(pose=enricher)
        image = np.zeros((32, 24, 3), dtype=np.uint8)
        start = datetime(2026, 1, 1, tzinfo=timezone.utc)
        def roi(at):
            return ConfirmedHumanROI("local-temporal", True, "protected_interior", "P1_urgent_presence", at, pose_sample={"image": image})

        first = pipeline.process([roi(start)], replay_simulation=True)
        runner.outputs = simcc_outputs("ground")
        second = pipeline.process([roi(start.replace(second=2))], replay_simulation=True)
        runner.outputs = simcc_outputs("upright")
        recovered = pipeline.process([roi(start.replace(second=4))], replay_simulation=True)
        self.assertEqual(first.pose.immobility_seconds, 0.0)
        self.assertAlmostEqual(second.pose.immobility_seconds, 2.0, places=3)
        self.assertTrue(recovered.pose.recovery_observed)

    def test_immobility_is_measured_for_upright_human(self):
        enricher, runner = self.available_enricher("upright")
        pipeline = VisionEnrichmentPipelineV3(pose=enricher)
        image = np.zeros((32, 24, 3), dtype=np.uint8)
        start = datetime(2026, 1, 1, tzinfo=timezone.utc)
        def roi(at):
            return ConfirmedHumanROI("local-upright", True, "protected_interior", "P1_urgent_presence", at, pose_sample={"image": image})

        pipeline.process([roi(start)], replay_simulation=True)
        runner.outputs = simcc_outputs("upright")
        second = pipeline.process([roi(start.replace(second=2))], replay_simulation=True)
        self.assertEqual(second.pose.posture, "upright")
        self.assertAlmostEqual(second.pose.immobility_seconds, 2.0, places=3)

    def test_pipeline_limits_roi_count_per_cycle(self):
        enricher, runner = self.available_enricher("upright")
        pipeline = VisionEnrichmentPipelineV3(pose=enricher, max_rois_per_cycle=1)
        first = self.roi(np.zeros((32, 24, 3), dtype=np.uint8), "local-first")
        second = self.roi(np.zeros((32, 24, 3), dtype=np.uint8), "local-second")
        pipeline.process([first, second], replay_simulation=True)
        self.assertEqual(len(runner.inputs), 1)


if __name__ == "__main__":
    unittest.main()
