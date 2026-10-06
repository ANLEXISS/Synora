# MLP trace V1

The Core emits `synora.mlp-trace/v1` inside the `core.decision` payload when
the promoted V1 CPU bundle performs an inference. The trace is observational:
the historical Core and its deterministic safety gate remain the only
decision and execution authorities.

The trace contains the loaded model version, the static head/layer topology,
bounded per-layer summaries, at most five active nodes per layer, at most 64
active paths, the proposed output label, and elapsed time. The inference ID is
the source event ID. The runtime never serializes weights, raw input values,
embeddings, identities, media, secrets, or hardware identifiers.

The authenticated API projects this message at:

- `GET /api/intelligence/topology`: latest redacted topology, or an empty
  topology when no model trace has been observed;
- `GET /api/intelligence/traces`: the last 24 traces;
- `GET /api/intelligence/events`: the last 64 bounded recent-event summaries;
- WebSocket `/api/ws`: an initial snapshot containing topology, traces, and
  events, followed by `intelligence.inference` messages containing one
  projected trace and one recent-event summary.

The recent-event projection is `synora.recent-event/v1`. It contains only the
event time, inference/model references, live/test status, and an optional
proposed-output label. It never copies topology, activations, paths, raw
inputs, weights, embeddings, media, identities, secrets, or hardware paths.
When no redacted trace has been observed, the endpoint and the interface
return an empty list and display `Aucun événement récent`.

The pilot state is a separate aggregate read model. V1 Core snapshots are now
targeted to the API as a read-only event, and V3 snapshots use the same
projection path. The API never treats an absent snapshot as a healthy or empty
state: it returns `503` with `status=unknown` until one is observed.

The API projection is independently allow-listed and capped. The frontend
keeps only the last 24 traces and presents proposed outputs as consultative
responses; it cannot dispatch hardware commands.
