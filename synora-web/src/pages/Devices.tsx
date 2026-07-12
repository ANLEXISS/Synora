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
  Save,
  Search,
  ShieldCheck,
  Trash2,
  Wifi,
  X,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Panel } from "../components/Panel";
import { DevicePlacementMap } from "../components/DevicePlacementMap";
import { SynoraCameraPairingWizard } from "../components/SynoraCameraPairingWizard";
import { useSynoraData } from "../hooks/useSynoraData";
import { useAuth } from "../hooks/useAuth";
import { SynoraApiError } from "../lib/api";
import { buildDeviceMutationPayload, deviceToEditForm, type DeviceEditForm } from "../lib/device-form";
import { deleteDevice, getDevices, updateDevice } from "../lib/synora-api";
import { normalizeTopologyDevices } from "../lib/topology";
import { getRoomLabel, getTopologyRooms } from "../lib/topology";
import type { SynoraDevice } from "../lib/synora-types";
import type { TopologyDevice } from "../data/demo";

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
  if (status === "degraded" || status === "pending") return "warning";

  return "danger";
}

function statusLabel(status: DeviceStatus) {
  if (status === "online") return "Online";
  if (status === "degraded") return "Dégradé";
  if (status === "pending") return "En attente";

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

function deviceHealth(device: TopologyDevice) {
  if (device.status === "online") return 96;
  if (device.status === "degraded") return 61;
  if (device.status === "pending") return 42;

  return 8;
}

function deviceBattery(device: TopologyDevice) {
  if (device.type === "light") return null;
  if (device.status === "offline") return 0;
  if (device.status === "degraded" || device.status === "pending") return 42;

  return 87;
}

function isUnlocated(device: TopologyDevice) {
  return !device.node_id || device.node_id === "unlocated";
}

export function Devices() {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<DeviceFilter>("all");
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");
  const [notice, setNotice] = useState<string | null>(null);
  const [pairingOpen, setPairingOpen] = useState(false);
  const [deletingID, setDeletingID] = useState<string | null>(null);
  const [editingDevice, setEditingDevice] = useState<SynoraDevice | null>(null);
  const [editForm, setEditForm] = useState<DeviceEditForm | null>(null);
  const [placementOpen, setPlacementOpen] = useState(false);
  const [editBusy, setEditBusy] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);
  const [togglingDeviceID, setTogglingDeviceID] = useState<string | null>(null);
  const [enabledOverrides, setEnabledOverrides] = useState<Record<string, boolean>>({});

  const data = useSynoraData();
  const auth = useAuth();
  const topology = data.topology;
  const editRooms = useMemo(() => getTopologyRooms(data.topology), [data.topology]);
  const devices: TopologyDevice[] = normalizeTopologyDevices(data.devices, topology);
  const visibleDevices = devices;

  const filteredDevices = useMemo(() => {
    const query = search.trim().toLowerCase();

    return visibleDevices.filter((device) => {
      const matchSearch =
        query.length === 0 ||
        device.id.toLowerCase().includes(query) ||
        device.name.toLowerCase().includes(query) ||
        device.type.toLowerCase().includes(query) ||
        getRoomLabel(device.node_id, topology).toLowerCase().includes(query);

      const matchStatus =
        statusFilter === "all" ||
        (statusFilter === "unlocated" && isUnlocated(device)) ||
        device.status === statusFilter;

      const matchType = typeFilter === "all" || device.type === typeFilter;

      return matchSearch && matchStatus && matchType;
    });
  }, [visibleDevices, search, statusFilter, typeFilter, topology]);

  const online = visibleDevices.filter((device) => device.status === "online").length;
  const degraded = visibleDevices.filter((device) => device.status === "degraded").length;
  const offline = visibleDevices.filter((device) => device.status === "offline").length;
  const unlocated = visibleDevices.filter(isUnlocated).length;

  function openEditor(id: string) {
    if (!auth.isAdmin) {
      setNotice("Accès refusé : action réservée administrateur.");
      return;
    }
    const source = data.devices.find((device) => device.id === id);
    if (!source) {
      setNotice("Modification indisponible : le backend ne confirme pas ce périphérique.");
      return;
    }
    setEditingDevice(source);
    setEditForm(deviceToEditForm(source));
    setPlacementOpen(false);
    setEditError(null);
  }

  function closeEditor() {
    setEditingDevice(null);
    setEditForm(null);
    setPlacementOpen(false);
    setEditError(null);
  }

  async function handleSaveDevice() {
    if (!auth.isAdmin || !editingDevice || !editForm) {
      setEditError("Accès refusé : action réservée administrateur.");
      return;
    }
    const payload = buildDeviceMutationPayload(editForm);
    if (!payload.name) {
      setEditError("Le nom du périphérique est obligatoire.");
      return;
    }

    setEditBusy(true);
    setEditError(null);
    try {
      const updated = await updateDevice(editingDevice.id, payload);
      const responseNodeID = String(updated.node_id ?? updated.room ?? "unlocated");
      if (
        updated.id !== editingDevice.id ||
        String(updated.name ?? updated.display_name ?? "").trim() !== payload.name ||
        responseNodeID !== payload.node_id
      ) {
        throw new Error("device update response was not confirmed");
      }

      await data.refresh();
      const devicesAfterRefresh = await getDevices();
      const confirmed = devicesAfterRefresh.find((device) => device.id === editingDevice.id);
      if (
        !confirmed ||
        String(confirmed.name ?? confirmed.display_name ?? "").trim() !== payload.name ||
        String(confirmed.node_id ?? confirmed.room ?? "unlocated") !== payload.node_id
      ) {
        throw new Error("device update was not confirmed after refresh");
      }

      closeEditor();
      setNotice("Périphérique modifié.");
    } catch (error) {
      setEditError(error instanceof SynoraApiError && error.status === 403
        ? "Accès refusé : action réservée administrateur."
        : "La modification du périphérique n’a pas été confirmée par le backend.");
    } finally {
      setEditBusy(false);
    }
  }

  async function handleDelete(id: string) {
    if (!auth.isAdmin) {
      setNotice("Accès refusé : action réservée administrateur.");
      return;
    }
    if (data.error) {
      setNotice("Suppression indisponible : affichage en fallback démo.");
      return;
    }
    if (!window.confirm("Supprimer ce périphérique ? Les clips existants ne seront pas supprimés.")) {
      return;
    }

    setDeletingID(id);
    setNotice(null);
    try {
      const result = await deleteDevice(id);
      if (result.status !== "deleted" || result.device_id !== id) {
        throw new Error("backend deletion response was not confirmed");
      }
      await data.refresh();
      const devicesAfterRefresh = await getDevices();
      if (devicesAfterRefresh.some((device) => device.id === id)) {
        throw new Error("deleted device is still present after refresh");
      }
      setNotice("Périphérique supprimé. Les clips existants sont conservés.");
    } catch {
      setNotice("La suppression n’a pas été confirmée par le backend.");
    } finally {
      setDeletingID(null);
    }
  }

  async function handleToggleDevice(id: string, nextEnabled: boolean) {
    if (!auth.isAdmin) {
      setNotice("Accès refusé : action réservée administrateur.");
      return;
    }
    if (data.error) {
      setNotice("Modification indisponible : affichage en fallback démo.");
      return;
    }

    const displayedDevice = visibleDevices.find((device) => device.id === id);
    const previousEnabled = displayedDevice?.enabled !== false;
    setTogglingDeviceID(id);
    setNotice(null);

    try {
      const updated = await updateDevice(id, { enabled: nextEnabled });
      if (updated.id !== id || updated.enabled !== nextEnabled) {
        throw new Error("device enabled response was not confirmed");
      }

      const refreshResults = await Promise.allSettled([data.refresh(), getDevices()]);
      const devicesRefresh = refreshResults[1];
      if (devicesRefresh.status === "rejected") {
        throw devicesRefresh.reason;
      }
      const confirmed = devicesRefresh.value.find((device) => device.id === id);
      if (!confirmed || confirmed.enabled !== nextEnabled) {
        throw new Error("device enabled update was not confirmed after refresh");
      }

      setEnabledOverrides((current) => ({ ...current, [id]: confirmed.enabled === true }));
      setNotice(`Périphérique ${nextEnabled ? "activé" : "désactivé"}.`);
    } catch (error) {
      setEnabledOverrides((current) => ({ ...current, [id]: previousEnabled }));
      setNotice(error instanceof SynoraApiError && error.status === 403
        ? "Accès refusé : action réservée administrateur."
        : "La modification n’a pas été confirmée par le backend.");
    } finally {
      setTogglingDeviceID(null);
    }
  }

  async function refreshAndVerify(deviceID: string) {
    try {
      await data.refresh();
      const devicesAfterRefresh = await getDevices();
      if (!devicesAfterRefresh.some((device) => device.id === deviceID)) return false;
      setNotice("Caméra Synora ajoutée.");
      return true;
    } catch {
      setNotice("L’ajout de la caméra n’a pas été confirmé par le backend.");
      return false;
    }
  }

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
        action={auth.isAdmin ? (
          <button className="primary-button devices-add-button" onClick={() => { setNotice(null); setPairingOpen(true); }}>
            <Plus size={16} />
            Ajouter
          </button>
        ) : undefined}
      >
        {data.error && <div className="auth-error" role="alert">{data.error} <button type="button" className="secondary-button" onClick={() => void data.refresh()}>Réessayer</button></div>}
        {notice && <div className="auth-error">{notice}</div>}
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
              className={statusFilter === "pending" ? "active" : ""}
              onClick={() => setStatusFilter("pending")}
            >
              En attente
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
            const sourceDevice = data.devices.find((item) => item.id === device.id);
            const enabled = enabledOverrides[device.id] ?? device.enabled !== false;
            const toggling = togglingDeviceID === device.id;

            return (
              <article className={`device-card device-${tone}`} key={device.id}>
                <div className="device-card-header">
                  <div className={`device-card-icon ${tone}`}>
                    <Icon size={20} />
                  </div>

                  <div className="device-card-title">
                    <strong>{device.name}</strong>
                    <span>{device.id}</span>
                    {auth.isAdmin && device.node_id && <small>{device.node_id}</small>}
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
                    <strong>{getRoomLabel(device.node_id, topology)}</strong>
                  </div>
                </div>

                {(sourceDevice?.vendor || sourceDevice?.model || sourceDevice?.serial) && (
                  <div className="device-card-details">
                    {[sourceDevice.vendor, sourceDevice.model, sourceDevice.serial]
                      .filter((value): value is string => typeof value === "string" && value.trim().length > 0)
                      .join(" · ")}
                  </div>
                )}

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

                  <div className="device-card-controls">
                    {auth.isAdmin ? (
                      <button
                        type="button"
                        className={`device-toggle ${enabled ? "is-enabled" : "is-disabled"} ${toggling ? "is-loading" : ""}`}
                        role="switch"
                        aria-checked={enabled}
                        aria-busy={toggling}
                        aria-label={`Activer ou désactiver ${device.name}`}
                        disabled={toggling}
                        onClick={() => void handleToggleDevice(device.id, !enabled)}
                      >
                        <span className="device-toggle-track" aria-hidden="true">
                          <span className="device-toggle-thumb" />
                        </span>
                        <span className="device-toggle-label">{enabled ? "Activé" : "Désactivé"}</span>
                      </button>
                    ) : (
                      <span className={`device-enabled-badge ${enabled ? "is-enabled" : "is-disabled"}`}>
                        {enabled ? "Activé" : "Désactivé"}
                      </span>
                    )}

                    {auth.isAdmin && (
                      <div className="device-actions">
                        <button aria-label="Modifier" title="Modifier" onClick={() => openEditor(device.id)}>
                          <Pencil size={15} />
                        </button>
                        <button
                          aria-label="Supprimer"
                          title="Supprimer"
                          disabled={deletingID === device.id}
                          onClick={() => void handleDelete(device.id)}
                        >
                          <Trash2 size={15} />
                        </button>
                      </div>
                    )}
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
            <span>{data.error ? "Les périphériques ne sont pas disponibles." : "Aucun appareil enregistré ou aucun ne correspond aux filtres actifs."}</span>
          </div>
        )}
      </Panel>
      {editingDevice && editForm && auth.isAdmin && (
        <div className="device-edit-backdrop" role="presentation">
          <section className="device-edit-modal" role="dialog" aria-modal="true" aria-labelledby="device-edit-title">
            <header className="device-edit-header">
              <div>
                <span>Configuration du périphérique</span>
                <h2 id="device-edit-title">Modifier {String(editingDevice.name ?? editingDevice.id)}</h2>
              </div>
              <button type="button" className="icon-button" onClick={closeEditor} aria-label="Fermer">
                <X size={19} />
              </button>
            </header>

            <div className="device-edit-content">
              {editError && <div className="wizard-error" role="alert">{editError}</div>}
              <div className="device-edit-grid">
                <label>ID<input value={editingDevice.id} readOnly /></label>
                <label>Type<input value={String(editingDevice.type ?? "—")} readOnly /></label>
                <label>Nom affiché<input value={editForm.name} onChange={(event) => setEditForm({ ...editForm, name: event.target.value })} maxLength={128} /></label>
                <label>Vendor<input value={String(editingDevice.vendor ?? "—")} readOnly /></label>
                <label>Modèle<input value={String(editingDevice.model ?? "—")} readOnly /></label>
                <label>Serial<input value={String(editingDevice.serial ?? "—")} readOnly /></label>
                <label className="device-edit-wide">Pièce actuelle
                  <select value={editForm.node_id} onChange={(event) => setEditForm({ ...editForm, node_id: event.target.value })}>
                    <option value="unlocated">Non placé</option>
                    {editRooms.map((room) => <option key={room.id} value={room.id}>{room.name}{room.floorName ? ` · ${room.floorName}` : ""}</option>)}
                  </select>
                </label>
                <label className="device-edit-checkbox"><input type="checkbox" checked={editForm.enabled} onChange={(event) => setEditForm({ ...editForm, enabled: event.target.checked })} /> Périphérique activé</label>
              </div>

              <button type="button" className="secondary-button device-placement-toggle" onClick={() => setPlacementOpen(!placementOpen)}>
                <MapPin size={16} /> {placementOpen ? "Masquer le plan" : "Choisir sur le plan"}
              </button>

              {placementOpen && (
                <DevicePlacementMap
                  topology={data.topology}
                  devices={data.devices}
                  selectedDeviceId={editingDevice.id}
                  selectedNodeId={editForm.node_id}
                  onSelectRoom={(nodeID) => setEditForm({ ...editForm, node_id: nodeID })}
                />
              )}
            </div>

            <footer className="device-edit-footer">
              <button type="button" className="secondary-button" onClick={closeEditor} disabled={editBusy}>Annuler</button>
              <button type="button" className="primary-button" onClick={() => void handleSaveDevice()} disabled={editBusy || !editForm.name.trim()}>
                <Save size={16} /> {editBusy ? "Enregistrement…" : "Enregistrer"}
              </button>
            </footer>
          </section>
        </div>
      )}
      {pairingOpen && auth.isAdmin && (
        <SynoraCameraPairingWizard
          topology={data.topology}
          onClose={() => setPairingOpen(false)}
          onPaired={refreshAndVerify}
        />
      )}
    </div>
  );
}
