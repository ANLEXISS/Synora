"""Strict immutable FaceDB loading for the real Vision Worker boundary."""

import hashlib
import json
import logging
import math
import os
import threading


log = logging.getLogger("synora.vision.face_dataset")

SCHEMA_VERSION = 1
MANIFEST_MAX_BYTES = 4 * 1024 * 1024
CURRENT_MAX_BYTES = 256
EMBEDDING_DIMENSION = 512
SUPPORTED_IMAGE_EXTENSIONS = (".jpg", ".jpeg", ".png", ".webp")


class FaceDatasetError(RuntimeError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message


def safe_component(value):
    return (
        isinstance(value, str)
        and bool(value)
        and value not in (".", "..")
        and os.path.basename(value) == value
        and not os.path.isabs(value)
        and "/" not in value
        and "\\" not in value
        and "\x00" not in value
    )


def sha256_file(path):
    digest = hashlib.sha256()
    try:
        with open(path, "rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError as exc:
        raise FaceDatasetError("source_invalid", "dataset source unavailable") from exc
    return digest.hexdigest()


def model_fingerprint(path):
    return sha256_file(path)


def _no_checksum_manifest(manifest):
    value = dict(manifest)
    value["checksum"] = ""
    # Go's json.Marshal emits fields in struct order. JSON loaded from the Go
    # manifest retains that order; compact separators match its digest input.
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def manifest_checksum(manifest):
    return hashlib.sha256(_no_checksum_manifest(manifest)).hexdigest()


def _reject_symlink(path, root):
    root = os.path.abspath(root)
    path = os.path.abspath(path)
    try:
        if os.path.commonpath((root, path)) != root:
            raise FaceDatasetError("path_outside_root", "dataset path is outside canonical root")
    except ValueError as exc:
        raise FaceDatasetError("path_outside_root", "dataset path is outside canonical root") from exc
    relative = os.path.relpath(path, root)
    current = root
    for component in ("" if relative == "." else relative).split(os.sep):
        if not component:
            continue
        current = os.path.join(current, component)
        try:
            if os.lstat(current).st_mode & 0o170000 == 0o120000:
                raise FaceDatasetError("symlink_rejected", "dataset path contains a symlink")
        except FileNotFoundError:
            return


def _regular_file(path, root, code="source_invalid"):
    _reject_symlink(path, root)
    try:
        info = os.lstat(path)
    except OSError as exc:
        raise FaceDatasetError(code, "dataset file unavailable") from exc
    if not os.path.isfile(path) or info.st_mode & 0o170000 != 0o100000:
        raise FaceDatasetError(code, "dataset file is not regular")
    return info


def _load_manifest(version_dir, expected_version, recognizer):
    root = os.path.dirname(os.path.dirname(os.path.dirname(version_dir)))
    _reject_symlink(version_dir, root)
    if not safe_component(expected_version) or os.path.basename(version_dir) != expected_version:
        raise FaceDatasetError("path_outside_root", "invalid dataset version")
    manifest_path = os.path.join(version_dir, "manifest.json")
    info = _regular_file(manifest_path, root, "manifest_invalid")
    if info.st_size > MANIFEST_MAX_BYTES:
        raise FaceDatasetError("manifest_too_large", "dataset manifest is too large")
    try:
        with open(manifest_path, "rb") as stream:
            manifest = json.load(stream)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        raise FaceDatasetError("manifest_invalid", "dataset manifest is not valid JSON") from exc

    if manifest.get("schema_version") != SCHEMA_VERSION:
        raise FaceDatasetError("schema_version_unsupported", "unsupported dataset schema")
    if manifest.get("version") != expected_version:
        raise FaceDatasetError("manifest_invalid", "manifest version mismatch")
    supplied_checksum = manifest.get("checksum")
    if not isinstance(supplied_checksum, str) or supplied_checksum != manifest_checksum(manifest):
        raise FaceDatasetError("checksum_invalid", "dataset manifest checksum mismatch")

    entries = manifest.get("entries")
    dimension = manifest.get("embedding_dimension")
    if not isinstance(entries, list) or not isinstance(dimension, int) or dimension < 0:
        raise FaceDatasetError("manifest_invalid", "dataset manifest fields are invalid")
    if entries and dimension != getattr(recognizer, "embedding_dim", EMBEDDING_DIMENSION):
        raise FaceDatasetError("embedding_dimension_mismatch", "dataset embedding dimension mismatch")

    actual_fingerprint = recognizer.model_fingerprint()
    declared_fingerprint = manifest.get("model_fingerprint", "")
    if entries and (not declared_fingerprint or declared_fingerprint != actual_fingerprint):
        raise FaceDatasetError("model_fingerprint_mismatch", "dataset model fingerprint mismatch")

    seen_photos = set()
    normalized_entries = []
    for entry in entries:
        resident_id = entry.get("resident_id")
        photo_id = entry.get("photo_id")
        storage_key = entry.get("storage_key")
        if not safe_component(resident_id) or not safe_component(photo_id):
            raise FaceDatasetError("association_invalid", "invalid resident/photo association")
        if photo_id in seen_photos:
            raise FaceDatasetError("association_invalid", "duplicate photo association")
        seen_photos.add(photo_id)
        parts = storage_key.split("/") if isinstance(storage_key, str) else []
        if len(parts) != 2 or parts[0] != resident_id or not safe_component(parts[1]):
            raise FaceDatasetError("association_invalid", "invalid dataset storage key")
        embedding = entry.get("embedding")
        if not isinstance(embedding, list) or len(embedding) != dimension:
            raise FaceDatasetError("embedding_dimension_mismatch", "invalid dataset embedding dimension")
        try:
            values = [float(item) for item in embedding]
        except (TypeError, ValueError) as exc:
            raise FaceDatasetError("embedding_invalid", "dataset embedding is not numeric") from exc
        norm = math.sqrt(sum(item * item for item in values))
        if not math.isfinite(norm) or norm <= 0.0 or not all(math.isfinite(item) for item in values):
            raise FaceDatasetError("embedding_invalid", "dataset embedding is not finite")

        source_dir = os.path.join(version_dir, "sources", resident_id)
        _reject_symlink(source_dir, root)
        candidates = []
        try:
            for name in os.listdir(source_dir):
                if name.startswith(photo_id) and os.path.splitext(name)[1].lower() in SUPPORTED_IMAGE_EXTENSIONS:
                    candidates.append(name)
        except OSError as exc:
            raise FaceDatasetError("source_invalid", "dataset source directory unavailable") from exc
        if len(candidates) != 1:
            raise FaceDatasetError("association_invalid", "manifest photo source association is incomplete")
        source = os.path.join(source_dir, candidates[0])
        source_info = _regular_file(source, root)
        if source_info.st_size != int(entry.get("size_bytes", -1)):
            raise FaceDatasetError("source_invalid", "dataset source size mismatch")
        if sha256_file(source) != entry.get("checksum"):
            raise FaceDatasetError("source_invalid", "dataset source checksum mismatch")
        normalized_entries.append({
            "resident_id": resident_id,
            "photo_id": photo_id,
            "embedding": values,
        })

    return manifest, normalized_entries


class FaceDatasetManager:
    """Owns dataset lifecycle; FaceRecognizer owns the atomic DB pointer."""

    def __init__(self, root, recognizer):
        self.root = os.path.abspath(os.path.realpath(root))
        self.recognizer = recognizer
        self._state_lock = threading.RLock()
        self.status = "unavailable"
        self.failure_code = ""
        self.loaded_version = ""
        self.active_revision = 0
        self.embedding_dimension = getattr(recognizer, "embedding_dim", EMBEDDING_DIMENSION)
        self.model_fingerprint = ""

    def _version_dir(self, version, root=None):
        supplied_root = os.path.abspath(root or self.root)
        if supplied_root != self.root:
            raise FaceDatasetError("path_outside_root", "worker root does not match canonical root")
        if not safe_component(version):
            raise FaceDatasetError("path_outside_root", "invalid dataset version")
        versions_root = os.path.join(self.root, "datasets", "versions")
        path = os.path.join(versions_root, version)
        _reject_symlink(path, self.root)
        return path

    def reload(self, version, root=None):
        version = str(version or "").strip()
        with self._state_lock:
            if version == self.loaded_version:
                return self.snapshot()

        version_dir = self._version_dir(version, root)
        # All expensive validation and allocation happens before the active
        # pointer is touched or the recognizer lock is acquired.
        manifest, entries = _load_manifest(version_dir, version, self.recognizer)
        try:
            database = self.recognizer.build_face_db(entries)
        except (KeyError, ValueError) as exc:
            raise FaceDatasetError("embedding_invalid", "failed to build FaceDB") from exc
        self.recognizer.swap_face_db(
            database,
            version=manifest["version"],
            revision=manifest.get("desired_revision", 0),
            fingerprint=manifest.get("model_fingerprint", ""),
        )
        with self._state_lock:
            self.loaded_version = manifest["version"]
            self.active_revision = int(manifest.get("desired_revision", 0))
            self.embedding_dimension = int(manifest.get("embedding_dimension", 0))
            self.model_fingerprint = manifest.get("model_fingerprint", "")
            self.status = "active"
            self.failure_code = ""
            return self.snapshot()

    def startup(self):
        current_path = os.path.join(self.root, "datasets", "current")
        if not os.path.lexists(current_path):
            with self._state_lock:
                self.status = "empty"
                self.failure_code = ""
            return self.snapshot()
        try:
            _regular_file(current_path, self.root, "current_invalid")
            with open(current_path, "rb") as stream:
                raw = stream.read(CURRENT_MAX_BYTES + 1)
            if len(raw) > CURRENT_MAX_BYTES:
                raise FaceDatasetError("current_invalid", "current pointer is too large")
            version = raw.decode("utf-8").strip()
            if not safe_component(version):
                raise FaceDatasetError("current_invalid", "current pointer is invalid")
            return self.reload(version, self.root)
        except FaceDatasetError as exc:
            with self._state_lock:
                self.status = "error"
                self.failure_code = exc.code
            log.error("face dataset startup failed code=%s", exc.code)
            raise

    def snapshot(self):
        with self._state_lock:
            return {
                "loaded_version": self.loaded_version,
                "active_revision": self.active_revision,
                "dimension": self.embedding_dimension,
                "fingerprint": self.model_fingerprint,
                "status": self.status,
                "failure_code": self.failure_code,
            }
