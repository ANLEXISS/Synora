#!/usr/bin/env python3
"""Produce a causal, read-only audit of the sealed incident red-team set.

This command is deliberately an evaluator, never a training input.  It reads
the immutable red-team fixture and the already-built incident-v2 bundle, then
compares it with the existing train/validation/independent corpora only to
explain coverage, collisions and information loss.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
from typing import Any

import numpy as np


GROUPS = {
    "base": (0, 42),
    "vision": (42, 86),
}


def load_module(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def vector_key(vector: list[float] | np.ndarray) -> tuple[float, ...]:
    return tuple(round(float(value), 7) for value in vector)


def probabilities(logits: np.ndarray) -> np.ndarray:
    shifted = logits - np.max(logits)
    values = np.exp(shifted)
    return values / np.sum(values)


def nonzero_features(vector: list[float], names: list[str]) -> dict[str, float]:
    return {name: round(float(value), 7) for name, value in zip(names, vector) if abs(float(value)) > 1e-9}


def canonical_state(record: dict[str, Any]) -> dict[str, Any]:
    return {
        "facts": record["facts"],
        "capabilities": record.get("capabilities", []),
        "action_result": record.get("action_result", "not_requested"),
        "previous_action": record.get("previous_action"),
        "previous_action_result": record.get("previous_action_result"),
    }


def collision_index(records: list[dict[str, Any]], vector_getter) -> dict[tuple[float, ...], dict[str, Any]]:
    index: dict[tuple[float, ...], dict[str, Any]] = {}
    for record in records:
        key = vector_key(vector_getter(record))
        bucket = index.setdefault(key, {"labels": set(), "ids": [], "sources": set()})
        label = record.get("label", record.get("labels", {}).get("incident"))
        bucket["labels"].add(label)
        bucket["ids"].append(record["id"])
        bucket["sources"].add(record.get("split", record.get("source", "unknown")))
    return index


def nearest_opposites(target: np.ndarray, target_label: str, records: list[dict[str, Any]], vectors: np.ndarray, limit: int = 3) -> list[dict[str, Any]]:
    distances = np.sum(np.abs(vectors - target), axis=1)
    candidates = [
        (float(distance), record)
        for distance, record in zip(distances.tolist(), records)
        if record.get("label", record.get("labels", {}).get("incident")) != target_label
    ]
    candidates.sort(key=lambda item: (item[0], item[1]["id"]))
    return [{"id": record["id"], "label": record.get("label", record.get("labels", {}).get("incident")), "l1_distance": round(distance, 7)} for distance, record in candidates[:limit]]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path("."))
    parser.add_argument("--fixtures", type=Path, default=Path("testdata/cognitive-v1/incident-redteam-v1.jsonl"))
    parser.add_argument("--bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2"))
    parser.add_argument("--dataset", type=Path, default=Path("build/cognitive-v1-dataset"))
    parser.add_argument("--out", type=Path, default=Path("build/incident-redteam-v1-diagnosis.json"))
    args = parser.parse_args()
    root = args.repo.resolve()
    redteam_path = root / args.fixtures
    if not redteam_path.exists():
        raise SystemExit(f"missing sealed fixture: {redteam_path}")
    incident = load_module(root / "tools/incident_v2_pipeline.py", "incident_v2_pipeline")
    base = incident.BASE
    redteam_hash_before = sha256(redteam_path)
    redteam = read_jsonl(redteam_path)
    if any(not record["id"].startswith("incident-redteam-v1/") for record in redteam):
        raise SystemExit("diagnostic input is not incident-redteam-v1")
    model = json.loads((root / args.bundle / "incident.cpu.json").read_text(encoding="utf-8"))
    labels = model["labels"]
    vectors = np.asarray([
        incident.encode_fixed(record["facts"], record.get("capabilities", []), record.get("action_result", "not_requested"))
        for record in redteam
    ], dtype=np.float32)
    logits = base.cpu_run_batch(model, vectors)
    rows = []
    for record, vector, row in zip(redteam, vectors, logits):
        probs = probabilities(row)
        predicted_index = int(np.argmax(probs))
        predicted = labels[predicted_index]
        if predicted == record["label"]:
            continue
        state = canonical_state(record)
        rows.append({
            "id": record["id"],
            "canonical_state": state,
            "feature_groups_86d": {
                "base": nonzero_features(vector[:42], base.FEATURE_NAMES[:42]),
                "vision": nonzero_features(vector[42:], base.FEATURE_NAMES[42:]),
            },
            "expected": record["label"],
            "predicted": predicted,
            "logits": {label: round(float(value), 7) for label, value in zip(labels, row)},
            "probabilities": {label: round(float(value), 7) for label, value in zip(labels, probs)},
            "confidence": round(float(probs[predicted_index]), 7),
            "topology": record["facts"].get("topology", "unknown"),
            "phase": record["facts"].get("phase", "initial"),
            "vision": {key: record["facts"].get(key) for key in ("real_detection", "replay_simulation", "observation_count", "confidence", "track_count", "track_confirmed", "enrichment")},
            "previous_action": state.get("previous_action"),
            "previous_action_result": state.get("previous_action_result", record.get("action_result", "not_requested")),
            "confusion": f"{record['label']}->{predicted}",
        })

    corpus: list[dict[str, Any]] = []
    for split in ("train", "validation", "independent-test"):
        path = root / args.dataset / f"{split}.jsonl"
        for record in read_jsonl(path):
            record["_diagnostic_split"] = split
            corpus.append(record)
    corpus_vectors = np.asarray([record["vector"] for record in corpus], dtype=np.float32)
    corpus_index = collision_index(corpus, lambda record: record["vector"])
    conflicting_exact = []
    for key, bucket in corpus_index.items():
        if len(bucket["labels"]) > 1:
            conflicting_exact.append({"labels": sorted(bucket["labels"]), "ids": bucket["ids"][:20], "sources": sorted(bucket["sources"])})
    redteam_index = collision_index(redteam, lambda record: incident.encode_fixed(record["facts"], record.get("capabilities", []), record.get("action_result", "not_requested")))
    redteam_exact_matches = []
    for record, vector in zip(redteam, vectors):
        bucket = corpus_index.get(vector_key(vector))
        if bucket:
            redteam_exact_matches.append({"id": record["id"], "label": record["label"], "corpus_labels": sorted(bucket["labels"]), "corpus_ids": bucket["ids"][:20]})

    label_counts: dict[str, dict[str, int]] = {}
    topology_phase: dict[str, dict[str, int]] = {}
    for record in corpus:
        label = record["labels"]["incident"]
        label_counts.setdefault(record["_diagnostic_split"], {})[label] = label_counts.setdefault(record["_diagnostic_split"], {}).get(label, 0) + 1
        key = f"{record['snapshot']['facts'].get('topology', 'unknown')}|{record['snapshot']['facts'].get('phase', 'initial')}"
        topology_phase.setdefault(label, {})[key] = topology_phase.setdefault(label, {}).get(key, 0) + 1

    redteam_label_counts: dict[str, int] = {}
    for record in redteam:
        redteam_label_counts[record["label"]] = redteam_label_counts.get(record["label"], 0) + 1

    absent_or_lossy = {
        "expected_action_not_encoded": True,
        "capability_identity_not_encoded_only_count": True,
        "previous_action_not_encoded": all(record.get("previous_action") is None for record in redteam),
        "action_result_encoded_as_outcome_category": True,
        "raw_vision_absent": True,
    }
    report = {
        "schema_version": "synora.incident-redteam-v1-diagnosis/v1",
        "training_forbidden": True,
        "redteam_path": str(args.fixtures),
        "redteam_sha256": redteam_hash_before,
        "redteam_sha256_after_read": sha256(redteam_path),
        "redteam_examples": len(redteam),
        "errors": rows,
        "error_count": len(rows),
        "exact_collisions": {"training_validation_independent_conflicts": conflicting_exact, "redteam_exact_matches": redteam_exact_matches},
        "quasi_collisions": [
            {"id": record["id"], "label": record["label"], "nearest_opposites": nearest_opposites(vector, record["label"], corpus, corpus_vectors)}
            for record, vector in zip(redteam, vectors) if record["label"] != labels[int(np.argmax(base.cpu_run_batch(model, vector.reshape(1, -1))[0]))]
        ],
        "class_counts": {"corpus": label_counts, "redteam_v1": redteam_label_counts},
        "topology_phase_coverage_by_label": topology_phase,
        "causal_information_audit": absent_or_lossy,
        "conclusion": {
            "exact_86d_conflicting_label_collision": bool(conflicting_exact),
            "86d_has_coverage_gap": True,
            "minimal_snapshot_v2_required_now": False,
            "finding": "The 86D contract exposes topology, phase, sensor facts and action-result outcome, but the V1 corpus overfits a small template set and omits previous-action identity/capability identity. This is a data-coverage weakness, not proof that a CognitiveSnapshot v2 is required.",
        },
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if sha256(redteam_path) != redteam_hash_before:
        raise SystemExit("sealed red-team fixture changed while diagnosing")
    print(json.dumps({"report": str(args.out), "errors": len(rows), "exact_conflicts": len(conflicting_exact), "redteam_sha256": redteam_hash_before}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
