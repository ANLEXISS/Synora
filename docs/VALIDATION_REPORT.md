# SYNORA Validation Report

## Executive result

Validation was performed on 2026-11-22 UTC against commit `535431259eac0839cf5edf77beadc434df604c05` (`master`). The codebase compiles, the Python vision suite passes, and all Go packages outside the central harness pass. The repository-wide Go suite and official central harness do **not** pass at this commit in a fresh checkout because required cognitive model bundles are absent from `build/`.

This report does not claim production readiness, security certification, hardware qualification, or physical-action capability.

## Status vocabulary

- **IMPLEMENTED**: code path exists.
- **TESTED**: reproduced successfully in this audit.
- **PARTIALLY TESTED**: only component/synthetic layers passed.
- **EXPERIMENTAL**: dry-run, replay, candidate, or test-only path.
- **PLANNED**: declared without a complete active implementation.
- **UNKNOWN**: insufficient evidence.

## Validation environment

| Attribute | Value |
| --- | --- |
| Date | 2026-11-22 UTC |
| OS | Bazzite 42, Linux 6.16.4, x86_64 |
| Go | 1.25.10 |
| Python | 3.13.7 |
| Node.js / npm | 20.20.1 / 10.8.2 |
| Git | 2.51.0 |
| Repository state before documentation | Clean |
| Hardware exercised | None |
| Services started | None outside hermetic test processes |

Go build cache and Python bytecode cache were redirected to `/tmp`. Tests used temporary directories and ephemeral loopback/Unix sockets. No systemd, wireless, firewall, camera, NPU, or actuator operation was authorized or executed.

## Current executed results

Counts from `go test -json` are test/subtest action counts; package counts are reported separately. This avoids pretending that source-level function counts and runtime subtests are identical.

| Suite | Command | UTC start | Duration | Passed | Failed | Skipped | Exit |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| Go repository suite | `GOCACHE=/tmp/synora-audit-gocache go test -json ./...` | 21:10:55 | 19 s | 655 test/subtest events; 38 packages | 7 test events; 1 package | 5 test events; 12 no-test packages | 1 |
| Go packages excluding central harness | derived `go list` plus `go test -json`, excluding `synora/cmd/synora-central-test` | 21:13:19 | 1 s | 623 test/subtest events; 38 packages | 0 | 5 test events; 12 no-test packages | 0 |
| Python vision unit suite | `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=services/vision-worker python3 -B -m unittest discover -s services/vision-worker/tests -p 'test_*.py' -v` | 21:12:03 | 2 s wall; unittest 0.347 s | 112 | 0 | 0 | 0 |
| Central E2E, default selection | `CENTRAL_E2E_OUT=/tmp/synora-audit-central-e2e.json make test-central-v1` | 21:11:36 | 5 s wall; report 3204.213 ms | 31 | 508 | 0 | 2 (Make) |
| Central E2E, V1 selection | `CENTRAL_E2E_OUT=/tmp/synora-audit-central-v1.json make test-central-v1 BUNDLE=v1` | 21:13:38 | 1 s wall; report 579.722 ms | 30 | 123 | 0 | 2 (Make) |
| Go compile | `GOCACHE=/tmp/synora-audit-gocache go build -buildvcs=false ./...` | 21:12:18 | 1 s | build passed | 0 | n/a | 0 |
| Python syntax compile | `PYTHONPYCACHEPREFIX=/tmp/synora-audit-pycache python3 -m compileall -q services/vision-worker` | 21:12:19 | 1 s | passed | 0 | n/a | 0 |
| Configuration templates | `go run ./cmd/synora-bootstrap-config validate --templates configs --template --dry-run` | 21:12:20 | <1 s | passed | 0 | n/a | 0 |

The `make test` aggregate was not rerun after its individual stages were measured: it starts with `test-central-v1`, which is known to fail and stops the Make target before the later stages.

## Failure analysis

All seven current Go test failures are in `cmd/synora-central-test`:

1. `TestDuplicateCameraReplayAccountingRequiresSuccessfulWrite`
2. `TestTripletRepresentativeCasesTraverseTheCentralStoreSafely`
3. `TestCameraJ2FixturesTraverseCentralPipelineAsSimulatedEvidence`
4. `TestLe2iAggregateEvidenceTraversesCore`
5. `TestCameraMockE2EIsTransportOnlyAndPreservesProvenance`
6. `TestOEMModelPackageScenariosHaveCompleteCentralJourneys`
7. `TestMLPObservationUsesNamedProbabilitiesAndNoRawVision`

