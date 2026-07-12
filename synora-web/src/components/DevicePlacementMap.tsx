import { MapPin, Router } from "lucide-react";
import { useMemo, useState } from "react";
import { getDevicesByRoom, getTopologyRooms, type TopologyRoom } from "../lib/topology";
import type { ApiTopologyNode, SynoraDevice } from "../lib/synora-types";

type DevicePlacementMapProps = {
  topology: ApiTopologyNode[];
  devices: SynoraDevice[];
  selectedDeviceId: string;
  selectedNodeId: string;
  onSelectRoom: (nodeID: string) => void;
};

type RoomGroup = {
  key: string;
  zoneName: string;
  floorName: string;
  rooms: TopologyRoom[];
};

export function DevicePlacementMap({
  topology,
  devices,
  selectedDeviceId,
  selectedNodeId,
  onSelectRoom,
}: DevicePlacementMapProps) {
  const [hoveredRoom, setHoveredRoom] = useState<string | null>(null);
  const rooms = useMemo(() => getTopologyRooms(topology), [topology]);
  const devicesByRoom = useMemo(() => getDevicesByRoom(devices), [devices]);
  const groups = useMemo(() => {
    const grouped = new Map<string, RoomGroup>();
    for (const room of rooms) {
      const key = `${room.zoneName}::${room.floorName}`;
      const group = grouped.get(key) ?? {
        key,
        zoneName: room.zoneName,
        floorName: room.floorName,
        rooms: [],
      };
      group.rooms.push(room);
      grouped.set(key, group);
    }
    return [...grouped.values()];
  }, [rooms]);

  if (rooms.length === 0) {
    return <p className="device-placement-empty">Topologie indisponible. Utilisez la liste des pièces.</p>;
  }

  return (
    <div className="device-placement-map" aria-label="Choisir une pièce">
      <div className="device-placement-hint">
        <MapPin size={15} />
        Cliquez sur une pièce pour préparer le déplacement de ce périphérique.
      </div>
      {groups.map((group) => (
        <section className="device-placement-group" key={group.key}>
          <div className="device-placement-group-title">
            <span>{group.zoneName || "Maison"}</span>
            <strong>{group.floorName || "Étage"}</strong>
          </div>
          <div className="device-placement-rooms">
            {group.rooms.map((room) => {
              const roomDevices = devicesByRoom[room.id] ?? [];
              const selected = room.id === selectedNodeId;
              const containsSelected = roomDevices.some((device) => device.id === selectedDeviceId);
              return (
                <button
                  type="button"
                  key={room.id}
                  className={`device-placement-room${selected ? " selected" : ""}${containsSelected ? " current" : ""}`}
                  aria-pressed={selected}
                  onMouseEnter={() => setHoveredRoom(room.id)}
                  onMouseLeave={() => setHoveredRoom(null)}
                  onFocus={() => setHoveredRoom(room.id)}
                  onBlur={() => setHoveredRoom(null)}
                  onClick={() => onSelectRoom(room.id)}
                >
                  <span className="device-placement-room-icon"><Router size={17} /></span>
                  <span className="device-placement-room-copy">
                    <strong>{room.name}</strong>
                    <small>{room.id}</small>
                  </span>
                  <span className={`device-placement-count${hoveredRoom === room.id ? " hovered" : ""}`}>
                    {roomDevices.length}
                  </span>
                </button>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}
