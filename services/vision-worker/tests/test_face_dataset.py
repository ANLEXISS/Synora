import hashlib
import json
import os
import socket
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

import cv2
import numpy as np

ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from face_dataset import FaceDatasetError, FaceDatasetManager, manifest_checksum
from modules.face.FaceRecognizer import FaceRecognizer
import worker as worker_module
from worker import VisionWorker


class FakeRecognizer:
    embedding_dim = 3

    def __init__(self, vector=None):
        self.vector = np.asarray(vector if vector is not None else [1, 0, 0], dtype=np.float32)
        self.active = None

    def model_fingerprint(self):
        return "fingerprint-test"

    def build_face_db(self, entries):
        database = {}
        for entry in entries:
            database.setdefault(entry["resident_id"], {})[entry["photo_id"]] = np.asarray(entry["embedding"], dtype=np.float32)
        return database

    def swap_face_db(self, database, version="", revision=0, fingerprint=""):
        self.active = (database, version, revision, fingerprint)

    @staticmethod
    def validate_embedding(value, expected_dim=3):
        value = np.asarray(value, dtype=np.float32).flatten() if value is not None else np.array([])
        if value.size != expected_dim:
            return None, "embedding_dimension_mismatch"
        if not np.isfinite(value).all():
            return None, "embedding_non_finite"
        norm = np.linalg.norm(value)
        if norm <= 0 or not np.isfinite(norm):
            return None, "embedding_zero"
        return value / norm, ""


def write_version(root, version, entries, revision=1, fingerprint="fingerprint-test"):
    version_dir = os.path.join(root, "datasets", "versions", version)
    source_dir = os.path.join(version_dir, "sources", "resident-1")
    os.makedirs(source_dir, exist_ok=True)
    source = os.path.join(source_dir, "photo-1.png")
    cv2.imwrite(source, np.full((4, 4, 3), 127, dtype=np.uint8))
    size = os.path.getsize(source)
    with open(source, "rb") as stream:
        checksum = hashlib.sha256(stream.read()).hexdigest()
    manifest = {
        "schema_version": 1,
        "version": version,
        "desired_revision": revision,
        "built_at": "2026-08-10T00:00:00Z",
        "model_fingerprint": fingerprint,
        "embedding_dimension": 3,
        "entries": entries,
        "checksum": "",
    }
    if entries:
        manifest["entries"][0].update({"checksum": checksum, "size_bytes": size, "storage_key": "resident-1/source.png", "media_type": "image/png", "width": 4, "height": 4})
        # The builder stores the copied source under photo_id + extension.
        os.rename(source, os.path.join(source_dir, "photo-1.png"))
    manifest["checksum"] = manifest_checksum(manifest)
    with open(os.path.join(version_dir, "manifest.json"), "w", encoding="utf-8") as stream:
        json.dump(manifest, stream, separators=(",", ":"))
    return manifest


