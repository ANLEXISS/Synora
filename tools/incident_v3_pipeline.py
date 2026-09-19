#!/usr/bin/env python3
"""Build and evaluate an incident-only V3 corpus.

The V3 generator is intentionally independent of the sealed incident-redteam
V1 fixture.  It creates new scenario families, split namespaces and a hidden
V2 red-team set.  Only the incident CPU head is trained; the other heads are
copied byte-for-byte from the incident-v2 bundle.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
from typing import Any

import numpy as np


def load_module(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ROOT = Path(__file__).resolve().parents[1]
INCIDENT = load_module(ROOT / "tools/incident_v2_pipeline.py", "incident_v2_pipeline")
BASE = INCIDENT.BASE
LABELS = BASE.LABELS["incident"]
SEED = 20260919 + 3700
COUNTS = {"train": 4000, "validation": 1000, "independent-test": 1000}
REDTEAM_V2_COUNT = 400


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def write_jsonl(path: Path, records: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as stream:
        for record in records:
            stream.write(json.dumps(record, separators=(",", ":"), sort_keys=True) + "\n")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def facts_for(label: str, index: int, hidden: bool = False) -> tuple[dict[str, Any], list[str], str, dict[str, str]]:
    """Return an unseen-family state with controlled causal variation."""
    phase = ("confirmed", "candidate", "final", "initial")[(index + (7 if hidden else 0)) % 4]
    topology_cycle = ("public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown")
    topology = topology_cycle[(index * 3 + (2 if hidden else 0)) % len(topology_cycle)]
    facts: dict[str, Any] = {
        "topology": topology,
        "phase": phase,
        "priority": ("P1", "P2", "P3", "P4")[(index + (1 if hidden else 0)) % 4],
        "enrichment": ("unavailable", "not_requested", "uncertain", "recognized", "unknown")[(index * 2 + (3 if hidden else 0)) % 5],
        "human_present": False,
        "track_count": 0,
        "track_confirmed": False,
        "segment_count": 0,
        "gap_count": 0,
        "seconds_since_first": 0,
        "seconds_since_last": 0,
        "calm_seconds": 0,
        "real_detection": False,
        "replay_simulation": True,
        "observation_count": 0,
        "confidence": 0.0,
        "access_state": "unknown",
        "movement": False,
        "sensor_evidence": False,
        "alarm_state": "unknown",
    }
    capabilities: list[str] = []
    action_result = "not_requested"

    if label == "none":
        facts.update(topology="unknown" if index % 2 else "public_outdoor", phase=("initial", "candidate")[index % 2], priority="P4", human_present=False, real_detection=False, replay_simulation=True, sensor_evidence=False, movement=False, alarm_state="unknown", observation_count=index % 2, confidence=0.04 + (index % 7) / 100, access_state="unknown", segment_count=index % 2)
    elif label == "routine_presence":
        facts.update(topology="public_outdoor", phase=("candidate", "confirmed")[index % 2], priority="P3", human_present=True, track_count=1 + index % 3, track_confirmed=index % 2 == 1, real_detection=index % 3 != 0, replay_simulation=index % 3 == 0, observation_count=1 + index % 5, confidence=0.50 + (index % 40) / 100, movement=index % 3 != 1, sensor_evidence=index % 4 != 0, alarm_state="disarmed", segment_count=1 + index % 5, access_state="unknown")
    elif label == "perimeter_presence":
        facts.update(topology="private_perimeter", phase=("candidate", "confirmed", "final")[index % 3], priority=("P2", "P3")[index % 2], human_present=True, track_count=1 + index % 4, track_confirmed=index % 3 == 2, real_detection=index % 4 != 0, replay_simulation=index % 4 == 0, observation_count=1 + index % 6, confidence=0.55 + (index % 35) / 100, movement=True, sensor_evidence=index % 5 != 0, alarm_state=("armed", "disarmed")[index % 2], segment_count=1 + index % 6, gap_count=index % 3, access_state=("closed", "open")[index % 2])
    elif label == "threshold_presence":
        facts.update(topology="restricted_threshold", phase=("candidate", "confirmed")[index % 2], priority=("P1", "P2")[index % 2], human_present=True, track_count=1 + index % 3, track_confirmed=index % 2 == 1, real_detection=index % 5 != 0, replay_simulation=index % 5 == 0, observation_count=1 + index % 6, confidence=0.63 + (index % 30) / 100, movement=True, sensor_evidence=True, alarm_state=("armed", "triggered")[index % 2], segment_count=1 + index % 7, gap_count=index % 2, access_state=("closed", "forced")[index % 2])
    elif label == "interior_intrusion":
        facts.update(topology="protected_interior", phase=("confirmed", "candidate")[index % 2], priority="P1", human_present=True, track_count=1 + index % 3, track_confirmed=True, real_detection=True, replay_simulation=False, observation_count=2 + index % 6, confidence=0.72 + (index % 25) / 100, movement=index % 4 != 0, sensor_evidence=True, alarm_state="triggered", segment_count=2 + index % 7, gap_count=index % 3, access_state="forced")
    elif label == "anomaly":
        facts.update(topology=topology_cycle[(index + (1 if hidden else 0)) % 5], phase=("initial", "candidate", "confirmed")[index % 3], priority=("P2", "P3", "P4")[index % 3], human_present=False, track_count=0, track_confirmed=False, real_detection=index % 3 == 0, replay_simulation=index % 3 != 0, observation_count=1 + index % 4, confidence=0.30 + (index % 45) / 100, movement=index % 2 == 0, sensor_evidence=True, alarm_state=("triggered", "armed", "disarmed")[index % 3], segment_count=1 + index % 5, gap_count=index % 4, access_state=("open", "forced", "closed")[index % 3])
    elif label == "action_failure":
        facts.update(topology=("private_perimeter", "restricted_threshold", "protected_interior")[index % 3], phase=("candidate", "confirmed")[index % 2], priority=("P1", "P2")[index % 2], human_present=True, track_count=1 + index % 3, track_confirmed=index % 2 == 1, real_detection=index % 4 != 0, replay_simulation=index % 4 == 0, observation_count=2 + index % 5, confidence=0.58 + (index % 35) / 100, movement=True, sensor_evidence=index % 3 != 0, alarm_state=("armed", "triggered")[index % 2], segment_count=2 + index % 5, gap_count=index % 3, access_state=("open", "forced", "closed")[index % 3])
        capabilities = [("notify",), ("lock",), ("siren",), ()][index % 4]
        action_result = ("simulated_failure", "unavailable", "capability_unavailable")[index % 3]
    elif label == "technical":
        facts.update(topology="unknown", phase="initial", priority="P4", human_present=False, track_count=0, track_confirmed=False, real_detection=False, replay_simulation=False, observation_count=0, confidence=0.0, movement=False, sensor_evidence=False, alarm_state="unknown", segment_count=0, gap_count=0, access_state="unknown")
        action_result = "unavailable"
    elif label == "resolved":
        facts.update(topology=("unknown", "private_perimeter", "protected_interior")[index % 3], phase="final", priority="P4", human_present=False, track_count=0, track_confirmed=False, real_detection=index % 3 == 0, replay_simulation=index % 3 != 0, observation_count=2 + index % 6, confidence=0.12 + (index % 30) / 100, movement=False, sensor_evidence=index % 2 == 0, alarm_state=("disarmed", "armed")[index % 2], segment_count=2 + index % 8, gap_count=1 + index % 4, seconds_since_first=30 + index % 100, seconds_since_last=10 + index % 30, calm_seconds=30 + index % 180, access_state="closed")
        capabilities = ["record"]
        action_result = "simulated_success"
    else:
        raise ValueError(label)

    metadata = {
        "family_id": f"incident-v3-{('hidden-' if hidden else '')}family-{label}-{index:05d}",
        "template_id": f"incident-v3-{('hidden-' if hidden else '')}template-{label}-{(index * 11 + (3 if hidden else 0)) % 37:03d}",
    }
    return facts, list(capabilities), action_result, metadata


def make_record(split: str, label: str, index: int, hidden: bool = False) -> dict[str, Any]:
    facts, capabilities, action_result, metadata = facts_for(label, index, hidden)
    vector = INCIDENT.encode_fixed(facts, capabilities, action_result)
    return {
        "schema_version": "synora.cognitive-incident-v3-example/v1",
        "split": "redteam-v2" if hidden else split,
        "id": f"incident-redteam-v2/{label}-{index:05d}" if hidden else f"incident-v3/{split}/{label}-{index:05d}",
        "family_id": metadata["family_id"],
        "template_id": metadata["template_id"],
        "seed": SEED + index + (9000000 if hidden else {"train": 0, "validation": 1000000, "independent-test": 2000000}[split]),
        "topology_key": f"incident-v3-{('hidden-' if hidden else '')}{facts['topology']}-{index % 113:03d}",
        "sequence_key": f"incident-v3-{('hidden-' if hidden else '')}sequence-{index // 3:05d}",
        "facts": facts,
        "capabilities": capabilities,
        "action_result": action_result,
        "snapshot": {"schema_version": "cognitive-snapshot/v1", "facts": facts, "capabilities": capabilities, "action_result": action_result},
        "vector": vector,
        "feature_names": BASE.FEATURE_NAMES,
        "labels": {"danger": "none", "incident": label, "task": "monitor", "action": "no_action"},
        "source": "incident-v3-declarative-family",
        "scenario_version": "incident-v3",
    }


def build_dataset(output: Path) -> dict[str, Any]:
    output.mkdir(parents=True, exist_ok=True)
    splits: dict[str, list[dict[str, Any]]] = {}
    for split, per_class in COUNTS.items():
        records = [make_record(split, label, index, False) for label in LABELS for index in range(per_class)]
        splits[split] = records
        write_jsonl(output / f"{split}.jsonl", records)
    all_records = [record for split in splits.values() for record in split]
    write_jsonl(output / "corpus.jsonl", all_records)
    manifest = {
        "schema_version": "synora.cognitive-incident-v3-dataset/v1",
        "head": "incident",
        "dimension": BASE.DIMENSION,
        "feature_names": BASE.FEATURE_NAMES,
        "labels": LABELS,
        "splits": {name: len(records) for name, records in splits.items()},
        "class_counts": {name: {label: sum(record["labels"]["incident"] == label for record in records) for label in LABELS} for name, records in splits.items()},
        "split_policy": "new family/template/seed/topology/sequence namespaces; redteam V1 is never read or used",
        "training_forbidden_inputs": ["testdata/cognitive-v1/incident-redteam-v1.jsonl", "incident-redteam-v2"],
    }
    (output / "dataset-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return manifest


def train(records: list[dict[str, Any]]) -> dict[str, Any]:
    x = np.asarray([record["vector"] for record in records], dtype=np.float32)
    y = np.asarray([LABELS.index(record["labels"]["incident"]) for record in records], dtype=np.int64)
    hidden = 64
    rng = np.random.default_rng(SEED + len(LABELS))
    first = rng.normal(0, 0.06, (hidden, BASE.DIMENSION)).astype(np.float32)
    first_bias = np.zeros((hidden,), dtype=np.float32)
    weights = np.zeros((len(LABELS), hidden), dtype=np.float32)
    bias = np.zeros((len(LABELS),), dtype=np.float32)
    for epoch in range(120):
        order = np.random.default_rng(SEED + epoch + len(LABELS)).permutation(len(y))
        for start in range(0, len(y), 2048):
            batch = order[start:start + 2048]
            xb, yb = x[batch], y[batch]
            hidden_values = xb @ first.T + first_bias
            hidden_active = np.maximum(hidden_values, 0)
            logits = hidden_active @ weights.T + bias
            logits -= logits.max(axis=1, keepdims=True)
            probabilities = np.exp(logits)
            probabilities /= probabilities.sum(axis=1, keepdims=True)
            gradient = probabilities
            gradient[np.arange(len(yb)), yb] -= 1
            gradient /= len(yb)
            second_gradient = gradient.T @ hidden_active
            hidden_gradient = (gradient @ weights) * (hidden_values > 0)
            first_gradient = hidden_gradient.T @ xb
            rate = 0.06 if epoch < 40 else 0.02
            weights -= rate * second_gradient
            bias -= rate * gradient.sum(axis=0)
            first -= rate * first_gradient
            first_bias -= rate * hidden_gradient.sum(axis=0)
    return {"schema": "synora.cognitive-cpu-mlp/v1", "head": "incident", "input_size": BASE.DIMENSION, "output_size": len(LABELS), "labels": LABELS, "thresholds": [0.5] * len(LABELS), "vector_schema": "cognitive-encoder/v1", "output_names": ["logits"], "layers": [{"input_size": BASE.DIMENSION, "output_size": hidden, "activation": "relu", "weights": first.reshape(-1).tolist(), "bias": first_bias.tolist()}, {"input_size": hidden, "output_size": len(LABELS), "activation": "identity", "weights": weights.reshape(-1).tolist(), "bias": bias.tolist()}]}


def evaluate(model: dict[str, Any], records: list[dict[str, Any]]) -> dict[str, Any]:
    vectors = np.asarray([record["vector"] for record in records], dtype=np.float32)
    logits = BASE.cpu_run_batch(model, vectors)
    labels = model["labels"]
    expected = np.asarray([labels.index(record["labels"]["incident"] if "labels" in record else record["label"]) for record in records])
    predicted = np.argmax(logits, axis=1)
    matrix = [[0 for _ in labels] for _ in labels]
    for actual, guess in zip(expected.tolist(), predicted.tolist()):
        matrix[actual][guess] += 1
    precision, recall = {}, {}
    for index, label in enumerate(labels):
        tp = matrix[index][index]
        row = sum(matrix[index])
        column = sum(matrix[item][index] for item in range(len(labels)))
        precision[label] = tp / column if column else 0.0
        recall[label] = tp / row if row else 0.0
    return {"examples": len(records), "accuracy": float(np.mean(predicted == expected)), "precision": precision, "recall": recall, "confusion_matrix": matrix, "labels": labels}


def evaluate_redteam(model: dict[str, Any], records: list[dict[str, Any]]) -> dict[str, Any]:
    converted = []
    for record in records:
        converted.append({"vector": INCIDENT.encode_fixed(record["facts"], record.get("capabilities", []), record.get("action_result", "not_requested")), "label": record["label"]})
    return evaluate(model, converted)


def build_bundle(base_bundle: Path, output_bundle: Path, model: dict[str, Any]) -> None:
    if output_bundle.exists():
        shutil.rmtree(output_bundle)
    output_bundle.mkdir(parents=True)
    for head in BASE.HEADS:
        shutil.copy2(base_bundle / f"{head}.cpu.json", output_bundle / f"{head}.cpu.json")
    (output_bundle / "incident.cpu.json").write_text(json.dumps(model, separators=(",", ":")), encoding="utf-8")
    BASE.package(argparse.Namespace(bundle=output_bundle))


def main() -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    build = sub.add_parser("build")
    build.add_argument("--output", type=Path, default=Path("build/cognitive-incident-v3-dataset"))
    train_command = sub.add_parser("train-evaluate")
    train_command.add_argument("--dataset", type=Path, default=Path("build/cognitive-incident-v3-dataset"))
    train_command.add_argument("--base-bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2"))
    train_command.add_argument("--output-bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v3"))
    train_command.add_argument("--redteam-v1", type=Path, default=Path("testdata/cognitive-v1/incident-redteam-v1.jsonl"))
    args = parser.parse_args()
    if args.command == "build":
        print(json.dumps(build_dataset(args.output), sort_keys=True))
        return 0
    records = read_jsonl(args.dataset / "train.jsonl")
    model = train(records)
    build_bundle(args.base_bundle, args.output_bundle, model)
    reports = {split: evaluate(model, read_jsonl(args.dataset / f"{split}.jsonl")) for split in ("validation", "independent-test")}
    redteam_v1 = read_jsonl(args.redteam_v1)
    v2 = [make_record("redteam-v2", label, index, True) | {"label": label} for label in LABELS for index in range(REDTEAM_V2_COUNT)]
    redteam_v2_path = args.dataset / "incident-redteam-v2.jsonl"
    write_jsonl(redteam_v2_path, v2)
    reports["redteam_v1"] = evaluate_redteam(model, redteam_v1)
    reports["redteam_v2"] = evaluate_redteam(model, v2)
    reports["class_counts"] = {split: {label: sum(record["labels"]["incident"] == label for record in read_jsonl(args.dataset / f"{split}.jsonl")) for label in LABELS} for split in ("train", "validation", "independent-test")}
    reports["redteam_v2_hidden"] = True
    reports["redteam_v1_sha256"] = sha256(args.redteam_v1)
    reports["training_inputs"] = [str(args.dataset / "train.jsonl"), str(args.dataset / "validation.jsonl")]
    reports["training_forbidden"] = [str(args.redteam_v1), str(redteam_v2_path)]
    (args.output_bundle / "incident-v3-report.json").write_text(json.dumps(reports, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"bundle": str(args.output_bundle), "validation": reports["validation"]["accuracy"], "independent_test": reports["independent-test"]["accuracy"], "redteam_v1": reports["redteam_v1"]["accuracy"], "redteam_v2": reports["redteam_v2"]["accuracy"]}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
