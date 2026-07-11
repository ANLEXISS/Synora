import {
  AlertTriangle,
  Battery,
  Camera,
  Cpu,
  DoorOpen,
  Lightbulb,
  MapPin,
  Pencil,
  Plus,
  Radar,
  Search,
  ShieldCheck,
  Trash2,
  Wifi,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Panel } from "../components/Panel";
import {
  demoApiTopology,
  demoTopologyDevices,
  prettyTopologyName,
  type ApiTopologyNode,
  type TopologyDevice,
} from "../data/demo";

type DeviceStatus = TopologyDevice["status"];
type DeviceType = TopologyDevice["type"];

type DeviceFilter = "all" | DeviceStatus | "unlocated";
type TypeFilter = "all" | DeviceType;

function deviceIcon(type: DeviceType) {
  if (type === "camera") return Camera;
  if (type === "light") return Lightbulb;
  if (type === "sensor") return Radar;
  if (type === "lock") return DoorOpen;

  return Cpu;
}

function statusTone(status: DeviceStatus) {
  if (status === "online") return "success";
  if (status === "degraded") return "warning";

  return "danger";
}

function statusLabel(status: DeviceStatus) {
  if (status === "online") return "Online";
  if (status === "degraded") return "Dégradé";

  return "Offline";
}

function typeLabel(type: DeviceType) {
  if (type === "camera") return "Caméra";
  if (type === "light") return "Lumière";
  if (type === "sensor") return "Capteur";
  if (type === "siren") return "Sirène";
  if (type === "lock") return "Serrure";

  return "Device";
}

function flattenRooms(topology: ApiTopologyNode[]) {
  return topology.flatMap((zone) =>
    (zone.children ?? []).flatMap((floor) =>
      (floor.children ?? [])
        .filter((node) => node.type === "room")
        .map((room) => ({
          id: room.id,
          name: prettyTopologyName(room.name),
          floor: floor.name,
          zone: zone.name,
        }))
    )
  );
}

function getRoomLabel(roomId: string | null | undefined) {
  if (!roomId || roomId === "unlocated") return "Non placé";

  const room = flattenRooms(demoApiTopology).find((item) => item.id === roomId);

  if (!room) return roomId;

  return `${room.name} · ${room.floor}`;
}

function deviceHealth(device: TopologyDevice) {
  if (device.status === "online") return 96;
  if (device.status === "degraded") return 61;

  return 8;
}

function deviceBattery(device: TopologyDevice) {
  if (device.type === "light") return null;
  if (device.status === "offline") return 0;
  if (device.status === "degraded") return 42;

  return 87;
}

function isUnlocated(device: TopologyDevice) {
  return !device.node_id || device.node_id === "unlocated";
}

