import { isRecord, normalizeArray, normalizeCollection } from "./normalize";
import type { DangerLevel, SynoraEvent, SynoraResident } from "./synora-types";

export type DashboardRuntimeStatus = Record<string, unknown>;

export type DashboardDanger = {
  level: DangerLevel | "unknown";
  score: number;
  source: string;
  manualRiskActive: boolean;
  manualRiskLevel: DangerLevel | "";
  simulated: boolean;
  realOpenChainsCount: number;
  openChainsCount: number;
  lastRealSignificantEventAt: string | null;
  lastActionRequestAt: string | null;
  lastActionResultAt: string | null;
  blockingReasons: string[];
  visionWorkerStatus: string;
};

export type DashboardResidentsSummary = {
  known: number;
  present: number;
  latestLastSeen: string | null;
  latestResident: SynoraResident | null;
};

const SYSTEM_STATE_LABELS: Record<string, string> = {
  idle: "Repos",
  activity: "Activité",
  suspicious: "Suspect",
  intrusion: "Intrusion",
  "break-in": "Effraction",
  unknown: "Inconnu",
};

const DASHBOARD_DIAGNOSTIC_EVENTS = new Set([
  "discovery.worker.started",
  "discovery.worker.crashed",
  "discovery.runtime.status",
  "discovery.vision_worker.unavailable",
  "discovery.vision_ingress.status",
  "discovery.network.degraded",
  "system.health",
  "heartbeat",
  "runtime.status",
]);

function record(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {};
}

function nestedRecord(value: unknown, key: string) {
  const child = record(value)[key];
  return isRecord(child) ? child : {};
}

function firstString(...values: unknown[]): string | null {
  return values.find((value): value is string => typeof value === "string" && value.trim().length > 0)?.trim() ?? null;
}

function firstNumber(...values: unknown[]): number | null {
  return values.find((value): value is number => typeof value === "number" && Number.isFinite(value)) ?? null;
}

function firstBoolean(...values: unknown[]): boolean | null {
  return values.find((value): value is boolean => typeof value === "boolean") ?? null;
}

function normalizeLevel(value: unknown): DashboardDanger["level"] | null {
  if (typeof value !== "string") return null;
  const level = value.trim().toLowerCase();
  return ["none", "low", "medium", "high", "critical"].includes(level)
    ? level as DangerLevel
    : null;
}

function normalizeScore(value: number | null, level: DashboardDanger["level"]): number {
  if (level === "none" || value === null) return 0;
  return Math.max(0, Math.min(1, value));
}

export function normalizeDashboardSystemState(runtimeStatus: unknown, state: unknown): string {
  const runtime = record(runtimeStatus);
  const snapshot = record(state);
  const system = nestedRecord(snapshot, "system");
  const raw = firstString(
    runtime.current_state,
    snapshot.current_state,
    system.current_state,
    system.state,
  )?.toLowerCase() ?? "unknown";

  return SYSTEM_STATE_LABELS[raw] ?? "Inconnu";
}

export function normalizeDashboardDanger(runtimeStatus: unknown, state: unknown): DashboardDanger {
  const runtime = record(runtimeStatus);
  const snapshot = record(state);
  const system = nestedRecord(snapshot, "system");
  const runtimeLevel = normalizeLevel(runtime.danger_level);
  const stateLevel = normalizeLevel(snapshot.danger_level);
  const systemLevel = normalizeLevel(system.danger_level);
  const level = runtimeLevel ?? stateLevel ?? systemLevel ?? "none";
  const manualRiskActive = firstBoolean(
    runtime.manual_risk_active,
    snapshot.manual_risk_active,
    system.manual_risk_active,
  ) ?? false;
  const normalizedManualRiskLevel = normalizeLevel(runtime.manual_risk_level);
  const manualRiskLevel: DangerLevel | "" = normalizedManualRiskLevel && normalizedManualRiskLevel !== "unknown" ? normalizedManualRiskLevel : "";
  const simulated = firstBoolean(runtime.manual_risk_test, runtime.simulated, snapshot.simulated, system.simulated)
    ?? false;
  const score = normalizeScore(firstNumber(
    runtime.danger_score,
    snapshot.danger_score,
    system.danger_score,
  ), level);
  const rawSource = firstString(
    runtime.danger_source,
    snapshot.danger_source,
    system.danger_source,
  );
  const source = rawSource && rawSource.toLowerCase() !== "unknown"
    ? rawSource
    : level !== "none" && level !== "unknown" ? manualRiskActive ? "manual" : simulated ? "simulation" : "real" : "none";

  return {
    level,
    score,
    source,
    manualRiskActive,
    manualRiskLevel,
    simulated,
    realOpenChainsCount: firstNumber(runtime.real_open_chains_count, snapshot.real_open_chains_count) ?? 0,
    openChainsCount: firstNumber(runtime.open_chains_count, snapshot.open_chains_count) ?? 0,
    lastRealSignificantEventAt: firstString(runtime.last_real_significant_event_at, snapshot.last_real_significant_event_at, system.last_real_event_at),
    lastActionRequestAt: firstString(runtime.last_action_request_at, snapshot.last_action_request_at, system.last_action_request_at),
    lastActionResultAt: firstString(runtime.last_action_result_at, snapshot.last_action_result_at, system.last_action_at),
    blockingReasons: normalizeArray<unknown>(runtime.blocking_reasons ?? snapshot.blocking_reasons ?? system.blocking_reasons)
      .filter((reason): reason is string => typeof reason === "string" && reason.trim().length > 0),
    visionWorkerStatus: firstString(runtime.vision_worker_status, snapshot.vision_worker_status) ?? "unknown",
  };
}

