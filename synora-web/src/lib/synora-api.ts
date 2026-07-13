import { synoraFetch } from "./api";
import { buildApiUrl } from "./config";
import { buildFaceUploadFormData, type BaseFaceView } from "./face";
import { normalizeCgeSecurityProfile, normalizeCriticalChainMemory } from "./cge";
import { normalizeEventChain } from "./event-chains";
import { normalizeArray, normalizeBoolean, normalizeCollection, normalizeDateString, normalizeNumber, normalizeString, normalizeStringArray, isRecord } from "./normalize";
import { normalizeTopologyResponse } from "./topology";
import type { DashboardRuntimeStatus } from "./dashboard";
import { normalizeSecurityMode, type SecurityModeState } from "./security-mode";
import type {
  SynoraFacePhoto,
  SynoraFaceProfile,
  SynoraAutomation,
  SynoraDevice,
  SynoraResident,
  SynoraSnapshot,
  EventChainListResponse,
  ChainStatus,
  CriticalChainMemory,
  CgeChainFeedback,
  CgeChainFeedbackPayload,
  CgeEvaluationFeedback,
  CgeEvaluationFeedbackPayload,
  CgeSecurityProfile,
  CgeSecurityProfileInput,
  ResidentCreatePayload,
  ResidentMutationPayload,
} from "./synora-types";

export type SynoraCameraQRPayload = {
  type: "synora.camera";
  version: number;
  device_id: string;
  serial?: string;
  model?: string;
  setup_token: string;
};

export type SynoraCameraPairingStart = {
  session_id: string;
  device_id: string;
  serial?: string;
  model?: string;
  status: "ready_to_confirm" | string;
  expires_at: string;
};

export type SynoraCameraPairingConfirm = {
  device: SynoraDevice;
  status: "paired" | string;
};

export type DeleteDeviceResponse = {
  status: "deleted" | string;
  device_id: string;
};

export type DeviceMutationPayload = {
  name?: string;
  display_name?: string;
  node_id?: string;
  room?: string;
  enabled?: boolean;
};

export type ResidentFormMutationInput = {
  first_name: string;
  last_name: string;
  display_name: string;
  role: ResidentMutationPayload["role"];
  admin: boolean;
  trusted: boolean;
  enabled?: boolean;
  reference_node_id: string;
  account_id: string;
};

export function buildResidentMutationPayload(form: ResidentFormMutationInput): ResidentMutationPayload {
  return {
    first_name: form.first_name.trim(),
    last_name: form.last_name.trim(),
    display_name: form.display_name.trim(),
    role: form.role,
    admin: form.admin,
    trusted: form.trusted,
    ...(form.enabled === undefined ? {} : { enabled: form.enabled }),
    reference_node_id: form.reference_node_id.trim(),
    account_id: form.account_id.trim(),
  };
}

export function getState(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/state", { signal }).then(normalizeSnapshot);
}

export function getRuntimeStatus(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/cge/runtime-status", { signal }).then((value): DashboardRuntimeStatus => (
    isRecord(value) ? value : {}
  ));
}

export function getSecurityMode(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/security/mode", { signal }).then(normalizeSecurityMode);
}

export function armSecurity(payload: { mode: "night" | "away" | "high_security"; reason?: string; duration_seconds?: number }) {
  return synoraFetch<unknown>("/api/security/arm", { method: "POST", body: JSON.stringify(payload) }).then(normalizeSecurityMode);
}

export function disarmSecurity(payload: { reason?: string } = {}) {
  return synoraFetch<unknown>("/api/security/disarm", { method: "POST", body: JSON.stringify(payload) }).then(normalizeSecurityMode);
}

export function setManualRisk(payload: { danger_level: "low" | "medium" | "high" | "critical"; duration_seconds: number; test?: boolean; reason?: string }) {
  return synoraFetch<Record<string, unknown>>("/api/cge/manual-risk", { method: "POST", body: JSON.stringify(payload) });
}

export function clearManualRisk(payload: { reason?: string } = {}) {
  return synoraFetch<Record<string, unknown>>("/api/cge/manual-risk/clear", { method: "POST", body: JSON.stringify(payload) });
}

export type { SecurityModeState };

export function getDevices(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/devices", { signal }).then((value) => normalizeCollection<unknown>(value).map(normalizeDevice).filter((device) => device.id));
}

export type EventChainQuery = {
  status?: ChainStatus | "all";
  limit?: number;
  since?: string;
  severity?: string;
  simulated?: boolean;
};

