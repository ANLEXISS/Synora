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
  state?: "present" | "away" | "unknown" | string;
  node_id?: string | null;
  presence_score?: number;
  [key: string]: unknown;
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