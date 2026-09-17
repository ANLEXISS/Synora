# Synora-Eval

Minimal JSONL harness for comparing a future cognitive backend with the
deterministic Core teacher.

Run the included fixtures with the mock backend:

```sh
go run ./cmd/synora-eval -fixtures Synora-Eval/fixtures/basic.jsonl
```

To compare captured model outputs, pass `-outputs path/to/outputs.jsonl`.
Each output line is a `CognitiveOutput` envelope with a `fixture_id` (or a
matching `request_id`). The runner validates the cognitive schema, compares
classification, inferred state and requested capabilities, and rejects
executable action fields.