export function getEventChains(params: EventChainQuery = {}, signal?: AbortSignal) {
  const query = new URLSearchParams();
  if (params.status) query.set("status", params.status);
  if (params.limit !== undefined) query.set("limit", String(params.limit));
  if (params.since) query.set("since", params.since);
  if (params.severity) query.set("severity", params.severity);
  if (params.simulated !== undefined) query.set("simulated", String(params.simulated));
  const suffix = query.toString();
  return synoraFetch<unknown>(`/api/events/chains${suffix ? `?${suffix}` : ""}`, { signal }).then(normalizeEventChainList);
}

export function getEventChain(id: string, signal?: AbortSignal) {
  return synoraFetch<unknown>(`/api/events/chains/${encodeURIComponent(id)}`, { signal }).then(normalizeEventChain);
}

export function getCriticalChains(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/cge/critical-chains", { signal }).then((value) => normalizeArray<unknown>(value).map(normalizeCriticalChainMemory).filter((memory) => memory.id));
}

export function getCriticalChain(id: string, signal?: AbortSignal) {
  return synoraFetch<unknown>(`/api/cge/critical-chains/${encodeURIComponent(id)}`, { signal }).then(normalizeCriticalChainMemory);
}

export function getCgeSecurityProfile(signal?: AbortSignal) {
  return synoraFetch<CgeSecurityProfileInput | null>("/api/cge/security-profile", { signal }).then(normalizeCgeSecurityProfile);
}

export function updateCgeSecurityProfile(payload: CgeSecurityProfile) {
  return synoraFetch<CgeSecurityProfileInput | null>("/api/cge/security-profile", {
    method: "PATCH",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  }).then(normalizeCgeSecurityProfile);
}

