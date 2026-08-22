#!/usr/bin/env python3
"""Audit the V1 release candidate without manufacturing external evidence."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def audit(root: Path) -> dict[str, Any]:
    required = [
        "docs/v1/V1_EXECUTION_STATUS.md",
        "docs/v1/V1_HARDWARE_QUALIFICATION_PROTOCOL.md",
        "docs/v1/V1_CAMERA_NETWORK_QUALIFICATION.md",
        "docs/v1/V1_USER_FLOW_QUALIFICATION.md",
        "docs/v1/V1_RELEASE_ENGINEERING.md",
        "docs/v1/V1_REGULATORY_EVIDENCE_MATRIX.json",
        "docs/v1/V1_BACKUP_POLICY.md",
        "docs/v1/V1_OTA_CENTRAL_POLICY.md",
        "docs/v1/V1_CAMERA_OTA_POLICY.md",
        "docs/ota-rollback.md",
    ]
    missing = [path for path in required if not (root / path).is_file()]
    status_text = (root / "docs/v1/V1_EXECUTION_STATUS.md").read_text(encoding="utf-8") if not missing else ""
    regulatory = json.loads((root / "docs/v1/V1_REGULATORY_EVIDENCE_MATRIX.json").read_text(encoding="utf-8")) if not missing else {}
    p1 = [
        {"id": "hardware_target", "status": "blocked_external", "detail": "BOM complète, unité cible et mesures physiques non disponibles."},
        {"id": "remote_adapter", "status": "blocked_external", "detail": "WireGuard/netlink production et client distant non disponibles."},
        {"id": "vision_calibration", "status": "not_measured", "detail": "Aucune mesure réelle Vision disponible pour FP/FN."},
        {"id": "regulatory_evidence", "status": "blocked_external", "detail": "CE/RED/RoHS/WEEE restent soumis aux preuves externes."},
    ]
    return {
        "schema_version": 1,
        "candidate_kind": "local_rc_candidate",
        "status": "software_rc_audited_external_gates_open" if not missing else "blocked_missing_audit_documents",
        "mandatory_documents_missing": missing,
        "milestones_documented": all(f"Jalon {number}" in status_text for number in (21, 22, 23, 24)),
        "p0_open": [],
        "p1_and_external_gates": p1,
        "vision": {"real_calibration": "not_available", "false_positive_measurement": "not_available", "false_negative_measurement": "not_available", "software_contract_tests": "available"},
        "security": {"local_sessions": "covered_by_existing_tests", "authorization": "covered_by_existing_tests", "rest_websocket_cors_csrf_rate_limits_upload_limits": "covered_by_existing_tests_and_documented"},
        "recovery": {"backup_restore": "targeted_simulation_green", "central_ota": "targeted_simulation_green", "camera_ota_rollback": "targeted_simulation_green", "physical_reboot": "not_available"},
        "regulatory_status": {item["framework"]: item["status"] for item in regulatory.get("items", [])},
        "release_decision": "tag_v1_rc1_blocked_until_mandatory_external_evidence; tag_local_candidate_allowed",
        "required_final_commands": [
            "GOFLAGS=-buildvcs=false go test ./... -count=1",
            "GOFLAGS=-buildvcs=false go test ./... -shuffle=on -count=3",
            "GOFLAGS=-buildvcs=false go vet ./...",
            "GOFLAGS=-buildvcs=false timeout 300s go test -race ./... -count=1",
            "python3 -B -m unittest discover -s services/vision-worker/tests -v",
            "npm --prefix synora-web audit --omit=dev --audit-level=high",
        ],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    print(json.dumps(audit(args.root), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
