"""Shared V1 aggregate evidence contract adapter.

The JSON Schema in pkg/contract is the cross-language vocabulary source.
This validator intentionally rejects unknown fields, so pixel/raw payloads and
future semantic additions cannot leak through as extensions.
"""

from __future__ import annotations

from datetime import datetime
import json
import math
from pathlib import Path
import re
from typing import Any


_ROOT = Path(__file__).resolve().parents[3]
_SCHEMA = json.loads((_ROOT / "pkg/contract/vision-evidence-v1.schema.json").read_text())
SCHEMA_VERSION = _SCHEMA["$id"]
_OPAQUE_ID = re.compile(r"^(ev|ep)_[a-f0-9]{24,64}$")


class VisionEvidenceContractError(ValueError):
    pass


def _enum(value: Any, schema: dict[str, Any], label: str) -> None:
    if "enum" in schema and value not in schema["enum"]:
        raise VisionEvidenceContractError(f"{label}: unsupported categorical value")
    if "const" in schema and value != schema["const"]:
        raise VisionEvidenceContractError(f"{label}: unsupported schema version")


def _unit(value: Any, label: str) -> None:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= 1:
        raise VisionEvidenceContractError(f"{label}: expected finite value in [0,1]")


def _support(value: Any, window: float, label: str, availability: str, state: str, confidence: float, quality: float) -> None:
    schema = _SCHEMA["$defs"]["support"]
    _object(value, schema, label)
    if isinstance(value["valid_evaluations"], bool) or not isinstance(value["valid_evaluations"], int) or not 0 <= value["valid_evaluations"] <= 1_000_000:
        raise VisionEvidenceContractError(f"{label}: invalid valid_evaluations")
    if isinstance(value["gap_count"], bool) or not isinstance(value["gap_count"], int) or not 0 <= value["gap_count"] <= 1_000_000:
        raise VisionEvidenceContractError(f"{label}: invalid gap_count")
    _enum(value["continuity"], schema["properties"]["continuity"], label + ".continuity")
    seconds = value["supported_seconds"]
    if isinstance(seconds, bool) or not isinstance(seconds, (int, float)) or not math.isfinite(seconds) or not 0 <= seconds <= window:
        raise VisionEvidenceContractError(f"{label}: invalid supported_seconds")
    evaluations = value["valid_evaluations"]
    if availability == "evaluated":
        if evaluations == 0:
            raise VisionEvidenceContractError(f"{label}: evaluated requires valid evaluations")
    elif evaluations or seconds or value["gap_count"] or value["continuity"] != "unknown" or confidence or quality:
        raise VisionEvidenceContractError(f"{label}: unavailable/not_requested must have empty support and zero scores")
    if value["gap_count"] > evaluations > 0:
        raise VisionEvidenceContractError(f"{label}: gap_count exceeds valid evaluations")
    if availability == "evaluated" and value["continuity"] == "continuous" and value["gap_count"]:
        raise VisionEvidenceContractError(f"{label}: continuous support cannot contain gaps")
    if availability == "evaluated" and value["continuity"] in {"gapped", "intermittent"} and not value["gap_count"]:
        raise VisionEvidenceContractError(f"{label}: gapped/intermittent support requires gaps")
    if availability != "evaluated" and state != "unknown" and not (availability == "not_requested" and state == "none") and not (availability == "unavailable" and state == "unavailable"):
        raise VisionEvidenceContractError(f"{label}: non-evaluated state must be unknown")


def _object(value: Any, schema: dict[str, Any], label: str) -> None:
    if not isinstance(value, dict):
        raise VisionEvidenceContractError(f"{label}: expected object")
    required = set(schema.get("required", []))
    props = set(schema.get("properties", {}))
    if set(value) - props or required - set(value):
        raise VisionEvidenceContractError(f"{label}: fields do not match V1 schema")


def _measure(value: Any, window: float, label: str, states: list[str]) -> None:
    schema = _SCHEMA["$defs"]["measure"]
    _object(value, schema, label)
    _enum(value["availability"], _SCHEMA["$defs"]["availability"], label + ".availability")
    if value["state"] not in states:
        raise VisionEvidenceContractError(f"{label}: unsupported state")
    _unit(value["confidence"], label + ".confidence")
    _unit(value["quality"], label + ".quality")
    _support(value["support"], window, label + ".support", value["availability"], value["state"], value["confidence"], value["quality"])