The direct assertions report that `build/cognitive-mlp-v3-candidate/MANIFEST.v3.json` is missing. The default central report consequently records `triplet_frames_built=0`, `triplet_models_executed=false`, `triplet_decision_path_used=false`, and a pipeline accounting error. In the separate V1-specific run, 123 cases report the corresponding missing `build/cognitive-mlp-v1/MANIFEST.v1.json`.

The apparent scale of 508 default central failures is cascading: case families are marked failed because their required central journey cannot run. It must not be summarized as 508 independent implementation defects, but it also must not be called a pass.

## Test inventory

### Go

- 116 `*_test.go` files and 504 top-level `Test`/`Example` functions were found.
- Coverage includes Bus ACL/authentication/reconnect/concurrency; Core decisions, Store durability, compaction and idempotence; Discovery APIs, ingress, network configuration, worker pools, and resilience; security/session handling; OEM profile/model preflight; runtime manager; state/retention; contracts; and command entry points.
- Current result: **PARTIALLY TESTED** because the non-central packages pass while the central package does not.

### Python vision

- 18 `test_*.py` files and 112 test methods/functions were found.
- Coverage includes clip processing, bounded tracking, detector failure modes, edge manifests, pose/risk aggregates, event redaction, face dataset swap/reload, worker protocol, and Evidence V1 validation.
- Current result: **TESTED in a synthetic/component environment**, 112/112 passing.
- Limits: tests use fakes/mocks and do not load a real RKNN model on NPU hardware.

### Central integration harness

- Entry point: [`cmd/synora-central-test/`](../cmd/synora-central-test) through `make test-central-v1`.
- It creates temporary Unix bus clients and in-process Discovery/Core/API test boundaries, then emits a JSON report.
- The current manifest exposes 539 default aggregate cases when V3 families are included.
- Current result: **FAILED / NOT QUALIFIED** because required bundle artifacts are missing.

### API and communication tests

Go tests exercise HTTP handlers, WebSocket/session rules, Unix bus routing, RPC behavior, ingress authentication, and worker socket fragmentation/retry behavior. These passed outside the central package. No external network host or deployed TLS endpoint was exercised.

### Vision hardware/media tests

The repository contains camera qualification commands, external-suite manifests, MediaMTX adapters, and optional Le2i/RTMPose paths. They were not run because this checkout has no approved media root, RKNN model, compatible NPU runtime, live camera, or installed MediaMTX binary. Status: **NOT TESTED in the current audit**.

### Deployment and web tests

- Go compilation passed.
- Python compilation passed.
- Template validation passed.
- systemd installation/start/stop/doctor were not executed because they affect the host installation and network/services.
- The web package has no automated test script. Its build was not run because `node_modules` was absent and no dependency installation was needed for this audit.
- Status: **PARTIALLY TESTED**.

## Component maturity matrix

