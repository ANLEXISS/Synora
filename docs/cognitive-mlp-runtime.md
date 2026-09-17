# Cognitive MLP runtime v1

## Authority boundary

Core Go, EventStore, topology, temporal state, Device Store, engine,
automations and Action Ledger are the sources of truth. The MLP backend only
reads a versioned `StateFrame`, produces logits/probabilities and returns
advisory proposals. It has no command payload, device route or execution API.

The safety boundary is fail-closed: `SYNORA_COGNITIVE_DRY_RUN=1` is required
by the demonstration command, and the cognitive service publishes
`dry_run_only=true`. Omitting the variable does not enable physical execution.

## Runtime flow

```text
sensor event
  -> Core / EventStore / topology / temporal state
  -> StateFrame + state-encoder/v4 (477 float32 features)
  -> Danger head (softmax, 5 labels)
  -> Incident head (sigmoid tags + softmax phase)
  -> Task head (thresholded objectives)
  -> Action head + Device Store + Action Ledger
  -> advisory proposal + deterministic filtered reasons
  -> Safety Gate / dry-run trace
```

The State Encoder is deterministic and versioned as
`synora-state-encoder/4.0.0`. The action vector is exactly
`477 + 5 danger probabilities + 16 availability slots + 33 ledger features`.
Thresholds are copied from the verified bundle manifest and applied outside
the neural heads.

## Export and parity

From the isolated worktree, with the verified bundle mounted:

```bash
mkdir -p build/cognitive-mlp-v1 testdata/cognitive
PYTHONPATH="$SYNORA_PYTORCH_RUNTIME:/tmp/synora-ort-runtime" \
LD_LIBRARY_PATH="$SYNORA_PYTORCH_LIBS" \
python3 tools/cognitive/export_models.py \
  --bundle /home/rock/synora-cognitive-mlp-v1 \
  --output build/cognitive-mlp-v1

PYTHONPATH="$SYNORA_PYTORCH_RUNTIME:/tmp/synora-ort-runtime" \
LD_LIBRARY_PATH="$SYNORA_PYTORCH_LIBS" \
python3 tools/cognitive/parity.py \
  --bundle /home/rock/synora-cognitive-mlp-v1 \
  --export build/cognitive-mlp-v1 \
  --fixtures testdata/cognitive/mlp_parity_100.jsonl \
  --report build/cognitive-mlp-v1/parity.json
```

The export produces static batch-1 ONNX graphs and CPU JSON weights. The
parity harness checks PyTorch, ONNX Runtime and CPU weights on 100 deterministic
fixtures, including post-threshold labels.

## RKNN status on the prototype

The machine is a Radxa ROCK 5 ITX with Rockchip RK3588 and `aarch64`. The
runtime library `librknnrt.so.2.2.0`, `rknn_server` and Python `rknnlite 2.2.0`
are present. RKNN Toolkit2 conversion APIs are not installed, so ONNX-to-RKNN
conversion and NPU parity/latency measurements are deliberately not claimed.
The CPU backend is the reference fallback. No RKNN model is loaded by the
cognitive service.

## Out of scope

No backbone selection, LLM/VLM inference, LoRA, State Encoder learning,
paging, custom kernels, autonomous action execution, notifications, locks,
alarms, sirens or shutters are enabled by this change.

