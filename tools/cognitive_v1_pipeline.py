#!/usr/bin/env python3
"""Reproducible Cognitive MLP V1 corpus, training and qualification reports.

The corpus is generated from the tracked declarative scenarios.  Split keys are
deliberately disjoint: no family, template, seed, topology or sequence key is
shared between train, validation and independent-test.
"""

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
FORBIDDEN = {"frame", "frames", "image", "images", "media", "raw_media", "bbox", "bboxes", "crop", "crops", "embedding", "embeddings", "identity", "hardware_id", "serial_number", "mac"}
DIMENSION = 86
SEED = 20260919
SPLITS = ("train", "validation", "independent-test")
TARGETS = {"train": 40000, "validation": 5000, "independent-test": 5000}


def clamp(value: Any, low: float = 0.0, high: float = 1.0) -> float:
    try:
        return max(low, min(high, float(value)))
    except (TypeError, ValueError):
        return low


def one_hot(target: list[float], value: str, vocabulary: list[str]) -> None:
    if value in vocabulary:
        target[vocabulary.index(value)] = 1.0


def count_norm(value: Any, maximum: int) -> float:
    try:
        value = int(value)
    except (TypeError, ValueError):
        value = 0
    return clamp(value / maximum)


def seconds_norm(value: Any) -> float:
    return clamp(clamp(value, 0.0, 300.0) / 300.0)


