export type SynoraSystemState =
  | "idle"
  | "activity"
  | "suspicious"
  | "intrusion"
  | "break-in"
  | string;

export type SynoraSecurityModeState = {
  mode: "home" | "night" | "away" | "high_security" | string;
  armed: boolean;
  expected_occupancy: "unknown" | "occupied" | "empty" | string;
  set_by?: string;
  reason?: string;
  since?: string | null;
  expires_at?: string | null;
  source?: string;
};

export type SynoraDevice = {
  id: string;
  node_id?: string;
  nodeId?: string;
  room?: string;
  type?: string;
  role?: string;
  vendor?: string;
  model?: string;
  serial?: string;
  pairing_method?: string;
  online?: boolean;
  status?: "online" | "offline" | "degraded" | string;
  last_seen?: string | null;
  lastSeen?: string | null;
  [key: string]: unknown;
};

export type SynoraResident = {
  id: string;
  name?: string;
  first_name?: string;
  last_name?: string;
  display_name?: string;
  role?: string;
  admin?: boolean;
  trusted?: boolean;
  reference_node_id?: string | null;
  account_id?: string | null;
  face_profile?: SynoraFaceProfile;
  state?: "present" | "away" | "absent" | "unknown" | "no_data" | string;
  node_id?: string | null;
  presence_score?: number;
  confidence?: number;
  last_seen?: string | null;
  [key: string]: unknown;
};

export type ResidentRole = "owner" | "resident" | "guest" | "child";

export type ResidentMutationPayload = {
  first_name?: string;
  last_name?: string;
  display_name?: string;
  role?: ResidentRole;
  admin?: boolean;
  trusted?: boolean;
  enabled?: boolean;
  reference_node_id?: string;
  account_id?: string;
};

export type ResidentCreatePayload = ResidentMutationPayload & {
  id: string;
};

export type SynoraFacePhoto = {
  id: string;
  filename: string;
  path: string;
  view?: "face" | "up" | "left" | "right" | string;
  created_at: string;
  updated_at: string;
  source: string;
};

export type SynoraFaceProfile = {
  status: "empty" | "ready" | "needs_rebuild" | "error" | string;
  base_photos: SynoraFacePhoto[];
  auto_count: number;
  review_count: number;
  pending_count: number;
};

export type SynoraEvent = {
  id?: string;
  type?: string;
  event_type?: string;
  node_id?: string;
  device_id?: string;
  priority?: number;
  created_at?: string;
  timestamp?: string;
  payload?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  [key: string]: unknown;
};

export type SynoraAutomationTrigger = {
  event_type?: string;
  device_id?: string;
  node_id?: string;
  resident_id?: string;
  min_score?: number;
  state?: string;
  situation_type?: string;
  [key: string]: unknown;
};

export type SynoraAutomationCondition = {
  id?: string;
  field?: string;
  op?: string;
  value?: unknown;
  value_type?: string;
  negate?: boolean;
  [key: string]: unknown;
};

export type SynoraAutomationAction = {
  id?: string;
  type?: string;
  target?: string;
  data?: Record<string, unknown>;
  timeout_ms?: number;
  retry_count?: number;
  enabled?: boolean;
  order?: number;
  cooldown_key?: string;
  device?: string;
  command?: string;
  value?: unknown;
  channel?: string;
  residents?: string[];
  retry?: number;
  [key: string]: unknown;
};

export type SynoraAutomation = {
  id: string;
  name?: string;
  title?: string;
  description?: string;
  enabled?: boolean;
  state?: SynoraSystemState;
  event_type?: string;
  node_id?: string;
  trigger?: SynoraAutomationTrigger;
  condition_logic?: "all" | "any" | string;
  conditions?: SynoraAutomationCondition[];
  actions?: SynoraAutomationAction[];
  schedule?: unknown;
  cooldown_ms?: number;
  timeout_ms?: number;
  retry_count?: number;
  dry_run?: boolean;
  requires_validation?: boolean;
  [key: string]: unknown;
};

export type SynoraTopologyNode = {
  id: string;
  name: string;
  type: "zone" | "floor" | "room" | string;
  connect?: string[] | null;
  dynamic_score?: number;
  children?: SynoraTopologyNode[];
};

