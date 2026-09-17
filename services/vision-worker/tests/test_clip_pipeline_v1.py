import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
import json
import socket
import tempfile
import threading

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.clip_pipeline_v1 import (  # noqa: E402
    ClipMetadata,
    Detection,
    FixedClipManager,
    FrameObservation,
    IdentityResult,
    IdentityStatus,
    PlateResult,
    SensitiveObjectResult,
    SensitiveStatus,
    StaticFaceEnricher,
    StaticPlateEnricher,
    StaticSensitiveObjectEnricher,
    SubjectType,
    Topology,
    UnavailablePlateEnricher,
    VisionClipPipelineV1,
)
from core.unix_bus import UnixBusPublisher


class ClipPipelineV1Tests(unittest.TestCase):
    def setUp(self):
        self.base = datetime(2026, 9, 17, 12, 0, tzinfo=timezone.utc)
        self.topology = Topology("front", "exterior")

    def clip(self, **kwargs):
        return ClipMetadata(
            clip_id="clip-1", episode_id="episode-1", camera_id="cam-1", topology=self.topology,
            trigger_reason="motion.sensor.front", started_at=self.base,
            ends_at=self.base + timedelta(seconds=10), **kwargs,
        )

    def frame(self, seconds, *detections):
        return FrameObservation.from_values(self.base + timedelta(seconds=seconds), detections)

    def test_open_close_is_fixed_and_continuity_reuses_episode(self):
        ids = {"episode": 0, "clip": 0}
        def make_id(prefix):
            ids[prefix] += 1
            return f"{prefix}-{ids[prefix]}"
        manager = FixedClipManager(10, 5, id_factory=make_id)
        first = manager.trigger("cam-1", self.topology, "motion.sensor.front", self.base)
        closed = manager.close(self.base + timedelta(seconds=9), track_ids=["t-1"])
        self.assertEqual(closed.ends_at, self.base + timedelta(seconds=9))
        second = manager.trigger("cam-1", self.topology, "motion.sensor.front", self.base + timedelta(seconds=12))
        self.assertEqual(second.episode_id, first.episode_id)
        third = manager.close(self.base + timedelta(seconds=20), track_ids=[])
        new = manager.trigger("cam-1", self.topology, "motion.sensor.front", self.base + timedelta(seconds=40), track_ids=[])
        self.assertNotEqual(new.episode_id, third.episode_id)

    def test_human_recognized_requires_configured_result_and_keeps_provenance(self):
        pipeline = VisionClipPipelineV1(
            face_enricher=StaticFaceEnricher(IdentityResult(IdentityStatus.RECOGNIZED, .94, "local://vision/embeddings/e1")),
        )
        events = pipeline.process_frames(self.clip(), [
            self.frame(1, Detection("track-human", "human", .9, "local://clips/clip-1/roi/1")),
            self.frame(2, Detection("track-human", "human", .8, "local://clips/clip-1/roi/2")),
        ])
        summary = events[-1]["payload"]
        self.assertEqual(summary["schema"], "synora.vision.clip-summary/v1")
        self.assertEqual(summary["identity"]["status"], "recognized")
        self.assertEqual(summary["track"]["id"], "track-human")
        self.assertEqual(summary["topology"], {"node_id": "front", "zone": "exterior"})
        self.assertEqual(summary["trigger"]["reason"], "motion.sensor.front")
        self.assertNotIn("'roi':", str(summary))

    def test_human_uncertain_for_absent_or_blurry_face_and_never_unknown(self):
        pipeline = VisionClipPipelineV1(face_enricher=StaticFaceEnricher(
            IdentityResult(IdentityStatus.UNCERTAIN, .12, reason="face_absent_or_blurry")))
        events = pipeline.process_frames(self.clip(), [
            self.frame(1, Detection("track-human", "human", .8, "local://clips/clip-1/roi/1")),
        ])
        self.assertEqual(events[-1]["payload"]["identity"]["status"], "uncertain")

    def test_human_unknown_after_sufficient_quality_rejects_gallery(self):
        pipeline = VisionClipPipelineV1(face_enricher=StaticFaceEnricher(
            IdentityResult(IdentityStatus.UNKNOWN, .03)))
        events = pipeline.process_frames(self.clip(), [
            self.frame(1, Detection("track-human", "human", .8, "local://clips/clip-1/roi/1")),
            self.frame(2, Detection("track-human", "human", .85, "local://clips/clip-1/roi/2")),
        ])
        self.assertEqual(events[-1]["payload"]["identity"]["status"], "unknown")

    def test_animal_has_no_heavy_enrichment_and_vehicle_plate_is_unavailable(self):
        pipeline = VisionClipPipelineV1(plate_enricher=UnavailablePlateEnricher())
        events = pipeline.process_frames(self.clip(), [
            self.frame(1, Detection("animal-1", "animal", .7, "local://clips/clip-1/roi/a")),
            self.frame(1, Detection("car-1", "vehicle", .9, "local://clips/clip-1/roi/c")),
        ])
        by_id = {event["payload"]["track"]["id"]: event["payload"] for event in events if event["type"].endswith("clip-summary/v1")}
        self.assertEqual(by_id["animal-1"]["identity"]["status"], "not_available")
        self.assertEqual(by_id["car-1"]["plate"]["status"], "not_available")

    def test_critical_preliminary_alert_only_for_available_strong_fake(self):
        seen = []
        pipeline = VisionClipPipelineV1(
            sensitive_enricher=StaticSensitiveObjectEnricher(SensitiveObjectResult(
                SensitiveStatus.DETECTED, ({"kind": "firearm", "confidence": .96,
                                             "roi_ref": "local://clips/clip-1/roi/s"},), True, .96)),
            preliminary_sink=seen.append,
        )
        events = pipeline.process_frames(self.clip(), [self.frame(1, Detection("t-1", "human", .9, "local://clips/clip-1/roi/1"))])
        self.assertEqual(seen[0]["type"], "synora.vision.preliminary-alert/v1")
        self.assertEqual(events[0]["type"], "synora.vision.preliminary-alert/v1")
        self.assertEqual(events[-1]["type"], "synora.vision.clip-summary/v1")
        self.assertEqual(events[-1]["payload"]["sensitive_objects"]["status"], "detected")

    def test_no_available_enricher_does_not_emit_preliminary_or_physical_action(self):
        seen = []
        pipeline = VisionClipPipelineV1(preliminary_sink=seen.append)
        events = pipeline.process_frames(self.clip(), [self.frame(1, Detection("t-1", "human", .9))])
        self.assertEqual(seen, [])
        self.assertEqual(events[-1]["payload"]["sensitive_objects"]["status"], "not_available")
        self.assertEqual(events[-1]["payload"]["identity"]["status"], "uncertain")

    def test_unix_bus_publisher_uses_register_then_targeted_event(self):
        with tempfile.TemporaryDirectory() as root:
            path = str(Path(root) / "bus.sock")
            server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            server.bind(path)
            server.listen(1)
            received = []
            def read_bus():
                conn, _ = server.accept()
                with conn, conn.makefile("r") as reader:
                    received.append(json.loads(reader.readline()))
                    received.append(json.loads(reader.readline()))
            thread = threading.Thread(target=read_bus)
            thread.start()
            publisher = UnixBusPublisher(path)
            publisher.publish("synora.vision.preliminary-alert/v1", {"schema": "synora.vision.preliminary-alert/v1"})
            publisher.close()
            thread.join(timeout=2)
            server.close()
        self.assertEqual(received[0]["type"], "bus.register")
        self.assertEqual(received[1]["type"], "synora.vision.preliminary-alert/v1")
        self.assertEqual(received[1]["target"], "core")


if __name__ == "__main__":
    unittest.main()