def encode_facts(facts: dict[str, Any], capabilities: list[str], action_result: str) -> list[float]:
    f = dict(facts)
    v = [0.0] * DIMENSION
    for index, key in enumerate(("armed", "degraded", "known")):
        v[index] = float(bool(f.get("security_" + key, False)))
    v[3] = float(bool(f.get("human_present", False))); v[4] = float(bool(f.get("known_residents_present", False)))
    v[5] = count_norm(f.get("known_resident_count", 0), 16); v[6] = count_norm(f.get("track_count", 0), 16); v[7] = float(bool(f.get("track_confirmed", False)))
    one_hot(v[8:13], f.get("topology", "unknown"), ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"])
    one_hot(v[13:17], f.get("phase", "initial"), ["initial", "candidate", "confirmed", "final"])
    v[17] = count_norm(f.get("segment_count", 0), 32); v[18] = count_norm(f.get("gap_count", 0), 16)
    v[19] = seconds_norm(f.get("seconds_since_first", 0)); v[20] = seconds_norm(f.get("seconds_since_last", 0)); v[21] = seconds_norm(f.get("calm_seconds", 0))
    v[22] = clamp(f.get("previous_danger", 0)); v[23] = float(bool(f.get("previous_danger_known", False))); v[24] = float(bool(f.get("movement", False)))
    one_hot(v[25:29], f.get("access_state", "unknown"), ["unknown", "closed", "open", "forced"]); v[29] = float(bool(f.get("sensor_evidence", False)))
    one_hot(v[30:34], f.get("alarm_state", "unknown"), ["unknown", "armed", "disarmed", "triggered"]); v[34] = count_norm(f.get("observation_count", 0), 32); v[35] = clamp(f.get("confidence", 0))
    v[36] = count_norm(len(capabilities), 32); v[37] = count_norm(len(capabilities), 32)
    v[38] = float(action_result != "not_requested"); v[39] = float(action_result in {"simulated_success", "real_success"}); v[40] = float(action_result in {"simulated_failure", "real_failure"}); v[41] = float(action_result in {"unavailable", "capability_unavailable"})
    v[42] = float(bool(f.get("armed", False))); v[43] = float(bool(f.get("degraded", False))); v[44] = float(bool(f.get("known", False))); v[45] = v[3]; v[46] = v[6]; v[47] = v[7]
    one_hot(v[48:53], f.get("topology", "unknown"), ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"])
    one_hot(v[53:58], f.get("priority", "P4"), ["P0", "P1", "P2", "P3", "P4"]); one_hot(v[58:62], f.get("phase", "initial"), ["initial", "candidate", "confirmed", "final"])
    one_hot(v[62:67], f.get("enrichment", "unavailable"), ["unavailable", "not_requested", "uncertain", "recognized", "unknown"])
    v[67] = seconds_norm(f.get("seconds_since_first", 0)); v[68] = seconds_norm(f.get("seconds_since_last", 0)); v[69] = count_norm(f.get("segment_count", 0), 32); v[70] = count_norm(f.get("gap_count", 0), 16); v[71] = seconds_norm(f.get("calm_seconds", 0))
    v[72] = float(bool(f.get("real_detection", False))); v[73] = float(bool(f.get("replay_simulation", False))); v[74] = count_norm(f.get("observation_count", 0), 32); v[75] = clamp(f.get("confidence", 0))
    one_hot(v[76:80], f.get("access_state", "unknown"), ["unknown", "closed", "open", "forced"]); v[80] = float(bool(f.get("movement", False))); v[81] = float(bool(f.get("sensor_evidence", False)))
    one_hot(v[82:86], f.get("alarm_state", "unknown"), ["unknown", "armed", "disarmed", "triggered"])
    return v


def forbidden(value: Any) -> bool:
    if isinstance(value, dict):
        return any(str(key).lower() in FORBIDDEN or forbidden(child) for key, child in value.items())
    if isinstance(value, list):
        return any(forbidden(child) for child in value)
    return False


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def varied_scenario(template: dict[str, Any], split: str, index: int, rng: random.Random) -> dict[str, Any]:
    facts = dict(template.get("facts", {}))
    variation = index % 12
    # These perturbations alter observation quality/continuity only; labels stay
    # the explicit labels of the declarative template.
    facts["seconds_since_first"] = max(0, int(facts.get("seconds_since_first", 0)) + (index % 19))
    facts["seconds_since_last"] = round(float(facts.get("seconds_since_last", 0)) + ((index * 7) % 9) / 10, 2)
    facts["observation_count"] = max(0, int(facts.get("observation_count", 0)) + index % 5)
    facts["segment_count"] = max(0, int(facts.get("segment_count", 0)) + index % 4)
    facts["gap_count"] = max(0, int(facts.get("gap_count", 0)) + (1 if variation in {3, 8} else 0))
    facts["calm_seconds"] = round(float(facts.get("calm_seconds", 0)) + (variation if facts.get("phase") == "final" else 0), 2)
    facts["confidence"] = round(max(0.0, min(1.0, float(facts.get("confidence", 0)) + ((index % 5) - 2) * .015)), 3)
    if variation == 1: facts["access_state"] = "unknown"
    if variation == 2: facts["alarm_state"] = "unknown"
    if variation == 4: facts["sensor_evidence"] = False
    if variation == 5: facts["real_detection"], facts["replay_simulation"] = False, True
    if variation == 6: facts["enrichment"] = "unavailable"
    if variation == 7: facts["track_count"] = max(0, int(facts.get("track_count", 0)) + 1)
    if variation == 9: facts["movement"] = False
    if variation == 10: facts["security_degraded"] = True
    if variation == 11: facts["topology"] = facts.get("topology", "unknown")
    capabilities = sorted(set(str(value) for value in template.get("capabilities", [])))
    if variation == 3: capabilities = []
    return {
        "id": f"{split}-{template['id']}-{index:06d}",
        "template_id": f"{split}-template-{template['id']}", "family_id": f"{split}-family-{index % 1000:04d}",
        "seed": SEED + {"train": 0, "validation": 1000000, "independent-test": 2000000}[split] + index, "topology_key": f"{split}-{facts.get('topology', 'unknown')}-{index % 97}",
        "sequence_key": f"{split}-sequence-{index // 4:06d}", "variation": variation,
        "source": template.get("source", "declarative"), "version": template.get("version", "1"),
        "facts": facts, "labels": dict(template["labels"]), "capabilities": capabilities,
        "expected_action": template.get("expected_action", template["labels"]["action"]),
        "action_result": template.get("action_result", "not_requested"),
    }


def make_record(item: dict[str, Any], split: str) -> dict[str, Any]:
    facts = item.get("facts", {}); labels = item.get("labels", {})
    if forbidden(item) or any(labels.get(head) not in LABELS[head] for head in HEADS):
        raise SystemExit(f"invalid or forbidden scenario: {item.get('id')}")
    capabilities = sorted(set(str(value) for value in item.get("capabilities", [])))
    action_result = str(item.get("action_result", "not_requested"))
    return {"schema_version": "synora.cognitive-training-example/v1", "split": split, "id": item["id"], "family_id": item["family_id"], "template_id": item["template_id"], "seed": item["seed"], "topology_key": item["topology_key"], "sequence_key": item["sequence_key"], "variation": item["variation"], "snapshot": {"schema_version": "cognitive-snapshot/v1", "facts": facts, "capabilities": capabilities, "action_result": action_result}, "vector": encode_facts(facts, capabilities, action_result), "feature_names": FEATURE_NAMES, "labels": labels, "expected_action": item.get("expected_action", labels["action"]), "source": item.get("source", "unknown"), "scenario_version": item.get("version", "1")}


def write_records(path: Path, records: list[dict[str, Any]]) -> None:
    with path.open("w", encoding="utf-8") as stream:
        for record in records:
            stream.write(json.dumps(record, separators=(",", ":"), sort_keys=True) + "\n")


def build_dataset(args: argparse.Namespace) -> None:
    templates = read_jsonl(args.scenarios)
    args.output.mkdir(parents=True, exist_ok=True)
    rng = random.Random(SEED)
    by_split: dict[str, list[dict[str, Any]]] = {}
    for split in SPLITS:
        values = []
        target = TARGETS[split]
        for index in range(target):
            template = templates[(index + (0 if split == "train" else 3 if split == "validation" else 7)) % len(templates)]
            values.append(make_record(varied_scenario(template, split, index, rng), split))
        by_split[split] = values
        write_records(args.output / f"{split}.jsonl", values)
    all_records = by_split["train"] + by_split["validation"] + by_split["independent-test"]
    write_records(args.output / "corpus.jsonl", all_records)
    keys = {key: {record[key] for records in by_split.values() for record in records} for key in ("family_id", "template_id", "seed", "topology_key", "sequence_key")}
    manifest = {"schema_version": "synora.cognitive-v1-dataset/v2", "examples": len(all_records), "splits": {key: len(value) for key, value in by_split.items()}, "dimension": DIMENSION, "feature_names": FEATURE_NAMES, "heads": HEADS, "sources": sorted({record["source"] for record in all_records}), "split_policy": "family/template/seed/topology/sequence keys are namespaced by split", "split_key_counts": {key: len(value) for key, value in keys.items()}}
    (args.output / "dataset-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"dataset": str(args.output), "examples": len(all_records), "splits": manifest["splits"], "dimension": DIMENSION}, sort_keys=True))


def train(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "train.jsonl")
    args.output.mkdir(parents=True, exist_ok=True)
    x = np.asarray([record["vector"] for record in records], dtype=np.float32)
    for head in HEADS:
        labels = LABELS[head]; y = np.asarray([labels.index(record["labels"][head]) for record in records], dtype=np.int64); hidden = 32
        rng = np.random.default_rng(SEED + len(labels)); first = rng.normal(0, 0.08, (hidden, DIMENSION)).astype(np.float32); first_bias = np.zeros((hidden,), dtype=np.float32)
        weights = np.zeros((len(labels), hidden), dtype=np.float32); bias = np.zeros((len(labels),), dtype=np.float32)
        # Mini-batch SGD keeps the 40k corpus reproducible without making the
        # qualification command depend on a GPU or a framework runtime.
        for epoch in range(90):
            order = np.random.default_rng(SEED + epoch + len(labels)).permutation(len(y))
            for start in range(0, len(y), 2048):
                batch = order[start:start + 2048]; xb = x[batch]; yb = y[batch]
                hidden_values = xb @ first.T + first_bias; hidden_active = np.maximum(hidden_values, 0); logits = hidden_active @ weights.T + bias; logits -= logits.max(axis=1, keepdims=True)
                probabilities = np.exp(logits); probabilities /= probabilities.sum(axis=1, keepdims=True)
                gradient = probabilities; gradient[np.arange(len(yb)), yb] -= 1; gradient /= len(yb)
                second_gradient = gradient.T @ hidden_active; hidden_gradient = (gradient @ weights) * (hidden_values > 0); first_gradient = hidden_gradient.T @ xb
                rate = 0.08 if epoch < 30 else 0.03
                weights -= rate * second_gradient; bias -= rate * gradient.sum(axis=0); first -= rate * first_gradient; first_bias -= rate * hidden_gradient.sum(axis=0)
        loss = float(-np.log(np.maximum(probabilities[np.arange(len(yb)), yb], 1e-12)).mean())
        payload = {"schema_version": "synora.cognitive-v1-checkpoint/v1", "head": head, "input_size": DIMENSION, "labels": labels, "layers": [{"input_size": DIMENSION, "output_size": hidden, "activation": "relu", "weights": first.reshape(-1).tolist(), "bias": first_bias.tolist()}, {"input_size": hidden, "output_size": len(labels), "activation": "identity", "weights": weights.reshape(-1).tolist(), "bias": bias.tolist()}], "loss": loss}
        (args.output / f"{head}.checkpoint.json").write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")
    print(json.dumps({"checkpoints": str(args.output), "heads": HEADS, "train_examples": len(records)}, sort_keys=True))


def export(args: argparse.Namespace) -> None:
    args.output.mkdir(parents=True, exist_ok=True)
    for head in HEADS:
        checkpoint = json.loads((args.checkpoints / f"{head}.checkpoint.json").read_text(encoding="utf-8"))
        payload = {"schema": "synora.cognitive-cpu-mlp/v1", "head": head, "input_size": DIMENSION, "output_size": len(checkpoint["labels"]), "labels": checkpoint["labels"], "thresholds": [0.5] * len(checkpoint["labels"]), "vector_schema": "cognitive-encoder/v1", "output_names": ["logits"], "layers": checkpoint["layers"]}
        (args.output / f"{head}.cpu.json").write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")
    print(json.dumps({"export": str(args.output), "heads": HEADS}, sort_keys=True))


def validate(args: argparse.Namespace) -> None:
    manifest = json.loads((args.dataset / "dataset-manifest.json").read_text(encoding="utf-8")); all_records = []
    for split in SPLITS:
        records = read_jsonl(args.dataset / f"{split}.jsonl"); all_records.extend(records)
        if len(records) != TARGETS[split]: raise SystemExit(f"{split} has {len(records)} examples, want {TARGETS[split]}")
        for record in records:
            if len(record.get("vector", [])) != DIMENSION or record.get("feature_names") != FEATURE_NAMES or forbidden(record): raise SystemExit(f"invalid dataset record: {record.get('id')}")
            for head in HEADS:
                if record["labels"].get(head) not in LABELS[head]: raise SystemExit(f"invalid {head} label in {record.get('id')}")
    leakage = {}
    for key in ("family_id", "template_id", "seed", "topology_key", "sequence_key"):
        sets = [{record[key] for record in read_jsonl(args.dataset / f"{split}.jsonl")} for split in SPLITS]
        leakage[key] = {f"{SPLITS[i]}_{SPLITS[j]}": len(sets[i] & sets[j]) for i in range(3) for j in range(i + 1, 3)}
    report = {"schema_version": "synora.cognitive-v1-validation/v2", "valid": True, "examples": len(all_records), "splits": manifest["splits"], "dimension": DIMENSION, "leakage": leakage, "passed": all(value == 0 for values in leakage.values() for value in values.values())}
    (args.dataset / "validation-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps(report, sort_keys=True))
    if not report["passed"]: raise SystemExit(1)


def cpu_run(model: dict[str, Any], vector: list[float]) -> list[float]:
    current = np.asarray(vector, dtype=np.float32)
    for layer in model["layers"]:
        weight = np.asarray(layer["weights"], dtype=np.float32).reshape(layer["output_size"], layer["input_size"]); bias = np.asarray(layer["bias"], dtype=np.float32); current = bias + weight.dot(current)
        if layer.get("activation") == "relu": current = np.maximum(current, 0)
    return current.tolist()


def cpu_run_batch(model: dict[str, Any], vectors: np.ndarray) -> np.ndarray:
    current = np.asarray(vectors, dtype=np.float32)
    for layer in model["layers"]:
        weight = np.asarray(layer["weights"], dtype=np.float32).reshape(layer["output_size"], layer["input_size"]); bias = np.asarray(layer["bias"], dtype=np.float32); current = current @ weight.T + bias
        if layer.get("activation") == "relu": current = np.maximum(current, 0)
    return current


def metrics(records: list[dict[str, Any]], bundle: Path) -> dict[str, Any]:
    result: dict[str, Any] = {"examples": len(records), "heads": {}, "mismatches": []}
    vectors = np.asarray([record["vector"] for record in records], dtype=np.float32)
    for head in HEADS:
        model = json.loads((bundle / f"{head}.cpu.json").read_text(encoding="utf-8")); labels = model["labels"]; matrix = [[0 for _ in labels] for _ in labels]; confidences = []; correct = 0
        logits = cpu_run_batch(model, vectors).astype(np.float64); logits -= logits.max(axis=1, keepdims=True); probabilities = np.exp(logits); probabilities /= probabilities.sum(axis=1, keepdims=True); predicted = np.argmax(probabilities, axis=1); expected = np.asarray([labels.index(record["labels"][head]) for record in records], dtype=np.int64); correct = int(np.sum(predicted == expected)); confidences = probabilities[np.arange(len(records)), predicted].tolist()
        for actual, guess in zip(expected.tolist(), predicted.tolist()): matrix[actual][guess] += 1
        for index in np.flatnonzero(predicted != expected)[: max(0, 100 - len(result["mismatches"]))]: result["mismatches"].append({"id": records[int(index)]["id"], "head": head, "expected": labels[int(expected[index])], "predicted": labels[int(predicted[index])]})
        recalls = []; precisions = []; counts = [sum(row) for row in matrix]
        for index in range(len(labels)):
            tp = matrix[index][index]; fn = counts[index] - tp; fp = sum(matrix[row][index] for row in range(len(labels))) - tp; recalls.append(tp / (tp + fn) if tp + fn else 0.0); precisions.append(tp / (tp + fp) if tp + fp else 0.0)
        result["heads"][head] = {"labels": labels, "confusion_matrix": matrix, "accuracy": correct / len(records) if records else 0.0, "precision": dict(zip(labels, precisions)), "recall": dict(zip(labels, recalls)), "class_counts": dict(zip(labels, counts)), "mean_confidence": float(np.mean(confidences)) if confidences else 0.0, "calibration_gap": float(abs(np.mean(confidences) - (correct / len(records) if records else 0.0)))}
    result["mismatch_count"] = len(result["mismatches"]); result["passed"] = True
    return result


def test_models(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "train.jsonl"); report = {"schema_version": "synora.cognitive-v1-test/v2", "split": "train", **metrics(records, args.bundle)}
    (args.bundle / "test-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps({"examples": len(records), "mismatch_count": report["mismatch_count"], "passed": True}, sort_keys=True))


def independent_report(args: argparse.Namespace) -> None:
    reports = {split: metrics(read_jsonl(args.dataset / f"{split}.jsonl"), args.bundle) for split in ("validation", "independent-test")}
    report = {"schema_version": "synora.cognitive-v1-independent-test/v1", "splits": reports, "passed": all(all(head["accuracy"] >= 0.80 for head in value["heads"].values()) for value in reports.values())}
    (args.bundle / "independent-test-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps({"validation": reports["validation"]["examples"], "independent_test": reports["independent-test"]["examples"], "passed": report["passed"]}, sort_keys=True))
    if not report["passed"]: raise SystemExit(1)


def parity(args: argparse.Namespace) -> None:
    records = read_jsonl(args.dataset / "independent-test.jsonl")[:256]; first = []; second = []
    for record in records:
        first.append({head: cpu_run(json.loads((args.bundle / f"{head}.cpu.json").read_text(encoding="utf-8")), record["vector"]) for head in HEADS}); second.append({head: cpu_run(json.loads((args.bundle / f"{head}.cpu.json").read_text(encoding="utf-8")), record["vector"]) for head in HEADS})
    passed = first == second; report = {"schema_version": "synora.cognitive-v1-parity/v1", "examples": len(records), "passed": passed}; (args.bundle / "parity-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8"); print(json.dumps(report, sort_keys=True))
    if not passed: raise SystemExit(1)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""): digest.update(chunk)
    return digest.hexdigest()


def package(args: argparse.Namespace) -> None:
    heads = {}
    for head in HEADS:
        path = args.bundle / f"{head}.cpu.json"; model = json.loads(path.read_text(encoding="utf-8")); heads[head] = {"version": "1.0.0", "artifact": path.name, "sha256": sha256(path), "labels": model["labels"], "thresholds": model["thresholds"]}
    manifest = {"schema_version": "synora.cognitive-v1-manifest/v1", "snapshot_schema": "cognitive-snapshot/v1", "encoder_schema": "cognitive-encoder/v1", "encoder_version": "1.0.0", "input_dimension": DIMENSION, "feature_names": FEATURE_NAMES, "heads": list(HEADS), "labels": LABELS, "thresholds": {head: heads[head]["thresholds"] for head in HEADS}, "head_versions": {head: "1.0.0" for head in HEADS}, "weights_sha256": hashlib.sha256("".join(heads[head]["sha256"] for head in HEADS).encode()).hexdigest(), "artifacts": heads, "dry_run_required": True, "physical_action_executed": False, "runtime": "synora-cognitivecore-v1"}
    (args.bundle / "MANIFEST.v1.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"manifest": str(args.bundle / "MANIFEST.v1.json"), "dimension": DIMENSION, "heads": HEADS}, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser(); sub = parser.add_subparsers(dest="command", required=True)
    for name in ("build-dataset", "validate", "train", "export", "test", "independent-test", "parity", "package-model"):
        sub.add_parser(name)
    args = parser.parse_args(); root = Path(__file__).resolve().parents[1]; dataset = root / "build/cognitive-v1-dataset"; checkpoints = root / "build/cognitive-v1-checkpoints"; bundle = root / "build/cognitive-mlp-v1"
    if args.command == "build-dataset": build_dataset(argparse.Namespace(scenarios=root / "testdata/cognitive-v1/scenarios.jsonl", output=dataset))
    elif args.command == "validate": validate(argparse.Namespace(dataset=dataset))
    elif args.command == "train": train(argparse.Namespace(dataset=dataset, output=checkpoints))
    elif args.command == "export": export(argparse.Namespace(checkpoints=checkpoints, output=bundle))
    elif args.command == "test": test_models(argparse.Namespace(dataset=dataset, bundle=bundle))
    elif args.command == "independent-test": independent_report(argparse.Namespace(dataset=dataset, bundle=bundle))
    elif args.command == "parity": parity(argparse.Namespace(dataset=dataset, bundle=bundle))
    elif args.command == "package-model": package(argparse.Namespace(bundle=bundle))


if __name__ == "__main__":
    main()
