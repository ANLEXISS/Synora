import {
  ArrowDown,
  ArrowUp,
  Building2,
  Camera,
  Cpu,
  DoorOpen,
  GitBranch,
  Lightbulb,
  Lock,
  Map as MapIcon,
  Radar,
  ShieldCheck,
} from "lucide-react";
import {
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Panel } from "../components/Panel";
import { useSynoraData } from "../hooks/useSynoraData";
import {
  prettyTopologyName,
  type ApiTopologyNode,
  type TopologyDevice,
} from "../data/demo";
import { normalizeTopologyDevices } from "../lib/topology";

const MIN_ROOM_W = 155;
const MAX_ROOM_W = 260;
const ROOM_RATIO = 0.72;
const MIN_GAP = 28;
const MAX_GAP = 78;
const CANVAS_PAD = 28;

type Tone = "success" | "warning" | "danger";

type RoomNode = ApiTopologyNode & {
  zoneId: string;
  floorId: string;
  floorName: string;
};

type FloorNode = ApiTopologyNode & {
  zoneId: string;
};

type PositionedRoom = RoomNode & {
  x: number;
  y: number;
};

type RoomEdge = {
  from: PositionedRoom;
  to: PositionedRoom;
};

type LayoutMetrics = {
  roomW: number;
  roomH: number;
  gap: number;
  pad: number;
};

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value));
}

function useElementWidth<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  const [width, setWidth] = useState(1000);

  useLayoutEffect(() => {
    if (!ref.current) return;

    const observer = new ResizeObserver(([entry]) => {
      setWidth(entry.contentRect.width);
    });

    observer.observe(ref.current);

    return () => observer.disconnect();
  }, []);

  return { ref, width };
}

function deviceIcon(type: TopologyDevice["type"]) {
  if (type === "camera") return Camera;
  if (type === "light") return Lightbulb;
  if (type === "sensor") return Radar;
  if (type === "lock") return DoorOpen;

  return Cpu;
}

function statusTone(status: TopologyDevice["status"]): Tone {
  if (status === "online") return "success";
  if (status === "degraded" || status === "pending") return "warning";

  return "danger";
}

function devicesForRoom(roomId: string, devices: TopologyDevice[]) {
  return devices.filter((device) => device.node_id === roomId);
}

function roomTone(room: RoomNode, devices: TopologyDevice[]): Tone {
  const roomDevices = devicesForRoom(room.id, devices);

  if ((room.dynamic_score ?? 0) >= 0.75) return "danger";
  if ((room.dynamic_score ?? 0) >= 0.35) return "warning";
  if (roomDevices.some((device) => device.status === "offline")) return "danger";
  if (roomDevices.some((device) => device.status === "degraded" || device.status === "pending")) return "warning";

  return "success";
}

function getZones(topology: ApiTopologyNode[]) {
  return topology.filter((node) => node.type === "zone");
}

function getFloorsForZone(zone: ApiTopologyNode): FloorNode[] {
  return (zone.children ?? [])
    .filter((child) => child.type === "floor")
    .map((floor) => ({
      ...floor,
      zoneId: zone.id,
    }));
}

function getRoomsForFloor(zone: ApiTopologyNode, floor: FloorNode): RoomNode[] {
  return (floor.children ?? [])
    .filter((child) => child.type === "room")
    .map((room) => ({
      ...room,
      zoneId: zone.id,
      floorId: floor.id,
      floorName: floor.name,
    }));
}

function getRoomsForZone(zone: ApiTopologyNode) {
  return getFloorsForZone(zone).flatMap((floor) =>
    getRoomsForFloor(zone, floor)
  );
}

function getFloorKeyFromRoomId(roomId: string) {
  const parts = roomId.split(".");
  if (parts.length < 2) return null;

  return `${parts[0]}.${parts[1]}`;
}

function getRoomNameFromId(roomId: string) {
  return prettyTopologyName(roomId.split(".").at(-1) ?? roomId);
}

function floorNumber(floorId: string) {
  const raw = floorId.split(".").at(-1) ?? "";
  const n = Number(raw.replace("L", ""));

  return Number.isNaN(n) ? 0 : n;
}

function roomPriority(room: RoomNode) {
  const name = room.name.toLowerCase();

  if (name.includes("entree")) return 100;
  if (name.includes("couloir")) return 90;
  if (name.includes("salon")) return 80;
  if (name.includes("salle_a_manger")) return 75;
  if (name.includes("cuisine")) return 70;
  if (name.includes("bureau")) return 60;
  if (name.includes("chambre")) return 50;
  if (name.includes("bain")) return 40;

  return 10;
}