class FaceDatasetTests(unittest.TestCase):
    def test_manifest_current_and_reload_idempotent(self):
        with tempfile.TemporaryDirectory() as root:
            entry = {"resident_id": "resident-1", "photo_id": "photo-1", "embedding": [1, 0, 0]}
            manifest = write_version(root, "v-1", [entry])
            os.makedirs(os.path.join(root, "datasets"), exist_ok=True)
            with open(os.path.join(root, "datasets", "current"), "w") as stream:
                stream.write("v-1\n")
            recognizer = FakeRecognizer()
            manager = FaceDatasetManager(root, recognizer)
            first = manager.startup()
            second = manager.reload("v-1", root)
            self.assertEqual(first["loaded_version"], "v-1")
            self.assertEqual(second, first)
            self.assertEqual(recognizer.active[1], "v-1")
            self.assertEqual(manifest["embedding_dimension"], 3)

    def test_invalid_checksum_and_fingerprint_keep_old_database(self):
        with tempfile.TemporaryDirectory() as root:
            write_version(root, "v-1", [{"resident_id": "resident-1", "photo_id": "photo-1", "embedding": [1, 0, 0]}])
            manager = FaceDatasetManager(root, FakeRecognizer())
            manager.reload("v-1", root)
            bad = write_version(root, "v-2", [{"resident_id": "resident-1", "photo_id": "photo-1", "embedding": [0, 1, 0]}], fingerprint="wrong")
            with self.assertRaises(FaceDatasetError) as raised:
                manager.reload("v-2", root)
            self.assertEqual(raised.exception.code, "model_fingerprint_mismatch")
            self.assertEqual(manager.snapshot()["loaded_version"], "v-1")
            bad["checksum"] = "bad"

    def test_current_corruption_path_and_symlink_are_rejected(self):
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as outside:
            manager = FaceDatasetManager(root, FakeRecognizer())
            os.makedirs(os.path.join(root, "datasets"), exist_ok=True)
            with open(os.path.join(root, "datasets", "current"), "w") as stream:
                stream.write("../outside\n")
            with self.assertRaises(FaceDatasetError) as raised:
                manager.startup()
            self.assertEqual(raised.exception.code, "current_invalid")
            os.unlink(os.path.join(root, "datasets", "current"))
            os.makedirs(os.path.join(outside, "v"), exist_ok=True)
            os.symlink(os.path.join(outside, "v"), os.path.join(root, "datasets", "versions"))
            with self.assertRaises(FaceDatasetError) as raised:
                manager.reload("v", root)
            self.assertEqual(raised.exception.code, "symlink_rejected")

    def test_start_without_dataset_is_empty_and_removed_photo_is_not_recognized(self):
        with tempfile.TemporaryDirectory() as root:
            recognizer = FaceRecognizer.__new__(FaceRecognizer)
            recognizer.embedding_dim = 3
            recognizer.match_threshold = 0.35
            recognizer.uncertain_threshold = 0.20
            recognizer._db_lock = threading.RLock()
            recognizer.resident_embeddings = {}
            manager = FaceDatasetManager(root, FakeRecognizer())
            self.assertEqual(manager.startup()["status"], "empty")
            recognizer.swap_face_db({"resident-1": {"photo-1": np.asarray([1, 0, 0], dtype=np.float32)}}, "v-1", 1, "fp")
            self.assertEqual(recognizer.identify_embedding(np.asarray([1, 0, 0], dtype=np.float32))[1], "resident-1")
            recognizer.swap_face_db({}, "v-2", 2, "fp")
            self.assertEqual(recognizer.identify_embedding(np.asarray([1, 0, 0], dtype=np.float32))[0], "unknown")

    def test_concurrent_recognition_sees_complete_snapshot(self):
        with tempfile.TemporaryDirectory() as root:
            write_version(root, "v-1", [{"resident_id": "resident-1", "photo_id": "photo-1", "embedding": [1, 0, 0]}])
            recognizer = FakeRecognizer()
            manager = FaceDatasetManager(root, recognizer)
            errors = []

            def reload():
                try:
                    manager.reload("v-1", root)
                except Exception as exc:
                    errors.append(exc)

            threads = [threading.Thread(target=reload) for _ in range(8)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join()
            self.assertFalse(errors)
            self.assertEqual(manager.snapshot()["loaded_version"], "v-1")


class EmbedBoundaryTests(unittest.TestCase):
    def make_worker(self, root, detector_faces, embedding):
        worker = VisionWorker.__new__(VisionWorker)
        worker.face_dataset = object()
        worker.face_recognizer = FakeEmbedRecognizer(embedding)
        worker.pipeline = FakePipeline(detector_faces)
        worker.face_dataset_startup_error = None
        return worker

    def test_embed_uses_real_command_boundary_and_correlates_request(self):
        with tempfile.TemporaryDirectory() as root:
            source_dir = os.path.join(root, "sources", "resident-1")
            os.makedirs(source_dir)
            source = os.path.join(source_dir, "source.png")
            cv2.imwrite(source, np.full((160, 160, 3), 127, dtype=np.uint8))
            worker = self.make_worker(root, [{"bbox": (20, 20, 120, 120), "landmarks": [[40, 40], [90, 40], [65, 65], [45, 95], [85, 95]]}], [1, 0, 0])
            with mock.patch.object(worker_module, "FACE_DATA_ROOT", root):
                result = worker.process_request({"request_id": "req-1", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "storage_key": "resident-1/source.png"})
            self.assertEqual(result["request_id"], "req-1")
            self.assertEqual(result["embedding"], [1.0, 0.0, 0.0])
            self.assertNotIn("path", result)

    def test_embed_failure_codes_cover_face_and_embedding_validation(self):
        cases = [([], "no_face"), ([{"bbox": (20, 20, 120, 120), "landmarks": []}, {"bbox": (30, 30, 130, 130), "landmarks": []}], "multiple_faces")]
        with tempfile.TemporaryDirectory() as root:
            source_dir = os.path.join(root, "sources", "resident-1")
            os.makedirs(source_dir)
            cv2.imwrite(os.path.join(source_dir, "source.png"), np.full((160, 160, 3), 127, dtype=np.uint8))
            for faces, code in cases:
                worker = self.make_worker(root, faces, [1, 0, 0])
                with mock.patch.object(worker_module, "FACE_DATA_ROOT", root):
                    result = worker.process_request({"request_id": "req", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "storage_key": "resident-1/source.png"})
                self.assertEqual(result["failure_code"], code)
            for faces, code in [([{"bbox": (20, 20, 40, 40), "landmarks": []}], "face_too_small")]:
                worker = self.make_worker(root, faces, [1, 0, 0])
                with mock.patch.object(worker_module, "FACE_DATA_ROOT", root):
                    result = worker.process_request({"request_id": "req", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "storage_key": "resident-1/source.png"})
                self.assertEqual(result["failure_code"], code)
            for vector, code in [([1, 0], "embedding_dimension_mismatch"), ([float("nan"), 0, 0], "embedding_non_finite"), ([0, 0, 0], "embedding_zero")]:
                worker = self.make_worker(root, [{"bbox": (20, 20, 120, 120), "landmarks": [[40, 40], [90, 40], [65, 65], [45, 95], [85, 95]]}], vector)
                with mock.patch.object(worker_module, "FACE_DATA_ROOT", root):
                    result = worker.process_request({"request_id": "req", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "storage_key": "resident-1/source.png"})
                self.assertEqual(result["failure_code"], code)

    def test_embed_rejects_arbitrary_path_and_backend_timeout(self):
        with tempfile.TemporaryDirectory() as root:
            source_dir = os.path.join(root, "sources", "resident-1")
            os.makedirs(source_dir)
            cv2.imwrite(os.path.join(source_dir, "source.png"), np.full((160, 160, 3), 127, dtype=np.uint8))
            worker = self.make_worker(root, [], [1, 0, 0])
            with mock.patch.object(worker_module, "FACE_DATA_ROOT", root):
                result = worker.process_request({"request_id": "req", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "path": "/etc/passwd"})
            self.assertEqual(result["failure_code"], "path_not_allowed")
            worker = self.make_worker(root, [{"bbox": (20, 20, 120, 120), "landmarks": [[40, 40], [90, 40], [65, 65], [45, 95], [85, 95]]}], [1, 0, 0])
            worker.face_recognizer.embed = lambda _aligned: time.sleep(0.3)
            with mock.patch.object(worker_module, "FACE_DATA_ROOT", root), mock.patch.object(worker_module, "COMMAND_TIMEOUT_SECONDS", 0.1):
                result = worker.process_request({"request_id": "req", "operation": "face_dataset.embed", "resident_id": "resident-1", "photo_id": "photo-1", "storage_key": "resident-1/source.png"})
            self.assertEqual(result["failure_code"], "timeout")

class FakeEmbedRecognizer(FakeRecognizer):
    embedding_dim = 3

    def __init__(self, embedding):
        self.embedding = embedding

    def embed(self, _aligned):
        return np.asarray(self.embedding, dtype=np.float32)


class FakePipeline:
    FACE_MIN_SIZE = 60

    def __init__(self, faces):
        self.face_detector = self
        self.faces = faces

    def detect(self, _image):
        return self.faces

    def make_square_crop(self, image, x1, y1, x2, y2):
        return image[int(y1):int(y2), int(x1):int(x2)]

    def face_quality(self, crop):
        return 1.0 if crop.size else 0.0

    def align_face_arcface(self, image, _landmarks):
        return image[:112, :112]


if __name__ == "__main__":
    unittest.main()
