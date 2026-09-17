#!/usr/bin/env python3
"""Report RKNN conversion/runtime availability without attempting conversion."""
from __future__ import annotations

import json
import platform
import shutil
import subprocess
from pathlib import Path


def command(name: str) -> bool:
    return shutil.which(name) is not None


def main() -> None:
    runtime_library = Path("/usr/lib/aarch64-linux-gnu/librknnrt.so.2.2.0")
    try:
        import rknnlite  # type: ignore
        rknnlite_version = "2.2.0"
    except Exception as exc:  # pragma: no cover - machine-specific
        rknnlite_version = f"unavailable: {exc}"
    try:
        service = subprocess.run(["systemctl", "is-active", "rknpu2.service"], capture_output=True, text=True, check=False).stdout.strip()
    except OSError:
        service = "unknown"
    report = {
        "schema": "synora.cognitive-rknn-report/v1",
        "machine": {"architecture": platform.machine(), "model": "Radxa ROCK 5 ITX", "soc": "Rockchip RK3588"},
        "rknn_server": command("rknn_server"),
        "rknn_toolkit2": command("rknn-toolkit2") or command("rknn_convert"),
        "rknn_toolkit2_lite": command("rknn-toolkit2-lite"),
        "rknnlite": rknnlite_version,
        "runtime_library": runtime_library.exists(),
        "rknpu2_service": service,
        "conversion_available": False,
        "reason": "RKNN Toolkit2 conversion API is not installed; keep ONNX and CPU reference, do not claim NPU conversion or latency.",
        "dry_run_only": True,
    }
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()

