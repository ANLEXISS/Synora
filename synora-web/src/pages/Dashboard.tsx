import { Activity, Brain, Cpu, ShieldAlert, Users } from "lucide-react";
import { EventRow } from "../components/EventRow";
import { Panel } from "../components/Panel";
import { StatCard } from "../components/StatCard";
import { useSynoraData } from "../hooks/useSynoraData";


function dangerTone(score: number): "success" | "warning" | "danger" {
  if (score >= 0.75) return "danger";
  if (score >= 0.35) return "warning";
  return "success";
}

function devicesTone(online: number, total: number): "success" | "warning" | "danger" {
  if (total === 0) return "danger";

  const ratio = online / total;

  if (ratio === 1) return "success";
  if (ratio < 0.5) return "danger";
  return "warning";
}

function systemTone(state: string): "neutral" | "success" | "warning" | "danger" {
  const normalized = state.toLowerCase();

  if (normalized === "—") return "neutral";
  if (normalized.includes("break") || normalized.includes("intrusion")) return "danger";
  if (
    normalized.includes("suspicious") ||
    normalized.includes("suspect")
  ) return "warning";
  return "success";
}

function normalizeDeviceStatus(device: {
  enabled?: unknown;
  online?: unknown;
  status?: unknown;
}): string {
  if (device.enabled === false) return "offline";
  if (typeof device.status === "string" && device.status.trim()) {
    return device.status;
  }
  return device.online === true ? "online" : "offline";
}

export function Dashboard() {
  const data = useSynoraData();
  const danger = data.dangerScore;
  const devices = data.devices.slice(0, 6).map((device) => ({
    id: device.id,
    name: String(device["name"] ?? device.id),
    status: normalizeDeviceStatus(device),
    node: String(device.node_id ?? device.room ?? "unlocated"),
  }));
  const events = data.events.slice(0, 6).map((event) => ({
    type: event.type ?? event.event_type ?? "event",
    title: String(event["title"] ?? event.type ?? event.event_type ?? "Événement"),
    subtitle: String(event["description"] ?? event.device_id ?? event.node_id ?? "Synora"),
    tone: (event.priority && event.priority >= 8 ? "danger" : event.priority && event.priority >= 5 ? "warning" : "neutral") as "neutral" | "warning" | "danger",
  }));
  const devicesOnline = devices.filter((device) => device.status === "online").length;
  const deviceTone = devicesTone(devicesOnline, devices.length);
  const systemState = data.systemState;
  const residentsPresent = data.residents.filter((resident) => resident.state === "present").length;

  return (
    <div className="dashboard-grid">
      <StatCard
        title="État système"
        value={systemState}
        label={data.error ? "Données partielles" : "API synora-api"}
        tone={systemTone(systemState)}
      />

      <StatCard
        title="Danger"
        value={danger.toFixed(2)}
        label="Score global courant"
        tone={dangerTone(danger)}
      />

      <StatCard
        title="Devices"
        value={`${devicesOnline}/${devices.length}`}
        label="Périphériques actifs"
        tone={deviceTone}
      />

      <StatCard
        title="Résidents"
        value={residentsPresent}
        label="Présent actuellement"
        tone={residentsPresent > 0 ? "success" : "warning"}
      />

      <Panel
        title="Événements récents"
        className="card-wide"
        action={
          <span className={`badge ${data.connection === "connected" ? "success" : "warning"}`}>
            {data.connection === "connected" ? "Connecté" : "Dégradé"}
          </span>
        }
      >
        {data.error && <div className="auth-error" role="alert">{data.error}</div>}
        <div className="event-list">
          {events.length === 0 ? <div className="empty-state"><strong>Aucun événement récent.</strong><span>Les événements apparaîtront ici lorsqu’ils seront reçus.</span></div> : events.map((event, index) => (
            <EventRow
              key={`${event.type}-${event.title}-${index}`}
              type={event.type}
              title={event.title}
              subtitle={event.subtitle}
              tone={event.tone}
            />
          ))}
        </div>
      </Panel>

      <Panel title="CGE Risk" className="card-side">
        <div className={`risk-card risk-${dangerTone(danger)}`}>
          <div className="risk-score">
            <ShieldAlert size={22} />
            <strong>{Math.round(danger * 100)}%</strong>
          </div>

          <p>
            Chaîne critique reconnue en mode observation. Aucune action réelle
            exécutée.
          </p>

          <div className="risk-meter">
            <span style={{ width: `${Math.round(danger * 100)}%` }} />
          </div>

          <div className="risk-meta">
            <span>expected_state</span>
            <strong>suspicious</strong>
          </div>

          <button type="button" className="primary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "live" }))}>Inspecter le CGE</button>
        </div>
      </Panel>

      <Panel
        title="Périphériques clés"
        className="card-side"
        action={<Cpu size={17} />}
      >
        <div className="compact-list">
          {devices.length === 0 ? <div className="empty-state"><strong>Aucun appareil enregistré.</strong><span>Ajoutez un appareil depuis la page Périphériques.</span></div> : devices.map((device) => (
            <div className="compact-row" key={device.id}>
              <div>
                <strong>{device.name}</strong>
                <span>{device.node}</span>
              </div>

              <span
                className={`badge ${
                  device.status === "online" ? "success" : "warning"
                }`}
              >
                {device.status}
              </span>
            </div>
          ))}
        </div>
      </Panel>

      <Panel
        title="Chaînes critiques"
        className="card-wide"
        action={<Brain size={17} />}
      >
        <div className="critical-list">
          <div className="empty-state"><Activity size={20} /><strong>Aucune chaîne critique connue pour l’instant.</strong><span>Les chaînes apparaîtront après des incidents ou simulations.</span></div>
        </div>
      </Panel>

      <Panel
        title="Présence"
        className="card-full"
        action={<Users size={17} />}
      >
        <div className="presence-banner">
          <div>
            <strong>Alexis reconnu</strong>
            <span>Dernière présence confirmée · zoneA.L0.salon</span>
          </div>

          <button type="button" className="secondary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "residents" }))}>Voir les résidents</button>
        </div>
      </Panel>
    </div>
  );
}
