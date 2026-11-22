# Getting Started with SYNORA

## Current onboarding status

The repository can be compiled and most component tests can be run on a generic Linux x86_64 host without IoT hardware. A complete default runtime demonstration is **not currently self-contained**: model bundles, RKNN models, and the MediaMTX executable are not committed, and the central harness fails when the missing cognitive bundles are required.

This guide targets commit `535431259eac0839cf5edf77beadc434df604c05` and uses only commands present in the repository or read-only audit commands derived from them.

## Prerequisites

### Required for repository-level validation

- Linux. Installation scripts explicitly support `apt-get` or `dnf`; system services assume systemd.
- Go 1.24 or later. [`go.mod`](../go.mod) declares `go 1.24.0`.
- Python 3. The repository does not declare a minimum version; the current audit used 3.13.7.
- GNU Make and Bash.
- Git for checkout/version metadata.

### Required for specific components

- Web client: Node.js and npm; versions are not constrained outside [`synora-web/package-lock.json`](../synora-web/package-lock.json). No web tests are defined.
- Vision worker: packages pinned in [`services/vision-worker/requirements.txt`](../services/vision-worker/requirements.txt): OpenCV headless 4.10.0.84, NumPy 1.26.4, SciPy 1.13.1, and RKNN Toolkit Lite 2.3.2 only on ARM64.
- Installed runtime: `curl`, `rsync`, `jq`, OpenSSL, systemd, and potentially hostapd/dnsmasq/network tools used by Discovery.
- Media: an externally provisioned MediaMTX executable; only its configuration and license are in `tools/mediamtx/`.
- RK3588 inference: externally provisioned `.rknn` files and compatible RKNN runtime/hardware.
- Full central harness: cognitive bundle directories expected at `build/cognitive-mlp-v1` and/or `build/cognitive-mlp-v3-candidate`.

Do not run installation or network targets on a workstation without reviewing them. `make install`, `make start`, `make update`, network configuration, and service units write system paths or control services.

## Clone and inspect

```bash
git clone https://github.com/ANLEXISS/Synora.git
cd Synora
git status --short --branch
go version
python3 --version
```

The repository's root [`README.md`](../README.md) contains only the project title; the Makefile is the authoritative command index.

## Safe developer validation

These commands do not require IoT hardware and can keep caches outside the checkout:

```bash
GOCACHE=/tmp/synora-gocache go build -buildvcs=false ./...

PYTHONPYCACHEPREFIX=/tmp/synora-pycache \
  python3 -m compileall -q services/vision-worker

PYTHONDONTWRITEBYTECODE=1 \
PYTHONPATH=services/vision-worker \
  python3 -B -m unittest discover \
  -s services/vision-worker/tests -p 'test_*.py'

GOCACHE=/tmp/synora-gocache go test ./...
```

At the audited commit, the final command is expected to return non-zero because seven tests in `cmd/synora-central-test` require the absent V3 candidate bundle. Do not interpret that failure as a successful qualification. See [`VALIDATION_REPORT.md`](VALIDATION_REPORT.md).

To isolate the currently passing Go component packages without changing code:

```bash
packages=$(go list ./... | grep -v '^synora/cmd/synora-central-test$')
GOCACHE=/tmp/synora-gocache go test $packages
```

This is an audit convenience, not a replacement for `go test ./...`.

## Repository commands

| Task | Command | Side effects / audit status |
| --- | --- | --- |
| Show supported targets | `make help` | Read-only. |
| Build installed Go binaries into `bin/` | `make build` | Writes `bin/` and `build/version.json`; not used in this audit because `go build ./...` provided a non-writing compile check. |
| Build the web client | `make web-build` | Requires dependencies in `synora-web/node_modules`; not run because they were absent and no installation was authorized/needed for the code audit. |
| Validate configuration templates | `go run ./cmd/synora-bootstrap-config validate --templates configs --template --dry-run` | Executed successfully in this audit. |
| Central hermetic harness | `make test-central-v1` | Writes a report under `/tmp` by default; currently fails because bundle artifacts are absent. |
| Full test target | `make test` | Runs central harness first, then Go and Python tests. It stops at the current central failure. |
| Inspect installed runtime | `make doctor` | Intended as diagnostic, but probes installed services/files and runs Go tests; not executed because no installed runtime was in scope. |
| Start/stop installed services | `make start`, `make stop` | Controls systemd and removes the bus socket on stop; not executed. |
| Remove local build artifacts | `make clean` | Deletes `bin/` and selected `/tmp` logs; review before use. |
| Destructive uninstall | `make delete CONFIRM=YES` | Deletes system installation/data after backup attempt; never use as a development cleanup command. |