function runtimeResidents(snapshot: unknown): SynoraResident[] {
  return normalizeCollection<unknown>(record(snapshot).residents)
    .filter(isRecord) as SynoraResident[];
}

function presenceEntries(snapshot: unknown): Map<string, unknown> {
  const source = record(snapshot);
  const presence = source.presence ?? nestedRecord(source, "system").presence;
  const entries = new Map<string, unknown>();
  if (Array.isArray(presence)) {
    for (const item of presence) {
      const value = record(item);
      const id = firstString(value.resident_id, value.id);
      if (id) entries.set(id, item);
    }
  } else if (isRecord(presence)) {
    for (const [id, value] of Object.entries(presence)) entries.set(id, value);
  }
  return entries;
}

function isPresent(value: unknown): boolean {
  if (value === true) return true;
  if (typeof value === "string") return ["present", "home", "online"].includes(value.toLowerCase());
  const source = record(value);
  const state = firstString(source.state, source.presence_state, source.status)?.toLowerCase();
  return source.present === true || state === "present" || state === "home" || state === "online";
}

function lastSeen(value: unknown): string | null {
  const source = record(value);
  return firstString(source.last_seen, source.lastSeen, source.timestamp);
}

export function normalizeDashboardResidents(residents: SynoraResident[], state: unknown): DashboardResidentsSummary {
  const runtime = runtimeResidents(state);
  const knownResidents = residents.length > 0 ? residents : runtime;
  const runtimeById = new Map(runtime.map((resident) => [resident.id, resident]));
  const presence = presenceEntries(state);
  let present = 0;
  let latestResident: SynoraResident | null = null;
  let latestLastSeen: string | null = null;

  for (const resident of knownResidents) {
    const runtimeResident = runtimeById.get(resident.id);
    const presenceValue = presence.get(resident.id);
    if (isPresent(resident) || isPresent(runtimeResident) || isPresent(presenceValue)) present += 1;
    const candidate = lastSeen(resident) ?? lastSeen(runtimeResident) ?? lastSeen(presenceValue);
    if (candidate && (!latestLastSeen || Date.parse(candidate) > Date.parse(latestLastSeen))) {
      latestLastSeen = candidate;
      latestResident = resident;
    }
  }

  return { known: knownResidents.length, present, latestLastSeen, latestResident };
}

function eventType(event: SynoraEvent): string {
  return String(event.type ?? event.event_type ?? "event");
}

export function isDashboardDiagnosticEvent(event: SynoraEvent): boolean {
  const type = eventType(event);
  return DASHBOARD_DIAGNOSTIC_EVENTS.has(type) || type.startsWith("discovery.");
}

export function isDashboardPriorityEvent(event: SynoraEvent): boolean {
  const type = eventType(event);
  return type === "manual.risk" || type.startsWith("vision.") || [
    "camera.offline",
    "device.offline",
    "action.result",
    "action.request",
    "automation.matched",
  ].includes(type) || type.startsWith("event.chain.");
}

export function filterDashboardEvents(events: SynoraEvent[], includeDiagnostics = false): SynoraEvent[] {
  const visible = includeDiagnostics ? events : events.filter((event) => !isDashboardDiagnosticEvent(event));
  return visible
    .map((event, index) => ({ event, index }))
    .sort((left, right) => {
      const priority = Number(isDashboardPriorityEvent(right.event)) - Number(isDashboardPriorityEvent(left.event));
      if (priority !== 0) return priority;
      const leftTimestamp = Date.parse(String(left.event.timestamp ?? left.event.created_at ?? ""));
      const rightTimestamp = Date.parse(String(right.event.timestamp ?? right.event.created_at ?? ""));
      return (Number.isFinite(rightTimestamp) ? rightTimestamp : 0) - (Number.isFinite(leftTimestamp) ? leftTimestamp : 0) || left.index - right.index;
    })
    .map(({ event }) => event);
}