function getUndirectedSameFloorConnections(
  room: RoomNode,
  rooms: RoomNode[]
) {
  const roomIds = new Set(rooms.map((item) => item.id));
  const connected = new Set<string>();

  for (const targetId of room.connect ?? []) {
    if (roomIds.has(targetId)) connected.add(targetId);
  }

  for (const other of rooms) {
    if (other.id === room.id) continue;
    if ((other.connect ?? []).includes(room.id)) connected.add(other.id);
  }

  return [...connected];
}

function pickAnchorRoom(rooms: RoomNode[]) {
  const entree = rooms.find((room) =>
    room.name.toLowerCase().includes("entree")
  );

  if (entree) return entree;

  const couloir = rooms.find((room) =>
    room.name.toLowerCase().includes("couloir")
  );

  if (couloir) return couloir;

  return [...rooms].sort((a, b) => {
    const degreeA = getUndirectedSameFloorConnections(a, rooms).length;
    const degreeB = getUndirectedSameFloorConnections(b, rooms).length;

    if (degreeB !== degreeA) return degreeB - degreeA;

    return roomPriority(b) - roomPriority(a);
  })[0];
}

function findNearestFreeSlot(
  originX: number,
  originY: number,
  occupied: Set<string>
) {
  for (let radius = 1; radius < 12; radius++) {
    for (let dx = -radius; dx <= radius; dx++) {
      for (let dy = -radius; dy <= radius; dy++) {
        if (Math.abs(dx) + Math.abs(dy) !== radius) continue;

        const x = originX + dx;
        const y = originY + dy;
        const key = `${x},${y}`;

        if (!occupied.has(key)) return { x, y };
      }
    }
  }

  return { x: originX + 1, y: originY + 1 };
}

function buildTopDownLayout(rooms: RoomNode[]): PositionedRoom[] {
  if (rooms.length === 0) return [];

  const roomMap = new Map(rooms.map((room) => [room.id, room]));
  const anchor = pickAnchorRoom(rooms);

  if (!anchor) return [];

  const placed = new Map<string, { x: number; y: number }>();
  const occupied = new Set<string>();
  const queue: string[] = [anchor.id];

  placed.set(anchor.id, { x: 0, y: 0 });
  occupied.add("0,0");

  while (queue.length > 0) {
    const currentId = queue.shift();
    if (!currentId) continue;

    const currentRoom = roomMap.get(currentId);
    const currentPos = placed.get(currentId);

    if (!currentRoom || !currentPos) continue;

    const connections = getUndirectedSameFloorConnections(currentRoom, rooms);

    const preferredSlots = [
      { x: currentPos.x + 1, y: currentPos.y },
      { x: currentPos.x - 1, y: currentPos.y },
      { x: currentPos.x, y: currentPos.y + 1 },
      { x: currentPos.x, y: currentPos.y - 1 },
    ];

    for (const connectedId of connections) {
      if (placed.has(connectedId)) continue;

      const chosen =
        preferredSlots.find((slot) => !occupied.has(`${slot.x},${slot.y}`)) ??
        findNearestFreeSlot(currentPos.x, currentPos.y, occupied);

      placed.set(connectedId, chosen);
      occupied.add(`${chosen.x},${chosen.y}`);
      queue.push(connectedId);
    }
  }

  for (const room of rooms) {
    if (placed.has(room.id)) continue;

    const extra = findNearestFreeSlot(0, 0, occupied);

    placed.set(room.id, extra);
    occupied.add(`${extra.x},${extra.y}`);
  }

  const coords = [...placed.values()];
  const minX = Math.min(...coords.map((coord) => coord.x));
  const minY = Math.min(...coords.map((coord) => coord.y));

  return rooms.map((room) => {
    const pos = placed.get(room.id)!;

    return {
      ...room,
      x: pos.x - minX,
      y: pos.y - minY,
    };
  });
}

function buildMobileVerticalLayout(rooms: RoomNode[]): PositionedRoom[] {
  if (rooms.length === 0) return [];

  const roomMap = new Map(rooms.map((room) => [room.id, room]));
  const anchor = pickAnchorRoom(rooms);

  if (!anchor) return [];

  const visited = new Set<string>();
  const ordered: RoomNode[] = [];
  const queue: string[] = [anchor.id];

  visited.add(anchor.id);

  while (queue.length > 0) {
    const currentId = queue.shift();
    if (!currentId) continue;

    const currentRoom = roomMap.get(currentId);
    if (!currentRoom) continue;

    ordered.push(currentRoom);

    const neighbors = getUndirectedSameFloorConnections(currentRoom, rooms)
      .map((id) => roomMap.get(id))
      .filter(Boolean) as RoomNode[];

    neighbors.sort((a, b) => roomPriority(b) - roomPriority(a));

    for (const neighbor of neighbors) {
      if (visited.has(neighbor.id)) continue;

      visited.add(neighbor.id);
      queue.push(neighbor.id);
    }
  }

  for (const room of rooms) {
    if (!visited.has(room.id)) {
      ordered.push(room);
    }
  }

  return ordered.map((room, index) => ({
    ...room,
    x: 0,
    y: index,
  }));
}

