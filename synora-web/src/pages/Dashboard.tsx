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

function systemTone(state: string): "success" | "warning" | "danger" {
  const normalized = state.toLowerCase();

  if (normalized.includes("break") || normalized.includes("intrusion")) return "danger";
  if (normalized.includes("suspicious") || normalized.includes("degraded")) return "warning";
  return "success";
}

export function Dashboard() {
  const danger = demoStats.dangerScore;
  const deviceTone = devicesTone(demoStats.devicesOnline, demoStats.devicesTotal);

  return (
    <div className="dashboard-grid">
      <StatCard
        title="État système"
        value={demoStats.systemState}
        label="Aucune menace active"
        tone={systemTone(demoStats.systemState)}
      />

      <StatCard
        title="Danger"
        value={danger.toFixed(2)}
        label="Score global courant"
        tone={dangerTone(danger)}
      />

      <StatCard
        title="Devices"
        value={`${demoStats.devicesOnline}/${demoStats.devicesTotal}`}
        label="Périphériques actifs"
        tone={deviceTone}
      />

      <StatCard
        title="Résidents"
        value={demoStats.residentsPresent}
        label="Présent actuellement"
        tone={demoStats.residentsPresent > 0 ? "success" : "warning"}
      />

      <Panel
        title="Événements récents"
        className="card-wide"
        action={<span className="badge warning">Live waiting</span>}
      >
        <div className="event-list">
          {demoEvents.map((event) => (
            <EventRow
              key={`${event.type}-${event.title}`}
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
          {demoDevices.map((device) => (
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