| Component | Implementation | Test status | Evidence | Known limitations |
| --- | --- | --- | --- | --- |
| Unix event bus | **IMPLEMENTED** | **TESTED** at component level | [`internal/bus/`](../internal/bus) | Non-durable transport; bounded incoming queue may drop; deployed identities not audited. |
| Core decision service | **IMPLEMENTED** | **PARTIALLY TESTED** | [`internal/cognitivecore/`](../internal/cognitivecore), passing component tests | Model-backed central path blocked by absent bundles; dry-run only. |
| Universal Store | **IMPLEMENTED** | **TESTED** at component level | [`internal/cognitivecore/store.go`](../internal/cognitivecore/store.go) and persistence tests | Single-node local store; no replicated availability test. |
| Discovery service | **IMPLEMENTED** | **PARTIALLY TESTED** | [`internal/discovery/`](../internal/discovery) | No deployed hardware/network validation; broad privileged responsibility. |
| Device vision ingress | **IMPLEMENTED** | **TESTED** with HTTP test servers/fakes | [`internal/discovery/ingress/`](../internal/discovery/ingress) | No real camera/TLS interoperability result. |
| Python vision worker | **IMPLEMENTED** | **TESTED** synthetically, 112/112 | [`services/vision-worker/`](../services/vision-worker) | Real RKNN inference and semantic accuracy unqualified. |
| Cognitive V1 bundle | Loader **IMPLEMENTED** | **NOT REPRODUCIBLE** | [`internal/cognitivecore/test_inference_test.go`](../internal/cognitivecore/test_inference_test.go) | Bundle absent from fresh checkout. |
| Cognitive V3 candidate | **EXPERIMENTAL** active-dry-run path | **NOT REPRODUCIBLE** | [`internal/cognitivecore/core_v3.go`](../internal/cognitivecore/core_v3.go) | Candidate manifest/bundle absent; no promotion evidence. |
| Safety Gate/actions | **IMPLEMENTED dry-run** | **TESTED** at component level | [`internal/cognitivecore/decision.go`](../internal/cognitivecore/decision.go), [`internal/discovery/manager.go`](../internal/discovery/manager.go) | No physical executor or actuator feedback qualification. |
| Runtime Manager | **IMPLEMENTED** | **TESTED** with injected probes | [`internal/manager/`](../internal/manager) | Installed systemd/MediaMTX runtime not exercised. |
| `synora-api` and WebSocket | **IMPLEMENTED** | **TESTED** at handler level | [`cmd/synora-api/`](../cmd/synora-api) | Missing service unit/start integration; port ownership conflicts with Discovery defaults. |
| React web client | **IMPLEMENTED source** | **NOT TESTED currently** | [`synora-web/`](../synora-web) | No test script; current build not reproduced. |
| SynoraNet host network | **IMPLEMENTED host controls** | **PARTIALLY TESTED** with config/fakes | [`internal/discovery/network/`](../internal/discovery/network) | Hardware interface, firewall, hostapd/dnsmasq behavior not validated. |
| MediaMTX integration | **IMPLEMENTED adapter/config** | **PARTIALLY TESTED** with HTTP fakes | [`internal/mediamtx/`](../internal/mediamtx), [`tools/mediamtx/mediamtx.yml`](../tools/mediamtx/mediamtx.yml) | Executable absent; live RTSP/HLS/WebRTC untested. |
| Connectivity agent | **EXPERIMENTAL foundation** | **TESTED** locally | [`internal/connectivity/`](../internal/connectivity) | Explicitly no external control plane, tunnel, NAT traversal, or route changes. |
| MQTT | **PLANNED/UNUSED** | **NOT TESTED** | module/config only | No source integration or service deployment. |
| Camera/NPU/face recognition | Adapters and contracts **IMPLEMENTED** | **NOT QUALIFIED** | worker and OEM preflight code | Models/hardware/consented datasets and independent metrics absent. |

## Historical evidence versus current reproduction

Historical Markdown is useful context but is not current proof:

| Historical claim | Source | Current disposition |
| --- | --- | --- |
| `go test ./...` green; Python 89 tests green; web build green; central 402/402 | [`docs/j1-data-erasure-v1.md`](j1-data-erasure-v1.md) | **Not reproduced.** Current Python suite has grown to 112 and passes; current Go/central suites fail; web build not run. |
| Central harness 401/401 after a timing fix; Go/Python green | [`SYNORA_V1_PROGRESS_AUDIT.md`](../SYNORA_V1_PROGRESS_AUDIT.md) | **Historical only.** No matching current bundle artifacts are present in the checkout. |
| Initial audit: central 394/401 and Le2i 48/48 with semantic qualification explicitly absent | [`SYNORA_V1_PROGRESS_AUDIT.md`](../SYNORA_V1_PROGRESS_AUDIT.md) | **Not reproduced.** External media/model paths were unavailable; historical report already disclaims semantic qualification. |
| 401-scenario harness composition | [`docs/rtmpose-rk3588-audit.md`](rtmpose-rk3588-audit.md) | **Superseded composition.** Current default report enumerates 539 cases. |

No timestamped machine-readable historical artifact was found in the checkout that could requalify the current commit. The current executed results take precedence.

## Risk assessment

### High

