import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
import json
import socket
import tempfile
import threading
import types
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.clip_pipeline_v1 import (  # noqa: E402
    ClipMetadata,
    ClipTrackerV1,
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
    TrackEnrichmentPolicy,
    TrackEnrichmentState,
    AdaptiveSamplingPolicy,
    EpisodeEvidenceLedger,
    PriorityFrameQueue,
    VisionPriority,
    VisionPriorityScheduler,
    TopologyClass,
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

    def test_tracker_is_deterministic_and_keeps_track_through_short_occlusion(self):
        tracker = ClipTrackerV1(iou_threshold=.2, max_track_gap_seconds=1.0,
                                max_active_tracks=4, min_bbox_width=10, min_bbox_height=10)
        first = tracker.update([
            {"bbox": [0, 0, 40, 40], "score": .8},
            {"bbox": [100, 0, 140, 40], "score": .7},
        ], self.base)
        self.assertEqual([item["track_id"] for item in first], ["human-0", "human-1"])
        self.assertEqual(tracker.update([], self.base + timedelta(milliseconds=500)), [])
        continued = tracker.update([{"bbox": [4, 0, 44, 40], "score": .9}], self.base + timedelta(seconds=.8))
        self.assertEqual(continued[0]["track_id"], "human-0")
        crossing = tracker.update([
            {"bbox": [96, 0, 136, 40], "score": .7},
            {"bbox": [8, 0, 48, 40], "score": .6},
        ], self.base + timedelta(seconds=1.0))
        self.assertEqual([item["track_id"] for item in crossing], ["human-0", "human-1"])

    def test_tracker_rejects_tiny_boxes_and_expires_tracks_before_allocating(self):
        tracker = ClipTrackerV1(max_track_gap_seconds=1.0, max_active_tracks=1,
                                min_bbox_width=20, min_bbox_height=20)
        self.assertEqual(tracker.update([{"bbox": [0, 0, 5, 5], "score": .99}], self.base), [])
        self.assertEqual(tracker.update([{"bbox": [0, 0, 30, 30], "score": .9}], self.base),
                         [{"track_id": "human-0", "bbox": (0, 0, 30, 30), "score": .9,
                           "item": {"bbox": [0, 0, 30, 30], "score": .9}}])
        self.assertEqual(tracker.update([
            {"bbox": [50, 0, 80, 30], "score": .95},
            {"bbox": [100, 0, 130, 30], "score": .94},
        ], self.base + timedelta(seconds=.5)), [])
        replacement = tracker.update([{"bbox": [100, 0, 130, 30], "score": .95}], self.base + timedelta(seconds=2))
        self.assertEqual(replacement[0]["track_id"], "human-1")

    def test_track_enrichment_policy_only_stops_stable_track(self):
        policy = TrackEnrichmentPolicy(max_occlusion_frames=2)
        self.assertEqual(policy.observe("human-0", self.base), TrackEnrichmentState.CANDIDATE)
        self.assertTrue(policy.begin_enrichment("human-0"))
        state = policy.complete("human-0", IdentityResult(IdentityStatus.RECOGNIZED, .95), stable=True)
        self.assertEqual(state, TrackEnrichmentState.RECOGNIZED_STABLE)
        self.assertFalse(policy.should_enrich("human-0"))
        self.assertEqual(policy.observe("human-1", self.base), TrackEnrichmentState.CANDIDATE)
        self.assertTrue(policy.should_enrich("human-1"))
        policy.observe("human-0", self.base, visible=False)
        policy.observe("human-0", self.base, visible=False)
        self.assertTrue(policy.should_enrich("human-0"))
        self.assertEqual(policy.state("human-0"), TrackEnrichmentState.CANDIDATE)

    def test_adaptive_sampling_never_disables_detection_and_enters_quiet_mode(self):
        policy = AdaptiveSamplingPolicy(quiet_after_clean_samples=2, minimum_detection_fps=1)
        self.assertEqual(policy.fps(), 5.0)
        policy.observe(has_human=False)
        policy.observe(has_human=False)
        self.assertEqual(policy.state, "quiet")
        self.assertEqual(policy.fps(), 2.0)
        policy.observe(has_human=True)
        self.assertEqual(policy.state, "active")
        self.assertGreaterEqual(policy.fps(), 1.0)

    def test_priority_taxonomy_and_core_p0_boundary(self):
        scheduler = VisionPriorityScheduler()
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.PROTECTED_INTERIOR).priority_hint,
                         VisionPriority.P1_URGENT_PRESENCE.value)
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.RESTRICTED_THRESHOLD).priority_hint,
                         VisionPriority.P1_URGENT_PRESENCE.value)
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.PRIVATE_PERIMETER).priority_hint,
                         VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value)
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.PUBLIC_OUTDOOR).priority_hint,
                         VisionPriority.P4_LOW_PRIORITY_CONTEXT.value)
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.PUBLIC_OUTDOOR,
                                            trigger_reason="motion.sensor.gate").priority_hint,
                         VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value)
        self.assertEqual(scheduler.classify("animal", .9, TopologyClass.PROTECTED_INTERIOR).priority_hint,
                         VisionPriority.P4_LOW_PRIORITY_CONTEXT.value)
        self.assertEqual(scheduler.classify("human", .9, TopologyClass.UNKNOWN).priority_hint,
                         VisionPriority.P4_LOW_PRIORITY_CONTEXT.value)
        p2 = scheduler.classify("human", .9, TopologyClass.PRIVATE_PERIMETER)
        self.assertEqual(scheduler.merge_with_core(p2, VisionPriority.P0_SYSTEM_CRITICAL.value).priority_hint,
                         VisionPriority.P0_SYSTEM_CRITICAL.value)

    def test_priority_queue_preempts_low_priority_without_exceeding_three(self):
        scheduler = VisionPriorityScheduler()
        queue = PriorityFrameQueue(scheduler)
        items = [types.SimpleNamespace(frame_index=index) for index in range(3)]
        for item in items:
            self.assertTrue(queue.enqueue(item, VisionPriority.P4_LOW_PRIORITY_CONTEXT.value))
        urgent = types.SimpleNamespace(frame_index=3)
        self.assertTrue(queue.enqueue(urgent, VisionPriority.P1_URGENT_PRESENCE.value))
        drained = queue.drain_temporal()
        self.assertEqual(len(drained), 3)
        self.assertIn(urgent, drained)
        self.assertEqual(scheduler.priority_evictions, 1)

    def test_evidence_ledger_is_bounded_and_deduplicated(self):
        ledger = EpisodeEvidenceLedger(max_entries=2)
        self.assertTrue(ledger.append(1, VisionPriority.P1_URGENT_PRESENCE.value,
                                      ["human_detected", "protected_interior"], "human-0", self.base))
        self.assertFalse(ledger.append(1, VisionPriority.P1_URGENT_PRESENCE.value,
                                       ["human_detected", "protected_interior"], "human-0", self.base))
        self.assertTrue(ledger.append(2, VisionPriority.P2_CONTEXTUAL_ENRICHMENT.value,
                                      ["human_detected", "private_perimeter"], "human-0", self.base))
        self.assertFalse(ledger.append(3, VisionPriority.P4_LOW_PRIORITY_CONTEXT.value,
                                       ["animal_detected"], "animal-0", self.base))
        self.assertEqual([entry["sequence"] for entry in ledger.snapshot()], [1, 2])
        self.assertEqual(ledger.dropped_entries, 1)

    def test_process_frames_carries_priority_timeline_without_raw_data(self):
        frames = [self.frame(0.1, Detection("human-0", "human", .9, "local://roi/0"))]
        clip = ClipMetadata(clip_id="clip-1", episode_id="episode-1", camera_id="cam-1",
                            topology=Topology("entry", "protected", TopologyClass.PROTECTED_INTERIOR),
                            trigger_reason="motion.sensor.front", started_at=self.base,
                            ends_at=self.base + timedelta(seconds=10))
        events = VisionClipPipelineV1().process_frames(clip, frames,
                                                       {"name": "existing_detector", "model_version": "test", "real_model": True,
                                                        "status": "ok", "frames_sampled": 1, "detections_total": 1,
                                                        "latency_ms": 1.0, "non_human_ignored": 0})
        summary = events[0]["payload"]
        self.assertEqual(summary["priority_hint"], VisionPriority.P1_URGENT_PRESENCE.value)
        self.assertEqual(summary["topology_class"], TopologyClass.PROTECTED_INTERIOR.value)
        self.assertEqual(summary["priority_timeline"][0]["priority_hint"], VisionPriority.P1_URGENT_PRESENCE.value)
        self.assertNotIn('"bbox"', json.dumps(summary))
        self.assertNotIn('"crop"', json.dumps(summary))

    def test_process_video_uses_fake_capture_detector_and_honors_clip_duration(self):
        class FakeFrame:
            def __getitem__(self, _key):
                return self

        class FakeCapture:
            def __init__(self, _path):
                self.frames = 0
                self.released = False

            def isOpened(self):
                return True

            def get(self, _prop):
                return 5.0

            def read(self):
                if self.frames >= 20:
                    return False, None
                self.frames += 1
                return True, FakeFrame()

            def release(self):
                self.released = True

        class FakeDetector:
            def __init__(self):
                self.calls = 0

            def detect(self, _frame):
                self.calls += 1
                return [{"bbox": [0, 0, 30, 30], "score": .8}]

        capture = FakeCapture("clip.mp4")
        cv2_fake = types.SimpleNamespace(VideoCapture=lambda path: capture, CAP_PROP_FPS=5)
        detector = FakeDetector()
        clip = ClipMetadata(
            clip_id="clip-1", episode_id="episode-1", camera_id="cam-1", topology=self.topology,
            trigger_reason="motion.sensor.front", started_at=self.base,
            ends_at=self.base + timedelta(seconds=.4),
        )
        with patch.dict(sys.modules, {"cv2": cv2_fake}):
            events = VisionClipPipelineV1({"tracker_min_bbox_width": 20, "tracker_min_bbox_height": 20}).process_video(
                clip, "clip.mp4", detector, sample_period_seconds=.2)
        summaries = [event for event in events if event["type"].endswith("clip-summary/v1")]
        self.assertEqual(len(summaries), 1)
        self.assertEqual(summaries[0]["payload"]["track"]["id"], "human-0")
        self.assertEqual(summaries[0]["payload"]["track"]["last_seen_at"], (self.base + timedelta(seconds=.4)).isoformat())
        self.assertLessEqual(detector.calls, 3)
        self.assertTrue(capture.released)

    def test_process_video_emits_ordered_deduplicated_observations_and_wall_metrics(self):
        class FakeFrame:
            shape = (40, 40, 3)
            def __getitem__(self, _key):
                return self

        class FakeCapture:
            def __init__(self, _path):
                self.frames = 0
            def isOpened(self):
                return True
            def get(self, _prop):
                return 5.0
            def read(self):
                if self.frames >= 12:
                    return False, None
                self.frames += 1
                return True, FakeFrame()
            def release(self):
                pass

        class FakeDetector:
            def __init__(self):
                self.calls = 0
            def detect_many(self, frames, timestamps):
                self.calls += 1
                return [[{"bbox": [0, 0, 30, 30], "score": .8}] for _ in frames]
            def diagnostic(self):
                return {"name": "fake", "model_version": "fake", "real_model": True, "status": "ok",
                        "frames_sampled": 0, "detections_total": 0, "latency_ms": 3.0,
                        "detector_compute_sum_ms": 3.0, "non_human_ignored": 0}

        cv2_fake = types.SimpleNamespace(VideoCapture=FakeCapture, CAP_PROP_FPS=5)
        pipeline = VisionClipPipelineV1()
        with patch.dict(sys.modules, {"cv2": cv2_fake}):
            events = pipeline.process_video(self.clip(), "clip.mp4", FakeDetector())
        observations = [event for event in events if event["type"].endswith("clip-observation/v1")]
        summaries = [event for event in events if event["type"].endswith("clip-summary/v1")]
        self.assertGreaterEqual(len(observations), 1)
        self.assertTrue(summaries)
        self.assertLess(events.index(observations[-1]), events.index(summaries[0]))
        sequences = [event["payload"]["sequence"] for event in observations]
        self.assertEqual(sequences, sorted(set(sequences)))
        metrics = summaries[0]["payload"]["metrics"]
        self.assertEqual(metrics["peak_frames_in_flight"], 3)
        self.assertGreaterEqual(metrics["frames_sampled"], 1)
        self.assertGreaterEqual(metrics["vision_wall_latency_ms"], metrics["first_observation_wall_ms"])

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
