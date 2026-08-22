#!/usr/bin/env python3
"""Check the V1 user-flow coverage manifest without claiming browser evidence."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as stream:
        value = json.load(stream)
    if not isinstance(value, dict):
        raise ValueError("manifest must be a JSON object")
    return value


def qualify(root: Path, manifest: dict[str, Any]) -> dict[str, Any]:
    if manifest.get("schema_version") != 1:
        raise ValueError("unsupported manifest schema")
    if manifest.get("source") != "repository_static_qualification_not_browser_evidence":
        raise ValueError("manifest source must remain explicitly non-browser")
    flows = manifest.get("flows")
    if not isinstance(flows, list) or not flows:
        raise ValueError("flows must be a non-empty list")
    results = []
    for flow in flows:
        if not isinstance(flow, dict) or not isinstance(flow.get("id"), str):
            raise ValueError("flow id is required")
        artifacts = flow.get("artifacts")
        if not isinstance(artifacts, list) or not artifacts:
            raise ValueError(f"{flow['id']}: artifacts are required")
        artifact_results = []
        for artifact in artifacts:
            if not isinstance(artifact, dict) or not isinstance(artifact.get("path"), str):
                raise ValueError(f"{flow['id']}: invalid artifact")
            path = root / artifact["path"]
            if not path.is_file():
                raise ValueError(f"{flow['id']}: missing artifact {artifact['path']}")
            content = path.read_text(encoding="utf-8")
            markers = artifact.get("markers")
            if not isinstance(markers, list) or any(marker not in content for marker in markers):
                missing = [marker for marker in markers if marker not in content]
                raise ValueError(f"{flow['id']}: missing markers in {artifact['path']}: {missing}")
            artifact_results.append({"path": artifact["path"], "markers": len(markers), "status": "covered"})
        results.append({"id": flow["id"], "status": flow.get("status", "unknown"), "artifacts": artifact_results, "reason": flow.get("reason")})
    remote = next((flow for flow in results if flow["id"] == "remote_access"), None)
    if remote is None or remote["status"] != "blocked_external_adapter":
        raise ValueError("remote access must remain an explicit external-adapter decision")
    return {
        "schema_version": 1,
        "source": manifest["source"],
        "status": "local_user_flow_qualified_remote_access_blocked",
        "browser_evidence": "not_run",
        "flows": results,
        "limitations": [
            "This report checks source artifacts and does not simulate a browser.",
            "Remote access remains blocked until the real WireGuard/netlink adapter is available.",
            "A physical central, camera and remote client are required for final end-to-end evidence.",
        ],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--manifest", type=Path, default=Path("docs/v1/V1_USER_FLOW_QUALIFICATION.json"))
    args = parser.parse_args()
    report = qualify(args.root, load(args.root / args.manifest))
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
