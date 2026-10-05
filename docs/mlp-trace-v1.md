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
- WebSocket `/api/ws`: `intelligence.inference` messages containing one
  projected trace.

The API projection is independently allow-listed and capped. The frontend
keeps only the last 24 traces and presents proposed outputs as consultative
responses; it cannot dispatch hardware commands.
