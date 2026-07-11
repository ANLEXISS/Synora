import { Activity, Brain, Cpu, ShieldAlert, Users } from "lucide-react";
import { EventRow } from "../components/EventRow";
import { Panel } from "../components/Panel";
import { StatCard } from "../components/StatCard";
import {
  demoCriticalChains,
  demoDevices,
  demoEvents,
  demoStats,
} from "../data/demo";
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
  const demoFallback = !data.snapshot;
  const danger = demoFallback ? demoStats.dangerScore : data.dangerScore;
  const devices = demoFallback ? demoDevices : data.devices.slice(0, 6).map((device) => ({
    id: device.id,
    name: String(device["name"] ?? device.id),
    status: normalizeDeviceStatus(device),
    node: String(device.node_id ?? device.room ?? "unlocated"),
  }));
  const events = demoFallback ? demoEvents : data.events.slice(0, 6).map((event) => ({
    type: event.type ?? event.event_type ?? "event",
    title: String(event["title"] ?? event.type ?? event.event_type ?? "Événement"),
    subtitle: String(event["description"] ?? event.device_id ?? event.node_id ?? "Synora"),
    tone: (event.priority && event.priority >= 8 ? "danger" : event.priority && event.priority >= 5 ? "warning" : "neutral") as "neutral" | "warning" | "danger",
  }));
  const devicesOnline = devices.filter((device) => device.status === "online").length;
  const deviceTone = devicesTone(devicesOnline, devices.length);
  const systemState = demoFallback ? "—" : data.systemState;
  const residentsPresent = demoFallback
    ? demoStats.residentsPresent
    : data.residents.filter((resident) => resident.state === "present").length;

  return (
    <div className="dashboard-grid">
      <StatCard
        title="État système"
        value={systemState}
        label={demoFallback ? "Démo fallback" : "API synora-api"}
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
          <span className={`badge ${demoFallback ? "warning" : data.connection === "connected" ? "success" : "warning"}`}>
            {demoFallback ? "Démo fallback" : data.connection === "connected" ? "Connecté" : "Dégradé"}
          </span>
        }
      >
        <div className="event-list">
          {events.map((event, index) => (
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
        <div className={`risk-card risk-${dangerTone(0.72)}`}>
          <div className="risk-score">
            <ShieldAlert size={22} />
            <strong>72%</strong>
          </div>

          <p>
            Chaîne critique reconnue en mode observation. Aucune action réelle
            exécutée.
          </p>

          <div className="risk-meter">
            <span style={{ width: "72%" }} />
          </div>

          <div className="risk-meta">
            <span>expected_state</span>
            <strong>suspicious</strong>
          </div>

          <button className="primary-button">Inspecter le CGE</button>
        </div>
      </Panel>

      <Panel
        title="Périphériques clés"
        className="card-side"
        action={<Cpu size={17} />}
      >
        <div className="compact-list">
          {devices.map((device) => (
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
          {demoCriticalChains.map((chain) => (
            <div className="critical-row" key={chain.id}>
              <div className={`critical-icon ${dangerTone(chain.score)}`}>
                <Activity size={17} />
              </div>

              <div>
                <strong>{chain.label}</strong>
                <span>{chain.id}</span>
              </div>

              <div className={`critical-score ${dangerTone(chain.score)}`}>
                <span>{chain.state}</span>
                <strong>{chain.score.toFixed(2)}</strong>
              </div>
            </div>
          ))}
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

          <button className="secondary-button">Voir les résidents</button>
        </div>
      </Panel>
    </div>
  );
}