def validate_vision_evidence_v1(value: Any) -> dict[str, Any]:
    _object(value, _SCHEMA, "evidence")
    for key, child in _SCHEMA["properties"].items():
        if key in value and isinstance(child, dict):
            _enum(value[key], child, key)
    if not isinstance(value["event_id"], str) or not _OPAQUE_ID.fullmatch(value["event_id"]) or not value["event_id"].startswith("ev_"):
        raise VisionEvidenceContractError("event_id must be opaque")
    if not isinstance(value["episode_id"], str) or not _OPAQUE_ID.fullmatch(value["episode_id"]) or not value["episode_id"].startswith("ep_"):
        raise VisionEvidenceContractError("episode_id must be opaque")
    try:
        start = datetime.fromisoformat(value["window_start"].replace("Z", "+00:00"))
        end = datetime.fromisoformat(value["window_end"].replace("Z", "+00:00"))
    except (TypeError, ValueError, AttributeError) as exc:
        raise VisionEvidenceContractError("invalid time window") from exc
    duration = value["window_seconds"]
    if start.tzinfo is None or end.tzinfo is None or start.utcoffset().total_seconds() != 0 or end.utcoffset().total_seconds() != 0 or end <= start or isinstance(duration, bool) or not isinstance(duration, (int, float)) or not math.isfinite(duration) or not 0 <= duration <= 86400 or abs((end - start).total_seconds() - duration) > .001:
        raise VisionEvidenceContractError("inconsistent time window")
    if value["provenance"] not in {"real", "replay", "simulated_test"} or not isinstance(value["simulated_camera"], bool):
        raise VisionEvidenceContractError("invalid provenance")
    if (value["provenance"] == "simulated_test") != value["simulated_camera"]:
        raise VisionEvidenceContractError("simulated provenance marker mismatch")
    if value["producer_health"] not in _SCHEMA["properties"]["producer_health"]["enum"] or value["processing_status"] not in _SCHEMA["properties"]["processing_status"]["enum"]:
        raise VisionEvidenceContractError("invalid producer/processing health")

    health_states = _SCHEMA["$defs"]["health"]["allOf"][1]["properties"]["state"]["enum"]
    _measure(value["camera_health"], duration, "camera_health", health_states)
    _measure(value["trigger"], duration, "trigger", _SCHEMA["properties"]["trigger"]["properties"]["state"]["enum"])
    _object(value["presence"], _SCHEMA["properties"]["presence"], "presence")
    presence_states = _SCHEMA["$defs"]["presence"]["allOf"][1]["properties"]["state"]["enum"]
    for name in ("human", "vehicle", "animal"):
        _measure(value["presence"][name], duration, "presence." + name, presence_states)
    human_tracks = value["presence"].get("human_track_count", 0)
    confirmed_tracks = value["presence"].get("confirmed_human_tracks", 0)
    vehicle_tracks = value["presence"].get("vehicle_track_count", 0)
    animal_tracks = value["presence"].get("animal_track_count", 0)
    if any(isinstance(item, bool) or not isinstance(item, int) or not 0 <= item <= 1_000_000 for item in (human_tracks, confirmed_tracks, vehicle_tracks, animal_tracks)) or confirmed_tracks > human_tracks:
        raise VisionEvidenceContractError("presence: invalid aggregate human track counts")
    _measure(value["activity"], duration, "activity", _SCHEMA["properties"]["activity"]["properties"]["state"]["enum"])

    pose = value["pose"]
    pose_schema = _SCHEMA["properties"]["pose"]
    _object(pose, pose_schema, "pose")
    _enum(pose["availability"], _SCHEMA["$defs"]["availability"], "pose.availability")
    if pose["posture"] not in pose_schema["properties"]["posture"]["enum"]:
        raise VisionEvidenceContractError("pose: unsupported posture")
    _unit(pose["posture_confidence"], "pose.posture_confidence")
    _unit(pose["transition_to_ground_confidence"], "pose.transition_to_ground_confidence")
    _unit(pose["quality"], "pose.quality")
    _support(pose["support"], duration, "pose.support", pose["availability"], pose["posture"], pose["posture_confidence"], pose["quality"])
    immobility = pose["immobility_seconds"]
    if isinstance(immobility, bool) or not isinstance(immobility, (int, float)) or not math.isfinite(immobility) or not 0 <= immobility <= duration or (immobility > 0 and value["presence"]["human"]["state"] != "present"):
        raise VisionEvidenceContractError("pose: invalid immobility duration")

    semantic_schema = _SCHEMA["$defs"]["semantic_result"]
    for family in ("face", "plate"):
        item = value[family]
        _object(item, semantic_schema, family)
        _enum(item["availability"], _SCHEMA["$defs"]["availability"], family + ".availability")
        result_schema = semantic_schema["properties"]["result"]
        if family == "face" and item["result"] == "candidate":
            if item["availability"] != "evaluated":
                raise VisionEvidenceContractError("face.candidate requires evaluated availability")
        else:
            _enum(item["result"], result_schema, family + ".result")
        _unit(item["confidence"], family + ".confidence")
        _unit(item["quality"], family + ".quality")
        _support(item["support"], duration, family + ".support", item["availability"], item["result"], item["confidence"], item["quality"])

    sensitive = value["sensitive_object"]
    _object(sensitive, _SCHEMA["properties"]["sensitive_object"], "sensitive_object")
    _enum(sensitive["availability"], _SCHEMA["$defs"]["availability"], "sensitive_object.availability")
    _enum(sensitive["category"], _SCHEMA["properties"]["sensitive_object"]["properties"]["category"], "sensitive_object.category")
    _unit(sensitive["confidence"], "sensitive_object.confidence")
    _unit(sensitive["quality"], "sensitive_object.quality")
    _support(sensitive["support"], duration, "sensitive_object.support", sensitive["availability"], sensitive["category"], sensitive["confidence"], sensitive["quality"])

    media = value["media"]
    _object(media, _SCHEMA["properties"]["media"], "media")
    _enum(media["availability"], _SCHEMA["$defs"]["availability"], "media.availability")
    _enum(media["episode_state"], _SCHEMA["properties"]["media"]["properties"]["episode_state"], "media.episode_state")
    _support(media["support"], duration, "media.support", media["availability"], media["episode_state"], 0, 0)
    if media["availability"] == "evaluated":
        state, support = media["episode_state"], media["support"]
        if state == "continuous" and (support["gap_count"] or support["continuity"] != "continuous"):
            raise VisionEvidenceContractError("media: continuous state conflicts with support")
        if state == "gapped" and (not support["gap_count"] or support["continuity"] not in {"gapped", "intermittent"}):
            raise VisionEvidenceContractError("media: gapped state conflicts with support")
        if state == "recovered" and not support["gap_count"]:
            raise VisionEvidenceContractError("media: recovered state requires a prior gap")
    if value.get("error_code") is not None and (not isinstance(value["error_code"], str) or not re.fullmatch(r"[a-z][a-z0-9_]{0,63}", value["error_code"])):
        raise VisionEvidenceContractError("invalid error_code")
    aggregate = value.get("runtime_aggregate")
    if aggregate is not None:
        schema = _SCHEMA["properties"]["runtime_aggregate"]
        _object(aggregate, schema, "runtime_aggregate")
        for key, child_schema in schema["properties"].items():
            if key in aggregate and isinstance(child_schema, dict):
                _enum(aggregate[key], child_schema, "runtime_aggregate." + key)
        for key in ("risk_confidence", "face_quality", "face_confidence", "aggregate_confidence"):
            _unit(aggregate[key], "runtime_aggregate." + key)
        for key in ("ground_duration_seconds", "risk_persistence_seconds"):
            number = aggregate[key]
            if isinstance(number, bool) or not isinstance(number, (int, float)) or not math.isfinite(number) or not 0 <= number <= duration:
                raise VisionEvidenceContractError("runtime_aggregate: invalid duration")
        for key in ("risk_observation_count", "face_consensus_frames"):
            number = aggregate[key]
            if isinstance(number, bool) or not isinstance(number, int) or not 0 <= number <= 1_000_000:
                raise VisionEvidenceContractError("runtime_aggregate: invalid count")
        if aggregate["physical_interaction_candidate"] and aggregate["interaction_state"] != "candidate":
            raise VisionEvidenceContractError("runtime_aggregate: interaction state mismatch")
        if aggregate["risk_status"] == "confirmed" and aggregate["risk_kind"] == "none":
            raise VisionEvidenceContractError("runtime_aggregate: confirmed risk requires kind")
        if aggregate["face_status"] == "recognized" and aggregate["face_confidence"] == 0:
            raise VisionEvidenceContractError("runtime_aggregate: recognized face requires confidence")
        if aggregate["real_detection"] and aggregate["replay_simulation"]:
            raise VisionEvidenceContractError("runtime_aggregate: provenance conflict")
    reasons = value.get("priority_reasons", [])
    if not isinstance(reasons, list) or len(reasons) > 32 or any(not isinstance(reason, str) or not re.fullmatch(r"[a-z][a-z0-9_-]{0,63}", reason) for reason in reasons):
        raise VisionEvidenceContractError("invalid priority_reasons")
    return value


def encode_vision_evidence_v1(value: Any) -> bytes:
    """Validate then emit deterministic UTF-8 JSON for cross-language fixtures."""
    validated = validate_vision_evidence_v1(value)
    return json.dumps(validated, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
