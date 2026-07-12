import type { SynoraDevice } from "./synora-types";
import type { DeviceMutationPayload } from "./synora-api";

export type DeviceEditForm = {
  name: string;
  node_id: string;
  enabled: boolean;
};

export function deviceToEditForm(device: SynoraDevice): DeviceEditForm {
  return {
    name: String(device.name ?? device.display_name ?? device.id).trim(),
    node_id: typeof device.node_id === "string" && device.node_id.trim()
      ? device.node_id.trim()
      : typeof device.room === "string" ? device.room.trim() : "unlocated",
    enabled: device.enabled !== false,
  };
}

export function buildDeviceMutationPayload(form: DeviceEditForm): DeviceMutationPayload {
  return {
    name: form.name.trim(),
    node_id: form.node_id.trim() || "unlocated",
    enabled: form.enabled,
  };
}
