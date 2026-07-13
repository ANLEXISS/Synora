import { Activity, Brain, Cpu, ShieldAlert, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { EventRow } from "../components/EventRow";
import { Panel } from "../components/Panel";
import { StatCard } from "../components/StatCard";
import { useSynoraData } from "../hooks/useSynoraData";
import { getCriticalChains } from "../lib/synora-api";
import { armSecurity, clearManualRisk, disarmSecurity, setManualRisk } from "../lib/synora-api";
import {
  filterDashboardEvents,
  normalizeDashboardDanger,
  normalizeDashboardResidents,
  normalizeDashboardSystemState,
  type DashboardDanger,
} from "../lib/dashboard";
import { formatDateTime } from "../lib/format";
import { getHumanCriticalChainSummary, getHumanCriticalChainTitle, normalizeCriticalChainMemory } from "../lib/cge";
import { formatDangerLevel } from "../lib/event-chains";
import { securityModeLabel, type SecurityMode } from "../lib/security-mode";
import { useAuth } from "../hooks/useAuth";
import type { CriticalChainMemory, SynoraDevice, SynoraEvent } from "../lib/synora-types";

function levelTone(level: DashboardDanger["level"]): "neutral" | "success" | "warning" | "danger" {
  if (level === "high" || level === "critical") return "danger";
  if (level === "medium" || level === "medium_high") return "warning";
  if (level === "low" || level === "none") return "success";
  return "neutral";
}

function systemTone(state: string): "neutral" | "success" | "warning" | "danger" {
  if (state === "Inconnu") return "neutral";
  if (state === "Effraction" || state === "Intrusion") return "danger";
  if (state === "Suspect") return "warning";
  return "success";
}

function normalizeDeviceStatus(device: SynoraDevice): string {
  if (device.enabled === false) return "disabled";
  if (device.status === "degraded") return "degraded";
  if (device.status === "online" || device.online === true || device.active === true) return "online";
  return "offline";
}

function deviceStatusLabel(status: string) {
  if (status === "disabled") return "Désactivé";
  if (status === "degraded") return "Dégradé";
  if (status === "online") return "En ligne";
  return "Hors ligne";
}

function deviceTone(status: string): "success" | "warning" | "danger" {
  if (status === "online") return "success";
  if (status === "degraded") return "warning";
  return "danger";
}

function isDeviceActive(device: SynoraDevice) {
  return device.enabled !== false && (device.online === true || device.active === true || device.status === "online");
}

function isDeviceOffline(device: SynoraDevice) {
  return device.enabled !== false && device.status !== "degraded" && !isDeviceActive(device);
}

function eventText(event: SynoraEvent, key: string, fallback: string) {
  const value = event[key];
  return typeof value === "string" && value.trim() ? value : fallback;
}

function sourceLabel(danger: DashboardDanger) {
  if (danger.simulated || danger.source.toLowerCase() === "test" || danger.source.toLowerCase() === "simulated") return "Simulation";
  if (danger.source.toLowerCase() === "manual" || danger.manualRiskActive) return "Manuel";
  if (danger.source === "none") return "Aucune";
  return danger.source;
}

function riskDescription(danger: DashboardDanger, systemState: string) {
  if (danger.level !== "none" && danger.level !== "unknown") {
    return `État courant : ${systemState.toLowerCase()} · score ${danger.score.toFixed(2)}.`;
  }
  if (danger.realOpenChainsCount === 0 && danger.openChainsCount > 0) return "Aucune chaîne réelle ouverte ; les chaînes visibles sont simulées.";
  if (danger.visionWorkerStatus === "unavailable") return "Aucun risque actif ; les modèles vision sont indisponibles.";
  if (danger.lastRealSignificantEventAt) return "Aucun risque actif ; le dernier risque réel est expiré.";
  return "Aucun risque actif ; aucune chaîne réelle ouverte.";
}

export function Dashboard() {
  const data = useSynoraData();
  const auth = useAuth();
  const [criticalChains, setCriticalChains] = useState<CriticalChainMemory[]>([]);
  const [criticalError, setCriticalError] = useState<string | null>(null);
  const [criticalLoading, setCriticalLoading] = useState(true);
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [controlBusy, setControlBusy] = useState(false);
  const [controlError, setControlError] = useState<string | null>(null);
  const [manualDuration, setManualDuration] = useState(60);

  async function refreshCriticalChains() {
    setCriticalLoading(true);
    try {
      setCriticalChains((await getCriticalChains()).map(normalizeCriticalChainMemory));
      setCriticalError(null);
    } catch (cause) {
      setCriticalError(cause instanceof Error ? cause.message : "Impossible de charger les chaînes critiques.");
    } finally {
      setCriticalLoading(false);
    }
  }

  useEffect(() => { void refreshCriticalChains(); }, []);

  const snapshot = data.snapshot;
  const systemState = normalizeDashboardSystemState(data.runtimeStatus, snapshot);
  const danger = normalizeDashboardDanger(data.runtimeStatus, snapshot);
  const residents = normalizeDashboardResidents(data.residents, snapshot);
  const allDevices = data.devices;
  const devices = allDevices.slice(0, 6).map((device) => {
    const status = normalizeDeviceStatus(device);
    return {
      id: device.id,
      name: String(device.name ?? device.display_name ?? device.id),
      status,
      node: String(device.node_id ?? device.room ?? "unlocated"),
    };
  });
  const devicesTotal = allDevices.length;
  const devicesActive = allDevices.filter(isDeviceActive).length;
  const devicesEnabled = allDevices.filter((device) => device.enabled !== false).length;
  const devicesOnline = allDevices.filter((device) => device.enabled !== false && (device.online === true || device.status === "online")).length;
  const devicesOffline = allDevices.filter(isDeviceOffline).length;
  const deviceToneValue = devicesTotal === 0 ? "danger" : devicesOffline > 0 ? "warning" : "success";
  const events = filterDashboardEvents(data.events, showDiagnostics).slice(0, 6);
  const realCriticalChains = criticalChains.filter((chain) => chain.source !== "simulation" && chain.simulated !== true);
  const hiddenSimulationCount = criticalChains.length - realCriticalChains.length;
  const cgeRiskActive = danger.level !== "none" && danger.level !== "unknown";

  async function runControl(action: () => Promise<unknown>, confirmation?: string) {
    if (confirmation && !window.confirm(confirmation)) return;
    setControlBusy(true);
    setControlError(null);
    try {
      await action();
      await data.refresh();
    } catch (cause) {
      setControlError(cause instanceof Error ? cause.message : "Contrôle sécurité impossible.");
    } finally {
      setControlBusy(false);
    }
  }

  function arm(mode: Exclude<SecurityMode, "home">) {
    void runControl(
      () => armSecurity({ mode, reason: mode === "high_security" ? "Sécurité élevée activée depuis le Dashboard" : "Mode sécurité activé depuis le Dashboard" }),
      mode === "high_security" ? "Armer la sécurité élevée ? Personne n’est censé être présent." : undefined,
    );
  }

  function triggerManualRisk(level: "low" | "medium" | "high" | "critical") {
    void runControl(
      () => setManualRisk({ danger_level: level, duration_seconds: manualDuration, reason: `Risque manuel ${level} depuis le Dashboard` }),
      level === "high" || level === "critical" ? `Injecter un danger manuel ${level} pendant ${manualDuration}s ?` : undefined,
    );
  }

  return (
    <div className="dashboard-grid">
      <div className="dashboard-overview">
        <StatCard
          title="État système"
          value={systemState}
          label={data.error ? "Données partielles" : "Statut courant"}
          tone={systemTone(systemState)}
        />

        <StatCard
          title="Devices"
          value={`${devicesActive}/${devicesTotal}`}
          label={`${devicesEnabled} activés · ${devicesOnline} en ligne · ${devicesOffline} hors ligne`}
          tone={deviceToneValue}
        />

        <StatCard
          title="Résidents"
          value={`${residents.present}/${residents.known}`}
          label={`Présents maintenant · ${residents.known} connus`}
          tone={residents.present > 0 ? "success" : "warning"}
        />
      </div>

      <Panel title="Contrôle sécurité" className="card-full">
        {controlError && <div className="auth-error" role="alert">{controlError}</div>}
        <div className="security-control-status" aria-label="Résumé sécurité">
          <div className="security-summary-item"><span>Mode</span><strong>{securityModeLabel(data.securityMode.mode)}</strong></div>
          <div className="security-summary-item"><span>Armé</span><strong>{data.securityMode.armed ? "Oui" : "Non"}</strong></div>
          <div className="security-summary-item"><span>Occupation attendue</span><strong>{data.securityMode.expected_occupancy === "empty" ? "Personne" : data.securityMode.expected_occupancy === "occupied" ? "Occupé" : "Inconnue"}</strong></div>
          <div className="security-summary-item"><span>Danger manuel</span><strong>{danger.manualRiskActive ? `Actif · ${formatDangerLevel(danger.manualRiskLevel || danger.level)}` : "Inactif"}</strong></div>
        </div>
        {auth.isAdmin ? <>
          <div className="security-control-group">
            <div className="security-control-group-title"><strong>Mode sécurité</strong><span>Contexte durable du système</span></div>
            <div className="security-control-actions">
              <button type="button" className={`security-mode-action ${data.securityMode.mode === "home" ? "selected" : ""}`} aria-pressed={data.securityMode.mode === "home"} disabled={controlBusy} onClick={() => void runControl(() => disarmSecurity({ reason: "Désarmement depuis le Dashboard" }))}>Repos / Désarmer</button>
              <button type="button" className={`security-mode-action ${data.securityMode.mode === "night" ? "selected" : ""}`} aria-pressed={data.securityMode.mode === "night"} disabled={controlBusy} onClick={() => arm("night")}>Mode nuit</button>
              <button type="button" className={`security-mode-action ${data.securityMode.mode === "away" ? "selected" : ""}`} aria-pressed={data.securityMode.mode === "away"} disabled={controlBusy} onClick={() => arm("away")}>Absent</button>
              <button type="button" className={`security-mode-action ${data.securityMode.mode === "high_security" ? "selected" : ""}`} aria-pressed={data.securityMode.mode === "high_security"} disabled={controlBusy} onClick={() => arm("high_security")}>Sécurité élevée</button>
            </div>
          </div>
          <div className="security-control-group">
            <div className="security-control-group-heading"><div className="security-control-group-title"><strong>Danger manuel</strong><span>Forçage temporaire</span></div><details className="security-options"><summary>Durée : {manualDuration >= 60 ? `${manualDuration / 60} min` : `${manualDuration} s`}</summary><label>Durée<select value={manualDuration} disabled={controlBusy} onChange={(event) => setManualDuration(Number(event.target.value))}><option value={30}>30 s</option><option value={60}>60 s</option><option value={300}>5 min</option><option value={900}>15 min</option></select></label></details></div>
            <div className="security-control-actions">
              {(["low", "medium", "high", "critical"] as const).map((level) => <button key={level} type="button" className={`security-danger-action security-danger-${level} ${danger.manualRiskActive && danger.manualRiskLevel === level ? "selected" : ""}`} aria-pressed={danger.manualRiskActive && danger.manualRiskLevel === level} disabled={controlBusy} onClick={() => triggerManualRisk(level)}>Danger {formatDangerLevel(level)}</button>)}
              <button type="button" className="security-mode-action" disabled={controlBusy || !danger.manualRiskActive} onClick={() => void runControl(() => clearManualRisk({ reason: "Annulation depuis le Dashboard" }))}>Annuler danger manuel</button>
            </div>
          </div>
        </> : <small>Lecture seule : les contrôles de sécurité sont réservés aux administrateurs.</small>}
      </Panel>

      <Panel
        title="Événements récents"
        className="dashboard-events card-wide"
        action={
          <div className="dashboard-panel-actions">
            <button type="button" className="text-button" onClick={() => setShowDiagnostics((value) => !value)}>
              {showDiagnostics ? "Masquer les diagnostics runtime" : "Afficher diagnostics runtime"}
            </button>
            <span className={`badge ${data.connection === "connected" ? "success" : "warning"}`}>
              {data.connection === "connected" ? "Connecté" : "Dégradé"}
            </span>
          </div>
        }
      >
        {data.error && <div className="auth-error" role="alert">{data.error}</div>}
        <div className="event-list">
          {events.length === 0 ? <div className="empty-state"><strong>Aucun événement significatif récent.</strong><span>Les diagnostics runtime sont masqués par défaut.</span></div> : events.map((event, index) => {
            const type = String(event.type ?? event.event_type ?? "event");
            const priority = typeof event.priority === "number" ? event.priority : 0;
            return (
              <EventRow
                key={`${type}-${event.id ?? index}`}
                type={type}
                title={eventText(event, "title", type)}
                subtitle={eventText(event, "description", eventText(event, "device_id", eventText(event, "node_id", "Synora")))}
                tone={(priority >= 8 || type === "manual.risk" || type === "action.result" ? "danger" : priority >= 5 ? "warning" : "neutral") as "neutral" | "warning" | "danger"}
              />
            );
          })}
        </div>
      </Panel>

      <Panel title="CGE Risk" className="dashboard-risk card-side">
        <div className={`risk-card risk-${cgeRiskActive ? levelTone(danger.level) : "success"}`}>
          <div className="risk-score">
            <ShieldAlert size={22} />
            <strong>{danger.score.toFixed(2)}</strong>
          </div>

          <p>
            {cgeRiskActive
              ? `Risque ${formatDangerLevel(danger.level).toLowerCase()} · état système ${systemState.toLowerCase()}.`
              : riskDescription(danger, systemState)}
          </p>

          <div className="risk-badges">
            <span className={`badge ${levelTone(danger.level)}`}>{cgeRiskActive ? `Risque ${formatDangerLevel(danger.level).toLowerCase()}` : "Aucun risque actif"}</span>
            <span className={`badge ${danger.simulated ? "simulation" : danger.source === "none" ? "neutral" : "success"}`}>{sourceLabel(danger)}</span>
          </div>

          <div className="risk-meter">
            <span style={{ width: `${Math.round(danger.score * 100)}%` }} />
          </div>

          <div className="risk-meta">
            <span>Source</span>
            <strong>{sourceLabel(danger)}</strong>
          </div>
          {danger.lastActionRequestAt && <div className="risk-meta"><span>Action récente</span><strong>{formatDateTime(danger.lastActionRequestAt)}</strong></div>}
          {danger.blockingReasons.length > 0 && <div className="risk-meta"><span>Blocages</span><strong>{danger.blockingReasons.join(" · ")}</strong></div>}

          <button type="button" className="primary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "live" }))}>Inspecter le CGE</button>
        </div>
      </Panel>

      <Panel title="Périphériques clés" className="card-side" action={<Cpu size={17} />}>
        <div className="compact-list">
          {devices.length === 0 ? <div className="empty-state"><strong>Aucun appareil enregistré.</strong><span>Ajoutez un appareil depuis la page Périphériques.</span></div> : devices.map((device) => (
            <div className="compact-row" key={device.id}>
              <div><strong>{device.name}</strong><span>{device.node}</span></div>
              <span className={`badge ${deviceTone(device.status)}`}>{deviceStatusLabel(device.status)}</span>
            </div>
          ))}
        </div>
      </Panel>

      <Panel title="Chaînes critiques" className="card-wide" action={<Brain size={17} />}>
        {criticalError && <div className="auth-error" role="alert">{criticalError} <button type="button" className="secondary-button" onClick={() => void refreshCriticalChains()}>Réessayer</button></div>}
        <div className="critical-list">
          {criticalLoading ? <div className="empty-state"><Activity size={20} /><span>Chargement des chaînes critiques…</span></div> : realCriticalChains.length === 0 ? <div className="empty-state"><Activity size={20} /><strong>Aucune chaîne critique réelle connue.</strong><span>{hiddenSimulationCount > 0 ? `${hiddenSimulationCount} chaîne${hiddenSimulationCount > 1 ? "s" : ""} simulée${hiddenSimulationCount > 1 ? "s" : ""} masquée${hiddenSimulationCount > 1 ? "s" : ""}.` : "Les chaînes réelles apparaîtront après des incidents ou observations confirmés."}</span><button type="button" className="secondary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "cge" }))}>Ouvrir le CGE</button></div> : realCriticalChains.slice(0, 3).map((chain) => <div className="critical-row" key={chain.id}><div><strong>{getHumanCriticalChainTitle(chain)}</strong><span>{getHumanCriticalChainSummary(chain)}</span></div><span className="badge danger">{chain.occurrences} occurrences</span></div>)}
        </div>
      </Panel>

      <Panel title="Présence" className="card-full" action={<Users size={17} />}>
        <div className="presence-banner">
          <div>
            <strong>Présents maintenant : {residents.present}</strong>
            <span>Résidents connus : {residents.known}</span>
            {residents.latestLastSeen && <span>Dernière présence : {formatDateTime(residents.latestLastSeen)}</span>}
          </div>
          <button type="button" className="secondary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "residents" }))}>Voir les résidents</button>
        </div>
      </Panel>
    </div>
  );
}