function buildSameFloorEdges(rooms: PositionedRoom[]): RoomEdge[] {
  const byId = new Map(rooms.map((room) => [room.id, room]));
  const seen = new Set<string>();
  const edges: RoomEdge[] = [];

  for (const room of rooms) {
    for (const targetId of getUndirectedSameFloorConnections(room, rooms)) {
      const target = byId.get(targetId);
      if (!target) continue;

      const key = [room.id, target.id].sort().join("__");

      if (seen.has(key)) continue;

      seen.add(key);
      edges.push({ from: room, to: target });
    }
  }

  return edges;
}

function getRoomCenter(room: PositionedRoom, metrics: LayoutMetrics) {
  return {
    x:
      metrics.pad +
      room.x * (metrics.roomW + metrics.gap) +
      metrics.roomW / 2,
    y:
      metrics.pad +
      room.y * (metrics.roomH + metrics.gap) +
      metrics.roomH / 2,
  };
}

function connectionPath(edge: RoomEdge, metrics: LayoutMetrics) {
  const from = getRoomCenter(edge.from, metrics);
  const to = getRoomCenter(edge.to, metrics);

  if (from.x === to.x || from.y === to.y) {
    return `M ${from.x} ${from.y} L ${to.x} ${to.y}`;
  }

  const midX = (from.x + to.x) / 2;

  return `M ${from.x} ${from.y} L ${midX} ${from.y} L ${midX} ${to.y} L ${to.x} ${to.y}`;
}

function getCrossFloorConnections(room: RoomNode) {
  return (room.connect ?? []).filter(
    (targetId) => getFloorKeyFromRoomId(targetId) !== room.floorId
  );
}

function computeMetrics(
  boardWidth: number,
  columns: number,
  mobile: boolean
): LayoutMetrics {
  const safeColumns = Math.max(columns, 1);

  if (mobile) {
    const pad = 16;
    const available = Math.max(boardWidth - pad * 2 - 8, 180);

    const roomW = Math.round(clamp(available, 180, 280));
    const roomH = Math.round(clamp(roomW * 0.68, 132, 170));

    return {
      roomW,
      roomH,
      gap: 34,
      pad,
    };
  }

  const available = Math.max(boardWidth - CANVAS_PAD * 2 - 20, 320);

  const rawGap =
    safeColumns <= 1
      ? MAX_GAP
      : (available - safeColumns * MIN_ROOM_W) / (safeColumns - 1);

  const gap = Math.round(clamp(rawGap, MIN_GAP, MAX_GAP));

  const roomW = Math.round(
    clamp(
      (available - (safeColumns - 1) * gap) / safeColumns,
      MIN_ROOM_W,
      MAX_ROOM_W
    )
  );

  const roomH = Math.round(clamp(roomW * ROOM_RATIO, 128, 190));

  return {
    roomW,
    roomH,
    gap,
    pad: CANVAS_PAD,
  };
}

