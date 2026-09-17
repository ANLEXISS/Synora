# Synora CGE — Home World Model V1

## Baseline M001

The existing runtime path is:

```text
contract.Event
  → ingest/parser and Core event loop
  → internal/engine.Engine.Analyze / ObserveContext
  → internal/engine/adapter.BuildResult
  → internal/state.Store via stateapply.Apply
  → internal/snapshot.Builder
  → Decision and existing automation/incident consumers
```

The existing CGE already provides a graph memory, contextual event chains,
topology nodes, resident tracks, presence records, situation values and
structured evidence in several subpackages. Those records have different
lifetimes and authorities, so the Home Model is an additive projection rather
than a replacement for them.

## Target architecture

```text
Observation (canonical, event time + received time)
  → Home Model normalizer
  → bounded observation index
  → deterministic continuity correlation
  → HomeEntity + semantic trajectory
  → Facts / Hypotheses / DerivedContext
  → Presence V2 + Situation Engine
  → HomeModelSnapshot (public projection)
```

`Decision` remains the existing CGE decision contract. A Home Model situation
is descriptive and does not authorize automation. No LLM, cloud call, vision
model or manufacturer-specific behavior is required.

## Mapping from existing contracts

| Home Model concept | Existing Synora input or output | Compatibility rule |
| --- | --- | --- |
| Observation | `contract.Event` | Adapted; `Timestamp` is event time and `ReceivedAt` is retained separately. |
| source / local track | `Source`, `DeviceID`, `TrackID` | Device ID is preferred as source; source name is the fallback. |
| semantic zone | `Event.NodeID`, payload `zone`, configured topology node | Existing node IDs remain valid; unknown zones remain unknown. |
| identity candidate | `Identity`, `ResidentID`, payload candidate | Candidate confidence is preserved; certainty requires a configurable threshold. |
| HomeEntity | new `HomeEntity` projection | Never aliases a camera track; one entity can contain many source/track refs. |
| trajectory | new `HomeTrajectorySegment` | Missing coverage produces `UNKNOWN`; inferred paths are hypotheses only. |
| presence | existing `PresenceState` plus new Home Model presence | Existing resident presence is unchanged; Home Model presence is additive. |
| situation | existing `internal/engine/situation` plus Home Model situations | Existing incident/decision paths continue unchanged. |
| persistence | existing StateStore file | `home_model` is an additive persisted field; V1 state files remain readable. |
| public API | existing `PublicSnapshot` | New `home` field is optional; all prior fields remain. |

## Invariants

- Observations, facts, hypotheses, derived context and decisions are separate
  data categories.
- Every important derived item carries observation references, source evidence,
  timestamps and confidence where available.
- Duplicate IDs are idempotent, event-time ordering is deterministic, and
  delayed events are sorted before rebuilding the bounded working set.
- `impossible` topology and `unlikely` topology are classifications, not
  intrusion decisions.
- The working set is bounded by observation, entity and evidence limits.

## Configuration

The model consumes the existing topology configuration. Each configured node
is a semantic zone; its metadata can set `exterior: true`. Source coverage is
declared through the source-neutral `SetSensorCoverage` API and can cover
multiple zones. No zone names are hard-coded in the model.

The default software limits are 256 observations, 128 entities, a 15-minute
active observation window, a 24-hour entity retention window, a 10-minute
correlation window and a 0.85 identity certainty threshold. They are exposed
through `homemodel.Config`; `Engine.SetHomeModelConfig` can apply a validated
deployment-specific configuration without changing the contracts.

