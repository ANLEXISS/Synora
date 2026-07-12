import { synoraFetch } from "./api";
import { buildApiUrl } from "./config";
import { buildFaceUploadFormData, type BaseFaceView } from "./face";
import type {
  SynoraFacePhoto,
  SynoraFaceProfile,
  SynoraAutomation,
  SynoraDevice,
  SynoraResident,
  SynoraSnapshot,
  EventChain,
  EventChainListResponse,
  ChainStatus,
  CriticalChainMemory,
  CgeChainFeedback,
  CgeEvaluationFeedback,
  CgeSecurityProfile,
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
  return synoraFetch<SynoraSnapshot>("/api/state", { signal });
}

export function getDevices(signal?: AbortSignal) {
  return synoraFetch<SynoraDevice[]>("/api/devices", { signal });
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
  return synoraFetch<EventChainListResponse>(`/api/events/chains${suffix ? `?${suffix}` : ""}`, { signal });
}

export function getEventChain(id: string, signal?: AbortSignal) {
  return synoraFetch<EventChain>(`/api/events/chains/${encodeURIComponent(id)}`, { signal });
}

export function getCriticalChains(signal?: AbortSignal) {
  return synoraFetch<CriticalChainMemory[]>("/api/cge/critical-chains", { signal });
}

export function getCriticalChain(id: string, signal?: AbortSignal) {
  return synoraFetch<CriticalChainMemory>(`/api/cge/critical-chains/${encodeURIComponent(id)}`, { signal });
}

export function getCgeSecurityProfile(signal?: AbortSignal) {
  return synoraFetch<CgeSecurityProfile>("/api/cge/security-profile", { signal });
}

export function updateCgeSecurityProfile(payload: CgeSecurityProfile) {
  return synoraFetch<CgeSecurityProfile>("/api/cge/security-profile", {
    method: "PATCH",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function submitCgeEvaluationFeedback(payload: CgeEvaluationFeedback) {
  return synoraFetch<CgeEvaluationFeedback>("/api/cge/feedback/evaluation", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function submitCgeChainFeedback(payload: CgeChainFeedback) {
  return synoraFetch<{ feedback: CgeChainFeedback; critical_chain?: CriticalChainMemory }>("/api/cge/feedback/chain", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getCgeFeedback(params: { chain_id?: string } = {}, signal?: AbortSignal) {
  const query = params.chain_id ? `?chain_id=${encodeURIComponent(params.chain_id)}` : "";
  return synoraFetch<Array<CgeEvaluationFeedback | CgeChainFeedback>>(`/api/cge/feedback${query}`, { signal });
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
  });
}

export function createDevice(payload: Record<string, unknown>) {
  return synoraFetch<SynoraDevice>("/api/devices", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getTopology(signal?: AbortSignal) {
  return synoraFetch<unknown>("/api/topology", { signal });
}

export function getResidents(signal?: AbortSignal) {
  return synoraFetch<SynoraResident[]>("/api/residents", { signal });
}

export function getAutomations(signal?: AbortSignal) {
  return synoraFetch<SynoraAutomation[]>("/api/automations", { signal });
}

export function getAutomationCatalog(signal?: AbortSignal) {
  return synoraFetch<Record<string, unknown>>("/api/automations/catalog", { signal });
}

export function patchDevice(
  id: string,
  payload: DeviceMutationPayload
) {
  return synoraFetch<SynoraDevice>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "PATCH",
    cache: "no-store",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
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
  return synoraFetch<SynoraResident>("/api/residents", {
    method: "POST",
    body: JSON.stringify({ id, ...cleanResidentMutationPayload(mutation) }),
  });
}

export function updateResident(id: string, patch: ResidentMutationPayload) {
  return synoraFetch<SynoraResident>(`/api/residents/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: {
      "Accept": "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(cleanResidentMutationPayload(patch)),
  });
}

export function deleteResident(id: string) {
  return synoraFetch<void>(`/api/residents/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function getResidentFace(id: string) {
  return synoraFetch<SynoraFaceProfile>(`/api/residents/${encodeURIComponent(id)}/face`);
}

export function uploadResidentBaseFace(id: string, view: BaseFaceView, file: File) {
  const body = buildFaceUploadFormData(view, file);
  return synoraFetch<SynoraFacePhoto>(`/api/residents/${encodeURIComponent(id)}/face/base`, {
    method: "POST",
    body,
  });
}

export function deleteResidentBaseFace(id: string, photoID: string) {
  return synoraFetch<void>(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}`,
    { method: "DELETE" }
  );
}

export function replaceResidentBaseFace(id: string, photoID: string, view: BaseFaceView, file: File) {
  const body = buildFaceUploadFormData(view, file);
  return synoraFetch<SynoraFacePhoto>(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}/replace`,
    { method: "POST", body }
  );
}

export function rebuildResidentFace(id: string) {
  return synoraFetch<SynoraFaceProfile>(`/api/residents/${encodeURIComponent(id)}/face/rebuild`, {
    method: "POST",
  });
}

export function getResidentFaceImageUrl(id: string, photoID: string) {
  return buildApiUrl(
    `/api/residents/${encodeURIComponent(id)}/face/base/${encodeURIComponent(photoID)}/image`
  );
}

export function getResidentFaceReview(id: string) {
  return synoraFetch<SynoraFacePhoto[]>(`/api/residents/${encodeURIComponent(id)}/face/review`);
}

export function acceptResidentFaceReview(id: string, cropID: string) {
  return synoraFetch<SynoraFacePhoto>(
    `/api/residents/${encodeURIComponent(id)}/face/review/${encodeURIComponent(cropID)}/accept`,
    { method: "POST" }
  );
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
  return synoraFetch<SynoraAutomation>("/api/automations", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateAutomation(id: string, patch: Record<string, unknown>) {
  return synoraFetch<SynoraAutomation>(`/api/automations/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function deleteAutomation(id: string) {
  return synoraFetch<void>(`/api/automations/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function setAutomationEnabled(id: string, enabled: boolean) {
  return updateAutomation(id, { enabled });
}
