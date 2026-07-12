import type {
  CgeChainFeedback,
  CgeEvaluationFeedback,
  CgeSecurityMode,
  CgeSecurityProfile,
  EventChain,
  ChainEvaluation,
  DangerLevel,
} from "./synora-types";

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
  return {
    ...profile,
    global_sensitivity: Math.max(0, Math.min(1, Number(profile.global_sensitivity))),
    night_sensitivity_multiplier: Math.max(0.1, Math.min(5, Number(profile.night_sensitivity_multiplier))),
    armed_sensitivity_multiplier: Math.max(0.1, Math.min(5, Number(profile.armed_sensitivity_multiplier))),
    unknown_persistence_seconds: Math.max(1, Math.round(Number(profile.unknown_persistence_seconds))),
    significant_inactivity_timeout_seconds: Math.max(1, Math.round(Number(profile.significant_inactivity_timeout_seconds))),
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
