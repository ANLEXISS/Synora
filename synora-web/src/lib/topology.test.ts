import { getDevicesByRoom, getRoomLabel, getTopologyRooms, normalizeTopologyResponse } from "./topology";

// Kept framework-free so the adapter can be exercised by any frontend test
// runner without adding a production dependency.
export function topologyAdapterFixtureTest() {
  const topology = normalizeTopologyResponse({
    nodes: [
      { id: "zoneA", name: "zoneA", type: "zone" },
      { id: "zoneA.L0", name: "L0", type: "floor", parent: "zoneA" },
      {
        id: "zoneA.L0.entree",
        name: "entree",
        type: "room",
        parent: "zoneA.L0",
        neighbors: ["zoneA.L0.salon"],
      },
      { id: "zoneA.L0.salon", name: "salon", type: "room", parent: "zoneA.L0" },
    ],
    links: [{ from: "zoneA.L0.entree", to: "zoneA.L0.salon" }],
    locked: true,
    version: 1,
  });

  const zone = topology[0];
  const floor = zone?.children[0];
  const room = floor?.children[0];
  if (topology.length !== 1 || !zone || !floor || !room) {
    throw new Error("topology adapter did not rebuild zone/floor/room");
  }
  if (!(room.connect ?? []).includes("zoneA.L0.salon") || room.children.length !== 0) {
    throw new Error("topology adapter did not preserve room connections/children");
  }
  const rooms = getTopologyRooms(topology);
  if (rooms.length !== 2 || getRoomLabel("zoneA.L0.entree", topology) !== "Entree · L0") {
    throw new Error("topology room helpers did not expose normalized rooms");
  }
  const grouped = getDevicesByRoom([{ id: "cam_01", node_id: "zoneA.L0.entree" }, { id: "sensor_01" }]);
  if (grouped["zoneA.L0.entree"]?.[0]?.id !== "cam_01" || grouped.unlocated?.[0]?.id !== "sensor_01") {
    throw new Error("device room grouping failed");
  }
}
