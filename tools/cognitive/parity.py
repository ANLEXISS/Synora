#!/usr/bin/env python3
"""Compare PyTorch, exported ONNX and reference-CPU representations."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

import numpy as np
import onnxruntime as ort
import torch

sys.path.insert(0, str(Path(__file__).resolve().parent))
from export_models import DangerNet, IncidentTagNet, MultiLabelNet, load_checkpoint


def cpu_linear(values, layer):
    values = np.asarray(values, dtype=np.float32)
    result = np.asarray(layer["bias"], dtype=np.float32) + np.asarray(layer["weights"], dtype=np.float32).reshape(layer["output_size"], layer["input_size"]).dot(values)
    return np.maximum(result, 0) if layer["activation"] == "relu" else result


def cpu_run(model, values):
    current = np.asarray(values, dtype=np.float32)
    for layer in model["layers"]:
        current = cpu_linear(current, layer)
    return [current]


def cpu_incident(model, values):
    current = np.asarray(values, dtype=np.float32)
    for layer in model["shared_layers"]:
        current = cpu_linear(current, layer)
    return [cpu_linear(current, model["heads"]["tag_logits"]), cpu_linear(current, model["heads"]["phase_logits"])]


def sigmoid(values):
    return 1 / (1 + np.exp(-np.asarray(values, dtype=np.float32)))


def softmax(values):
    values = np.asarray(values, dtype=np.float32)
    values = values - values.max(axis=-1, keepdims=True)
    result = np.exp(values)
    return result / result.sum(axis=-1, keepdims=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--bundle", type=Path, required=True)
    parser.add_argument("--export", type=Path, required=True)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    rng = np.random.default_rng(20260917)
    args.fixtures.parent.mkdir(parents=True, exist_ok=True)
    records = []
    stats = {}

    danger_ckpt = load_checkpoint(args.bundle / "artifacts/danger-mlp-cognitive-v2plus-v3boundary-v4high.pt")
    danger_model = DangerNet(477); danger_model.load_state_dict(danger_ckpt["state_dict"]); danger_model.eval()
    incident_ckpt = load_checkpoint(args.bundle / "artifacts/incident-tags-mlp-v4-topology-perimeter.pt")
    incident_model = IncidentTagNet(477, len(incident_ckpt["kinds"]), len(incident_ckpt["phases"])); incident_model.load_state_dict(incident_ckpt["state_dict"]); incident_model.eval()
    task_ckpt = load_checkpoint(args.bundle / "artifacts/task-mlp-v4-boundaries.pt")
    task_model = MultiLabelNet(496, 10); task_model.load_state_dict(task_ckpt["state_dict"]); task_model.eval()
    action_ckpt = load_checkpoint(args.bundle / "artifacts/action-mlp-v2-lifecycle.pt")
    action_model = MultiLabelNet(531, 16); action_model.load_state_dict(action_ckpt["state_dict"]); action_model.eval()
    models = {"danger": danger_model, "incident": incident_model, "task": task_model, "action": action_model}
    cpu_models = {name: json.loads((args.export / f"{name}.cpu.json").read_text(encoding="utf-8")) for name in models}
    sessions = {name: ort.InferenceSession(str(args.export / f"{name}.onnx"), providers=["CPUExecutionProvider"]) for name in models}
    maximums = {name: 0.0 for name in models}
    label_mismatches = []

    with args.fixtures.open("w", encoding="utf-8") as fixture_stream:
        for index in range(100):
            state = rng.normal(0, 0.35, size=(477,)).astype(np.float32)
            state[:4] = 0; state[index % 4] = 1
            masked = state.copy(); masked[33:37] = 0
            availability = np.asarray([(index + slot) % 3 != 0 for slot in range(16)], dtype=np.float32)
            ledger = np.zeros((33,), dtype=np.float32); ledger[0] = float(index % 2); ledger[1 + (index % 16)] = float(index % 4 == 0)
            for slot in range(16):
                if ledger[1 + slot] > 0.5:
                    availability[slot] = 0
            with torch.no_grad():
                danger_pt = danger_model(torch.from_numpy(state[None, :])).numpy()
                incident_pt = [part.numpy() for part in incident_model(torch.from_numpy(masked[None, :]))]
                danger_prob = softmax(danger_pt[0])
                incident_tag_prob = sigmoid(incident_pt[0][0])
                incident_phase_prob = softmax(incident_pt[1][0])
                task_input = np.concatenate([masked, danger_prob, incident_tag_prob, incident_phase_prob]).astype(np.float32)
                task_pt = task_model(torch.from_numpy(task_input[None, :])).numpy()
                action_input = np.concatenate([state, danger_prob, availability, ledger]).astype(np.float32)
                action_pt = action_model(torch.from_numpy(action_input[None, :])).numpy()
            inputs = {"danger": state, "incident": masked, "task": task_input, "action": action_input}
            pytorch_outputs = {"danger": [danger_pt], "incident": incident_pt, "task": [task_pt], "action": [action_pt]}
            expected = {}
            for name in models:
                onnx_outputs = [np.asarray(item) for item in sessions[name].run(None, {"features": inputs[name][None, :]})]
                cpu_outputs = [np.asarray(item, dtype=np.float32)[None, :] for item in (cpu_incident(cpu_models[name], inputs[name]) if name == "incident" else cpu_run(cpu_models[name], inputs[name]))]
                pytorch = [np.asarray(item) for item in pytorch_outputs[name]]
                maximums[name] = max(maximums[name], max(float(np.max(np.abs(left - right))) for left, right in zip(pytorch, onnx_outputs)))
                maximums[name] = max(maximums[name], max(float(np.max(np.abs(left - right))) for left, right in zip(pytorch, cpu_outputs)))
                if name == "danger":
                    pt_labels = int(np.argmax(softmax(pytorch[0][0])))
                    onnx_labels = int(np.argmax(softmax(onnx_outputs[0][0])))
                    cpu_labels = int(np.argmax(softmax(cpu_outputs[0][0])))
                elif name == "incident":
                    thresholds = np.asarray(incident_ckpt["thresholds"], dtype=np.float32)
                    pt_labels = [((sigmoid(pytorch[0][0]) >= thresholds).astype(int).tolist()), int(np.argmax(softmax(pytorch[1][0])))]
                    onnx_labels = [((sigmoid(onnx_outputs[0][0]) >= thresholds).astype(int).tolist()), int(np.argmax(softmax(onnx_outputs[1][0])))]
                    cpu_labels = [((sigmoid(cpu_outputs[0][0]) >= thresholds).astype(int).tolist()), int(np.argmax(softmax(cpu_outputs[1][0])))]
                else:
                    ckpt = task_ckpt if name == "task" else action_ckpt
                    thresholds = np.asarray(ckpt["thresholds" if name == "task" else "threshold"], dtype=np.float32)
                    pt_labels = ((sigmoid(pytorch[0][0]) >= thresholds) & (availability > 0.5 if name == "action" else True)).astype(int).tolist()
                    onnx_labels = ((sigmoid(onnx_outputs[0][0]) >= thresholds) & (availability > 0.5 if name == "action" else True)).astype(int).tolist()
                    cpu_labels = ((sigmoid(cpu_outputs[0][0]) >= thresholds) & (availability > 0.5 if name == "action" else True)).astype(int).tolist()
                if pt_labels != onnx_labels or pt_labels != cpu_labels:
                    label_mismatches.append({"fixture": index, "head": name, "pytorch": pt_labels, "onnx": onnx_labels, "cpu": cpu_labels})
                expected[name] = pt_labels
            record = {"id": f"parity-{index:03d}", "schema": "synora.cognitive-mlp-parity-fixture/v1", "state": state.tolist(), "availability": availability.tolist(), "ledger": ledger.tolist(), "expected": expected}
            fixture_stream.write(json.dumps(record, separators=(",", ":")) + "\n")

    report = {"schema": "synora.cognitive-mlp-parity/v1", "fixtures": 100, "maximum_absolute_difference": maximums, "label_mismatches": label_mismatches, "passed": not label_mismatches and all(value <= 1e-4 for value in maximums.values())}
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))
    if not report["passed"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()

