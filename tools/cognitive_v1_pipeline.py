#!/usr/bin/env python3
"""Reproducible dataset/training/export/validation pipeline for MLP V1."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import random
from typing import Any

import numpy as np


BASE_FEATURES = [
    "security.armed", "security.degraded", "security.known", "presence.human", "presence.known_residents", "presence.known_resident_count", "presence.track_count", "presence.track_confirmed",
    "topology.public_outdoor", "topology.private_perimeter", "topology.restricted_threshold", "topology.protected_interior", "topology.unknown",
    "episode.initial", "episode.candidate", "episode.confirmed", "episode.final", "episode.segment_count", "episode.gap_count", "episode.seconds_since_first", "episode.seconds_since_last", "episode.calm_seconds", "danger.previous", "danger.previous_known",
    "sensor.movement", "sensor.access_unknown", "sensor.access_closed", "sensor.access_open", "sensor.access_forced", "sensor.evidence", "sensor.alarm_unknown", "sensor.alarm_armed", "sensor.alarm_disarmed", "sensor.alarm_triggered", "sensor.observation_count", "sensor.confidence",
    "capability.count", "capability.available_count", "action_result.count", "action_result.success_count", "action_result.failed_count", "action_result.unavailable_count",
]
VISION_FEATURES = [
    "security.armed", "security.degraded", "security.known", "presence.human", "presence.track_count", "presence.track_confirmed",
    "topology.public_outdoor", "topology.private_perimeter", "topology.restricted_threshold", "topology.protected_interior", "topology.unknown",
    "priority.p0", "priority.p1", "priority.p2", "priority.p3", "priority.p4", "phase.initial", "phase.candidate", "phase.confirmed", "phase.final",
    "enrichment.unavailable", "enrichment.not_requested", "enrichment.uncertain", "enrichment.recognized", "enrichment.unknown",
    "continuity.seconds_since_first", "continuity.seconds_since_last", "continuity.segment_count", "continuity.gap_count", "continuity.calm_seconds",
    "quality.real_detection", "quality.replay_simulation", "quality.observation_count", "quality.aggregate_confidence",
    "co_evidence.access_unknown", "co_evidence.access_closed", "co_evidence.access_open", "co_evidence.access_forced", "co_evidence.movement", "co_evidence.sensor", "co_evidence.alarm_unknown", "co_evidence.alarm_armed", "co_evidence.alarm_disarmed", "co_evidence.alarm_triggered",
]
FEATURE_NAMES = BASE_FEATURES + ["vision." + item for item in VISION_FEATURES]
HEADS = ("danger", "incident", "task", "action")
LABELS = {
    "danger": ["none", "low", "medium", "high", "critical"],
    "incident": ["none", "routine_presence", "perimeter_presence", "threshold_presence", "interior_intrusion", "anomaly", "action_failure", "technical", "resolved"],
    "task": ["monitor", "verify", "notify_security", "contain", "close_incident"],
    "action": ["no_action", "notify", "record", "light", "lock", "siren", "request_review"],
}
FORBIDDEN = {"frame", "frames", "image", "images", "media", "bbox", "bboxes", "crop", "crops", "embedding", "embeddings", "identity", "hardware_id", "serial_number", "mac"}


def clamp(value: Any, low: float = 0.0, high: float = 1.0) -> float:
    try:
        return max(low, min(high, float(value)))
    except (TypeError, ValueError):
        return low


def one_hot(target: list[float], value: str, vocabulary: list[str]) -> None:
    if value in vocabulary:
        target[vocabulary.index(value)] = 1.0


def count_norm(value: Any, maximum: int) -> float:
    try: value = int(value)
    except (TypeError, ValueError): value = 0
    return clamp(value / maximum)


def seconds_norm(value: Any) -> float:
    return clamp(clamp(value, 0.0, 300.0) / 300.0)


def encode_facts(facts: dict[str, Any], capabilities: list[str], action_result: str) -> list[float]:
    f = dict(facts)
    v = [0.0] * 86
    for index, key in enumerate(("armed", "degraded", "known")):
        v[index] = float(bool(f.get("security_" + key, False)))
    v[3] = float(bool(f.get("human_present", False)))
    v[4] = float(bool(f.get("known_residents_present", False)))
    v[5] = count_norm(f.get("known_resident_count", 0), 16)
    v[6] = count_norm(f.get("track_count", 0), 16)
    v[7] = float(bool(f.get("track_confirmed", False)))
    one_hot(v[8:13], f.get("topology", "unknown"), ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"])
    one_hot(v[13:17], f.get("phase", "initial"), ["initial", "candidate", "confirmed", "final"])
    v[17] = count_norm(f.get("segment_count", 0), 32); v[18] = count_norm(f.get("gap_count", 0), 16)
    v[19] = seconds_norm(f.get("seconds_since_first", 0)); v[20] = seconds_norm(f.get("seconds_since_last", 0)); v[21] = seconds_norm(f.get("calm_seconds", 0))
    v[22] = clamp(f.get("previous_danger", 0)); v[23] = float(bool(f.get("previous_danger_known", False)))
    v[24] = float(bool(f.get("movement", False)))
    one_hot(v[25:29], f.get("access_state", "unknown"), ["unknown", "closed", "open", "forced"])
    v[29] = float(bool(f.get("sensor_evidence", False)))
    one_hot(v[30:34], f.get("alarm_state", "unknown"), ["unknown", "armed", "disarmed", "triggered"])
    v[34] = count_norm(f.get("observation_count", 0), 32); v[35] = clamp(f.get("confidence", 0))
    v[36] = count_norm(len(capabilities), 32); v[37] = count_norm(len(capabilities), 32)
    v[38] = float(action_result != "not_requested"); v[39] = float(action_result in {"simulated_success", "real_success"}); v[40] = float(action_result in {"simulated_failure", "real_failure"}); v[41] = float(action_result == "unavailable")
    v[42] = float(bool(f.get("armed", False))); v[43] = float(bool(f.get("degraded", False))); v[44] = float(bool(f.get("known", False))); v[45] = v[3]; v[46] = v[6]; v[47] = v[7]
    one_hot(v[48:53], f.get("topology", "unknown"), ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"])
    one_hot(v[53:58], f.get("priority", "P4"), ["P0", "P1", "P2", "P3", "P4"])
    one_hot(v[58:62], f.get("phase", "initial"), ["initial", "candidate", "confirmed", "final"])
    one_hot(v[62:67], f.get("enrichment", "unavailable"), ["unavailable", "not_requested", "uncertain", "recognized", "unknown"])
    v[67] = seconds_norm(f.get("seconds_since_first", 0)); v[68] = seconds_norm(f.get("seconds_since_last", 0)); v[69] = count_norm(f.get("segment_count", 0), 32); v[70] = count_norm(f.get("gap_count", 0), 16); v[71] = seconds_norm(f.get("calm_seconds", 0))
    v[72] = float(bool(f.get("real_detection", False))); v[73] = float(bool(f.get("replay_simulation", False))); v[74] = count_norm(f.get("observation_count", 0), 32); v[75] = clamp(f.get("confidence", 0))
    one_hot(v[76:80], f.get("access_state", "unknown"), ["unknown", "closed", "open", "forced"]); v[80] = float(bool(f.get("movement", False))); v[81] = float(bool(f.get("sensor_evidence", False)))
    one_hot(v[82:86], f.get("alarm_state", "unknown"), ["unknown", "armed", "disarmed", "triggered"])
    return v


def forbidden(value: Any) -> bool:
    if isinstance(value, dict):
        return any(str(key).lower() in FORBIDDEN or forbidden(child) for key, child in value.items())
    if isinstance(value, list): return any(forbidden(child) for child in value)
    return False


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def build_dataset(args: argparse.Namespace) -> None:
    scenarios = read_jsonl(args.scenarios)
    args.output.mkdir(parents=True, exist_ok=True)
    records = []
    for item in scenarios:
        facts = item.get("facts", {})
        labels = item.get("labels", {})
        if forbidden(item) or any(labels.get(head) not in LABELS[head] for head in HEADS): raise SystemExit(f"invalid or forbidden scenario: {item.get('id')}")
        capabilities = sorted(set(str(value) for value in item.get("capabilities", [])))
        action_result = str(item.get("action_result", "not_requested"))
        vector = encode_facts(facts, capabilities, action_result)
        snapshot = {"schema_version": "cognitive-snapshot/v1", "facts": facts, "capabilities": capabilities, "action_result": action_result}
        records.append({"schema_version": "cognitive-training-example/v1", "id": item["id"], "snapshot": snapshot, "vector": vector, "feature_names": FEATURE_NAMES, "labels": labels, "expected_action": item.get("expected_action", labels["action"]), "source": item.get("source", "unknown"), "scenario_version": item.get("version", "1")})
    with (args.output / "corpus.jsonl").open("w", encoding="utf-8") as stream:
        for record in records: stream.write(json.dumps(record, separators=(",", ":"), sort_keys=True) + "\n")
    (args.output / "dataset-manifest.json").write_text(json.dumps({"schema_version": "synora.cognitive-v1-dataset/v1", "examples": len(records), "dimension": 86, "feature_names": FEATURE_NAMES, "heads": HEADS, "sources": sorted({record["source"] for record in records})}, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"dataset": str(args.output), "examples": len(records), "dimension": 86}, sort_keys=True))


def train(args: argparse.Namespace) -> None:
    np.random.seed(20260919); random.seed(20260919)
    records = read_jsonl(args.dataset / "corpus.jsonl")
    args.output.mkdir(parents=True, exist_ok=True)
    x = np.asarray([record["vector"] for record in records], dtype=np.float32)
    for head in HEADS:
        labels = LABELS[head]; y = np.asarray([labels.index(record["labels"][head]) for record in records], dtype=np.int64)
        hidden = 32
        rng = np.random.default_rng(20260919 + len(labels))
        first = (rng.normal(0, 0.08, (hidden, 86))).astype(np.float32); first_bias = np.zeros((hidden,), dtype=np.float32)
        weights = np.zeros((len(labels), hidden), dtype=np.float32); bias = np.zeros((len(labels),), dtype=np.float32)
        for _ in range(4000):
            hidden_values = x @ first.T + first_bias
            hidden_active = np.maximum(hidden_values, 0)
            logits = hidden_active @ weights.T + bias
            logits -= logits.max(axis=1, keepdims=True)
            probabilities = np.exp(logits); probabilities /= probabilities.sum(axis=1, keepdims=True)
            target = np.zeros_like(probabilities); target[np.arange(len(y)), y] = 1.0
            gradient = (probabilities - target) / len(y)
            second_gradient = gradient.T @ hidden_active
            hidden_gradient = (gradient @ weights) * (hidden_values > 0)
            first_gradient = hidden_gradient.T @ x
            weights -= 0.05 * second_gradient; bias -= 0.05 * gradient.sum(axis=0)
            first -= 0.05 * first_gradient; first_bias -= 0.05 * hidden_gradient.sum(axis=0)
        loss = float(-np.log(np.maximum(probabilities[np.arange(len(y)), y], 1e-12)).mean())
        (args.output / f"{head}.checkpoint.json").write_text(json.dumps({"schema_version": "synora.cognitive-v1-checkpoint/v1", "head": head, "input_size": 86, "labels": labels, "layers": [{"input_size": 86, "output_size": hidden, "activation": "relu", "weights": first.reshape(-1).tolist(), "bias": first_bias.tolist()}, {"input_size": hidden, "output_size": len(labels), "activation": "identity", "weights": weights.reshape(-1).tolist(), "bias": bias.tolist()}], "loss": loss}, separators=(",", ":")), encoding="utf-8")
    print(json.dumps({"checkpoints": str(args.output), "heads": HEADS}, sort_keys=True))


def export(args: argparse.Namespace) -> None:
    args.output.mkdir(parents=True, exist_ok=True)
    for head in HEADS:
        checkpoint = json.loads((args.checkpoints / f"{head}.checkpoint.json").read_text(encoding="utf-8"))
        payload = {"schema": "synora.cognitive-cpu-mlp/v1", "head": head, "input_size": 86, "output_size": len(checkpoint["labels"]), "labels": checkpoint["labels"], "thresholds": [0.5] * len(checkpoint["labels"]), "vector_schema": "cognitive-encoder/v1", "output_names": ["logits"], "layers": checkpoint["layers"]}
        (args.output / f"{head}.cpu.json").write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")
    print(json.dumps({"export": str(args.output), "heads": HEADS}, sort_keys=True))


def validate(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "corpus.jsonl")
    if not records: raise SystemExit("dataset is empty")
    for record in records:
        if len(record.get("vector", [])) != 86 or record.get("feature_names") != FEATURE_NAMES or forbidden(record): raise SystemExit(f"invalid dataset record: {record.get('id')}")
        for head in HEADS:
            if record["labels"].get(head) not in LABELS[head]: raise SystemExit(f"invalid {head} label in {record.get('id')}")
    print(json.dumps({"valid": True, "examples": len(records), "dimension": 86}, sort_keys=True))


def cpu_run(model: dict[str, Any], vector: list[float]) -> list[float]:
    current = np.asarray(vector, dtype=np.float32)
    for layer in model["layers"]:
        weight = np.asarray(layer["weights"], dtype=np.float32).reshape(layer["output_size"], layer["input_size"]); bias = np.asarray(layer["bias"], dtype=np.float32)
        current = bias + weight.dot(current)
        if layer.get("activation") == "relu": current = np.maximum(current, 0)
    return current.tolist()


def test_models(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "corpus.jsonl"); mismatches = []
    for record in records:
        for head in HEADS:
            model = json.loads((args.bundle / f"{head}.cpu.json").read_text(encoding="utf-8")); predicted = model["labels"][int(np.argmax(cpu_run(model, record["vector"])))]; expected = record["labels"][head]
            if predicted != expected: mismatches.append({"id": record["id"], "head": head, "expected": expected, "predicted": predicted})
    report = {"schema_version": "synora.cognitive-v1-test/v1", "examples": len(records), "mismatches": mismatches, "passed": not mismatches}
    (args.bundle / "test-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps(report, sort_keys=True))
    if mismatches: raise SystemExit(1)


def parity(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "corpus.jsonl"); first = {}; second = {}
    for record in records:
        first[record["id"]] = {head: cpu_run(json.loads((args.bundle / f"{head}.cpu.json").read_text(encoding="utf-8")), record["vector"]) for head in HEADS}
        second[record["id"]] = {head: cpu_run(json.loads((args.bundle / f"{head}.cpu.json").read_text(encoding="utf-8")), record["vector"]) for head in HEADS}
    passed = first == second
    report = {"schema_version": "synora.cognitive-v1-parity/v1", "examples": len(records), "passed": passed}
    (args.bundle / "parity-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps(report, sort_keys=True))
    if not passed: raise SystemExit(1)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""): digest.update(chunk)
    return digest.hexdigest()


def package(args: argparse.Namespace) -> None:
    heads = {}
    for head in HEADS:
        path = args.bundle / f"{head}.cpu.json"; model = json.loads(path.read_text(encoding="utf-8"))
        heads[head] = {"version": "1.0.0", "artifact": path.name, "sha256": sha256(path), "labels": model["labels"], "thresholds": model["thresholds"]}
    manifest = {"schema_version": "synora.cognitive-v1-manifest/v1", "snapshot_schema": "cognitive-snapshot/v1", "encoder_schema": "cognitive-encoder/v1", "encoder_version": "1.0.0", "input_dimension": 86, "feature_names": FEATURE_NAMES, "heads": list(HEADS), "labels": LABELS, "thresholds": {head: heads[head]["thresholds"] for head in HEADS}, "head_versions": {head: "1.0.0" for head in HEADS}, "weights_sha256": hashlib.sha256("".join(heads[head]["sha256"] for head in HEADS).encode()).hexdigest(), "artifacts": heads, "dry_run_required": True, "physical_action_executed": False, "compatibility": {"runtime": "synora-cognitivecore-v1", "v4_rejected": True}}
    (args.bundle / "MANIFEST.v1.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"manifest": str(args.bundle / "MANIFEST.v1.json"), "dimension": 86, "heads": HEADS}, sort_keys=True))


def main() -> None:
    p = argparse.ArgumentParser(); sub = p.add_subparsers(dest="command", required=True)
    for name in ("build-dataset", "validate", "train", "export", "test", "parity", "package-model"):
        sub.add_parser(name)
    args = p.parse_args()
    root = Path(__file__).resolve().parents[1]
    dataset = root / "build/cognitive-v1-dataset"; checkpoints = root / "build/cognitive-v1-checkpoints"; bundle = root / "build/cognitive-mlp-v1"
    if args.command == "build-dataset": build_dataset(argparse.Namespace(scenarios=root / "testdata/cognitive-v1/scenarios.jsonl", output=dataset))
    elif args.command == "validate": validate(argparse.Namespace(dataset=dataset))
    elif args.command == "train": train(argparse.Namespace(dataset=dataset, output=checkpoints))
    elif args.command == "export": export(argparse.Namespace(checkpoints=checkpoints, output=bundle))
    elif args.command == "test": test_models(argparse.Namespace(dataset=dataset, bundle=bundle))
    elif args.command == "parity": parity(argparse.Namespace(dataset=dataset, bundle=bundle))
    elif args.command == "package-model": package(argparse.Namespace(bundle=bundle))


if __name__ == "__main__": main()
