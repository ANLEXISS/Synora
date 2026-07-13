import { getRoomLabel } from "./topology";
import type { ApiTopologyNode, ChainEvaluation, ChainStatus, DangerLevel, EventChain, EventChainEvent } from "./synora-types";
import { normalizeArray, normalizeBoolean, normalizeDangerLevel, normalizeDateString, normalizeNumber, normalizeString, normalizeStringArray, isRecord } from "./normalize";

export type EventChainUpdate = Partial<EventChain> & {
  chain_id?: string;
  updated_at?: string;
};

export function chainSourceLabel(chain: Pick<EventChain, "source" | "validation" | "simulated">) {
  if (chain.validation || chain.source === "validation") return "Validation";
  if (chain.simulated || chain.source === "simulation") return "Simulation";
  if (chain.source === "mixed") return "Mixte";
  return "Réel";
}

export function chainSourceTone(chain: Pick<EventChain, "source" | "validation" | "simulated">) {
  const source = chainSourceLabel(chain);
  return source === "Simulation" ? "simulation" : source === "Validation" ? "validation" : source === "Mixte" ? "neutral" : "success";
}

export function normalizeEventChain(raw: unknown): EventChain {
  const source = isRecord(raw) ? raw : {};
  const status: ChainStatus = source.status === "closed" ? "closed" : "open";
  const events = normalizeArray<unknown>(source.recent_events).map(normalizeEventChainEvent);
  const evaluations = normalizeArray<unknown>(source.evaluations).map(normalizeChainEvaluation);
  return {
    id: normalizeString(source.id),
    status,
    activation_id: normalizeString(source.activation_id),
    sequence_key: normalizeString(source.sequence_key),
    started_at: normalizeDateString(source.started_at),
    updated_at: normalizeDateString(source.updated_at),
    last_event_at: normalizeDateString(source.last_event_at),
    last_significant_event_at: normalizeDateString(source.last_significant_event_at),
    closed_at: normalizeDateString(source.closed_at, "") || null,
    closed_reason: normalizeString(source.closed_reason),
    primary_device_id: normalizeString(source.primary_device_id),
    primary_node_id: normalizeString(source.primary_node_id),
    resident_id: normalizeString(source.resident_id),
    identity_id: normalizeString(source.identity_id),
    track_ids: normalizeStringArray(source.track_ids),
    clip_ids: normalizeStringArray(source.clip_ids),
    current_state: normalizeString(source.current_state),
    danger_level: normalizeDangerLevel(source.danger_level),
    danger_score: normalizeNumber(source.danger_score),
    max_danger_level: normalizeDangerLevel(source.max_danger_level),
    max_danger_score: normalizeNumber(source.max_danger_score),
    danger_reasons: normalizeStringArray(source.danger_reasons),
    title: normalizeString(source.title),
    summary: normalizeString(source.summary),
    events_count: Math.max(0, Math.round(normalizeNumber(source.events_count))),
    significant_events_count: Math.max(0, Math.round(normalizeNumber(source.significant_events_count))),
    contextual_events_count: Math.max(0, Math.round(normalizeNumber(source.contextual_events_count))),
    motion_count: Math.max(0, Math.round(normalizeNumber(source.motion_count))),
    recent_events: events,
    evaluations,
    rolling_summary: normalizeString(source.rolling_summary),
    compaction: isRecord(source.compaction) ? {
      total_events_count: Math.max(0, Math.round(normalizeNumber(source.compaction.total_events_count))),
      retained_events_count: Math.max(0, Math.round(normalizeNumber(source.compaction.retained_events_count))),
      compacted_contextual_count: Math.max(0, Math.round(normalizeNumber(source.compaction.compacted_contextual_count))),
      rolling_summary: normalizeString(source.compaction.rolling_summary),
    } : undefined,
    critical: normalizeBoolean(source.critical),
    simulated: normalizeBoolean(source.simulated),
    test_run_id: normalizeString(source.test_run_id),
    scenario_id: normalizeString(source.scenario_id),
    created_by: normalizeString(source.created_by),
    source: normalizeString(source.source, source.validation === true ? "validation" : source.simulated === true ? "simulation" : "real"),
    validation: normalizeBoolean(source.validation),
    validation_learn: normalizeBoolean(source.validation_learn),
    validation_id: normalizeString(source.validation_id),
  };
}

