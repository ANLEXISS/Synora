import {
  formatChainEventLocation,
  getChainEventDangerLevel,
  getChainEventKind,
  getChainRailItems,
} from "./event-chains";
import type { EventChain } from "./synora-types";

export function eventChainRailFixtureTest() {
  const chain: EventChain = {
    id: "validation-chain",
    status: "closed",
    started_at: "2026-07-13T10:00:00Z",
    updated_at: "2026-07-13T10:00:03Z",
    last_event_at: "2026-07-13T10:00:03Z",
    last_significant_event_at: "2026-07-13T10:00:03Z",
    danger_level: "critical",
    danger_score: 0.95,
    events_count: 3,
    significant_events_count: 2,
    contextual_events_count: 1,
    motion_count: 1,
    source: "validation",
    validation: true,
    recent_events: [
      { id: "unknown", type: "vision.unknown", timestamp: "2026-07-13T10:00:00Z", node_id: "zoneA.L0.entree", significant: true, contextual: false },
      { id: "motion", type: "vision.motion", timestamp: "2026-07-13T10:00:01Z", significant: false, contextual: true },
      { id: "weapon", type: "vision.weapon", timestamp: "2026-07-13T10:00:03Z", significant: true, contextual: false, payload: { danger_level: "medium_high" } },
    ],
    evaluations: [{ index: 1, event_id: "weapon", timestamp: "2026-07-13T10:00:03Z", danger_level: "medium_high", danger_score: 0.72 }],
  };
  const items = getChainRailItems(chain);
  if (items.length !== 3 || getChainEventKind(items[1].event) !== "contextual") {
    throw new Error("motion should be rendered as a contextual rail item");
  }
  if (getChainEventDangerLevel(items[2].event, items[2].evaluation) !== "medium_high") {
    throw new Error("medium_high should remain visible on the rail");
  }
  if (formatChainEventLocation({ type: "manual.risk", timestamp: "", significant: true, contextual: false }) !== "Emplacement inconnu") {
    throw new Error("events without a node or device should have a safe location label");
  }
  if (chain.status !== "closed" || getChainRailItems({ ...chain, status: "open" }).length !== 3) {
    throw new Error("open and closed chains should both expose rail items");
  }
}
