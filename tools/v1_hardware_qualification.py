#!/usr/bin/env python3
"""Reproducible V1 hardware qualification harness.

This tool observes the host or an explicit fixture. It never labels host or
fixture observations as physical qualification of a Synora target unit.
"""

import argparse
import hashlib
import json
import os
import platform
import shutil
import socket
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

SCHEMA_VERSION = 1
SAMPLE_NAME = "hardware.samples.ndjson"
MANIFEST_NAME = "hardware.manifest.json"
SUMMARY_NAME = "hardware.summary.json"
JOURNAL_NAME = "power-cut.journal.json"


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def digest_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def atomic_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.parent.chmod(0o700)
    temporary = path.with_name("." + path.name + ".tmp")
    data = json.dumps(value, sort_keys=True, indent=2).encode("utf-8") + b"\n"
    with temporary.open("wb") as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.chmod(0o600)
    os.replace(temporary, path)


def read_json(path):
    with Path(path).open("r", encoding="utf-8") as stream:
        return json.load(stream)


def load_fixture(path):
    if not path:
        return {}
    value = read_json(path)
    if not isinstance(value, dict):
        raise ValueError("fixture must be a JSON object")
    return value


def read_thermal():
    values = {}
    for path in sorted(Path("/sys/class/thermal").glob("thermal_zone*/temp")):
        try:
            values[path.parent.name] = int(path.read_text().strip()) / 1000.0
        except (OSError, ValueError):
            continue
    return values


def read_load():
    try:
        values = Path("/proc/loadavg").read_text().split()
        return [float(value) for value in values[:3]]
    except (OSError, ValueError):
        return []


def read_network():
    result = {}
    try:
        lines = Path("/proc/net/dev").read_text().splitlines()[2:]
    except OSError:
        return result
    for line in lines:
        if ":" not in line:
            continue
        name, payload = line.split(":", 1)
        fields = payload.split()
        if len(fields) >= 9:
            try:
                result[name.strip()] = {"rx_bytes": int(fields[0]), "tx_bytes": int(fields[8])}
            except ValueError:
                continue
    return result


def read_ssd():
    result = {}
    for path in sorted(Path("/sys/block").glob("*/stat")):
        try:
            fields = path.read_text().split()
            if len(fields) < 7:
                continue
            rotational_path = path.parent / "queue" / "rotational"
            rotational = rotational_path.read_text().strip() == "1"
            result[path.parent.name] = {
                "sectors_written": int(fields[6]),
                "rotational": rotational,
            }
        except (OSError, ValueError):
            continue
    return result


def collect_sample(fixture):
    fixture = fixture or {}
    storage_path = str(fixture.get("storage_path", "/var/lib/synora"))
    try:
        usage = shutil.disk_usage(storage_path)
        storage = {"path": storage_path, "total_bytes": usage.total, "free_bytes": usage.free, "used_bytes": usage.used}
    except OSError as exc:
        storage = {"path": storage_path, "available": False, "reason": type(exc).__name__}
    thermal = fixture.get("thermal_c", read_thermal())
    load = fixture.get("loadavg", read_load())
    network = fixture.get("network", read_network())
    ssd = fixture.get("ssd", read_ssd())
    return {
        "schema_version": SCHEMA_VERSION,
        "observed_at": utc_now(),
        "source": "fixture" if fixture else "host_observation",
        "physical_qualification": "blocked_no_target_confirmation",
        "thermal_c": thermal,
        "loadavg": load,
        "storage": storage,
        "network": network,
        "ssd": ssd,
    }


def load_bom(path):
    path = Path(path)
    value = read_json(path)
    if value.get("schema_version") != SCHEMA_VERSION or value.get("do_not_infer") is not True:
        raise ValueError("BOM reference must be schema v1 and do_not_infer=true")
    return value, digest_file(path)


def base_manifest(bom, bom_sha):
    return {
        "schema_version": SCHEMA_VERSION,
        "run_id": datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ"),
        "created_at": utc_now(),
        "python": platform.python_version(),
        "platform": platform.system(),
        "bom_sha256": bom_sha,
        "bom_status": bom.get("status", "unknown"),
        "physical_qualification": "blocked_until_target_unit_and_bom_are_attached",
        "blocked_reasons": [
            "target_unit_not_confirmed_by_harness",
            "external_bom_fields_required",
        ],
    }


