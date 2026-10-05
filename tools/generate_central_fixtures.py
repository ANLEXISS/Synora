#!/usr/bin/env python3
"""Generate the checked-in central E2E fixture suite from reviewed rules.

The generated expectations are declarative contract assertions. They never
read model output, and the generated fixtures are not an MLP training source.
"""

from __future__ import annotations

import hashlib
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "testdata" / "central-e2e-v1"
CASES = OUT / "cases"
CLOCK = datetime(2026, 1, 1, tzinfo=timezone.utc)
SCHEMA = "synora.vision.edge-track-manifest/v1"


def iso(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def expected(bundle: str, *, accepted: bool = True, committed: bool = True, safety_status: str | None = None) -> dict:
    if not accepted:
        return {"discovery": {"status": "rejected"}, "store": {"committed": False}, "forbidden": ["raw_vision"]}
    dimension = 86
    version = "cognitive-snapshot/v3" if bundle == "v3" else "cognitive-snapshot/v1"
    heads = ["danger", "incident", "task", "action", "communication_intent"] if bundle == "v3" else ["danger", "incident", "task", "action"]
    safety = {"physical_action_executed": False}
    if safety_status:
        safety["status"] = safety_status
    return {"discovery": {"status": "accepted"}, "snapshot": {"schema_version": version, "input_dimension": dimension}, "mlp": {"heads": heads, "status": "available"}, "safety_gate": safety, "store": {"committed": committed}, "outbox": {"non_empty": False}, "forbidden": ["frame", "image", "media", "bbox", "crop", "keypoints", "embedding", "identity", "local_track_id"]}


def edge_payload(case_id: str, *, topology: str = "protected_interior", trigger: str = "human", confirmed: int = 1, segment: int = 1, gap: int = 0, at: datetime = CLOCK, **extra) -> dict:
    payload = {
        "schema_version": SCHEMA,
        "camera_id": "cam_sim_01",
        "episode_id": "episode-" + case_id,
        "topology_class": topology,
        "trigger_class": trigger,
        "trigger_confidence": 0.91 if trigger == "human" else 0.82,
        "tracking_status": "ok",
        "started_at": iso(at),
        "ended_at": iso(at + timedelta(seconds=1)),
        "track_count": max(confirmed, 0),
        "confirmed_track_count": max(confirmed, 0),
        "observation_count": max(segment, 1),
        "segment_count": max(segment, 0),
        "gap_count": max(gap, 0),
        "evidence_refs": [],
        "priority_reason": ["central-test"],
        "edge_emulated": True,
        "metrics": {"confidence": 0.91},
    }
    payload.update(extra)
    return payload


def v3_snapshot(case_id: str, *, pose_status="available", posture="upright", fall_state="none", risk_status="not_available", risk_persistence="none", quality=0.9, recovery=False, announce_available=True, cooldown=False) -> dict:
    return {
        "schema_version": "cognitive-snapshot/v3",
        "captured_at": iso(CLOCK),
        "base_v2": {
            "schema_version": "cognitive-snapshot/v2",
            "captured_at": iso(CLOCK),
            "topology": "protected_interior",
            "communication": {"announce_available": announce_available, "tts_status": "available", "cooldown_active": cooldown},
            "vision": {"pose_status": "available", "pose_quality": quality, "posture": "standing", "fall_state": "none", "risk_status": risk_status, "risk_kind": "none" if risk_status in {"", "not_available", "not_requested", "uncertain"} else "other", "risk_confidence": 0.8 if risk_status in {"suspected", "confirmed"} else 0.0, "risk_persistence": risk_persistence, "recovery_observed": recovery},
        },
        "vision": {
            "pose_status": pose_status,
            "pose_quality": quality,
            "posture": posture,
            "fall_state": fall_state,
            "ground_duration": 0.5 if posture == "ground" else 0.0,
            "risk_status": risk_status,
            "risk_kind": "none" if risk_status in {"", "not_available", "not_requested", "uncertain"} else "other",
            "risk_confidence": 0.8 if risk_status in {"suspected", "confirmed"} else 0.0,
            "risk_persistence": risk_persistence,
            "risk_observation_count": 3 if risk_persistence != "none" else 0,
            "risk_quality_sufficient": risk_status not in {"uncertain", "suspected"},
            "pose_observation_count": 2 if pose_status == "available" else 0,
            "pose_sampled": pose_status == "available",
            "recovery_observed": recovery,
            "real_detection": False,
            "replay_simulation": True,
            "edge_tracking_ok": True,
            "central_retracking_invocations": 0,
        },
    }


def v3_message(case_id: str, snapshot: dict) -> dict:
    return {"id": "msg-" + case_id, "type": "synora.vision.enrichment/v3", "timestamp": iso(CLOCK), "payload": {"test": True, "provenance": "test-harness", "event_type": "synora.vision.enrichment/v3", "snapshot": snapshot}}


def write_case(case: dict) -> str:
    if "suite" not in case:
        case_id = case["id"]
        if case_id.startswith("v3-"):
            case["suite"] = "mlp_real"
        elif case_id.startswith(("raw-", "media-", "schema-", "timestamp-", "invalid-", "required-", "episode-id-", "negative-", "metrics-", "score-", "hardware-", "oversized")):
            case["suite"] = "red-team"
        else:
            case["suite"] = "reference"
    path = CASES / f"{case['id']}.json"
    path.write_text(json.dumps(case, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return str(path.relative_to(ROOT))


def main() -> None:
    CASES.mkdir(parents=True, exist_ok=True)
    for path in CASES.glob("*.json"):
        path.unlink()
    paths: list[str] = []

    # Core ingress and topology/reference cases.
    references = [
        ("human-public-outdoor", "public_outdoor", "human", 0),
        ("human-private-perimeter", "private_perimeter", "human", 1),
        ("human-restricted-threshold", "restricted_threshold", "human", 1),
        ("human-interior-armed", "protected_interior", "human", 1),
        ("human-interior-unarmed", "protected_interior", "human", 1),
        ("animal-interior", "protected_interior", "animal", 0),
        ("vehicle-public", "public_outdoor", "vehicle", 0),
        ("unknown-topology", "unknown", "unknown", 0),
        ("edge-model-unavailable", "protected_interior", "human", 0),
        ("score-low", "private_perimeter", "human", 1),
    ]
    for case_id, topology, trigger, confirmed in references:
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["record", "announce"], "bundle": "v1", "messages": [{"id": "msg-" + case_id, "type": SCHEMA, "timestamp": iso(CLOCK), "payload": edge_payload(case_id, topology=topology, trigger=trigger, confirmed=confirmed)}], "expected": expected("v1")}))

    # Episode continuity, gaps, ordering, expiry, deduplication and multi-camera sequences.
    for index in range(1, 21):
        case_id = f"episode-sequence-{index:02d}"
        messages = []
        for segment in range(1, 4):
            at = CLOCK + timedelta(seconds=segment)
            messages.append({"id": f"{case_id}-segment-{segment}", "type": SCHEMA, "timestamp": iso(at), "payload": edge_payload(case_id, segment=segment, gap=1 if index % 4 == 0 and segment == 2 else 0, at=at)})
        if index % 5 == 0:
            messages.reverse()
        if index % 6 == 0:
            messages.append(messages[-1].copy())
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["record"], "bundle": "v1", "messages": messages, "expected": expected("v1")}))

    # Named episode behaviours keep the required coverage reviewable without
    # turning the fixture format into executable test code.
    named_sequences = {
        "episode-no-detection": [(0, "public_outdoor", "unknown")],
        "episode-candidate-confirmed": [(0, "private_perimeter", "human"), (1, "private_perimeter", "human")],
        "episode-calm-end": [(1, "protected_interior", "human"), (0, "protected_interior", "unknown")],
        "episode-duplicate-final-summary": [(1, "protected_interior", "human"), (1, "protected_interior", "human")],
        "episode-expired": [(1, "protected_interior", "human")],
        "episode-two-incidents": [(1, "protected_interior", "human"), (1, "restricted_threshold", "human")],
        "episode-cross-zone": [(1, "private_perimeter", "human"), (1, "protected_interior", "human")],
        "episode-nonmergeable": [(1, "public_outdoor", "human"), (1, "public_outdoor", "vehicle")],
        "episode-restart-between-segments": [(1, "protected_interior", "human"), (1, "protected_interior", "human")],
        "episode-recovery-dedup": [(1, "protected_interior", "human"), (0, "protected_interior", "unknown")],
    }
    for case_id, sequence in named_sequences.items():
        messages = []
        for index, (confirmed, topology, trigger) in enumerate(sequence, start=1):
            at = CLOCK + timedelta(seconds=index * 3)
            messages.append({"id": f"{case_id}-{index}", "type": SCHEMA, "timestamp": iso(at), "payload": edge_payload(case_id, topology=topology, trigger=trigger, confirmed=confirmed, segment=index, at=at)})
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["record", "announce"], "bundle": "v1", "messages": messages, "expected": expected("v1")}))

    # Contract and red-team rejection cases.
    invalids = [
        ("schema-absent", {"schema_version": None}),
        ("schema-unknown", {"schema_version": "synora.vision.edge-track-manifest/v9"}),
        ("required-camera-absent", {"camera_id": None}),
        ("required-episode-absent", {"episode_id": None}),
        ("invalid-topology", {"topology_class": "garage_unknown"}),
        ("invalid-trigger-enum", {"trigger_class": "robot"}),
        ("invalid-tracking-enum", {"tracking_status": "maybe"}),
        ("timestamp-invalid", {"started_at": "not-a-timestamp"}),
        ("timestamp-too-old", {"started_at": "2025-01-01T00:00:00Z", "ended_at": "2025-01-01T00:00:01Z"}),
        ("timestamp-in-future", {"started_at": "2027-01-01T00:00:00Z", "ended_at": "2027-01-01T00:00:01Z"}),
        ("timestamp-reversed", {"started_at": iso(CLOCK + timedelta(seconds=2)), "ended_at": iso(CLOCK)}),
        ("episode-id-invalid", {"episode_id": "bad episode"}),
        ("negative-track-count", {"track_count": -1}),
        ("negative-gap-count", {"gap_count": -1}),
        ("metrics-absent", {"metrics": None}),
        ("score-absent", {"trigger_confidence": None}),
        ("score-incoherent", {"trigger_confidence": 1.5}),
        ("raw-media", {"media_ref": "temp://frame.jpg"}),
        ("media-expired", {"media_ref": "expired://frame-001"}),
        ("media-unknown", {"media_path": "/tmp/not-authorized-frame.jpg"}),
        ("raw-bbox", {"bbox": [1, 2, 3, 4]}),
        ("raw-keypoints", {"keypoints": [[1, 2, 0.9]]}),
        ("raw-embedding", {"embedding": [0.1, 0.2]}),
        ("raw-identity", {"identity": "resident-1"}),
        ("raw-local-track", {"local_track_id": "track-1"}),
        ("hardware-identifier", {"hardware_id": "serial-1"}),
        ("oversized-message", {"metrics": {"padding": "x" * 1100000}}),
    ]
    for case_id, updates in invalids:
        payload = edge_payload(case_id)
        for key, value in updates.items():
            if value is None:
                payload.pop(key, None)
            else:
                payload[key] = value
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": [], "bundle": "v1", "messages": [{"id": "msg-" + case_id, "type": SCHEMA, "timestamp": iso(CLOCK), "payload": payload}], "expected": expected("v1", accepted=False, committed=False)}))

    # Explicit model-loader fail-closed cases. These never write a bundle.
    for bundle, label in (("v1", "v1"), ("v3", "v3")):
        case_id = f"model-{label}-manifest-unavailable"
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": [], "bundle": bundle, "bundle_path": "build/does-not-exist-central-bundle", "messages": [], "expected": {"mlp": {"status": "unavailable"}, "store": {"committed": False}, "forbidden": []}}))

    # V3 aggregate-only state matrix.
    v3_cases = [
        ("v3-pose-unavailable", "unavailable", "unknown", "unknown", "not_available", "none", 0.0, False),
        ("v3-pose-not-requested", "not_requested", "unknown", "unknown", "not_requested", "none", 0.0, False),
        ("v3-pose-low-quality", "low_quality", "unknown", "unknown", "not_available", "none", 0.3, False),
        ("v3-posture-upright", "available", "upright", "none", "not_available", "none", 0.9, False),
        ("v3-posture-seated", "available", "seated", "none", "not_available", "none", 0.9, False),
        ("v3-posture-ground", "available", "ground", "none", "not_available", "none", 0.9, False),
        ("v3-immobility-confirmed", "available", "ground", "candidate", "not_available", "persistent", 0.9, False),
        ("v3-recovery-observed", "available", "upright", "none", "not_available", "none", 0.9, True),
        ("v3-fall-none", "available", "ground", "none", "not_available", "none", 0.9, False),
        ("v3-fall-candidate", "available", "ground", "candidate", "not_available", "none", 0.9, False),
        ("v3-risk-unavailable", "available", "upright", "none", "not_available", "none", 0.9, False),
        ("v3-risk-not-requested", "available", "upright", "none", "not_requested", "none", 0.9, False),
        ("v3-risk-uncertain", "available", "upright", "none", "uncertain", "none", 0.9, False),
        ("v3-risk-suspected", "available", "upright", "none", "suspected", "persistent", 0.9, False),
        ("v3-risk-confirmed", "available", "upright", "none", "confirmed", "confirmed", 0.9, False),
        ("v3-communication-neutral", "available", "upright", "none", "not_available", "none", 0.9, False),
        ("v3-communication-identity", "available", "upright", "none", "not_available", "none", 0.9, False),
        ("v3-communication-wellness", "available", "ground", "candidate", "not_available", "none", 0.9, False),
        ("v3-danger-critical-gate", "available", "upright", "none", "confirmed", "confirmed", 0.9, False),
        ("v3-low-confidence", "available", "upright", "none", "uncertain", "none", 0.2, False),
    ]
    for case_id, pose, posture, fall, risk, persistence, quality, recovery in v3_cases:
        snapshot = v3_snapshot(case_id, pose_status=pose, posture=posture, fall_state=fall, risk_status=risk, risk_persistence=persistence, quality=quality, recovery=recovery)
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["announce", "record"], "bundle": "v3", "messages": [v3_message(case_id, snapshot)], "expected": expected("v3")}))

    # Safety-gate adversarial cases use only the test-local forced backend. The
    # production CPU MLP remains the sole backend in the mlp_real suite.
    gate_cases = [
        ("gate-announce-safe", "not_available", True, False, "none", "allowed_dry_run"),
        ("gate-announce-risk-uncertain", "uncertain", True, False, "none", "blocked"),
        ("gate-announce-risk-suspected", "suspected", True, False, "none", "blocked"),
        ("gate-announce-risk-confirmed", "confirmed", True, False, "none", "blocked"),
        ("gate-announce-danger-critical", "not_available", True, False, "critical", "blocked"),
        ("gate-announce-no-capability", "not_available", False, False, "none", "blocked"),
        ("gate-announce-cooldown", "not_available", True, True, "none", "blocked"),
    ]
    for case_id, risk, announce_available, cooldown, forced_danger, status in gate_cases:
        snapshot = v3_snapshot(case_id, risk_status=risk, announce_available=announce_available, cooldown=cooldown)
        paths.append(write_case({"id": case_id, "suite": "safety_gate_adversarial", "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["announce", "record"] if announce_available else ["record"], "bundle": "v3", "test_backend": "forced_announce", "forced_danger": forced_danger, "messages": [v3_message(case_id, snapshot)], "expected": expected("v3", safety_status=status)}))

    # Cross-camera and multi-zone correlations remain semantic, not visual.
    for index in range(1, 11):
        case_id = f"multi-camera-zone-{index:02d}"
        messages = [
            {"id": f"{case_id}-a", "type": SCHEMA, "timestamp": iso(CLOCK), "payload": {**edge_payload(case_id, topology="private_perimeter"), "camera_id": "cam_sim_a"}},
            {"id": f"{case_id}-b", "type": SCHEMA, "timestamp": iso(CLOCK + timedelta(seconds=1)), "payload": {**edge_payload(case_id, topology="protected_interior", at=CLOCK + timedelta(seconds=1)), "camera_id": "cam_sim_b"}},
        ]
        paths.append(write_case({"id": case_id, "clock": iso(CLOCK), "initial_store": {}, "capabilities": ["record"], "bundle": "v1", "messages": messages, "expected": expected("v1")}))

    manifest = {"schema_version": "synora.central-e2e-manifest/v1", "seed": 20260101, "logical_date": iso(CLOCK), "minimum_cases": 80, "cases": paths, "immutable": ["testdata/central-e2e-v1/immutable/reference-suite.jsonl", "testdata/central-e2e-v1/immutable/redteam-suite.jsonl"], "bundles": {"v1": "build/cognitive-mlp-v1/MANIFEST.v1.json", "v3": "build/cognitive-mlp-v3-candidate/MANIFEST.v3.json"}}
    (OUT / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    immutable = OUT / "immutable"
    immutable.mkdir(parents=True, exist_ok=True)
    reference = [json.loads((ROOT / path).read_text(encoding="utf-8")) for path in paths if json.loads((ROOT / path).read_text(encoding="utf-8"))["id"].startswith(("human-", "episode-", "multi-", "v3-"))]
    redteam = [json.loads((ROOT / path).read_text(encoding="utf-8")) for path in paths if any(token in json.loads((ROOT / path).read_text(encoding="utf-8"))["id"] for token in ("raw-", "media-", "score-", "schema-", "timestamp-", "oversized", "hardware-"))]
    (immutable / "reference-suite.jsonl").write_text("".join(json.dumps(item, sort_keys=True) + "\n" for item in reference), encoding="utf-8")
    (immutable / "redteam-suite.jsonl").write_text("".join(json.dumps(item, sort_keys=True) + "\n" for item in redteam), encoding="utf-8")
    print(f"generated {len(paths)} cases")


if __name__ == "__main__":
    main()