function normalizeEventChainEvent(raw: unknown): EventChainEvent {
  const source = isRecord(raw) ? raw : {};
  return {
    id: normalizeString(source.id),
    type: normalizeString(source.type, "unknown"),
    timestamp: normalizeDateString(source.timestamp),
    device_id: normalizeString(source.device_id),
    node_id: normalizeString(source.node_id),
    activation_id: normalizeString(source.activation_id),
    sequence_key: normalizeString(source.sequence_key),
    clip_id: normalizeString(source.clip_id),
    clip_index: Math.max(0, Math.round(normalizeNumber(source.clip_index))),
    track_id: normalizeString(source.track_id),
    severity: normalizeString(source.severity),
    significant: normalizeBoolean(source.significant),
    contextual: normalizeBoolean(source.contextual),
    simulated: normalizeBoolean(source.simulated),
    validation: normalizeBoolean(source.validation),
    validation_learn: normalizeBoolean(source.validation_learn),
    test_run_id: normalizeString(source.test_run_id),
    payload: isRecord(source.payload) ? source.payload : {},
  };
}

function normalizeChainEvaluation(raw: unknown) {
  const source = isRecord(raw) ? raw : {};
  return {
    index: Math.max(0, Math.round(normalizeNumber(source.index))),
    event_id: normalizeString(source.event_id),
    timestamp: normalizeDateString(source.timestamp),
    state: normalizeString(source.state),
    danger_level: normalizeDangerLevel(source.danger_level),
    danger_score: normalizeNumber(source.danger_score),
    reasons: normalizeStringArray(source.reasons),
    hypotheses: normalizeStringArray(source.hypotheses),
    recommended_actions: normalizeStringArray(source.recommended_actions),
    engine_version: normalizeString(source.engine_version),
  };
}

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
  return normalizeEventChain({
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
  });
}

export function formatDangerLevel(level: DangerLevel | string | undefined) {
  switch (level) {
    case "none": return "Aucun";
    case "low": return "Faible";
    case "medium": return "Moyen";
    case "medium_high": return "Moyen élevé";
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
    case "medium_high": return "warning";
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
    "vision.unknown": "Présence inconnue",
    "vision.identity": "Résident reconnu",
    "vision.motion": "Mouvement",
    "vision.weapon": "Arme détectée",
    "vision.fall": "Chute détectée",
    "camera.offline": "Caméra hors ligne",
    "camera.online": "Caméra reconnectée",
    "door.open": "Porte ouverte",
    "door.forced": "Porte forcée",
  };
  if (labels[type]) return labels[type];
  const readable = type.replaceAll(".", " · ").replaceAll("_", " ").trim();
  return readable ? readable.charAt(0).toUpperCase() + readable.slice(1) : "Événement";
}

export function formatChainEventLabel(event: EventChainEvent) {
  return formatEventType(event?.type || "event");
}

export function formatChainEventLocation(event: EventChainEvent, topology: ApiTopologyNode[] = []) {
  if (event?.node_id) return getRoomLabel(event.node_id, topology) || event.node_id;
  if (event?.device_id) return event.device_id;
  return "Emplacement inconnu";
}

export function getChainEventDangerLevel(event: EventChainEvent, evaluation?: ChainEvaluation): DangerLevel {
  if (evaluation?.danger_level) return normalizeDangerLevel(evaluation.danger_level);
  const payload = event?.payload ?? {};
  return normalizeDangerLevel(payload.danger_level ?? payload.danger_level_hint);
}

