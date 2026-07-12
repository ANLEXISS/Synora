import type {
  CgeChainFeedback,
  CgeEvaluationFeedback,
  CgeSecurityMode,
  CgeSecurityProfileInput,
  CgeSecurityProfile,
  EventChain,
  ChainEvaluation,
  DangerLevel,
} from "./synora-types";

const SECURITY_MODES: CgeSecurityMode[] = ["relaxed", "balanced", "strict", "paranoid"];
const DANGER_LEVELS: DangerLevel[] = ["none", "low", "medium", "high", "critical"];

function boundedNumber(value: unknown, fallback: number, minimum: number, maximum: number) {
  const number = typeof value === "number" && Number.isFinite(value) ? value : fallback;
  return Math.max(minimum, Math.min(maximum, number));
}

function boundedInteger(value: unknown, fallback: number, minimum: number, maximum: number) {
  return Math.round(boundedNumber(value, fallback, minimum, maximum));
}

/** Normalize untrusted API data before the security settings UI consumes it. */
export function normalizeCgeSecurityProfile(raw: CgeSecurityProfileInput | null | undefined): CgeSecurityProfile {
  const source = raw ?? {};
  const mode = SECURITY_MODES.includes(source.mode as CgeSecurityMode) ? source.mode as CgeSecurityMode : "balanced";
  const unknownPersonTolerance = source.unknown_person_tolerance === "low" || source.unknown_person_tolerance === "high"
    ? source.unknown_person_tolerance
    : "medium";
  const minimumNotifyDangerLevel = DANGER_LEVELS.includes(source.minimum_notify_danger_level as DangerLevel)
    ? source.minimum_notify_danger_level as DangerLevel
    : "medium";
  const minimumAutoActionDangerLevel = DANGER_LEVELS.includes(source.minimum_auto_action_danger_level as DangerLevel)
    ? source.minimum_auto_action_danger_level as DangerLevel
    : "high";
  const criticalRooms = Array.isArray(source.critical_rooms) ? source.critical_rooms.filter((room): room is string => typeof room === "string") : [];
  const ignoredMotionRooms = Array.isArray(source.ignored_motion_rooms) ? source.ignored_motion_rooms.filter((room): room is string => typeof room === "string") : [];

  return {
    mode,
    global_sensitivity: boundedNumber(source.global_sensitivity, 0.5, 0, 1),
    unknown_person_tolerance: unknownPersonTolerance,
    night_sensitivity_multiplier: boundedNumber(source.night_sensitivity_multiplier, 1.3, 0.1, 5),
    armed_sensitivity_multiplier: boundedNumber(source.armed_sensitivity_multiplier, 1.5, 0.1, 5),
    critical_rooms: criticalRooms,
    ignored_motion_rooms: ignoredMotionRooms,
    minimum_notify_danger_level: minimumNotifyDangerLevel,
    minimum_auto_action_danger_level: minimumAutoActionDangerLevel,
    require_human_confirmation_for_siren: typeof source.require_human_confirmation_for_siren === "boolean" ? source.require_human_confirmation_for_siren : true,
    allow_automatic_lights: typeof source.allow_automatic_lights === "boolean" ? source.allow_automatic_lights : true,
    allow_automatic_recording: typeof source.allow_automatic_recording === "boolean" ? source.allow_automatic_recording : true,
    allow_automatic_notifications: typeof source.allow_automatic_notifications === "boolean" ? source.allow_automatic_notifications : true,
    unknown_persistence_seconds: boundedInteger(source.unknown_persistence_seconds, 10, 1, 86400),
    significant_inactivity_timeout_seconds: boundedInteger(source.significant_inactivity_timeout_seconds, 30, 1, 86400),
  };
}

export function formatSecurityMode(mode: CgeSecurityMode | string) {
  switch (mode) {
    case "relaxed": return "Relaxed";
    case "balanced": return "Balanced";
    case "strict": return "Strict";
    case "paranoid": return "Paranoid";
    default: return mode || "Balanced";
  }
}

export function buildSecurityProfilePayload(profile: CgeSecurityProfile): CgeSecurityProfile {
  const normalized = normalizeCgeSecurityProfile(profile);
  return {
    ...normalized,
  };
}

export function buildEvaluationFeedbackPayload(
  chain: EventChain,
  evaluation: ChainEvaluation,
  correction: Omit<CgeEvaluationFeedback, "chain_id" | "event_id" | "evaluation_index">,
): CgeEvaluationFeedback {
  return {
    ...correction,
    chain_id: chain.id,
    event_id: evaluation.event_id,
    evaluation_index: evaluation.index,
  };
}

export function buildChainFeedbackPayload(
  chain: EventChain,
  feedback: Omit<CgeChainFeedback, "chain_id">,
): CgeChainFeedback {
  return { ...feedback, chain_id: chain.id };
}

export function dangerLevelOptions(): Array<{ value: DangerLevel; label: string }> {
  return [
    { value: "none", label: "Aucun" },
    { value: "low", label: "Faible" },
    { value: "medium", label: "Moyen" },
    { value: "high", label: "Élevé" },
    { value: "critical", label: "Critique" },
  ];
}
