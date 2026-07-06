# Cognitive Graph Engine

`internal/engine` is the Cognitive Graph Engine (CGE) boundary. Its job is to
learn event sequences, compare incoming events with learned graph behavior, and
return a decision without depending on Synora runtime domains.

## Package Boundaries

- `adapter`: Synora boundary adapter. It may translate `pkg/contract` events,
  `state`, `device`, and topology-facing data into CGE contracts, and it builds
  the Synora `Result` returned to the core app.
- `contracts`: CGE-native data structures exchanged by graph and cognitive
  packages.
- `graph`: graph memory and navigation. It must remain independent from Synora
  bus, API, actions, discovery, and state packages.
- `cognitive`: decision analysis over CGE contracts and graph memory. It must
  remain independent from Synora bus, API, actions, discovery, and state
  packages.
- `beliefs`, `hypotheses`, `situation`, `planner`, `memory`: reserved package
  boundaries for future CGE concepts. They intentionally contain no runtime
  logic yet.

## Flow

1. `engine.Engine.Analyze` receives a Synora `contract.Event`.
2. `adapter.NormalizeEvent` fills Synora-level defaults.
3. `adapter.ToCGEEvent` converts the event to `engine/contracts.Event`.
4. `graph.GraphMemory` learns the CGE event.
5. `cognitive.Engine` computes a CGE `DecisionResult`.
6. `adapter.BuildResult` converts the decision back into Synora state updates.

The root `engine` package remains a facade for existing callers. New CGE logic
should prefer CGE contracts and stay below `adapter` unless it explicitly
belongs to Synora integration.
