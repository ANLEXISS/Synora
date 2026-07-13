import { isRecord } from "./normalize";

export type SecurityMode = "home" | "night" | "away" | "high_security";
export type ExpectedOccupancy = "unknown" | "occupied" | "empty";

export type SecurityModeState = {
  mode: SecurityMode;
  armed: boolean;
  expected_occupancy: ExpectedOccupancy;
  set_by: string;
  reason: string;
  since: string | null;
  expires_at: string | null;
  source: string;
};

export function normalizeSecurityMode(value: unknown): SecurityModeState {
  const source = isRecord(value) ? value : {};
  const rawMode = typeof source.mode === "string" ? source.mode.trim().toLowerCase() : "home";
  const mode: SecurityMode = rawMode === "night" || rawMode === "away" || rawMode === "high_security" ? rawMode : "home";
  const occupancy: ExpectedOccupancy = mode === "night"
    ? "occupied"
    : mode === "away" || mode === "high_security"
      ? "empty"
      : source.expected_occupancy === "occupied" || source.expected_occupancy === "empty" ? source.expected_occupancy : "unknown";
  return {
    mode,
    armed: mode !== "home" && source.armed !== false,
    expected_occupancy: occupancy,
    set_by: typeof source.set_by === "string" ? source.set_by : "system",
    reason: typeof source.reason === "string" ? source.reason : "",
    since: typeof source.since === "string" ? source.since : null,
    expires_at: typeof source.expires_at === "string" ? source.expires_at : null,
    source: typeof source.source === "string" ? source.source : "system",
  };
}

export function securityModeLabel(mode: SecurityMode): string {
  return { home: "Maison", night: "Nuit", away: "Absent", high_security: "Sécurité élevée" }[mode];
}
