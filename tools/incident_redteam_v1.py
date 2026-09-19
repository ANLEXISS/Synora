#!/usr/bin/env python3
"""Evaluate incident-v2 on a sealed, never-trained adversarial fixture set."""

from __future__ import annotations

import argparse
import importlib.util
import json
from pathlib import Path
from typing import Any

import numpy as np


def load_incident_pipeline(root: Path):
    path = root / "tools/incident_v2_pipeline.py"
    spec = importlib.util.spec_from_file_location("incident_v2_pipeline", path)
    if spec is None or spec.loader is None:
        raise RuntimeError("incident-v2 pipeline unavailable")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def metrics(labels: list[str], expected: list[int], predicted: list[int]) -> dict[str, Any]:
    matrix = [[0 for _ in labels] for _ in labels]
    for actual, guess in zip(expected, predicted):
        matrix[actual][guess] += 1
    precision: dict[str, float] = {}
    recall: dict[str, float] = {}
    for index, label in enumerate(labels):
        tp = matrix[index][index]
        row = sum(matrix[index])
        column = sum(matrix[item][index] for item in range(len(labels)))
        precision[label] = tp / column if column else 0.0
        recall[label] = tp / row if row else 0.0
    return {
        "examples": len(expected),
        "accuracy": sum(actual == guess for actual, guess in zip(expected, predicted)) / len(expected),
        "precision": precision,
        "recall": recall,
        "confusion_matrix": matrix,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path("."))
    parser.add_argument("--fixtures", type=Path, default=Path("testdata/cognitive-v1/incident-redteam-v1.jsonl"))
    parser.add_argument("--bundle", type=Path, default=Path("build/cognitive-mlp-v1"))
    parser.add_argument("--independent-report", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2/incident-v2-report.json"))
    parser.add_argument("--out", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2/incident-redteam-v1-report.json"))
    args = parser.parse_args()
    root = args.repo.resolve()
    pipeline = load_incident_pipeline(root)
    records = read_jsonl(root / args.fixtures)
    if not records:
        raise SystemExit("red-team fixture set is empty")
    if any(not record["id"].startswith("incident-redteam-v1/") for record in records):
        raise SystemExit("red-team IDs must use the incident-redteam-v1 namespace")
    if len({record["id"] for record in records}) != len(records):
        raise SystemExit("red-team IDs are not unique")
    forbidden = set(pipeline.BASE.FORBIDDEN)
    if any(pipeline.BASE.forbidden(record) for record in records):
        raise SystemExit("red-team fixture contains forbidden evidence")
    for split in ("train", "validation", "independent-test"):
        existing = root / "build/cognitive-incident-v2-dataset" / f"{split}.jsonl"
        if existing.exists() and {record["id"] for record in read_jsonl(existing)} & {record["id"] for record in records}:
            raise SystemExit(f"red-team fixture overlaps incident-v2 {split}")

    model = json.loads((root / args.bundle / "incident.cpu.json").read_text(encoding="utf-8"))
    labels = model["labels"]
    vectors = [
        pipeline.encode_fixed(record["facts"], record.get("capabilities", []), record.get("action_result", "not_requested"))
        for record in records
    ]
    logits = pipeline.BASE.cpu_run_batch(model, np.asarray(vectors, dtype=np.float32))
    predicted = np.argmax(logits, axis=1).tolist()
    expected = [labels.index(record["label"]) for record in records]
    result = metrics(labels, expected, predicted)
    result["errors"] = [
        {"id": record["id"], "expected": record["label"], "predicted": labels[guess], "facts": record["facts"]}
        for record, guess in zip(records, predicted)
        if record["label"] != labels[guess]
    ]
    independent = {}
    report_path = root / args.independent_report
    if report_path.exists():
        report = json.loads(report_path.read_text(encoding="utf-8"))
        independent = report.get("independent_test", {}).get("new", {})
    independent_accuracy = float(independent.get("accuracy", 0.0))
    result.update({
        "schema_version": "synora.cognitive-incident-redteam-report/v1",
        "dataset": str(args.fixtures),
        "training_forbidden": True,
        "independent_test": independent,
        "accuracy_delta_vs_independent": result["accuracy"] - independent_accuracy,
        "significant_drop": result["accuracy"] < independent_accuracy - 0.05,
        "retraining_performed": False,
        "debt_if_drop": "data-debt-only-no-retraining" if result["accuracy"] < independent_accuracy - 0.05 else "",
    })
    output = root / args.out
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(output), "accuracy": result["accuracy"], "independent_accuracy": independent_accuracy, "significant_drop": result["significant_drop"], "retraining_performed": False}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
