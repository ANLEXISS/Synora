import os
import json
import socket
import sys
import threading
import unittest
from unittest import mock

ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from worker import VisionWorker


class WorkerRuntimeTests(unittest.TestCase):

    def test_init_continues_when_flask_is_missing(self):
        real_import = __import__

        def import_without_flask(name, *args, **kwargs):
            if name == "flask":
                raise ImportError("flask unavailable")
            return real_import(name, *args, **kwargs)

        with mock.patch("builtins.__import__", side_effect=import_without_flask):
            worker = VisionWorker(dry_run=True)
            self.assertIsNone(worker.debug_app)
            self.assertEqual(worker.debug_http_error, "flask_not_installed")

    def test_protocol_hello_is_compatible_and_reports_capabilities(self):
        worker = VisionWorker(dry_run=True)
        response = worker.process_request({
            "request_id": "hello-1",
            "operation": "protocol.hello",
            "protocol_version": "synora.vision.v1",
        })

        self.assertEqual(response["request_id"], "hello-1")
        self.assertEqual(response["operation"], "protocol.hello")
        self.assertEqual(response["protocol_version"], "synora.vision.v1")
        self.assertEqual(response["status"], "normal")
        self.assertEqual(response["backend"], "dry_run")
        self.assertEqual(response["embedding_dimension"], 512)
        self.assertEqual(response["face_dataset"]["status"], "not_configured")
        self.assertIn("face_detection", response["capabilities"])

        encoded = json.dumps(response, separators=(",", ":"), allow_nan=False)
        self.assertEqual(json.loads(encoded), response)

    def test_protocol_hello_rejects_unknown_version(self):
        worker = VisionWorker(dry_run=True)
        response = worker.process_request({
            "request_id": "hello-2",
            "operation": "protocol.hello",
            "protocol_version": "legacy",
        })
        self.assertEqual(response["failure_code"], "protocol_version_unsupported")
        self.assertEqual(response["status"], "degraded")

    def test_degraded_runtime_never_produces_clip_result(self):
        worker = VisionWorker.__new__(VisionWorker)
        worker.dry_run = False
        worker.pipeline_error = "RKNN unavailable"
        worker.face_recognizer = None
        worker.person_detector = None
        worker.pipeline = None
        worker.face_dataset = None
        worker.face_dataset_startup_error = None
        worker.debug_http_error = None
        response = worker.process_request({
            "request_id": "clip-1",
            "operation": "clip.process",
            "clip_path": "/tmp/clip.mp4",
            "camera_id": "cam-1",
        })
        self.assertEqual(response["request_id"], "clip-1")
        self.assertEqual(response["failure_code"], "worker_degraded")
        self.assertEqual(response["capabilities"]["status"], "degraded")
        self.assertNotIn("events", response)

    def test_socket_session_requires_hello_before_clip(self):
        worker = VisionWorker(dry_run=True)
        client, server = socket.socketpair()
        thread = threading.Thread(target=worker.serve_connection, args=(server,))
        thread.start()
        try:
            client.sendall((json.dumps({
                "request_id": "clip-2",
                "operation": "clip.process",
                "clip_path": "/tmp/clip.mp4",
                "camera_id": "cam-1",
            }) + "\n").encode())
            response = json.loads(client.makefile("r").readline())
            self.assertEqual(response["failure_code"], "protocol_handshake_required")
        finally:
            client.close()
            thread.join(timeout=1)
            self.assertFalse(thread.is_alive())

    def test_dry_run_clip_summary_contains_non_real_backend_diagnostic(self):
        worker = VisionWorker(dry_run=True)
        response = worker.process_request({
            "request_id": "clip-v1-dry",
            "operation": "clip.process",
            "pipeline": "clip-v1",
            "clip_path": "/tmp/clip.mp4",
            "clip_id": "clip-v1-dry",
            "camera_id": "cam-1",
            "episode_id": "episode-1",
            "node_id": "entry",
            "zone": "interior_entry",
            "trigger_reason": "motion",
            "started_at": "2026-09-18T10:00:00Z",
            "ends_at": "2026-09-18T10:00:10Z",
            "mock_frames": [{
                "at": "2026-09-18T10:00:01Z",
                "detections": [{"track_id": "track-1", "subject_type": "human", "confidence": .8}],
            }],
        })
        self.assertEqual(response["request_id"], "clip-v1-dry")
        backend = response["events"][-1]["payload"]["backend"]
        self.assertFalse(backend["real_model"])
        self.assertEqual(backend["status"], "unavailable")
        self.assertNotIn("frame", backend)
        self.assertNotIn("embedding", response["events"][-1]["payload"])

    def test_real_clip_v1_requires_explicit_shadow_mode(self):
        worker = VisionWorker.__new__(VisionWorker)
        worker.dry_run = False
        worker.clip_v1_enabled = False
        worker.detector_mode = "unavailable"
        response = worker.process_request({
            "request_id": "clip-v1-disabled",
            "operation": "clip.process",
            "pipeline": "clip-v1",
        })
        self.assertEqual(response["failure_code"], "vision_clip_v1_disabled")

    def test_segment_process_reuses_clip_v1_contract_in_dry_run(self):
        worker = VisionWorker(dry_run=True)
        response = worker.process_request({
            "request_id": "segment-1", "operation": "segment.process", "pipeline": "clip-v1",
            "segment_id": "segment-1", "clip_path": "/tmp/segment.mp4", "camera_id": "cam-1",
            "episode_id": "episode-1", "node_id": "entry", "zone": "protected_interior",
            "topology_class": "protected_interior", "trigger_reason": "motion",
            "started_at": "2026-09-18T10:00:00Z", "ends_at": "2026-09-18T10:00:01Z",
            "mock_frames": [{"at": "2026-09-18T10:00:00.500Z", "detections": []}],
        })
        self.assertEqual(response["request_id"], "segment-1")
        self.assertEqual(response["events"][-1]["type"], "synora.vision.clip-summary/v1")
        self.assertEqual(response["events"][-1]["payload"]["episode_id"], "episode-1")


if __name__ == "__main__":
    unittest.main()
