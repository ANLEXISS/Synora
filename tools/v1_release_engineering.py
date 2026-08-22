#!/usr/bin/env python3
"""Generate honest, deterministic V1 release-engineering evidence."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

REQUIRED_PATHS = (
    "Makefile",
    "tools/install_plan.sh",
    "docs/boot-healthcheck.md",
    "docs/ota-rollback.md",
    "internal/security/support_bundle.go",
    "docs/v1/V1_HARDWARE_QUALIFICATION_PROTOCOL.md",
    "docs/v1/V1_CAMERA_NETWORK_QUALIFICATION.md",
)


def git(root: Path, *args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=root, text=True).strip()


def tracked_files(root: Path) -> list[str]:
    output = subprocess.check_output(["git", "ls-files", "-z"], cwd=root)
    return sorted(item.decode("utf-8") for item in output.split(b"\0") if item)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def release_manifest(root: Path) -> dict[str, Any]:
    files = [{"path": name, "sha256": sha256(root / name)} for name in tracked_files(root)]
    return {
        "schema_version": 1,
        "artifact_kind": "source_release_manifest",
        "source_revision": git(root, "rev-parse", "HEAD"),
        "reproducibility": "tracked_source_hashes_without_timestamp_or_host_data",
        "target_image": {"status": "not_built_in_repository", "reason": "image_builder_and_target_rootfs_are external"},
        "signing": {"status": "blocked_external_key_required", "private_key_present": False},
        "files": files,
    }


def go_modules(root: Path) -> list[dict[str, str]]:
    go_mod = (root / "go.mod").read_text(encoding="utf-8")
    entries = []
    in_require = False
    for line in go_mod.splitlines():
        stripped = line.strip()
        if stripped == "require (":
            in_require = True
            continue
        if in_require and stripped == ")":
            in_require = False
            continue
        if not in_require and not stripped.startswith("require "):
            continue
        value = stripped.removeprefix("require ").strip().split("//", 1)[0].split()
        if len(value) >= 2:
            entries.append({"name": value[0], "version": value[1], "license": "not_resolved_in_repository"})
    return sorted(entries, key=lambda item: (item["name"], item["version"]))


def node_modules(root: Path) -> list[dict[str, str]]:
    lock_path = root / "synora-web/package-lock.json"
    document = json.loads(lock_path.read_text(encoding="utf-8"))
    entries = []
    for path, package in document.get("packages", {}).items():
        if not path.startswith("node_modules/") or not isinstance(package, dict):
            continue
        name = path.removeprefix("node_modules/")
        entries.append({
            "name": name,
            "version": str(package.get("version", "unknown")),
            "license": str(package.get("license", "not_resolved_in_lockfile")),
        })
    return sorted(entries, key=lambda item: (item["name"], item["version"]))


def sbom(root: Path) -> dict[str, Any]:
    return {
        "bomFormat": "CycloneDX-compatible-inventory",
        "specVersion": "1.5",
        "metadata": {"component": {"name": "synora", "version": git(root, "rev-parse", "HEAD")}},
        "components": [{"scope": "runtime-go", **item} for item in go_modules(root)] + [{"scope": "web", **item} for item in node_modules(root)],
        "license_inventory_status": "node_licenses_from_lockfile_go_licenses_unresolved_require_external_resolution",
        "not_claimed": ["This inventory does not assert legal completeness for Go module licenses.", "No certification or third-party attestation is inferred."],
    }


def provisioning(root: Path) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "status": "ready_for_controlled_production_provisioning",
        "plan": "tools/install_plan.sh",
        "mutates_system": False,
        "secret_policy": {"source": "generated_or_enrolled_outside_git", "modes": {"api_token": "0600", "session_secret": "0640", "synoranet_psk": "0600", "admin_initial_password": "0600"}},
        "preserved_paths": ["/etc/synora", "/var/lib/synora", "/var/lib/synora/connectivity"],
        "preflight_sources_present": all((root / path).is_file() for path in ("tools/install_plan.sh", "cmd/synora-bootstrap-config/main.go", "cmd/synora-boot-healthcheck/main.go")),
        "external_steps": ["build target rootfs/image", "provision device identity and secrets", "install and verify on target", "record serial and operator sign-off"],
    }


def burn_in() -> dict[str, Any]:
    return {
        "schema_version": 1,
        "status": "protocol_ready_target_execution_required",
        "steps": [
            {"id": "hardware_harness", "command": "python3 -B tools/v1_hardware_qualification.py soak --output /run-local/synora-hw --duration 900 --interval 5", "evidence": "thermal/load/storage/network/SSD observations"},
            {"id": "camera_network", "command": "python3 -B tools/v1_camera_network_qualification.py --fixture docs/v1/V1_CAMERA_NETWORK_FIXTURE.json", "evidence": "replace fixture with target measurements before physical claim"},
            {"id": "boot_health", "command": "synora-boot-healthcheck run --readonly", "evidence": "health/version/identity report"},
            {"id": "power_cut", "command": "assisted power-cut protocol on target unit", "evidence": "operator-attested recovery journal; never simulated as physical result"},
        ],
        "pass_rule": "record every sample and stop on missing target identity, corruption, unsafe temperature, storage failure, or health failure",
    }


def support_diagnostics(root: Path) -> dict[str, Any]:
    source = (root / "internal/security/support_bundle.go").read_text(encoding="utf-8")
    markers = ("Redact", "redact", "0600", "secret")
    return {
        "schema_version": 1,
        "status": "redaction_implementation_present_tests_required",
        "source": "internal/security/support_bundle.go",
        "markers_present": {marker: marker in source for marker in markers},
        "policy": ["no raw token/cookie/key/biometric/path in support output", "diagnostics are bounded and read-only", "write support artifact with restrictive permissions"],
        "qualification": "go test ./internal/security -run Support -count=1",
    }


def check(root: Path) -> dict[str, Any]:
    missing = [path for path in REQUIRED_PATHS if not (root / path).is_file()]
    support = support_diagnostics(root)
    return {
        "schema_version": 1,
        "status": "release_engineering_ready_external_signing_and_target_validation_required" if not missing else "blocked_missing_release_inputs",
        "required_paths_missing": missing,
        "source_manifest": "available",
        "sbom": "inventory_generated_with_unresolved_go_license_fields",
        "provisioning": "read_only_plan_available",
        "burn_in": "protocol_available_target_execution_required",
        "support_diagnostics": "redaction_markers_present" if all(support["markers_present"].values()) else "review_required",
        "regulatory": {"CE": "external_evidence_required", "RED": "external_evidence_required", "RoHS": "external_evidence_required", "WEEE": "external_evidence_required"},
        "no_false_certification_claim": True,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("check", "manifest", "sbom", "provisioning", "burn-in", "support"))
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    values = {"check": check, "manifest": release_manifest, "sbom": sbom, "provisioning": provisioning, "burn-in": lambda root: burn_in(), "support": support_diagnostics}
    print(json.dumps(values[args.command](args.root), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