export function HomeMap() {
  const data = useSynoraData();
  const topology = data.topology;
  const devices: TopologyDevice[] = normalizeTopologyDevices(data.devices, topology);
  const { ref: boardRef, width: boardWidth } =
    useElementWidth<HTMLDivElement>();

  const zones = getZones(topology);
  const allFloors = zones.flatMap((zone) => getFloorsForZone(zone));
  const allRooms = zones.flatMap((zone) => getRoomsForZone(zone));

  const initialZoneId = zones[0]?.id ?? "";
  const initialFloorId = zones[0]
    ? getFloorsForZone(zones[0])[0]?.id ?? ""
    : "";

  const [selectedZoneId, setSelectedZoneId] = useState(initialZoneId);
  const [selectedFloorId, setSelectedFloorId] = useState(initialFloorId);

  const selectedZone =
    zones.find((zone) => zone.id === selectedZoneId) ?? zones[0] ?? null;

  const floors = useMemo(
    () => (selectedZone ? getFloorsForZone(selectedZone) : []),
    [selectedZone]
  );

  const selectedFloor =
    floors.find((floor) => floor.id === selectedFloorId) ?? floors[0] ?? null;

  const rooms = useMemo(
    () =>
      selectedZone && selectedFloor
        ? getRoomsForFloor(selectedZone, selectedFloor)
        : [],
    [selectedZone, selectedFloor]
  );

  const zoneRooms = useMemo(
    () => (selectedZone ? getRoomsForZone(selectedZone) : []),
    [selectedZone]
  );

  const isMobileTopology = boardWidth < 640;

  const positionedRooms = useMemo(
    () =>
      isMobileTopology
        ? buildMobileVerticalLayout(rooms)
        : buildTopDownLayout(rooms),
    [rooms, isMobileTopology]
  );

  const sameFloorEdges = useMemo(
    () => buildSameFloorEdges(positionedRooms),
    [positionedRooms]
  );

  const maxX = Math.max(...positionedRooms.map((room) => room.x), 0);
  const maxY = Math.max(...positionedRooms.map((room) => room.y), 0);

  const columns = maxX + 1;
  const rows = maxY + 1;

  const metrics = useMemo(
    () => computeMetrics(boardWidth, columns, isMobileTopology),
    [boardWidth, columns, isMobileTopology]
  );

  const canvasWidth =
    metrics.pad * 2 + columns * metrics.roomW + maxX * metrics.gap;

  const canvasHeight =
    metrics.pad * 2 + rows * metrics.roomH + maxY * metrics.gap;

  const totalConnections = zoneRooms.reduce(
    (count, room) => count + (room.connect?.length ?? 0),
    0
  );

  const unlocatedDevices = devices.filter(
    (device) => !device.node_id || device.node_id === "unlocated"
  );

  if (!selectedZone || !selectedFloor) {
    return (
      <div className="home-topdown-layout">
        <Panel title="Maison — vue du dessus" className="home-topdown-main">
          <div className="empty-state">
            <ShieldCheck size={24} />
            <strong>
              {data.apiStatus === "unauthenticated"
                ? "API non authentifiée"
                : data.topologySource === "unrecognized"
                  ? "Format topologie non reconnu"
                  : data.topologySource === "unavailable"
                    ? "API indisponible"
                    : "Topologie vide côté backend"}
            </strong>
            <span>{data.error ? "La topologie n’est pas disponible. Réessayez après vérification de l’API." : "Aucune zone ou étage disponible."}</span>
          </div>
        </Panel>
      </div>
    );
  }

  return (
    <div className="home-topdown-layout">
      <Panel
        title="Maison — vue du dessus"
        className="home-topdown-main"
        action={
          <span className="badge success">
            <Lock size={12} />
            {sourceLabel(data.apiStatus, data.topologySource)}
          </span>
        }
      >
        <div className="topology-overview topology-overview-compact">
          <div className="topology-metric">
            <Building2 size={18} />
            <div>
              <strong>{zones.length}</strong>
              <span>zone</span>
            </div>
          </div>

          <div className="topology-metric">
            <Cpu size={18} />
            <div>
              <strong>{devices.filter((device) => device.node_id !== null).length}</strong>
              <span>devices placés</span>
            </div>
          </div>

          <div className="topology-metric">
            <Radar size={18} />
            <div>
              <strong>{unlocatedDevices.length}</strong>
              <span>non placés</span>
            </div>
          </div>

          <div className="topology-metric">
            <MapIcon size={18} />
            <div>
              <strong>{allFloors.length}</strong>
              <span>étages</span>
            </div>
          </div>

          <div className="topology-metric">
            <ShieldCheck size={18} />
            <div>
              <strong>{allRooms.length}</strong>
              <span>pièces</span>
            </div>
          </div>

          <div className="topology-metric">
            <GitBranch size={18} />
            <div>
              <strong>{totalConnections}</strong>
              <span>connexions</span>
            </div>
          </div>
        </div>

        <div className="house-controls">
          <div className="house-zone-tabs">
            {zones.map((zone) => (
              <button
                key={zone.id}
                className={zone.id === selectedZone.id ? "active" : ""}
                onClick={() => {
                  const nextFloors = getFloorsForZone(zone);

                  setSelectedZoneId(zone.id);
                  setSelectedFloorId(nextFloors[0]?.id ?? "");
                }}
              >
                {prettyTopologyName(zone.name)}
              </button>
            ))}
          </div>

          <div className="house-floor-tabs">
            {floors.map((floor) => (
              <button
                key={floor.id}
                className={floor.id === selectedFloor.id ? "active" : ""}
                onClick={() => setSelectedFloorId(floor.id)}
              >
                {floor.name}
              </button>
            ))}
          </div>
        </div>

        <div className="toy-house-board" ref={boardRef}>
          <div
            className="toy-house-canvas"
            style={{
              width: canvasWidth,
              height: canvasHeight,
            }}
          >
            <svg
              className="room-connections-svg"
              width={canvasWidth}
              height={canvasHeight}
              viewBox={`0 0 ${canvasWidth} ${canvasHeight}`}
            >
              <defs>
                <marker
                  id="room-connection-arrow"
                  markerWidth="10"
                  markerHeight="10"
                  refX="8"
                  refY="5"
                  orient="auto-start-reverse"
                >
                  <path
                    d="M0,0 L10,5 L0,10 Z"
                    className="connection-arrow-head"
                  />
                </marker>
              </defs>

              {sameFloorEdges.map((edge) => (
                <path
                  key={`${edge.from.id}-${edge.to.id}`}
                  d={connectionPath(edge, metrics)}
                  className="room-connection-path"
                  markerStart="url(#room-connection-arrow)"
                  markerEnd="url(#room-connection-arrow)"
                />
              ))}
            </svg>

            {positionedRooms.map((room) => {
              const tone = roomTone(room, devices);
              const roomDevices = devicesForRoom(room.id, devices);
              const crossFloorConnections = getCrossFloorConnections(room);

              return (
                <article
                  key={room.id}
                  className={`toy-room toy-room-${tone}`}
                  style={{
                    left:
                      metrics.pad +
                      room.x * (metrics.roomW + metrics.gap),
                    top:
                      metrics.pad +
                      room.y * (metrics.roomH + metrics.gap),
                    width: metrics.roomW,
                    height: metrics.roomH,
                  }}
                >
                  <div className="toy-room-header">
                    <div>
                      <strong>{prettyTopologyName(room.name)}</strong>
                      <span>{room.id}</span>
                    </div>

                    <span className={`room-dot ${tone}`} />
                  </div>

                  <div className="toy-device-list">
                    {roomDevices.length === 0 ? (
                      <span className="room-empty">Aucun périphérique</span>
                    ) : (
                      roomDevices.map((device) => {
                        const Icon = deviceIcon(device.type);
                        const tone = statusTone(device.status);

                        return (
                          <div
                            key={device.id}
                            className={`toy-device-card ${tone}`}
                            title={`${device.name} · ${device.status}`}
                          >
                            <Icon size={16} />

                            <div>
                              <strong>{device.name}</strong>
                              <span>{device.id}</span>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>

                  {crossFloorConnections.length > 0 && (
                    <div className="toy-room-stairs">
                      {crossFloorConnections.map((connection) => {
                        const targetFloorId = getFloorKeyFromRoomId(connection);
                        const currentN = floorNumber(room.floorId);
                        const targetN = floorNumber(targetFloorId ?? "");
                        const isUp = targetN > currentN;

                        return (
                          <button
                            key={connection}
                            className="stairs-button"
                            onClick={() => {
                              if (targetFloorId) {
                                setSelectedFloorId(targetFloorId);
                              }
                            }}
                            title={`Aller vers ${targetFloorId}`}
                          >
                            {isUp ? (
                              <ArrowUp size={14} />
                            ) : (
                              <ArrowDown size={14} />
                            )}
                            <span>{getRoomNameFromId(connection)}</span>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
        </div>
      </Panel>

      <Panel
        title="Périphériques non placés"
        className="home-topdown-side"
        action={
          unlocatedDevices.length > 0 ? (
            <span className="badge warning">{unlocatedDevices.length}</span>
          ) : (
            <span className="badge success">0</span>
          )
        }
      >
        {unlocatedDevices.length === 0 ? (
          <div className="empty-state">
            <ShieldCheck size={24} />
            <strong>Tout est placé</strong>
            <span>Aucun périphérique hors topologie.</span>
          </div>
        ) : (
          <div className="compact-list">
            {unlocatedDevices.map((device) => (
              <div className="compact-row" key={device.id}>
                <div>
                  <strong>{device.name}</strong>
                  <span>{device.id}</span>
                </div>

                <span className={`badge ${statusTone(device.status)}`}>
                  {device.status}
                </span>
              </div>
            ))}
          </div>
        )}
      </Panel>
    </div>
  );
}

function sourceLabel(
  apiStatus: "connected" | "unauthenticated" | "unavailable",
  topologySource: string
) {
  if (apiStatus === "unauthenticated") return "non authentifiée";
  if (topologySource === "api") return "API réelle";
  if (topologySource === "snapshot") return "snapshot";
  if (topologySource === "empty") return "topologie vide";
  if (topologySource === "unrecognized") return "format non reconnu";
  if (topologySource === "unavailable") return "API indisponible";
  return "chargement";
}
