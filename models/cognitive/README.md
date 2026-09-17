# Cognitive MLP runtime assets

`MANIFEST.runtime.json` is the versioned contract for the verified bundle. The
export command writes `danger.onnx`, `incident.onnx`, `task.onnx`,
`action.onnx` and the matching `*.cpu.json` files beside it in a deployment
directory; weights are intentionally supplied from the verified bundle rather
than silently downloaded by Synora.

The runtime requires `state-encoder/v4` (477 float32 features). Action outputs
are generic `token@scope` proposals only. Device Store availability and the
Action Ledger are applied outside the model. Physical execution is disabled;
run demonstrations with `SYNORA_COGNITIVE_DRY_RUN=1`.