- **Non-self-contained qualification:** central success depends on unversioned/unbundled artifacts; a fresh clone cannot reproduce advertised historic passes.
- **Deployment topology ambiguity:** `synora-api` is installed but not supervised, and shares default ports with Discovery.
- **Privileged service surface:** Discovery and Runtime Manager run as root; real host-network/firewall behavior has not been audited here.
- **Media boundary exposure:** MediaMTX sample configuration allows unauthenticated RTSP publish/read under an isolation assumption.

### Medium

- **Availability:** custom bus queues can drop on overflow, and there is no broker-level durable delivery or multi-node failover evidence.
- **Protocol compatibility:** camera, RTSP, HLS, WebRTC, V4L2, and RKNN paths lack current device-level interoperability evidence.
- **Resource consumption:** bounded queues/limits exist, but no current sustained CPU, memory, disk-retention, or thermal benchmark was reproduced.
- **Model behavior:** synthetic contract tests do not measure false positives/negatives, calibration, latency on target, bias, or semantic accuracy.

### Security posture

The repository contains meaningful controls: bearer/session authentication, bcrypt, CSRF/origin checks, bus HMAC support, peer credential checks, ACLs, redaction, size limits, and fail-closed validation. However, no independent security audit, penetration test, dependency vulnerability report, or production configuration review was executed. The correct status is **PARTIALLY TESTED controls; security UNKNOWN at system level**.

## Current Technical Maturity

- **Functional prototype:** yes, at source/component-test level.
- **Validated components:** Go compilation, configuration validation, non-central Go packages, Python vision contracts/worker behavior, local Store and bus mechanisms.
- **Partial integrations:** Discovery/Core/bus/worker central flow, API/web deployment, MediaMTX, host networking, and OEM profiles.
- **Experimental features:** V3 cognitive candidate, test worker/replay paths, dry-run actions, connectivity foundation, external vision suites.
- **Not implemented or not active:** cloud control plane, real WireGuard transport, MQTT application integration, confirmed ONVIF integration, and physical actuator execution.
- **Not currently reproducible:** complete central qualification from a fresh checkout.
- **Not qualified:** real camera installation, NPU inference, fall/face/identity semantics, physical actions, voice interaction, and production security/availability.

## Recommended Next Validation Steps

1. **Make central qualification self-contained and green.** Define a licensed, hashed artifact acquisition/build procedure for V1 and V3 bundles, pin their compatibility metadata, then run `go test ./...` and `make test-central-v1` from a clean clone. This is the highest-value recruiter/demo improvement because it converts historical claims into reproducible current proof.
2. **Resolve the API deployment contract.** Select the owner of `:8080/:8443`, add or remove `synora-api` from the documented service topology accordingly, provide a unit if it remains, and add a clean-install smoke test covering both REST and WebSocket surfaces.
3. **Create one bounded hardware demonstration.** On a controlled target, validate one camera through authenticated ingress/MediaMTX to aggregate Evidence V1, Core Store commit, API projection, and dry-run action result. Record hashes, versions, resource use, recovery behavior, and explicit non-qualification limits.
4. Run web `npm ci`, build, lint, and add frontend tests in a clean CI environment with pinned Node tooling.
5. Add CI that publishes machine-readable test artifacts for Go, Python, central harness, build, and config validation on every commit.
6. Perform privilege and threat-boundary review of root services, MediaMTX authentication/isolation, bus secret provisioning, TLS lifecycle, and sensitive-data retention.
7. Add sustained resource/soak tests for bus overflow, worker restart, disk retention, Store recovery, and service restart ordering.

## Evidence index

- Commands and orchestration: [`Makefile`](../Makefile)
- Current test sources: [`cmd/synora-central-test/`](../cmd/synora-central-test), [`services/vision-worker/tests/`](../services/vision-worker/tests), and `*_test.go` files under `cmd/`, `internal/`, and `pkg/`
- Architecture and deployment: [`deployments/systemd/`](../deployments/systemd), [`internal/runtimeconfig/runtimeconfig.go`](../internal/runtimeconfig/runtimeconfig.go)
- Existing historical reports: [`SYNORA_V1_PROGRESS_AUDIT.md`](../SYNORA_V1_PROGRESS_AUDIT.md), [`docs/central-e2e-harness-v1.md`](central-e2e-harness-v1.md), and [`docs/j1-data-erasure-v1.md`](j1-data-erasure-v1.md)
