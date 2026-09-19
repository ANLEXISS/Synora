#!/usr/bin/env python3
"""Train and evaluate only the incident head on corrected categorical evidence."""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
from typing import Any

import numpy as np


def load_base():
	path = Path(__file__).with_name("cognitive_v1_pipeline.py")
	spec = importlib.util.spec_from_file_location("cognitive_v1_pipeline", path)
	if spec is None or spec.loader is None:
		raise RuntimeError("cannot load cognitive V1 pipeline")
	module = importlib.util.module_from_spec(spec)
	spec.loader.exec_module(module)
	return module


BASE = load_base()
HEAD = "incident"


def encode_fixed(facts: dict[str, Any], capabilities: list[str], action_result: str) -> list[float]:
	vector = BASE.encode_facts(facts, capabilities, action_result)

	def one_hot(start: int, values: list[str], value: Any) -> None:
		if value in values:
			vector[start + values.index(value)] = 1.0

	one_hot(8, ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"], facts.get("topology", "unknown"))
	one_hot(13, ["initial", "candidate", "confirmed", "final"], facts.get("phase", "initial"))
	one_hot(25, ["unknown", "closed", "open", "forced"], facts.get("access_state", "unknown"))
	one_hot(30, ["unknown", "armed", "disarmed", "triggered"], facts.get("alarm_state", "unknown"))
	one_hot(48, ["public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"], facts.get("topology", "unknown"))
	one_hot(53, ["P0", "P1", "P2", "P3", "P4"], facts.get("priority", "P4"))
	one_hot(58, ["initial", "candidate", "confirmed", "final"], facts.get("phase", "initial"))
	one_hot(62, ["unavailable", "not_requested", "uncertain", "recognized", "unknown"], facts.get("enrichment", "unavailable"))
	one_hot(76, ["unknown", "closed", "open", "forced"], facts.get("access_state", "unknown"))
	one_hot(82, ["unknown", "armed", "disarmed", "triggered"], facts.get("alarm_state", "unknown"))
	return vector


def build_dataset(base_dataset: Path, output: Path) -> None:
	output.mkdir(parents=True, exist_ok=True)
	for split in ("train", "validation", "independent-test"):
		records = BASE.read_jsonl(base_dataset / f"{split}.jsonl")
		converted = []
		for record in records:
			item = dict(record)
			item["schema_version"] = "synora.cognitive-incident-v2-training-example/v1"
			item["vector"] = encode_fixed(item["snapshot"]["facts"], item["snapshot"].get("capabilities", []), item["snapshot"].get("action_result", "not_requested"))
			item["evidence_revision"] = "categorical-one-hot-v2"
			converted.append(item)
		BASE.write_records(output / f"{split}.jsonl", converted)
	manifest = {"schema_version": "synora.cognitive-incident-v2-dataset/v1", "dimension": BASE.DIMENSION, "feature_names": BASE.FEATURE_NAMES, "head": HEAD, "splits": {split: len(BASE.read_jsonl(output / f"{split}.jsonl")) for split in ("train", "validation", "independent-test")}, "split_policy": "inherited disjoint family/template/seed/topology/sequence namespaces", "evidence_revision": "categorical-one-hot-v2", "forbidden_data": True}
	(output / "dataset-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


def train(records: list[dict[str, Any]]) -> dict[str, Any]:
	labels = BASE.LABELS[HEAD]
	x = np.asarray([record["vector"] for record in records], dtype=np.float32)
	y = np.asarray([labels.index(record["labels"][HEAD]) for record in records], dtype=np.int64)
	hidden = 32
	rng = np.random.default_rng(BASE.SEED + len(labels))
	first = rng.normal(0, 0.08, (hidden, BASE.DIMENSION)).astype(np.float32)
	first_bias = np.zeros((hidden,), dtype=np.float32)
	weights = np.zeros((len(labels), hidden), dtype=np.float32)
	bias = np.zeros((len(labels),), dtype=np.float32)
	for epoch in range(90):
		order = np.random.default_rng(BASE.SEED + epoch + len(labels)).permutation(len(y))
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
			rate = 0.08 if epoch < 30 else 0.03
			weights -= rate * second_gradient
			bias -= rate * gradient.sum(axis=0)
			first -= rate * first_gradient
			first_bias -= rate * hidden_gradient.sum(axis=0)
	return {"schema": "synora.cognitive-cpu-mlp/v1", "head": HEAD, "input_size": BASE.DIMENSION, "output_size": len(labels), "labels": labels, "thresholds": [0.5] * len(labels), "vector_schema": "cognitive-encoder/v1", "output_names": ["logits"], "layers": [{"input_size": BASE.DIMENSION, "output_size": hidden, "activation": "relu", "weights": first.reshape(-1).tolist(), "bias": first_bias.tolist()}, {"input_size": hidden, "output_size": len(labels), "activation": "identity", "weights": weights.reshape(-1).tolist(), "bias": bias.tolist()}]}


def accuracy(model: dict[str, Any], records: list[dict[str, Any]]) -> dict[str, Any]:
	labels = model["labels"]
	values = np.asarray([record["vector"] for record in records], dtype=np.float32)
	logits = BASE.cpu_run_batch(model, values)
	predicted = np.argmax(logits, axis=1)
	expected = np.asarray([labels.index(record["labels"][HEAD]) for record in records])
	matrix = [[0 for _ in labels] for _ in labels]
	for actual, guess in zip(expected.tolist(), predicted.tolist()):
		matrix[actual][guess] += 1
	precision, recall = {}, {}
	for index, label in enumerate(labels):
		tp = matrix[index][index]
		row = sum(matrix[index]); column = sum(matrix[item][index] for item in range(len(labels)))
		precision[label] = tp / column if column else 0.0
		recall[label] = tp / row if row else 0.0
	return {"examples": len(records), "accuracy": float(np.mean(predicted == expected)), "confusion_matrix": matrix, "precision": precision, "recall": recall, "labels": labels}


def error_analysis(model: dict[str, Any], records: list[dict[str, Any]]) -> dict[str, Any]:
	labels = model["labels"]
	values = np.asarray([record["vector"] for record in records], dtype=np.float32)
	predicted = np.argmax(BASE.cpu_run_batch(model, values), axis=1)
	counts: dict[str, dict[str, int]] = {}
	for record, guess in zip(records, predicted.tolist()):
		expected = record["labels"][HEAD]
		if labels[guess] == expected:
			continue
		facts = record["snapshot"]["facts"]
		for name, value in {"expected": expected, "predicted": labels[guess], "topology": facts.get("topology", "unknown"), "danger": record["labels"].get("danger", "none"), "phase": facts.get("phase", "initial"), "gap": str(facts.get("gap_count", 0)), "vision_availability": "real" if facts.get("real_detection", False) else "replay_or_unavailable", "action_result": record["snapshot"].get("action_result", "not_requested")}.items():
			bucket = counts.setdefault(name, {})
			bucket[str(value)] = bucket.get(str(value), 0) + 1
	return {"errors": sum(counts.get("expected", {}).values()), "by_dimension": counts}


def sha256(path: Path) -> str:
	digest = hashlib.sha256()
	with path.open("rb") as stream:
		for chunk in iter(lambda: stream.read(1024 * 1024), b""):
			digest.update(chunk)
	return digest.hexdigest()


def main() -> int:
	parser = argparse.ArgumentParser()
	parser.add_argument("--base-dataset", type=Path, default=Path("build/cognitive-v1-dataset"))
	parser.add_argument("--base-bundle", type=Path, default=Path("build/cognitive-mlp-v1"))
	parser.add_argument("--dataset", type=Path, default=Path("build/cognitive-incident-v2-dataset"))
	parser.add_argument("--output", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2"))
	parser.add_argument("--promote", action="store_true")
	args = parser.parse_args()
	build_dataset(args.base_dataset, args.dataset)
	train_records = BASE.read_jsonl(args.dataset / "train.jsonl")
	validation_records = BASE.read_jsonl(args.dataset / "validation.jsonl")
	test_records = BASE.read_jsonl(args.dataset / "independent-test.jsonl")
	incident_model = train(train_records)
	args.output.mkdir(parents=True, exist_ok=True)
	for head in BASE.HEADS:
		shutil.copy2(args.base_bundle / f"{head}.cpu.json", args.output / f"{head}.cpu.json")
	(args.output / "incident.cpu.json").write_text(json.dumps(incident_model, separators=(",", ":")), encoding="utf-8")
	BASE.package(argparse.Namespace(bundle=args.output))
	old_model = json.loads((args.base_bundle / "incident.cpu.json").read_text(encoding="utf-8"))
	report = {"schema_version": "synora.cognitive-incident-v2-report/v1", "dataset": str(args.dataset), "bundle": str(args.output), "evidence_revision": "categorical-one-hot-v2", "validation": accuracy(incident_model, validation_records), "independent_test": {"old": accuracy(old_model, test_records), "new": accuracy(incident_model, test_records)}, "error_analysis_old": error_analysis(old_model, test_records), "unchanged_heads": {head: sha256(args.base_bundle / f"{head}.cpu.json") == sha256(args.output / f"{head}.cpu.json") for head in ("danger", "task", "action")}, "same_dimension": all(len(record["vector"]) == BASE.DIMENSION for record in test_records), "forbidden_data_absent": all(not BASE.forbidden(record) for record in test_records), "teacher_and_action_scope": "incident head only; teacher labels and action head unchanged", "retained": False}
	report["improvement"] = report["independent_test"]["new"]["accuracy"] - report["independent_test"]["old"]["accuracy"]
	report["retained"] = bool(report["independent_test"]["new"]["accuracy"] >= 0.92 and report["improvement"] > 0 and all(report["unchanged_heads"].values()) and report["same_dimension"] and report["forbidden_data_absent"])
	(args.output / "incident-v2-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
	if args.promote and report["retained"]:
		(args.base_bundle / "incident.cpu.json").write_text((args.output / "incident.cpu.json").read_text(encoding="utf-8"), encoding="utf-8")
		BASE.package(argparse.Namespace(bundle=args.base_bundle))
		(args.base_bundle / "incident-v2-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
	print(json.dumps({"report": str(args.output / "incident-v2-report.json"), "retained": report["retained"], "old_accuracy": report["independent_test"]["old"]["accuracy"], "new_accuracy": report["independent_test"]["new"]["accuracy"]}, sort_keys=True))
	return 0 if report["retained"] else 1


if __name__ == "__main__":
	raise SystemExit(main())