export function submitCgeEvaluationFeedback(payload: CgeEvaluationFeedbackPayload) {
  return synoraFetch<CgeEvaluationFeedback>("/api/cge/feedback/evaluation", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function submitCgeChainFeedback(payload: CgeChainFeedbackPayload) {
  return synoraFetch<{ feedback: CgeChainFeedback; critical_chain?: CriticalChainMemory }>("/api/cge/feedback/chain", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getCgeFeedback(params: { chain_id?: string } = {}, signal?: AbortSignal) {
  const query = params.chain_id ? `?chain_id=${encodeURIComponent(params.chain_id)}` : "";
  return synoraFetch<unknown>(`/api/cge/feedback${query}`, { signal }).then((value) => normalizeArray<unknown>(value).map(normalizeCgeFeedback));
}

export function getDevicePairingCapabilities(signal?: AbortSignal) {
  return synoraFetch<{ synora_camera: { available: boolean; qr_scan: boolean; manual_code: boolean } }>(
    "/api/devices/pairing/capabilities",
    { signal, cache: "no-store" }
  );
}

export function startSynoraCameraPairing(payload: { qr_payload: SynoraCameraQRPayload } | { raw_code: string }) {
  return synoraFetch<SynoraCameraPairingStart>("/api/devices/pairing/synora-camera/start", {
    method: "POST",
    cache: "no-store",
    body: JSON.stringify(payload),
  });
}

export function confirmSynoraCameraPairing(payload: { session_id: string; name: string; node_id: string; enabled: boolean }) {
  return synoraFetch<SynoraCameraPairingConfirm>("/api/devices/pairing/synora-camera/confirm", {
    method: "POST",
    cache: "no-store",
    body: JSON.stringify(payload),
  }).then((response) => ({ ...response, device: normalizeDevice(response?.device) }));
}

export function createDevice(payload: Record<string, unknown>) {
  return synoraFetch<unknown>("/api/devices", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then(normalizeDevice);
}

export function getTopology(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/topology", { signal }).then(normalizeTopologyResponse);
}

export function getResidents(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/residents", { signal }).then((value) => normalizeCollection<unknown>(value).map(normalizeResident).filter((resident) => resident.id));
}

export function getAutomations(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/automations", { signal }).then((value) => normalizeCollection<unknown>(value).map(normalizeAutomation).filter((automation) => automation.id));
}

export function getAutomationCatalog(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/automations/catalog", { signal }).then((value) => isRecord(value) ? value : {});
}

export function patchDevice(
  id: string,
  payload: DeviceMutationPayload
) {
  return synoraFetch<unknown>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "PATCH",
    cache: "no-store",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  }).then(normalizeDevice);
}

export function updateDevice(id: string, patch: DeviceMutationPayload) {
  return patchDevice(id, patch);
}

export function deleteDevice(id: string) {
  return synoraFetch<DeleteDeviceResponse>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "DELETE",
    cache: "no-store",
  });
}

const residentMutationKeys: (keyof ResidentMutationPayload)[] = [
  "first_name", "last_name", "display_name", "role", "admin", "trusted",
  "enabled", "reference_node_id", "account_id",
];

function cleanResidentMutationPayload(payload: ResidentMutationPayload): ResidentMutationPayload {
  const clean: ResidentMutationPayload = {};
  for (const key of residentMutationKeys) {
    const value = payload[key];
    if (value !== undefined) {
      clean[key] = value as never;
    }
  }
  return clean;
}

export function createResident(payload: ResidentCreatePayload) {
  const { id, ...mutation } = payload;
  return synoraFetch<unknown>("/api/residents", {
    method: "POST",
    body: JSON.stringify({ id, ...cleanResidentMutationPayload(mutation) }),
  }).then(normalizeResident);
}

export function updateResident(id: string, patch: ResidentMutationPayload) {
  return synoraFetch<unknown>(`/api/residents/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: {
      "Accept": "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(cleanResidentMutationPayload(patch)),
  }).then(normalizeResident);
}

export function deleteResident(id: string) {
  return synoraFetch<void>(`/api/residents/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function getResidentFace(id: string) {
  return synoraFetch<unknown>(`/api/residents/${encodeURIComponent(id)}/face`).then(normalizeFaceProfile);
}

export function uploadResidentBaseFace(id: string, view: BaseFaceView, file: File) {
  const body = buildFaceUploadFormData(view, file);
  return synoraFetch<unknown>(`/api/residents/${encodeURIComponent(id)}/face/base`, {
    method: "POST",
    body,
  }).then(normalizeFacePhoto);
}

export function deleteResidentBaseFace(id: string, photoID: string) {
  return synoraFetch<void>(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}`,
    { method: "DELETE" }
  );
}

export function replaceResidentBaseFace(id: string, photoID: string, view: BaseFaceView, file: File) {
  const body = buildFaceUploadFormData(view, file);
  return synoraFetch<unknown>(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}/replace`,
    { method: "POST", body }
  ).then(normalizeFacePhoto);
}

export function rebuildResidentFace(id: string) {
  return synoraFetch<unknown>(`/api/residents/${encodeURIComponent(id)}/face/rebuild`, {
    method: "POST",
  }).then(normalizeFaceProfile);
}

export function getResidentFaceImageUrl(id: string, photoID: string) {
  return buildApiUrl(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}/image`
  );
}

export function getResidentFaceReview(id: string) {
  return synoraFetch<unknown>(`/api/residents/${encodeURIComponent(id)}/face/review`).then((value) => normalizeArray<unknown>(value).map(normalizeFacePhoto));
}

export function acceptResidentFaceReview(id: string, cropID: string) {
  return synoraFetch<unknown>(
    `/api/residents/${encodeURIComponent(id)}/face/review/${encodeURIComponent(cropID)}/accept`,
    { method: "POST" }
  ).then(normalizeFacePhoto);
}

export function deleteResidentFaceReview(id: string, cropID: string) {
  return synoraFetch<void>(
    `/api/residents/${encodeURIComponent(id)}/face/review/${encodeURIComponent(cropID)}`,
    { method: "DELETE" }
  );
}

export function getResidentFaceReviewImageUrl(id: string, cropID: string) {
  return buildApiUrl(
    `/api/residents/${encodeURIComponent(id)}/face/review/${encodeURIComponent(cropID)}/image`
  );
}

export function createAutomation(payload: Record<string, unknown>) {
  return synoraFetch<unknown>("/api/automations", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then(normalizeAutomation);
}

export function updateAutomation(id: string, patch: Record<string, unknown>) {
  return synoraFetch<unknown>(`/api/automations/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  }).then(normalizeAutomation);
}

