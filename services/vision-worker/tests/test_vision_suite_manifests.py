import json
import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
MANIFEST = ROOT / "testdata/vision-v1/suites/manifest.json"
MODULES = ROOT / "testdata/vision-v1/modules.json"
SUITE_MINIMUMS = {
    "face_known": 10,
    "face_unknown": 10,
    "face_ambiguous": 5,
    "vehicle_presence": 10,
    "plate_reading": 10,
    "animal_presence": 10,
    "camera_health": 10,
}
MODULE_BY_SUITE = {
    "face_known": "face_recognition",
    "face_unknown": "face_recognition",
    "face_ambiguous": "face_recognition",
    "vehicle_presence": "vehicle_classification",
    "plate_reading": "plate_reading",
    "animal_presence": "animal_classification",
    "camera_health": "camera_health",
}
FORBIDDEN_KEYS = re.compile(
    r"(?i)(image|frame|crop|bbox|keypoint|embedding|identity|resident_name|plate_text|local_track_id|face_vector)"
)
TOP_LEVEL_KEYS = {"schema_version", "version", "media_root_env", "slots"}
SLOT_KEYS = {
    "suite", "case_id", "clip_relative_path", "clip_sha256", "technical_metadata",
    "condition_tags", "module", "expected", "provenance", "license", "asset_status", "not_run_reason",
}
EXPECTED_KEYS = {"state", "presence", "confidence_minimum_percent", "subject_ref"}
CONDITION_TAGS = {
    "indoor", "outdoor", "day", "night", "low_light", "frontal", "profile", "occluded", "distant",
    "single_subject", "multi_subject", "empty_scene", "stationary", "moving", "clear", "degraded",
    "resident_known", "identity_unknown", "identity_ambiguous", "vehicle_present", "animal_present", "no_target",
    "healthy_camera", "stream_missing", "frozen_frame", "tamper_suspected",
}


def validate_manifest(data):
    if set(data) != TOP_LEVEL_KEYS or data.get("schema_version") != "synora.vision.media-suite-manifest/v1":
        raise ValueError("invalid manifest header or unknown keys")
    counts = {}
    seen = set()
    for slot in data["slots"]:
        if set(slot) - SLOT_KEYS or not SLOT_KEYS - {"not_run_reason"} <= set(slot):
            raise ValueError("invalid slot keys")
        path = slot["clip_relative_path"]
        if Path(path).is_absolute() or ".." in Path(path).parts or "://" in path:
            raise ValueError("unsafe media path")
        if FORBIDDEN_KEYS.search(path):
            raise ValueError("sensitive value in media path")
        if set(slot["expected"]) - EXPECTED_KEYS:
            raise ValueError("raw or unknown semantic expectation key")
        tags = slot.get("condition_tags")
        if not isinstance(tags, list) or not tags or len(tags) != len(set(tags)) or set(tags) - CONDITION_TAGS:
            raise ValueError("condition_tags are missing, duplicated, or outside the closed taxonomy")
        for key in slot["expected"]:
            if FORBIDDEN_KEYS.search(key):
                raise ValueError("raw Vision field in expectation")
        subject = slot["expected"].get("subject_ref")
        if subject is not None and (slot["suite"] != "face_known" or not re.fullmatch(r"resident_test_[0-9]{2}", subject)):
            raise ValueError("subject reference is not opaque")
        if slot["asset_status"] == "placeholder" and slot["clip_sha256"] != "0" * 64:
            raise ValueError("placeholder hash must remain pending")
        key = (slot["suite"], slot["case_id"])
        if key in seen:
            raise ValueError("duplicate case id")
        seen.add(key)
        counts[slot["suite"]] = counts.get(slot["suite"], 0) + 1
        relevance = {
            "face_known": "resident_known", "face_unknown": "identity_unknown", "face_ambiguous": "identity_ambiguous",
        }
        if slot["suite"] in relevance and relevance[slot["suite"]] not in tags:
            raise ValueError("identity suite is missing its corresponding condition tag")
        if slot["suite"] in {"vehicle_presence", "plate_reading"} and not ({"vehicle_present", "no_target"} & set(tags)):
            raise ValueError("vehicle suite is missing a target condition tag")
        if slot["suite"] == "animal_presence" and not ({"animal_present", "no_target"} & set(tags)):
            raise ValueError("animal suite is missing a target condition tag")
        if slot["suite"] == "camera_health" and not ({"healthy_camera", "stream_missing", "frozen_frame", "tamper_suspected"} & set(tags)):
            raise ValueError("camera-health suite is missing a health condition tag")
    if counts != SUITE_MINIMUMS:
        raise ValueError("slot inventory does not meet declared minima")


