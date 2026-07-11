import { synoraFetch } from "./api";
import type {
  SynoraAutomation,
  SynoraDevice,
  SynoraSnapshot,
  SynoraTopologyNode,
} from "./synora-types";

export function getState(signal?: AbortSignal) {
  return synoraFetch<SynoraSnapshot>("/api/state", { signal });
}

export function getDevices(signal?: AbortSignal) {
  return synoraFetch<SynoraDevice[]>("/api/devices", { signal });
}

export function getTopology(signal?: AbortSignal) {
  return synoraFetch<SynoraTopologyNode[]>("/api/topology", { signal });
}

export function getAutomations(signal?: AbortSignal) {
  return synoraFetch<SynoraAutomation[]>("/api/automations", { signal });
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

export function deleteDevice(id: string) {
  return synoraFetch<void>(`/api/devices/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}