export function Devices() {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<DeviceFilter>("all");
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");

  const devices = demoTopologyDevices;

  const filteredDevices = useMemo(() => {
    const query = search.trim().toLowerCase();

    return devices.filter((device) => {
      const matchSearch =
        query.length === 0 ||
        device.id.toLowerCase().includes(query) ||
        device.name.toLowerCase().includes(query) ||
        device.type.toLowerCase().includes(query) ||
        getRoomLabel(device.node_id).toLowerCase().includes(query);

      const matchStatus =
        statusFilter === "all" ||
        (statusFilter === "unlocated" && isUnlocated(device)) ||
        device.status === statusFilter;

      const matchType = typeFilter === "all" || device.type === typeFilter;

      return matchSearch && matchStatus && matchType;
    });
  }, [devices, search, statusFilter, typeFilter]);

  const online = devices.filter((device) => device.status === "online").length;
  const degraded = devices.filter((device) => device.status === "degraded").length;
  const offline = devices.filter((device) => device.status === "offline").length;
  const unlocated = devices.filter(isUnlocated).length;

  return (
    <div className="devices-layout">
      <div className="devices-stats">
        <Panel className="device-stat-card">
          <div className="device-stat-content">
            <div className="device-stat-icon success">
              <ShieldCheck size={18} />
            </div>
            <div>
              <strong>{online}</strong>
              <span>Online</span>
            </div>
          </div>
        </Panel>

        <Panel className="device-stat-card">
          <div className="device-stat-content">
            <div className="device-stat-icon warning">
              <AlertTriangle size={18} />
            </div>
            <div>
              <strong>{degraded}</strong>
              <span>Dégradés</span>
            </div>
          </div>
        </Panel>

        <Panel className="device-stat-card">
          <div className="device-stat-content">
            <div className="device-stat-icon danger">
              <Wifi size={18} />
            </div>
            <div>
              <strong>{offline}</strong>
              <span>Offline</span>
            </div>
          </div>
        </Panel>

        <Panel className="device-stat-card">
          <div className="device-stat-content">
            <div className="device-stat-icon neutral">
              <MapPin size={18} />
            </div>
            <div>
              <strong>{unlocated}</strong>
              <span>Non placés</span>
            </div>
          </div>
        </Panel>
      </div>

      <Panel
        title="Périphériques"
        className="devices-main-panel"
        action={
          <button className="primary-button devices-add-button">
            <Plus size={16} />
            Ajouter
          </button>
        }
      >
        <div className="devices-toolbar">
          <label className="device-search">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Rechercher un device, une pièce, un type..."
            />
          </label>

          <div className="device-filters">
            <button
              className={statusFilter === "all" ? "active" : ""}
              onClick={() => setStatusFilter("all")}
            >
              Tous
            </button>
            <button
              className={statusFilter === "online" ? "active" : ""}
              onClick={() => setStatusFilter("online")}
            >
              Online
            </button>
            <button
              className={statusFilter === "degraded" ? "active" : ""}
              onClick={() => setStatusFilter("degraded")}
            >
              Dégradés
            </button>
            <button
              className={statusFilter === "offline" ? "active" : ""}
              onClick={() => setStatusFilter("offline")}
            >
              Offline
            </button>
            <button
              className={statusFilter === "unlocated" ? "active" : ""}
              onClick={() => setStatusFilter("unlocated")}
            >
              Non placés
            </button>
          </div>

          <div className="device-type-filter">
            <select
              value={typeFilter}
              onChange={(event) => setTypeFilter(event.target.value as TypeFilter)}
            >
              <option value="all">Tous les types</option>
              <option value="camera">Caméras</option>
              <option value="sensor">Capteurs</option>
              <option value="light">Lumières</option>
              <option value="lock">Serrures</option>
              <option value="siren">Sirènes</option>
              <option value="unknown">Autres</option>
            </select>
          </div>
        </div>

        <div className="devices-grid">
          {filteredDevices.map((device) => {
            const Icon = deviceIcon(device.type);
            const tone = statusTone(device.status);
            const health = deviceHealth(device);
            const battery = deviceBattery(device);

            return (
              <article className={`device-card device-${tone}`} key={device.id}>
                <div className="device-card-header">
                  <div className={`device-card-icon ${tone}`}>
                    <Icon size={20} />
                  </div>

                  <div className="device-card-title">
                    <strong>{device.name}</strong>
                    <span>{device.id}</span>
                  </div>

                  <span className={`badge ${tone}`}>{statusLabel(device.status)}</span>
                </div>

                <div className="device-card-meta">
                  <div>
                    <span>Type</span>
                    <strong>{typeLabel(device.type)}</strong>
                  </div>

                  <div>
                    <span>Pièce</span>
                    <strong>{getRoomLabel(device.node_id)}</strong>
                  </div>
                </div>

                <div className="device-health">
                  <div className="device-health-row">
                    <span>Santé</span>
                    <strong>{health}%</strong>
                  </div>
                  <div className="device-meter">
                    <span style={{ width: `${health}%` }} />
                  </div>
                </div>

                <div className="device-card-footer">
                  {battery === null ? (
                    <span className="device-small-info">
                      <Wifi size={14} />
                      Alimenté secteur
                    </span>
                  ) : (
                    <span className="device-small-info">
                      <Battery size={14} />
                      {battery}%
                    </span>
                  )}

                  <div className="device-actions">
                    <button title="Modifier">
                      <Pencil size={15} />
                    </button>
                    <button title="Supprimer">
                      <Trash2 size={15} />
                    </button>
                  </div>
                </div>
              </article>
            );
          })}
        </div>

        {filteredDevices.length === 0 && (
          <div className="empty-state">
            <Cpu size={24} />
            <strong>Aucun périphérique</strong>
            <span>Aucun device ne correspond aux filtres actifs.</span>
          </div>
        )}
      </Panel>
    </div>
  );
}