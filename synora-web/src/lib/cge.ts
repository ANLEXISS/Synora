import type {
  CgeChainFeedback,
  CgeEvaluationFeedback,
  CgeSecurityMode,
  CgeSecurityProfileInput,
  CgeSecurityProfile,
  CriticalChainMemory,
  EventChain,
  ChainEvaluation,
  DangerLevel,
} from "./synora-types";
import { formatCgeReason, formatEventType } from "./event-chains";
import {
  normalizeDangerLevel,
  normalizeDateString,
  normalizeNumber,
  normalizeString,
  normalizeStringArray,
} from "./normalize";

const SECURITY_MODES: CgeSecurityMode[] = ["relaxed", "balanced", "strict", "paranoid"];
const DANGER_LEVELS: DangerLevel[] = ["none", "low", "medium", "medium_high", "high", "critical"];

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

export function normalizeCriticalChainMemory(raw: unknown): CriticalChainMemory {
  const source = raw && typeof raw === "object" && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
  return {
    id: normalizeString(source.id),
    template_id: normalizeString(source.template_id),
    first_seen: normalizeDateString(source.first_seen),
    last_seen: normalizeDateString(source.last_seen),
    occurrences: Math.max(0, Math.round(normalizeNumber(source.occurrences))),
    max_danger_level: normalizeDangerLevel(source.max_danger_level),
    max_danger_score: Math.max(0, normalizeNumber(source.max_danger_score)),
    representative_chain_id: normalizeString(source.representative_chain_id),
    recent_chain_ids: normalizeStringArray(source.recent_chain_ids),
    significant_event_types: normalizeStringArray(source.significant_event_types),
    node_pattern: normalizeStringArray(source.node_pattern),
    device_types: normalizeStringArray(source.device_types),
    identity_pattern: normalizeStringArray(source.identity_pattern),
    typical_state_path: normalizeStringArray(source.typical_state_path),
    typical_danger_path: normalizeStringArray(source.typical_danger_path),
    summary: normalizeString(source.summary),
    learned_reason: normalizeString(source.learned_reason),
    recommended_actions: normalizeStringArray(source.recommended_actions),
    actions_taken: normalizeStringArray(source.actions_taken),
    outcomes: normalizeStringArray(source.outcomes),
    confidence: Math.max(0, Math.min(1, normalizeNumber(source.confidence))),
    feedback_count: Math.max(0, Math.round(normalizeNumber(source.feedback_count))),
    last_feedback_at: normalizeDateString(source.last_feedback_at),
    simulated: source.simulated === true,
    source: source.source === "simulation" || source.source === "validation" || source.source === "mixed" ? source.source : "real",
    simulated_occurrences: Math.max(0, Math.round(normalizeNumber(source.simulated_occurrences))),
    real_occurrences: Math.max(0, Math.round(normalizeNumber(source.real_occurrences))),
    validation_occurrences: Math.max(0, Math.round(normalizeNumber(source.validation_occurrences))),
  };
}

export function getHumanCriticalChainTitle(memory: CriticalChainMemory) {
  const types = memory.significant_event_types ?? [];
  if (types.includes("vision.unknown")) return "Présence inconnue persistante";
  if (types.includes("vision.weapon")) return "Arme détectée à plusieurs reprises";
  if (types.includes("vision.fall")) return "Chutes détectées à plusieurs reprises";
  if (types.includes("camera.offline")) return "Caméra hors ligne récurrente";
  const firstType = types[0];
  return firstType ? `${formatEventType(firstType)} récurrent` : "Chaîne critique connue";
}

export function getHumanCriticalChainSummary(memory: CriticalChainMemory) {
  const reason = memory.learned_reason?.trim();
  if (reason && !/[;=]/.test(reason) && reason.length <= 150) return formatCgeReason(reason);
  const types = (memory.significant_event_types ?? []).map(formatEventType).slice(0, 2);
  return types.length > 0
    ? `${types.join(" · ")} observés dans des chaînes similaires.`
    : "Motif critique appris à partir de chaînes observées.";
}

export function formatCorrectionType(value: unknown) {
  switch (value) {
    case "false_positive": return "Faux positif";
    case "false_negative": return "Faux négatif";
    case "reaction_too_strong": return "Réaction trop forte";
    case "reaction_too_weak": return "Réaction insuffisante";
    case "correct_but_tune_actions": return "Évaluation correcte, réaction ajustée";
    case "too_low": return "Danger évalué trop bas";
    case "too_high": return "Danger évalué trop haut";
    case "wrong_state": return "État système incorrect";
    case "wrong_action": return "Action recommandée incorrecte";
    case "mark_normal": return "Pattern marqué normal";
    case "mark_critical": return "Pattern marqué critique";
    default: return "Correction moteur";
  }
}

export function formatFeedbackScope(value: unknown) {
  return value === "apply_to_similar_future_chains" ? "Cas similaires futurs" : "Seulement ce cas";
}

export function formatPreferredAction(value: unknown) {
  const labels: Record<string, string> = {
    observe: "Observer",
    notify_owner: "Notifier le propriétaire",
    notify_emergency_contact: "Notifier un contact d’urgence",
    record_clip: "Enregistrer un clip",
    lock_evidence: "Verrouiller la preuve",
    create_alert: "Créer une alerte",
    request_user_validation: "Demander validation utilisateur",
    ignore_pattern: "Ignorer ce pattern",
    activate_related_automation: "Activer une automation liée",
  };
  return labels[String(value)] ?? String(value || "Action non précisée");
}

export function getFeedbackSummary(item: CgeEvaluationFeedback | CgeChainFeedback) {
  const target = "event_id" in item && item.event_id ? `Évaluation · ${item.event_id}` : "Fin de chaîne";
  return `${formatCorrectionType(item.correction_type)} · ${target}`;
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
    { value: "medium_high", label: "Moyen élevé" },
    { value: "high", label: "Élevé" },
    { value: "critical", label: "Critique" },
  ];
}
