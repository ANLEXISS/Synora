# Pass 61: Core read-only context provider

This boundary supplies the CGE Shadow with a detached, redacted snapshot of
operational context owned by the Core. The default freshness policy is
`core-context-freshness-policy-v1:` with fresh data up to two minutes and stale
data after fifteen minutes. These thresholds are descriptive, not security
decisions.

The default provider is installed by `synora-core` when Shadow is enabled. It
copies a bounded view from `state.Store.ContextSnapshot`, resident topology
configuration, the read-only device registry, and the current topology. The
Store remains the owner of its maps and locks; no Store pointer or mutable
collection crosses the CGE boundary. A source revision is reported as zero
because the current Store has no global revision counter. The topology has its
own deterministic fingerprint and is sorted by node and canonical edge key.

The snapshot contains opaque SHA-256 fingerprints for residents, devices, and
cameras. It contains no names, raw resident or device IDs, IP/MAC addresses,
RTSP URLs, credentials, tokens, clips, or raw event identifiers. Context is
attached to the detached observation as a snapshot fingerprint and freshness
code, and the cognitive situation records a deterministic chain of those
fingerprints. Durable calibration records continue to apply their existing
redaction rules.

Residents are represented as present, absent, or unknown with bounded
confidence, location code, last-seen time, and freshness. Unknown or stale
presence is not converted into absence. Devices and cameras expose only
fingerprints, node codes, bounded kind/health values, availability booleans,
last-seen time, and freshness. Topology exposes canonical nodes, undirected
adjacency, the observation node, immediate neighbors, and a topology
fingerprint.

`ContextFreshnessPolicy` accepts only positive fresh/stale thresholds with
fresh no later than stale. Missing timestamps produce `unknown`; a snapshot
with no context facts is tracked as empty. The provider captures the bounded
source data before canonicalization and fingerprinting, and releases Core
locks before CGE processing.

If the provider is unavailable, cancelled, malformed, or panics, Shadow marks
the context boundary degraded and continues the historical and Shadow paths
with minimal context. No raw error is exposed in status. A stale or aging
snapshot remains usable as qualified evidence; it is not a negative fact.
The StaticProvider remains available for unit tests and explicit prototype
fallbacks.

Status and metrics are aggregate-only:

- `CoreContextProviderStatus` reports availability, degradation, request and
  success/failure counts, freshness counts, the last source revision,
  snapshot fingerprint, and a closed error code.
- Metrics include request, success, failure, fresh, aging, stale, empty, and
  snapshot-duration totals. They have no high-cardinality identity labels.

The Core remains the sole owner of operational state.
The CGE receives read-only contextual snapshots.
A context snapshot is evidence, not ground truth.
Stale or missing context must not be interpreted as a negative fact.
The provider cannot modify the StateStore.
Context does not grant decision, command, action, or authorization authority.
Historical production authority remains unchanged.

This pass does not add a context-driven decision, action, automation,
endpoint, or deployment configuration. The remaining limit is that this is
still a hermetic/host-side proof; the next step is controlled ARM64 build and
physical smoke with installed runtime services and a real camera event.
