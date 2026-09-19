#!/usr/bin/env python3
"""Apply the incident-v3 promotion gates without mutating incident-v2 on failure."""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
from typing import Any

import numpy as np


def load(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ROOT = Path(__file__).resolve().parents[1]
V3 = load(ROOT / "tools/incident_v3_pipeline.py", "incident_v3_pipeline")
BASE = V3.BASE


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def evaluate_model(model_path: Path, records: list[dict[str, Any]], redteam: bool = False) -> dict[str, Any]:
    model = json.loads(model_path.read_text(encoding="utf-8"))
    if redteam:
        converted = [{"vector": V3.INCIDENT.encode_fixed(record["facts"], record.get("capabilities", []), record.get("action_result", "not_requested")), "label": record["label"]} for record in records]
    else:
        converted = records
    return V3.evaluate(model, converted)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", type=Path, default=Path("build/cognitive-incident-v3-dataset"))
    parser.add_argument("--existing-dataset", type=Path, default=Path("build/cognitive-incident-v2-dataset"))
    parser.add_argument("--v2-bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2"))
    parser.add_argument("--v3-bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v3"))
    parser.add_argument("--out", type=Path, default=Path("build/cognitive-mlp-v1-incident-v3/promotion-gate-report.json"))
    args = parser.parse_args()
    v1_path = ROOT / "testdata/cognitive-v1/incident-redteam-v1.jsonl"
    v1 = V3.read_jsonl(v1_path)
    v2_path = args.dataset / "incident-redteam-v2.jsonl"
    v2 = V3.read_jsonl(v2_path)
    independent = V3.read_jsonl(args.dataset / "independent-test.jsonl")
    existing_independent = V3.read_jsonl(args.existing_dataset / "independent-test.jsonl")
    v2_model = args.v2_bundle / "incident.cpu.json"
    v3_model = args.v3_bundle / "incident.cpu.json"
    old_v1 = evaluate_model(v2_model, v1, redteam=True)
    new_v1 = evaluate_model(v3_model, v1, redteam=True)
    old_v2 = evaluate_model(v2_model, v2, redteam=True)
    new_v2 = evaluate_model(v3_model, v2, redteam=True)
    old_independent = evaluate_model(v2_model, independent)
    new_independent = evaluate_model(v3_model, independent)
    old_existing_independent = evaluate_model(v2_model, existing_independent)
    new_existing_independent = evaluate_model(v3_model, existing_independent)
    unchanged = {head: sha256(args.v2_bundle / f"{head}.cpu.json") == sha256(args.v3_bundle / f"{head}.cpu.json") for head in ("danger", "task", "action")}
    parity_records = independent[:256]
    candidate = json.loads(v3_model.read_text(encoding="utf-8"))
    first = [BASE.cpu_run(candidate, record["vector"]) for record in parity_records]
    second = [BASE.cpu_run(candidate, record["vector"]) for record in parity_records]
    parity = first == second
    v3_report = json.loads((args.v3_bundle / "incident-v3-report.json").read_text(encoding="utf-8"))
    v2_manifest = json.loads((args.v2_bundle / "MANIFEST.v1.json").read_text(encoding="utf-8"))
    v3_manifest = json.loads((args.v3_bundle / "MANIFEST.v1.json").read_text(encoding="utf-8"))
    critical_labels = ("anomaly", "action_failure")
    measurable = all(new_v2["precision"].get(label, 0) > 0 and new_v2["recall"].get(label, 0) > 0 for label in critical_labels)
    no_critical_class_regression = all(new_v1["recall"].get(label, 0) >= old_v1["recall"].get(label, 0) for label in critical_labels)
    gates = {
        "independent_improved": new_independent["accuracy"] > old_independent["accuracy"],
        "existing_independent_improved": new_existing_independent["accuracy"] > old_existing_independent["accuracy"],
        "redteam_v1_improved": new_v1["accuracy"] > old_v1["accuracy"],
        "redteam_v2_coherent": new_v2["accuracy"] >= 0.80 and measurable,
        "anomaly_action_failure_measurable": measurable,
        "no_critical_redteam_v1_class_regression": no_critical_class_regression,
        "danger_task_action_byte_identical": all(unchanged.values()),
        "manifest_dimension": v3_manifest["input_dimension"] == v2_manifest["input_dimension"] == 86,
        "manifest_parity": parity,
        "physical_action_false": bool(v3_report.get("redteam_v2_hidden")) and v3_manifest["physical_action_executed"] is False and v3_manifest["dry_run_required"] is True,
    }
    passed = all(gates.values())
    report = {
        "schema_version": "synora.cognitive-incident-v3-promotion-gate/v1",
        "candidate": str(args.v3_bundle),
        "runtime_bundle_retained": str(args.v2_bundle),
        "promoted": False,
        "gates": gates,
        "metrics": {"v2_independent": old_independent, "v3_independent": new_independent, "v2_existing_independent": old_existing_independent, "v3_existing_independent": new_existing_independent, "v2_redteam_v1": old_v1, "v3_redteam_v1": new_v1, "v2_redteam_v2": old_v2, "v3_redteam_v2": new_v2},
        "unchanged_heads": unchanged,
        "redteam_v1_sha256": sha256(v1_path),
        "training_forbidden": [str(v1_path), str(v2_path)],
        "decision": "promote" if passed else "retain incident-v2; report data debt; no retraining on redteam-v1",
    }
    if passed:
        (args.v2_bundle / "incident.cpu.json").write_bytes(v3_model.read_bytes())
        shutil.copy2(args.v3_bundle / "MANIFEST.v1.json", args.v2_bundle / "MANIFEST.v1.json")
        report["promoted"] = True
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"passed": passed, "promoted": report["promoted"], "v2_independent": old_independent["accuracy"], "v3_independent": new_independent["accuracy"], "v2_redteam_v1": old_v1["accuracy"], "v3_redteam_v1": new_v1["accuracy"], "report": str(args.out)}, sort_keys=True))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