## Python environment

The repository has a pinned requirements file but no venv creation target. A conventional isolated setup is:

```bash
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -r services/vision-worker/requirements.txt
```

This command was **not executed during the audit**. On x86_64, the RKNN package marker excludes `rknn-toolkit-lite2`; model-backed RKNN execution therefore remains unavailable. Installing Python packages proves dependency resolution only, not model or hardware qualification.

## Web client

```bash
cd synora-web
npm ci
npm run build
```

These commands originate from [`synora-web/package.json`](../synora-web/package.json). They were **not executed**: `node_modules` was absent, and this audit did not install additional components. The repository defines build and lint scripts but no web test script.

## Configuration preparation

The bootstrap command is dry-run unless `--apply` is supplied:

```bash
go run ./cmd/synora-bootstrap-config plan \
  --templates configs --etc /tmp/synora-config --dry-run

go run ./cmd/synora-bootstrap-config validate \
  --templates configs --template --dry-run
```

For an installed system, review the plan before using `init --apply`. Runtime secrets must be generated outside Git. Never copy real API tokens, bus secrets, passwords, private keys, device secrets, or certificates into documentation or commits.

## Minimal hardware-free scenario

The smallest reproducible, successful scenario at the audited commit is component validation rather than a running service stack:

```bash
GOCACHE=/tmp/synora-gocache go build -buildvcs=false ./...

packages=$(go list ./... | grep -v '^synora/cmd/synora-central-test$')
GOCACHE=/tmp/synora-gocache go test $packages

PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=services/vision-worker \
  python3 -B -m unittest discover \
  -s services/vision-worker/tests -p 'test_*.py'
```

Observed on 2026-11-22: Go compilation passed; 623 Go test/subtest pass events, 0 failures, and 5 skips were recorded outside the central-test package; Python reported 112 tests, all passing. These are software tests with fakes, temporary files, and synthetic aggregates. They do not validate cameras, RKNN inference, MediaMTX, systemd deployment, or physical actions.

## Central harness demonstration

The repository advertises this reproducible dry-run command:

```bash
GOCACHE=/tmp/synora-gocache \
CENTRAL_E2E_OUT=/tmp/synora-central-e2e-v1.json \
  make test-central-v1
```

Current result: non-zero. The default report records 539 total cases, 31 passed and 508 failed; direct test assertions identify the absent V3 candidate manifest as the immediate blocker. A separate V1-only invocation fails because `build/cognitive-mlp-v1/MANIFEST.v1.json` is absent. Provision only reviewed, version-compatible bundles; this guide does not provide or fabricate them.

## Installed runtime

The Makefile's intended host workflow is:

```bash
make install
make start
make doctor
make stop
```

It was **not executed**. It can install packages, create a system user, write `/opt/synora`, `/etc/synora`, `/var/lib/synora`, and `/etc/systemd/system`, change network services, and start privileged processes.

Before attempting it, resolve these repository-level blockers:

1. Provision a MediaMTX executable under `tools/mediamtx/`; it is absent from this checkout.
2. Provision required models outside Git and verify hashes/licensing.
3. Decide how `synora-api` is deployed. It is built and installed but has no unit, and its default ports conflict with Discovery.
4. Validate the host-specific wireless interface, firewall policy, TLS files, and service identities.

## Environment variables

The main runtime resolver is [`internal/runtimeconfig/runtimeconfig.go`](../internal/runtimeconfig/runtimeconfig.go). Values below are examples, never production secrets.

### Shared paths and endpoints