export type SynoraCgeSnapshot = {
  stats?: Record<string, unknown>;
  sequences?: unknown[];
  transitions?: unknown[];
  learned_behaviors?: unknown[];
  danger_assessments?: unknown[];
  [key: string]: unknown;
};

export type ApiTopologyNode = {
  id: string;
  name: string;
  type: "zone" | "floor" | "room" | string;
  connect: string[] | null;
  children: ApiTopologyNode[];
  dynamic_score?: number;
  locked?: boolean;
  version?: number;
};

export type SynoraSnapshot = {
  system_state?: SynoraSystemState;
  state?: SynoraSystemState;
  danger_score?: number;
  devices?: SynoraDevice[] | Record<string, SynoraDevice>;
  events?: SynoraEvent[];
  recent_events?: SynoraEvent[];
  residents?: SynoraResident[] | Record<string, SynoraResident>;
  automations?: SynoraAutomation[] | Record<string, SynoraAutomation>;
  nodes?: Record<string, unknown>;
  topology?: SynoraTopologyNode[];
  cge?: SynoraCgeSnapshot;
  system?: Record<string, unknown>;
  [key: string]: unknown;
};

export type SynoraWsMessage = {
  type?: string;
  topic?: string;
  payload?: unknown;
  snapshot?: SynoraSnapshot;
  state?: SynoraSnapshot;
  data?: unknown;
  [key: string]: unknown;
};

export type DangerLevel = "none" | "low" | "medium" | "medium_high" | "high" | "critical";
export type ChainStatus = "open" | "closed";

export type EventChainEvent = {
  id?: string;
  type: string;
  timestamp: string;
  device_id?: string;
  node_id?: string;
  activation_id?: string;
  sequence_key?: string;
  clip_id?: string;
  clip_index?: number;
  track_id?: string;
  severity?: string;
  significant: boolean;
  contextual: boolean;
  simulated?: boolean;
  validation?: boolean;
  validation_learn?: boolean;
  test_run_id?: string;
  payload?: Record<string, unknown>;
};

export type ChainEvaluation = {
  index: number;
  event_id: string;
  timestamp: string;
  state?: string;
  danger_level: DangerLevel | string;
  danger_score: number;
  reasons?: string[];
  hypotheses?: string[];
  recommended_actions?: string[];
  engine_version?: string;
};

export type EventChain = {
  id: string;
  status: ChainStatus;
  activation_id?: string;
  sequence_key?: string;
  started_at: string;
  updated_at: string;
  last_event_at: string;
  last_significant_event_at: string;
  closed_at?: string | null;
  closed_reason?: string;
  primary_device_id?: string;
  primary_node_id?: string;
  resident_id?: string;
  identity_id?: string;
  track_ids?: string[];
  clip_ids?: string[];
  current_state?: string;
  danger_level: DangerLevel | string;
  danger_score: number;
  max_danger_level?: DangerLevel | string;
  max_danger_score?: number;
  danger_reasons?: string[];
  title?: string;
  summary?: string;
  events_count: number;
  significant_events_count: number;
  contextual_events_count: number;
  motion_count: number;
  recent_events?: EventChainEvent[];
  evaluations?: ChainEvaluation[];
  rolling_summary?: string;
  compaction?: {
    total_events_count: number;
    retained_events_count: number;
    compacted_contextual_count: number;
    rolling_summary?: string;
  };
  critical?: boolean;
  simulated?: boolean;
  test_run_id?: string;
  scenario_id?: string;
  created_by?: string;
  source?: "real" | "simulation" | "validation" | "mixed" | string;
  validation?: boolean;
  validation_learn?: boolean;
  validation_id?: string;
};

export type EventChainListResponse = {
  chains: EventChain[];
  generated_at: string;
};