export function deleteAutomation(id: string) {
  return synoraFetch<void>(`/api/automations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function setAutomationEnabled(id: string, enabled: boolean) {
  return updateAutomation(id, { enabled });
}

function normalizeSnapshot(value: unknown): SynoraSnapshot {
  const source = isRecord(value) ? value : {};
  return {
    ...source,
    devices: normalizeCollection<unknown>(source.devices).map(normalizeDevice).filter((device) => device.id),
    residents: normalizeCollection<unknown>(source.residents).map(normalizeResident).filter((resident) => resident.id),
    automations: normalizeCollection<unknown>(source.automations).map(normalizeAutomation).filter((automation) => automation.id),
    events: normalizeArray<unknown>(source.events ?? source.recent_events).filter(isRecord).map((event) => ({
      ...event,
      id: normalizeString(event.id),
      type: normalizeString(event.type ?? event.event_type, "event"),
      timestamp: normalizeDateString(event.timestamp ?? event.created_at),
      significant: normalizeBoolean(event.significant),
      contextual: normalizeBoolean(event.contextual),
    })),
    topology: normalizeTopologyResponse(source.topology ?? source.nodes),
  };
}

function normalizeEventChainList(value: unknown): EventChainListResponse {
  const source = isRecord(value) ? value : {};
  const rawChains = source.chains ?? (Array.isArray(value) ? value : []);
  return {
    chains: normalizeArray<unknown>(rawChains).map(normalizeEventChain).filter((chain) => chain.id),
    generated_at: normalizeDateString(source.generated_at),
  };
}

function normalizeDevice(value: unknown): SynoraDevice {
  const source = isRecord(value) ? value : {};
  return {
    ...source,
    id: normalizeString(source.id ?? source.device_id),
    node_id: normalizeString(source.node_id ?? source.room),
    name: normalizeString(source.name ?? source.display_name ?? source.id ?? source.device_id),
    enabled: normalizeBoolean(source.enabled, true),
    status: normalizeString(source.status),
  };
}

function normalizeResident(value: unknown): SynoraResident {
  const source = isRecord(value) ? value : {};
  return {
    ...source,
    id: normalizeString(source.id ?? source.resident_id),
    display_name: normalizeString(source.display_name ?? source.name ?? source.id ?? source.resident_id),
    name: normalizeString(source.name ?? source.display_name ?? source.id ?? source.resident_id),
    node_id: typeof source.node_id === "string" ? source.node_id : null,
    reference_node_id: typeof source.reference_node_id === "string" ? source.reference_node_id : null,
    admin: normalizeBoolean(source.admin),
    trusted: normalizeBoolean(source.trusted),
    last_seen: normalizeDateString(source.last_seen, "") || null,
    presence_score: normalizeNumber(source.presence_score ?? source.confidence),
  };
}

function normalizeAutomation(value: unknown): SynoraAutomation {
  const source = isRecord(value) ? value : {};
  return {
    ...source,
    id: normalizeString(source.id ?? source.automation_id),
    name: normalizeString(source.name ?? source.title ?? source.id ?? source.automation_id),
    description: normalizeString(source.description),
    enabled: normalizeBoolean(source.enabled, true),
    conditions: normalizeArray<unknown>(source.conditions).filter(isRecord) as SynoraAutomation["conditions"],
    actions: normalizeArray<unknown>(source.actions).filter(isRecord) as SynoraAutomation["actions"],
  };
}

function normalizeFacePhoto(value: unknown): SynoraFacePhoto {
  const source = isRecord(value) ? value : {};
  return {
    id: normalizeString(source.id),
    filename: normalizeString(source.filename),
    path: normalizeString(source.path),
    view: normalizeString(source.view),
    created_at: normalizeDateString(source.created_at),
    updated_at: normalizeDateString(source.updated_at),
    source: normalizeString(source.source),
  };
}

function normalizeFaceProfile(value: unknown): SynoraFaceProfile {
  const source = isRecord(value) ? value : {};
  return {
    status: normalizeString(source.status, "empty"),
    base_photos: normalizeArray<unknown>(source.base_photos).map(normalizeFacePhoto).filter((photo) => photo.id),
    auto_count: Math.max(0, Math.round(normalizeNumber(source.auto_count))),
    review_count: Math.max(0, Math.round(normalizeNumber(source.review_count))),
    pending_count: Math.max(0, Math.round(normalizeNumber(source.pending_count))),
  };
}

function normalizeCgeFeedback(value: unknown): CgeEvaluationFeedback | CgeChainFeedback {
  const source = isRecord(value) ? value : {};
  const rawScope = source.scope;
  const scope = rawScope === "apply_to_similar_future_chains" || source.apply_to_similar_future_chains === true
    ? "apply_to_similar_future_chains"
    : "case_only";
  const preferredActions = normalizeStringArray(source.preferred_actions);
  const legacyOutcome = normalizeString(source.final_outcome);
  const correctionType = normalizeString(source.correction_type) || (legacyOutcome === "false_positive" ? "false_positive" : legacyOutcome === "real_incident" ? "false_negative" : "");
  return {
    ...source,
    id: normalizeString(source.id),
    chain_id: normalizeString(source.chain_id),
    correction_type: correctionType as CgeEvaluationFeedback["correction_type"],
    note: normalizeString(source.note),
    admin_note: normalizeString(source.admin_note ?? source.note),
    scope,
    created_by: normalizeString(source.created_by),
    created_at: normalizeDateString(source.created_at),
    preferred_actions: preferredActions,
    apply_to_similar_future_chains: normalizeBoolean(source.apply_to_similar_future_chains),
  } as unknown as CgeEvaluationFeedback | CgeChainFeedback;
}