| Variable | Purpose / safe example |
| --- | --- |
| `SYNORA_CONFIG_DIR` | Base config directory, e.g. `/tmp/synora-config`. |
| `SYNORA_SECURITY`, `SYNORA_AUTH`, `SYNORA_TOPOLOGY`, `SYNORA_RESIDENTS`, `SYNORA_DEVICE`, `SYNORA_NETWORK_CONFIG` | Override individual configuration files. |
| `SYNORA_BUS` | Unix bus path, e.g. `/tmp/synora-run/bus.sock`. |
| `SYNORA_STATE_PATH`, `SYNORA_STORE_DIR` | General state path and Core Universal Store directory. |
| `SYNORA_CLIP_ROOT` / `SYNORA_CLIP_DIR` | Local clip/cache root. |
| `SYNORA_FACE_DATA_ROOT`, `SYNORA_MODEL_ROOT` | Face-data and model roots. |
| `SYNORA_CONNECTIVITY_DIR`, `SYNORA_IDENTITY_REGISTRY` | Connectivity and device-identity persistence. |
| `SYNORA_VERSION_FILE` | Generated version manifest path. |
| `SYNORA_HTTP_ADDR`, `SYNORA_HTTPS_ADDR` | API listen addresses, e.g. `127.0.0.1:18080`. |
| `SYNORA_VISION_HEALTH_ADDR`, `SYNORA_VISION_HTTPS_ADDR` | Discovery health and vision ingress listeners. |
| `SYNORA_VISION_WORKER_SOCKET` / `SYNORA_VISION_SOCKET` | Python worker Unix socket. |
| `SYNORA_MEDIAMTX_CONFIG`, `SYNORA_MEDIAMTX_RTSP_URL`, `SYNORA_MEDIAMTX_API_URL` | MediaMTX config and endpoints. |
| `SYNORA_TLS_CERT_FILE`, `SYNORA_TLS_KEY_FILE` | TLS certificate/key paths; never commit the key. |
| `SYNORA_WEB_ROOT` | Static `synora-api` web root. |

### Timeouts, limits, and diagnostics

`SYNORA_BUS_CONNECT_TIMEOUT`, `SYNORA_BUS_RPC_TIMEOUT`, `SYNORA_HTTP_READ_TIMEOUT`, `SYNORA_HTTP_WRITE_TIMEOUT`, `SYNORA_HTTP_IDLE_TIMEOUT`, `SYNORA_HTTP_HEADER_TIMEOUT`, `SYNORA_SHUTDOWN_TIMEOUT`, `SYNORA_VISION_TIMEOUT`, `SYNORA_VISION_COMMAND_TIMEOUT`, `SYNORA_CLIP_MAX_COUNT`, `SYNORA_CLIP_MAX_BYTES`, `SYNORA_CLIP_PART_MAX_AGE`, `SYNORA_BUS_DEBUG`, `SYNORA_VISION_DEBUG`, and `SYNORA_LOG_PATH` tune bounded runtime behavior and diagnostics. Duration values use Go duration syntax where parsed by Go, such as `2s` or `1m`.

### Authentication and security

| Variable | Purpose |
| --- | --- |
| `SYNORA_API_TOKEN` | Bearer token for diagnostics/health clients. Use a placeholder only in examples. |
| `SYNORA_BUS_KEY_ID` | Identifier for the active bus HMAC key. |
| `SYNORA_BUS_SECRET_FILE` | Preferred file containing the bus secret. |
| `SYNORA_BUS_SECRET` | Environment fallback for the bus secret; avoid in production process environments. |
| `SYNORA_BUS_SOCKET_MODE` | Socket-permission override used by bus setup. |
| `SYNORA_ALLOW_INSECURE_INGRESS` | Explicit test/development override for non-TLS ingress. Do not enable on an untrusted network. |
| `SYNORA_HTTPS_ENABLED` | Overrides API HTTPS enablement. |

### Models, vision, and qualification

Model selectors include `SYNORA_COGNITIVE_BUNDLE`, `SYNORA_ARCFACE_MODEL`, `SYNORA_SCRFD_MODEL`, `SYNORA_YOLO_MODEL_PATH`, `SYNORA_POSE_MODEL`, `SYNORA_POSE_RKNN_MODEL`, `SYNORA_RISK_MODEL`, `SYNORA_RTMPOSE_MODEL_PATH`, `SYNORA_RTMPOSE_CHECKPOINT`, and module-specific `SYNORA_VISION_*_MODEL` variables. External-suite inputs include `SYNORA_VISION_MEDIA_ROOT`, `SYNORA_RTMPOSE_TEST_IMAGE`, `SYNORA_MMPOSE_ROOT`, `SYNORA_MMDEPLOY_ROOT`, and `SYNORA_PYTORCH_RUNTIME`.