def ensure_output(path):
    path = Path(path).resolve()
    path.mkdir(parents=True, exist_ok=True)
    path.chmod(0o700)
    return path


def append_sample(output, sample):
    path = output / SAMPLE_NAME
    with path.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(sample, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o600)


def run_once(output, bom_path, fixture_path):
    output = ensure_output(output)
    bom, bom_sha = load_bom(bom_path)
    manifest_path = output / MANIFEST_NAME
    if not manifest_path.exists():
        atomic_json(manifest_path, base_manifest(bom, bom_sha))
    sample = collect_sample(load_fixture(fixture_path))
    append_sample(output, sample)
    return sample


def run_soak(output, bom_path, fixture_path, duration, interval):
    started = time.monotonic()
    count = 0
    while count == 0 or time.monotonic() - started < duration:
        run_once(output, bom_path, fixture_path)
        count += 1
        if duration <= 0 or time.monotonic() - started >= duration:
            break
        time.sleep(interval)
    return {"samples": count, "physical_qualification": "blocked_no_target_confirmation"}


def power_cut(output, phase):
    output = ensure_output(output)
    if phase not in {"before_sample_write", "after_sample_write", "before_summary", "after_summary"}:
        raise ValueError("unsupported power-cut phase")
    journal = output / JOURNAL_NAME
    atomic_json(journal, {"schema_version": SCHEMA_VERSION, "phase": phase, "status": "interrupted", "recorded_at": utc_now()})
    interrupted = read_json(journal)
    interrupted["status"] = "recovered_without_claiming_physical_power_cycle"
    interrupted["recovered_at"] = utc_now()
    atomic_json(journal, interrupted)
    return interrupted


def build_report(output):
    output = Path(output).resolve()
    manifest = read_json(output / MANIFEST_NAME)
    samples_path = output / SAMPLE_NAME
    samples = []
    if samples_path.exists():
        for line in samples_path.read_text(encoding="utf-8").splitlines():
            if line:
                samples.append(json.loads(line))
    report = {
        "schema_version": SCHEMA_VERSION,
        "run_id": manifest["run_id"],
        "samples": len(samples),
        "physical_qualification": manifest["physical_qualification"],
        "blocked_reasons": manifest["blocked_reasons"],
        "source_counts": {"fixture": sum(item.get("source") == "fixture" for item in samples), "host_observation": sum(item.get("source") == "host_observation" for item in samples)},
        "checks": {"samples_parseable": True, "no_physical_result_invented": True},
    }
    atomic_json(output / SUMMARY_NAME, report)
    return report


def doctor(bom_path):
    bom, digest = load_bom(bom_path)
    tools = {name: bool(shutil.which(name)) for name in ("python3", "go")}
    return {
        "schema_version": SCHEMA_VERSION,
        "status": "ready_for_harness_blocked_for_physical_qualification",
        "bom_sha256": digest,
        "bom_status": bom.get("status"),
        "tools": tools,
        "physical_qualification": "blocked_no_target_confirmation",
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bom", default="docs/v1/V1_HARDWARE_BOM_REFERENCE.json")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("doctor")
    once = sub.add_parser("sample")
    once.add_argument("--output", required=True)
    once.add_argument("--fixture")
    soak = sub.add_parser("soak")
    soak.add_argument("--output", required=True)
    soak.add_argument("--fixture")
    soak.add_argument("--duration", type=float, default=0)
    soak.add_argument("--interval", type=float, default=5)
    cut = sub.add_parser("power-cut")
    cut.add_argument("--output", required=True)
    cut.add_argument("--phase", default="after_sample_write")
    report = sub.add_parser("report")
    report.add_argument("--output", required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == "doctor":
            value = doctor(args.bom)
        elif args.command == "sample":
            value = run_once(args.output, args.bom, args.fixture)
        elif args.command == "soak":
            value = run_soak(args.output, args.bom, args.fixture, args.duration, args.interval)
        elif args.command == "power-cut":
            value = power_cut(args.output, args.phase)
        else:
            value = build_report(args.output)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(json.dumps({"status": "error", "error": str(exc)}), file=sys.stderr)
        return 1
    print(json.dumps(value, sort_keys=True, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
