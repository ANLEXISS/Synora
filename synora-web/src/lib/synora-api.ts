import { synoraFetch } from "./api";
import { buildApiUrl } from "./config";
import type {
  SynoraFacePhoto,
  SynoraFaceProfile,
  SynoraAutomation,
  SynoraDevice,
  SynoraResident,
  SynoraSnapshot,
  ResidentCreatePayload,
  ResidentMutationPayload,
} from "./synora-types";

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
  payload: Record<string, unknown>
) {
  return synoraFetch<SynoraDevice>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
}

export function updateDevice(id: string, patch: Record<string, unknown>) {
  return patchDevice(id, patch);
}

export function deleteDevice(id: string) {
  return synoraFetch<void>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "DELETE",
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

export function uploadResidentBaseFace(id: string, file: File, view?: string) {
  const body = new FormData();
  body.append("file", file);
  if (view) body.append("view", view);
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

export function replaceResidentBaseFace(id: string, photoID: string, file: File, view?: string) {
  const body = new FormData();
  body.append("file", file);
  if (view) body.append("view", view);
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
