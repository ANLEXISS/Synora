import { buildDeviceMutationPayload, deviceToEditForm } from "./device-form";

// Framework-free fixture checks for the device editor payload boundary.
export function deviceFormFixtureTest() {
  const form = deviceToEditForm({
    id: "cam_01",
    name: "  Caméra entrée  ",
    node_id: "zoneA.L0.entree",
    enabled: true,
    online: true,
    secret: "must-not-be-sent",
  });
  const payload = buildDeviceMutationPayload(form);
  if (payload.name !== "Caméra entrée" || payload.node_id !== "zoneA.L0.entree" || payload.enabled !== true) {
    throw new Error("device editor payload was not normalized");
  }
  if ("secret" in payload || "online" in payload) {
    throw new Error("device editor payload leaked runtime or secret fields");
  }
}