class VisionSuiteManifestTests(unittest.TestCase):
    def test_manifest_slots_are_complete_and_only_placeholders(self):
        data = json.loads(MANIFEST.read_text(encoding="utf-8"))
        validate_manifest(data)
        self.assertEqual(data["schema_version"], "synora.vision.media-suite-manifest/v1")
        self.assertEqual(data["media_root_env"], "SYNORA_VISION_MEDIA_ROOT")
        counts = {}
        seen = set()
        for slot in data["slots"]:
            suite = slot["suite"]
            counts[suite] = counts.get(suite, 0) + 1
            self.assertEqual(slot["module"], MODULE_BY_SUITE[suite])
            self.assertEqual(slot["asset_status"], "placeholder")
            self.assertEqual(slot["clip_sha256"], "0" * 64)
            self.assertTrue(slot["not_run_reason"])
            self.assertTrue(slot["clip_relative_path"].startswith(suite + "/"))
            self.assertNotIn("..", Path(slot["clip_relative_path"]).parts)
            self.assertNotIn("://", slot["clip_relative_path"])
            key = (suite, slot["case_id"])
            self.assertNotIn(key, seen)
            seen.add(key)
            subject = slot["expected"].get("subject_ref")
            if subject is not None:
                self.assertEqual(suite, "face_known")
                self.assertRegex(subject, r"^resident_test_[0-9]{2}$")
            self.assertEqual(slot["provenance"], "external_consent_required")
            self.assertEqual(slot["license"], "pending_consent_or_license")
        self.assertEqual(counts, SUITE_MINIMUMS)
        ambiguous = [slot for slot in data["slots"] if slot["suite"] == "face_ambiguous"]
        condition_tags = {tag for slot in ambiguous for tag in slot.get("condition_tags", [])}
        self.assertTrue({"degraded", "profile", "occluded", "distant", "low_light", "identity_ambiguous"} <= condition_tags)

    def test_manifest_rejects_missing_or_unknown_condition_tags(self):
        source = json.loads(MANIFEST.read_text(encoding="utf-8"))
        for tags in ([], ["not_a_condition"]):
            candidate = json.loads(json.dumps(source))
            candidate["slots"][0]["condition_tags"] = tags
            with self.assertRaises(ValueError):
                validate_manifest(candidate)

    def test_inactive_modules_are_explicit(self):
        data = json.loads(MODULES.read_text(encoding="utf-8"))
        self.assertEqual(data["schema_version"], "synora.vision.module-registry/v1")
        modules = {item["name"]: item for item in data["modules"]}
        self.assertEqual(set(modules), set(MODULE_BY_SUITE.values()))
        for item in modules.values():
            self.assertEqual(item["state"], "not_configured")
            self.assertIsNone(item["model_version"])
            self.assertIsNone(item["model_sha256"])

    def test_manifest_rejects_urls_paths_and_raw_vision_fields(self):
        source = json.loads(MANIFEST.read_text(encoding="utf-8"))
        mutations = [
            ("clip_relative_path", "../../private.mp4"),
            ("clip_relative_path", "https://example.invalid/clip.mp4"),
            ("bbox", [1, 2, 3, 4]),
            ("embedding", [0.1, 0.2]),
            ("plate_text", "TEST-123"),
            ("identity", "A Real Person"),
        ]
        for key, value in mutations:
            candidate = json.loads(json.dumps(source))
            slot = candidate["slots"][0]
            if key == "clip_relative_path":
                slot[key] = value
            else:
                slot["expected"][key] = value
            with self.assertRaises(ValueError):
                validate_manifest(candidate)


if __name__ == "__main__":
    unittest.main()
