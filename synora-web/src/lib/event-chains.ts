import { getRoomLabel } from "./topology";
import type { ApiTopologyNode, ChainStatus, DangerLevel, EventChain, EventChainEvent } from "./synora-types";

export type EventChainUpdate = Partial<EventChain> & {
  chain_id?: string;
  updated_at?: string;
};

export function sortEventChains(chains: EventChain[], status?: ChainStatus | "all") {
  return [...chains]
    .filter((chain) => !status || status === "all" || chain.status === status)
    .sort((left, right) => {
      const leftDate = status === "closed" || left.status === "closed"
        ? left.closed_at ?? left.updated_at
        : left.updated_at;
      const rightDate = status === "closed" || right.status === "closed"
        ? right.closed_at ?? right.updated_at
        : right.updated_at;
      return Date.parse(rightDate) - Date.parse(leftDate);
    });
}

export function mergeChainUpdate(existing: EventChain | undefined, update: EventChainUpdate): EventChain {
  const id = update.id ?? update.chain_id ?? existing?.id ?? "";
  return {
    ...(existing ?? {
      id,
      status: "open",
      started_at: update.updated_at ?? new Date().toISOString(),
      updated_at: update.updated_at ?? new Date().toISOString(),
      last_event_at: update.updated_at ?? new Date().toISOString(),
      last_significant_event_at: "",
      danger_level: "none",
      danger_score: 0,
      events_count: 0,
      significant_events_count: 0,
      contextual_events_count: 0,
      motion_count: 0,
    }),
    ...update,
    id,
  } as EventChain;
}

export function formatDangerLevel(level: DangerLevel | string | undefined) {
  switch (level) {
    case "none": return "Aucun";
    case "low": return "Faible";
    case "medium": return "Moyen";
    case "high": return "Élevé";
    case "critical": return "Critique";
    default: return level?.trim() || "Inconnu";
  }
}

export function dangerTone(level: DangerLevel | string | undefined): "success" | "warning" | "danger" | "neutral" {
  switch (level) {
    case "critical":
    case "high": return "danger";
    case "medium": return "warning";
    case "low": return "success";
    default: return "neutral";
  }
}

export function formatClosedReason(reason: string | undefined) {
  switch (reason) {
    case "significant_inactivity_timeout": return "Clôturée après 30 s sans événement significatif";
    default: return reason?.trim() || "Clôturée";
  }
}

export function formatChainDuration(chain: EventChain, now = new Date()) {
  const start = Date.parse(chain.started_at);
  const end = Date.parse(chain.closed_at ?? now.toISOString());
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "Durée inconnue";
  const seconds = Math.max(0, Math.floor((end - start) / 1000));
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return remainingSeconds ? `${minutes} min ${remainingSeconds} s` : `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes ? `${hours} h ${remainingMinutes} min` : `${hours} h`;
}

export function getChainRoomLabel(chain: EventChain, topology: ApiTopologyNode[] = []) {
  if (chain.primary_node_id) return getRoomLabel(chain.primary_node_id, topology);
  return chain.primary_device_id ? `Périphérique ${chain.primary_device_id}` : "Emplacement inconnu";
}

export function getEventKey(event: EventChainEvent, index = 0) {
  return event.id || `${event.type}:${event.timestamp}:${index}`;
}

export function getLatestEventId(chain: EventChain) {
  const latest = (chain.recent_events ?? [])
    .map((event, index) => ({ event, index }))
    .sort((left, right) => Date.parse(left.event.timestamp) - Date.parse(right.event.timestamp))
    .at(-1);
  return latest ? getEventKey(latest.event, latest.index) : undefined;
}

export function getEvaluationForEvent(chain: EventChain, eventId: string) {
  return chain.evaluations?.find((evaluation) => evaluation.event_id === eventId);
}

export function formatEventType(type: string) {
  const labels: Record<string, string> = {
    "vision.unknown": "Inconnu détecté",
    "vision.identity": "Résident reconnu",
    "vision.motion": "Mouvement",
    "vision.weapon": "Arme détectée",
    "vision.fall": "Chute détectée",
    "camera.offline": "Caméra hors ligne",
  };
  return labels[type] ?? type.replaceAll(".", " · ");
}

export function isContextualEvent(event: EventChainEvent) {
  return event.contextual === true || event.type === "vision.motion";
}

export function isSignificantEvent(event: EventChainEvent) {
  return event.significant === true;
}
