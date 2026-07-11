export type SynoraSystemState =
  | "idle"
  | "activity"
  | "suspicious"
  | "intrusion"
  | "break-in"
  | string;

export type SynoraDevice = {
  id: string;
  node_id?: string;
  nodeId?: string;
  room?: string;
  type?: string;
  role?: string;
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

export type SynoraAutomation = {
  id: string;
  name?: string;
  enabled?: boolean;
  state?: SynoraSystemState;
  event_type?: string;
  node_id?: string;
  conditions?: unknown[];
  actions?: unknown[];
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
