# CGE Home World Model V1 — Acceptance

Date: 2026-09-01

## Architecture delivered

The Home Model is an additive `internal/homemodel` layer invoked by the
existing `internal/engine.Engine` for both analyzed and contextual events. It
normalizes `contract.Event` into `contract.HomeObservation`, keeps a bounded
event-time index, rebuilds deterministic HomeEntities and semantic
trajectories, then derives presence, situations, facts and hypotheses. The
existing Decision, incident and automation paths are unchanged.

The public projection is `contract.HomeModelSnapshot`, exposed as the optional
`home` field of the existing public snapshot. The StateStore persists the
bounded model state under the additive `home_model` field and restores it at
startup without replaying the full event history.

## Contracts added or modified

- `pkg/contract/home_model.go`: canonical observation, topology, entity,
  trajectory, presence, situation, fact, hypothesis and snapshot contracts.
- `internal/homemodel`: deterministic topology classification, correlation,
  multi-source fusion, semantic paths, uncertainty, explicit topology
  relations and scenario runner.
- `internal/state`: opaque Home Model persistence boundary, preserving state
  package independence from model implementation details.
- `internal/engine`: additive `Result.HomeModel`, startup restore and topology
  synchronization.
- `pkg/contract.PublicSnapshot`: optional `home` projection; all existing V1
  fields remain available.
- `homemodel.Config` and `Engine.SetHomeModelConfig`: configurable working-set,
  correlation, activity and identity thresholds with safe defaults.

## Reference scenarios

The material-free runner `ReferenceScenarios` contains S01–S10:

| Scenario | Result |
| --- | --- |
| S01 Resident arrives home | pass — resident transition is derived |
| S02 Unknown approaches entrance | pass — unknown approach plus evidence |
| S03 Delivery then departure | pass — departure is a situation, not a decision |
| S04 Unknown circles property | pass — semantic transitions remain descriptive |
| S05 Resident and unknown enter together | pass — entities remain distinct |
| S06 Vehicle arrives but nobody exits | pass — vehicle observation does not invent a person |
| S07 Person crosses camera blind spot | pass — `UNKNOWN` trajectory segment is retained |
| S08 Pet triggers multiple sensors | pass — matching pet evidence can fuse across sources |
| S09 Resident leaves while unknown remains | pass — resident and unknown presence remain separate |
| S10 Sensors disagree | pass — disagreement hypothesis survives with both observations |

## Explicit acceptance checks

- camera event is not a HomeEntity: pass; entities contain source/track refs.
- absence of observation is not absence: pass; stale entities become `unknown`
  or `lost`, while `absence` requires explicit departure evidence.
- hypothesis is not fact: pass; low-confidence identity is represented in
  `hypotheses` and is not copied to `entity.identity`.
- confidence and provenance are preserved: pass; conclusions carry confidence,
  source IDs, observation IDs and event timestamps.
- cross-camera continuity and multi-source fusion: pass; deterministic tests
  cover camera A → B and pet camera/radar fusion.
- semantic trajectories: pass; known zones, `UNKNOWN` gaps and likely paths
  are separate representations.
- contradictions survive: pass; same-time disagreement creates an open
  `sensor_disagreement` hypothesis.
- bounded working set: pass; defaults are 256 observations, 128 entities,
  24-hour entity retention and 512 evidence items.

## Validation performed

- `go test ./...`: pass.
- `go test -race ./internal/homemodel ./internal/engine`: pass.
- `go test -race ./internal/state ./internal/snapshot ./pkg/contract`: pass.
- S01–S10 deterministic scenario tests: pass.
- Software benchmark on the available arm64 environment:
  `BenchmarkHomeModelObserveSynthetic`: 25.5 ms/op, 11.2 MB/op, 8,634
  allocations/op over the bounded synthetic workload. This is a software
  baseline, not a claim about target hardware performance.

## Known limitations and hardware gates

- The V1 correlation engine is deterministic and heuristic. Appearance
  similarity consumes only supplied feature values; it does not add a vision
  model or infer missing features.
- Presence cannot prove a person left solely because they were not observed.
  Explicit departure evidence is required for `absence`; otherwise the state
  remains unknown/lost.
- Existing incidents consume the legacy danger/situation path. Home Model
  situations are exposed and ready for progressive adoption; incident policy
  was deliberately not rewritten.
- The benchmark is synthetic and software-only. Final latency, memory and
  sensor coverage validation require the target Rock 5 ITX deployment and
  representative live sensor rates.
- No cloud service or mandatory LLM is used.

## Next extensions

Source capability metadata, durable topology revisions, stronger calibrated
feature similarity, explicit resident registry integration for inferred
presence, and incremental (rather than bounded rebuild) correlation can be
added in a later version without changing the V1 observation or snapshot
contracts.

