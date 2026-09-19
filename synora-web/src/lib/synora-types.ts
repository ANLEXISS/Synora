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
  network?: { network_trust?: string; [key: string]: unknown };
  identity_status?: string;
  revoked?: boolean;
  online?: boolean;
  status?: "online" | "offline" | "degraded" | string;
  last_seen?: string | null;
  lastSeen?: string | null;
  [key: string]: unknown;
};

export type SynoraStreamDescriptor = {
  device_id: string;
  rtsp_publish_url: string;
  webrtc_url?: string;
  hls_url?: string;
  status: "unknown" | "online" | "offline" | string;
  live_available: boolean;
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
  dataset?: SynoraFaceDatasetState;
};

export type SynoraFaceDatasetState = {
  schema_version: number;
  desired_revision: number;
  active_version: string;
  active_revision: number;
  built_at: string;
  activated_at: string;
  status: "idle" | "building" | "ready" | "active" | "failed" | "unavailable" | string;
  failure_code: string;
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
  system?: Record<string, unknown>;
  [key: string]: unknown;
};

export type SynoraIncident = {
  id: string;
  status: "new" | "viewed" | "acknowledged" | "resolved" | string;
  created_at: string;
  updated_at: string;
  started_at: string;
  last_event_at: string;
  security_state: string;
  severity?: string;
  score: number;
  camera_id?: string;
  node_id?: string;
  identity_kind: "resident" | "unknown" | "uncertain" | "none" | string;
  resident_id?: string;
  entity_id?: string;
  track_id?: string;
  event_ids?: string[];
  clip_ids?: string[];
  cause?: { event_type?: string; decision_type?: string; reason?: string; contributors?: string[]; evidence?: string[] };
};

export type SynoraClip = {
  id: string;
  camera_id: string;
  node_id?: string;
  created_at: string;
  updated_at: string;
  status: "receiving" | "ready" | "processing" | "processed" | "failed" | "missing" | "expired" | string;
  size_bytes: number;
  duration?: number;
  incident_ids?: string[];
  failure_code?: string;
};

export type SynoraWsMessage = {
  schema_version?: string;
  type?: string;
  topic?: string;
  message_id?: string;
  occurred_at?: string;
  source?: string;
  epoch?: string;
  sequence?: number;
  revision?: number;
  payload?: unknown;
  snapshot?: SynoraSnapshot;
  state?: SynoraSnapshot;
  data?: unknown;
  [key: string]: unknown;
};

export type DangerLevel = "none" | "low" | "medium" | "medium_high" | "high" | "critical";
export type PolicyActionDecision = {
  id: string;
  command: string;
  target?: string;
  source: string;
  priority: number;
  cooldown_seconds?: number;
  reason: string;
  enabled: boolean;
  blocked: boolean;
  blocked_reason?: string;
  template?: string;
  message?: string;
};

export type ActionPlanItem = { id?: string; command: string; target?: string; source: string; priority: number; reason?: string };

export type ActionPolicyEntry = {
  id: string;
  command: string;
  target?: string;
  enabled: boolean;
  priority: number;
  cooldown_seconds?: number;
  conditions?: SynoraAutomationCondition[];
  template?: string;
  message?: string;
};

export type ActionPolicyLevel = { danger_level: DangerLevel; enabled: boolean; actions: ActionPolicyEntry[] };
export type ActionPolicy = {
  levels: Partial<Record<DangerLevel, ActionPolicyLevel>>;
  notifications?: { whatsapp?: { enabled?: boolean; dry_run?: boolean; phone_number_id_configured?: boolean; default_to_configured?: boolean; default_template?: string; provider?: string } };
};
