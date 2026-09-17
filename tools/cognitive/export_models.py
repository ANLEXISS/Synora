#!/usr/bin/env python3
"""Export the verified Synora MLP bundle to ONNX and a CPU reference format.

The JSON weight format is intentionally simple so the reference backend can
run without embedding Python or a model-specific runtime. ONNX remains the
interchange artifact for later RKNN conversion.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

import torch
from torch import nn


DANGER_LABELS = ["none", "low", "medium", "high", "critical"]
TASK_LABELS = ["monitor", "collect_evidence", "verify_identity", "deter_presence", "notify_resident", "notify_security", "emergency_response", "contain_hazard", "diagnose_fault", "close_incident"]
ACTION_SLOTS = ["observation.record@event_zone", "observation.increase@event_zone", "camera.record@event_zone", "camera.stream@event_zone", "light.activate@event_zone", "light.set_alert_mode@event_zone", "light.set_alert_mode@interior", "access.secure@event_zone", "access.close@event_zone", "shutter.close@perimeter", "gate.close@perimeter", "alarm.activate@global", "notify.resident@global", "notify.security@global", "notify.emergency@global", "incident.create@global"]


class DangerNet(nn.Module):
    def __init__(self, size: int):
        super().__init__()
        self.net = nn.Sequential(nn.Linear(size, 128), nn.ReLU(), nn.Dropout(.1), nn.Linear(128, 64), nn.ReLU(), nn.Linear(64, 5))

    def forward(self, x):
        return self.net(x)


class IncidentTagNet(nn.Module):
    def __init__(self, size: int, tags: int, phases: int):
        super().__init__()
        self.body = nn.Sequential(nn.Linear(size, 176), nn.ReLU(), nn.Dropout(.1), nn.Linear(176, 88), nn.ReLU())
        self.tags = nn.Linear(88, tags)
        self.phase = nn.Linear(88, phases)

    def forward(self, x):
        shared = self.body(x)
        return self.tags(shared), self.phase(shared)


class MultiLabelNet(nn.Module):
    def __init__(self, size: int, outputs: int):
        super().__init__()
        self.net = nn.Sequential(nn.Linear(size, 192), nn.ReLU(), nn.Dropout(.1), nn.Linear(192, 96), nn.ReLU(), nn.Linear(96, outputs))

    def forward(self, x):
        return self.net(x)


def load_checkpoint(path: Path):
    try:
        return torch.load(path, map_location="cpu", weights_only=False)
    except TypeError:
        return torch.load(path, map_location="cpu")


def linear_layers(model: nn.Module, names: list[str], final_identity: bool = True) -> list[dict]:
    state = model.state_dict()
    layers = []
    for name in names:
        weight = state[f"{name}.weight"].detach().cpu().float()
        bias = state[f"{name}.bias"].detach().cpu().float()
        layers.append({"input_size": int(weight.shape[1]), "output_size": int(weight.shape[0]), "activation": "identity" if final_identity and name == names[-1] else "relu", "weights": weight.reshape(-1).tolist(), "bias": bias.tolist()})
    return layers


def dump_cpu(path: Path, head: str, model: nn.Module, input_size: int, labels: list[str], thresholds=None, output_names=None, vector_schema="state-encoder/v4") -> None:
    if head == "danger":
        layers = linear_layers(model, ["net.0", "net.3", "net.5"])
        payload = {"schema": "synora.cognitive-cpu-mlp/v1", "head": head, "input_size": input_size, "output_size": len(labels), "labels": labels, "thresholds": thresholds or [], "vector_schema": vector_schema, "output_names": output_names or ["logits"], "layers": layers}
    elif head == "incident":
        state = model.state_dict()
        payload = {"schema": "synora.cognitive-cpu-mlp/v1", "head": head, "input_size": input_size, "output_size": len(labels), "labels": labels, "thresholds": thresholds or [], "vector_schema": vector_schema, "output_names": output_names or ["tag_logits", "phase_logits"], "shared_layers": linear_layers(model, ["body.0", "body.3"], final_identity=False), "heads": {"tag_logits": {"input_size": int(state["tags.weight"].shape[1]), "output_size": int(state["tags.weight"].shape[0]), "activation": "identity", "weights": state["tags.weight"].detach().cpu().float().reshape(-1).tolist(), "bias": state["tags.bias"].detach().cpu().float().tolist()}, "phase_logits": {"input_size": int(state["phase.weight"].shape[1]), "output_size": int(state["phase.weight"].shape[0]), "activation": "identity", "weights": state["phase.weight"].detach().cpu().float().reshape(-1).tolist(), "bias": state["phase.bias"].detach().cpu().float().tolist()}}}
    else:
        layers = linear_layers(model, ["net.0", "net.3", "net.5"])
        payload = {"schema": "synora.cognitive-cpu-mlp/v1", "head": head, "input_size": input_size, "output_size": len(labels), "labels": labels, "thresholds": thresholds or [], "vector_schema": vector_schema, "output_names": output_names or ["logits"], "layers": layers}
    path.write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")


def export_onnx(path: Path, model: nn.Module, input_size: int, output_names: list[str], destination: Path) -> None:
    dummy = torch.zeros((1, input_size), dtype=torch.float32)
    model.eval()
    torch.onnx.export(model, dummy, destination, input_names=["features"], output_names=output_names, opset_version=13, do_constant_folding=True)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--bundle", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    manifest = json.loads((args.bundle / "MANIFEST.json").read_text(encoding="utf-8"))
    args.output.mkdir(parents=True, exist_ok=True)
    heads = {}

    danger_path = args.bundle / "artifacts/danger-mlp-cognitive-v2plus-v3boundary-v4high.pt"
    danger_ckpt = load_checkpoint(danger_path)
    danger = DangerNet(int(danger_ckpt["feature_size"]))
    danger.load_state_dict(danger_ckpt["state_dict"]); danger.eval()
    export_onnx(danger_path, danger, 477, ["logits"], args.output / "danger.onnx")
    dump_cpu(args.output / "danger.cpu.json", "danger", danger, 477, DANGER_LABELS, output_names=["logits"])
    heads["danger"] = {"source": danger_path.name, "source_sha256": sha256(danger_path), "input_size": 477, "labels": DANGER_LABELS, "policy_cost": danger_ckpt.get("policy_cost", []), "policy_cost_weight": danger_ckpt.get("policy_cost_weight"), "onnx": "danger.onnx", "cpu": "danger.cpu.json", "postprocess": "softmax"}

    incident_path = args.bundle / "artifacts/incident-tags-mlp-v4-topology-perimeter.pt"
    incident_ckpt = load_checkpoint(incident_path)
    incident = IncidentTagNet(int(incident_ckpt["input_size"]), len(incident_ckpt["kinds"]), len(incident_ckpt["phases"]))
    incident.load_state_dict(incident_ckpt["state_dict"]); incident.eval()
    export_onnx(incident_path, incident, 477, ["tag_logits", "phase_logits"], args.output / "incident.onnx")
    incident_labels = list(incident_ckpt["kinds"]); incident_phases = list(incident_ckpt["phases"])
    dump_cpu(args.output / "incident.cpu.json", "incident", incident, 477, incident_labels + incident_phases, list(incident_ckpt["thresholds"]), ["tag_logits", "phase_logits"], "incident-tags-vector/v2+state-encoder/v4-capabilities-masked")
    heads["incident"] = {"source": incident_path.name, "source_sha256": sha256(incident_path), "input_size": 477, "labels": incident_labels, "phases": incident_phases, "thresholds": list(incident_ckpt["thresholds"]), "onnx": "incident.onnx", "cpu": "incident.cpu.json", "postprocess": "sigmoid-tags+softmax-phase"}

    task_path = args.bundle / "artifacts/task-mlp-v4-boundaries.pt"
    task_ckpt = load_checkpoint(task_path)
    task = MultiLabelNet(int(task_ckpt["input_size"]), int(task_ckpt["output_size"]))
    task.load_state_dict(task_ckpt["state_dict"]); task.eval()
    export_onnx(task_path, task, 496, ["logits"], args.output / "task.onnx")
    task_labels = list(task_ckpt["tasks"])
    dump_cpu(args.output / "task.cpu.json", "task", task, 496, task_labels, list(task_ckpt["thresholds"]), ["logits"], task_ckpt["vector_schema"])
    heads["task"] = {"source": task_path.name, "source_sha256": sha256(task_path), "input_size": 496, "labels": task_labels, "thresholds": list(task_ckpt["thresholds"]), "onnx": "task.onnx", "cpu": "task.cpu.json", "postprocess": "sigmoid"}

    action_path = args.bundle / "artifacts/action-mlp-v2-lifecycle.pt"
    action_ckpt = load_checkpoint(action_path)
    action = MultiLabelNet(int(action_ckpt["input_size"]), int(action_ckpt["output_size"]))
    action.load_state_dict(action_ckpt["state_dict"]); action.eval()
    export_onnx(action_path, action, 531, ["logits"], args.output / "action.onnx")
    action_labels = list(action_ckpt["slots"])
    dump_cpu(args.output / "action.cpu.json", "action", action, 531, action_labels, list(action_ckpt["threshold"]), ["logits"], action_ckpt["vector_schema"])
    heads["action"] = {"source": action_path.name, "source_sha256": sha256(action_path), "input_size": 531, "labels": action_labels, "thresholds": list(action_ckpt["threshold"]), "onnx": "action.onnx", "cpu": "action.cpu.json", "postprocess": "sigmoid+availability+ledger-mask"}

    runtime = {"schema": "synora.cognitive-runtime-manifest/v1", "bundle_schema": manifest["bundle_schema"], "encoder": {"schema": "state-encoder/v4", "feature_size": 477, "implementation": "synora-state-encoder/4.0.0"}, "heads": heads, "dry_run_required": True, "source_manifest_sha256": sha256(args.bundle / "MANIFEST.json")}
    (args.output / "MANIFEST.runtime.json").write_text(json.dumps(runtime, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "heads": sorted(heads), "dry_run_required": True}))


if __name__ == "__main__":
    main()