Feature/mode variables include `SYNORA_EDGE_VISION`, `SYNORA_TEST_VISION_WORKER`, `SYNORA_VISION_CLIP_V1_ENABLED`, `SYNORA_VISION_CLIP_V1_DETECTOR_MODE`, `SYNORA_VISION_DETECTOR_STRATEGY`, `SYNORA_VISION_FACE_ENABLED`, `SYNORA_VISION_FACE_GALLERY`, `SYNORA_VISION_PLATE_ENABLED`, and `SYNORA_VISION_SENSITIVE_OBJECTS_ENABLED`. The `SYNORA_VISION_V1_*` family controls sampling, tracking, duration, thresholds, crop limits, and enrichment cadence. These knobs do not themselves prove a backend is present or qualified.

### OEM, boot, release, and network metadata

`SYNORA_OEM_PROFILE`, `SYNORA_OEM_PACKAGE_ROOT`, `SYNORA_MODELS_MANIFEST`, `SYNORA_MODELS_ROOT`, `SYNORA_BOOT_BASE_URL`, `SYNORA_EXPECTED_GIT_COMMIT`, `SYNORA_GIT_COMMIT`, `SYNORA_BUILD_TIME`, `SYNORA_BUNDLE_ID`, `SYNORA_IMAGE_VERSION`, `SYNORA_VERSION`, `SYNORA_OS_BASE`, `SYNORA_TARGET_BOARD`, `SYNORA_KERNEL_EXPECTED`, `SYNORA_RKNN_RUNTIME_EXPECTED`, `SYNORA_NETWORK_STATUS_FILE`, and `SYNORA_NETWORK_PAIRING_STATE_FILE` support preflight, reports, or deployed metadata. `SYNORA_BACKUP_ROOT` and `SYNORA_STATE_DIR` redirect specific administrative/test storage paths.

For every variable's exact parser and default, search the current source rather than relying on this summary:

```bash
rg -n 'SYNORA_[A-Z0-9_]+' cmd internal services Makefile deployments
```

## Troubleshooting

### Central harness reports missing `MANIFEST.v1.json` or `MANIFEST.v3.json`

This was reproduced in the current audit. The checkout has no `build/` directory. Provision the expected reviewed bundle or run only component tests; do not mark the central suite passed.

### `go test ./...` fails only in `cmd/synora-central-test`

This was reproduced: seven tests failed because the V3 bundle manifest was absent. The remaining Go packages passed when executed separately. See [`VALIDATION_REPORT.md`](VALIDATION_REPORT.md) for exact counts.

### Vision models are unavailable

The repository has no `models/` directory. The worker deliberately reports `missing_file`/unavailable rather than fabricating detections. Provisioning a file is not qualification.

### MediaMTX unit cannot start

`tools/mediamtx/mediamtx` is absent in this checkout, while the unit expects `/opt/synora/mediamtx/mediamtx`. Supply a reviewed binary and license-compliant deployment artifact before starting the unit.

### API listener address is already in use

Discovery and `synora-api` both default to `:8080`/`:8443`. The default service set starts Discovery but not `synora-api`. Assign explicit non-conflicting addresses only after deciding the intended API topology.

### Python emits `datetime.utcnow()` deprecation warnings

The Python suite passed but emitted warnings from `services/vision-worker/core/observability.py` under Python 3.13. This is compatibility debt, not a test failure.

## Verified Environment

Validation date: **2026-11-22 UTC**.

| Item | Observed value |
| --- | --- |
| OS | Bazzite 42, Linux 6.16.4, x86_64 |
| Go | 1.25.10 |
| Python | 3.13.7 |
| Node.js / npm | 20.20.1 / 10.8.2 (web dependencies absent) |
| Git | 2.51.0 |
| Commit | `535431259eac0839cf5edf77beadc434df604c05` |

Executed successfully: `go build -buildvcs=false ./...`; Python `compileall`; 112 Python unit tests; configuration template validation; and Go packages excluding `cmd/synora-central-test`. Executed with failures: `go test ./...`, default central harness, and V1 central harness. No system service, camera, NPU, MediaMTX process, network reconfiguration, or physical action was started.