export function getChainEventKind(event: EventChainEvent): "contextual" | "significant" {
  return isContextualEvent(event) ? "contextual" : "significant";
}

export function getChainSourceBadge(chain: Pick<EventChain, "source" | "validation" | "simulated">) {
  return chainSourceLabel(chain);
}

export type EventChainRailItem = {
  event: EventChainEvent;
  eventId: string;
  evaluation?: ChainEvaluation;
  kind: "contextual" | "significant";
  dangerLevel: DangerLevel;
};

export function getChainRailItems(chain: EventChain): EventChainRailItem[] {
  const safeChain = normalizeEventChain(chain);
  return (safeChain.recent_events ?? [])
    .map((event, index) => {
      const eventId = getEventKey(event, index);
      const evaluation = event.id ? getEvaluationForEvent(safeChain, event.id) : undefined;
      return {
        event,
        eventId,
        evaluation,
        kind: getChainEventKind(event),
        dangerLevel: getChainEventDangerLevel(event, evaluation),
      };
    })
    .sort((left, right) => Date.parse(left.event.timestamp) - Date.parse(right.event.timestamp));
}

function isTechnicalText(value: string) {
  return /[;=]/.test(value) || /(?:^|[._\s])[a-z0-9]+_[a-z0-9_]+/.test(value) || value.length > 150;
}

function mostRelevantEventType(chain: EventChain) {
  const events = (chain.recent_events ?? [])
    .filter((event) => event.significant)
    .slice()
    .sort((left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp));
  return events[0]?.type ?? chain.recent_events?.[0]?.type ?? "";
}

export function getHumanChainTitle(chain: EventChain) {
  const type = mostRelevantEventType(chain);
  if (type === "vision.unknown") return "Présence inconnue";
  if (type === "vision.weapon") return "Arme détectée";
  if (type === "vision.fall") return "Chute détectée";
  if (type === "camera.offline") return "Caméra hors ligne";
  if (type) return formatEventType(type);
  if (chain.title && !isTechnicalText(chain.title)) return chain.title;
  if (chain.current_state === "break-in") return "Effraction détectée";
  if (chain.current_state === "intrusion") return "Intrusion détectée";
  return "Chaîne d’événements";
}

export function getHumanChainSummary(chain: EventChain) {
  if (chain.summary && !isTechnicalText(chain.summary)) return chain.summary;
  const title = getHumanChainTitle(chain);
  const count = chain.events_count === 1 ? "1 événement" : `${chain.events_count} événements`;
  return `${title} · ${count} observés par Synora.`;
}

export function formatCgeReason(reason: string) {
  const labels: Record<string, string> = {
    unknown_identity: "Présence inconnue",
    simulated_input: "Entrée de simulation",
    security_profile_night_multiplier: "Sensibilité nocturne appliquée",
    security_profile_armed_multiplier: "Mode armé appliqué",
    significant_inactivity_timeout: "Inactivité significative",
    critical_pattern: "Pattern critique",
  };
  if (labels[reason]) return labels[reason];
  const readable = reason.replaceAll(".", " · ").replaceAll("_", " ").trim();
  return readable ? readable.charAt(0).toUpperCase() + readable.slice(1) : "Raison non précisée";
}

export function compactReasonList(reasons: string[] | null | undefined, max = 3) {
  const values = (reasons ?? []).map(formatCgeReason).filter(Boolean);
  return values.slice(0, max);
}

export function isContextualEvent(event: EventChainEvent) {
  return event?.contextual === true || event?.type === "vision.motion" || event?.type === "motion.detected";
}

export function isSignificantEvent(event: EventChainEvent) {
  if (event?.significant === true) return true;
  return ["vision.unknown", "vision.identity", "vision.uncertain", "vision.weapon", "vision.fall", "camera.offline", "device.offline", "manual.risk"].includes(event?.type);
}