export type CgeSecurityMode = "relaxed" | "balanced" | "strict" | "paranoid";
export type CgeCorrectionType = "false_positive" | "false_negative" | "reaction_too_strong" | "reaction_too_weak" | "correct_but_tune_actions";
export type CgeLegacyCorrectionType = "too_low" | "too_high" | "wrong_state" | "wrong_action" | "mark_normal" | "mark_critical";
export type CgeFeedbackScope = "case_only" | "apply_to_similar_future_chains";
export type CgePreferredAction = "observe" | "notify_owner" | "notify_emergency_contact" | "record_clip" | "lock_evidence" | "create_alert" | "request_user_validation" | "ignore_pattern" | "activate_related_automation";

export type CgeEvaluationFeedbackPayload = {
  chain_id: string;
  event_id: string;
  evaluation_index?: number;
  correction_type: CgeCorrectionType;
  scope: CgeFeedbackScope;
  preferred_actions: CgePreferredAction[];
  admin_note?: string;
};

export type CgeChainFeedbackPayload = {
  chain_id: string;
  correction_type: CgeCorrectionType;
  scope: CgeFeedbackScope;
  preferred_actions: CgePreferredAction[];
  admin_note?: string;
};
export type CgeFinalOutcome = "normal" | "false_positive" | "real_incident" | "uncertain";

export type CgeSecurityProfile = {
  mode: CgeSecurityMode;
  global_sensitivity: number;
  unknown_person_tolerance: "low" | "medium" | "high";
  night_sensitivity_multiplier: number;
  armed_sensitivity_multiplier: number;
  critical_rooms: string[];
  ignored_motion_rooms: string[];
  minimum_notify_danger_level: DangerLevel;
  minimum_auto_action_danger_level: DangerLevel;
  require_human_confirmation_for_siren: boolean;
  allow_automatic_lights: boolean;
  allow_automatic_recording: boolean;
  allow_automatic_notifications: boolean;
  unknown_persistence_seconds: number;
  significant_inactivity_timeout_seconds: number;
};

export type CgeSecurityProfileInput = {
  [Key in keyof CgeSecurityProfile]?: CgeSecurityProfile[Key] | null;
};

export type CriticalChainMemory = {
  id: string;
  template_id: string;
  first_seen: string;
  last_seen: string;
  occurrences: number;
  max_danger_level: DangerLevel | string;
  max_danger_score: number;
  representative_chain_id: string;
  recent_chain_ids: string[];
  significant_event_types: string[];
  node_pattern: string[];
  device_types: string[];
  identity_pattern: string[];
  typical_state_path: string[];
  typical_danger_path: string[];
  summary?: string;
  learned_reason?: string;
  recommended_actions: string[];
  actions_taken: string[];
  outcomes: string[];
  confidence: number;
  feedback_count?: number;
  last_feedback_at?: string;
  simulated?: boolean;
  source?: "real" | "simulation" | "validation" | "mixed" | string;
  simulated_occurrences?: number;
  real_occurrences?: number;
  validation_occurrences?: number;
};

export type CgeValidationEventPayload = {
  event_type: string;
  device_id?: string;
  node_id?: string;
  identity?: string;
  confidence?: number;
  danger_level_hint?: string;
  learn?: boolean;
  reason?: string;
};

export type CgeValidationHistoryItem = {
  validation_id: string;
  event_id: string;
  event_type: string;
  timestamp: string;
  device_id?: string;
  node_id?: string;
  chain_id?: string;
  learn: boolean;
  reason?: string;
  source_type: string;
  test_mode: string;
};

export type CgeEvaluationFeedback = {
  id?: string;
  chain_id: string;
  event_id: string;
  evaluation_index?: number;
  correction_type: CgeCorrectionType;
  scope: CgeFeedbackScope;
  preferred_actions: CgePreferredAction[];
  admin_note?: string;
  corrected_state?: string;
  corrected_danger_level?: DangerLevel;
  note?: string;
  created_by?: string;
  created_at?: string;
};

export type CgeChainFeedback = {
  id?: string;
  chain_id: string;
  correction_type: CgeCorrectionType;
  scope: CgeFeedbackScope;
  preferred_actions: CgePreferredAction[];
  admin_note?: string;
  final_outcome?: CgeFinalOutcome;
  corrected_final_danger_level?: DangerLevel;
  apply_to_similar_future_chains?: boolean;
  note?: string;
  created_by?: string;
  created_at?: string;
};
