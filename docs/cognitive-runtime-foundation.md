# Cognitive runtime foundation

## Authority boundaries

`synora-core` remains the deterministic teacher and the only authority for
state transitions, topology interpretation, event classification, policy and
hardware actions. `synora-cognitive` is a separate bus service. Its outputs
are advisory observations only; they are never accepted as commands and never
enter the action dispatcher.

## Flow

```text
Core facts → StateFrame(v1) → StateEncoder → CognitiveInput(vector + catalog)
                                      → ModelBackend → DangerLogits/ActionLogits
                                      → Core teacher/policy gate (advisory)
```

When capture is enabled, the Core freezes the event, pre-processing state,
recent events, topology and residents at the input boundary. After deterministic
processing, the final teacher decision is appended to JSONL. Capture is
disabled by default and failures are best-effort: a full queue or file error
cannot block Core.

Activation requires `SYNORA_COGNITIVE_CAPTURE=true`; the append-only path is
configured with `SYNORA_COGNITIVE_DATASET_PATH` and otherwise defaults to
`/var/lib/synora/cognitive/dataset.jsonl`.

## Multi-adapter contract

Tasks request capabilities and modalities. `AdapterRegistry` exposes stable
descriptors with identifiers, versions, limits and enabled state. `Scheduler`
matches all requested capabilities/modalities and breaks ties by the smallest
descriptor footprint, then lexical adapter ID. The only active backend is
`MockBackend`; `vision_summary` and `planner` are disabled descriptors for
future work. `StateEncoder` produces a versioned `float32` vector with a frame
checksum. `ModelBackend` returns typed danger/action logits and catalog IDs,
never text or executable commands. A future backend implements `Backend`
without changing Core.

## Synora-Eval

`Synora-Eval` consumes JSONL fixtures and actual output JSONL, checks the
cognitive schema, classification, inferred state, requested capabilities and
the absence of executable action fields, then emits a JSON report.

## Explicitly out of scope

This foundation does not select a backbone, download models, run LLM/VLM
inference, train or fine-tune, add LoRA, add a State Encoder, paging, kernels,
or autonomous actions. Hardware control stays behind the existing Core and
action-service authority boundaries.

