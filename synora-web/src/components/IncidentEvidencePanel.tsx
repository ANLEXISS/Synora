import { Check, CircleAlert, Film, Eye, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Panel } from "./Panel";
import { useAuth } from "../hooks/useAuth";
import { getClips, getIncidents, updateIncidentStatus } from "../lib/synora-api";
import { formatDateTime } from "../lib/format";
import type { SynoraClip, SynoraIncident } from "../lib/synora-types";

function statusLabel(status: string) {
  return { new: "Nouveau", viewed: "Vu", acknowledged: "Acquitté", resolved: "Résolu" }[status] ?? status;
}

function identityLabel(kind: string) {
  return { resident: "Résident", unknown: "Inconnu", uncertain: "Incertain", none: "Non identifié" }[kind] ?? kind;
}

function clipStatusLabel(status: string) {
  return { ready: "Prêt", processing: "Analyse", processed: "Analysé", failed: "Échec", missing: "Manquant", expired: "Expiré" }[status] ?? status;
}

export function IncidentEvidencePanel() {
  const auth = useAuth();
  const [incidents, setIncidents] = useState<SynoraIncident[]>([]);
  const [clips, setClips] = useState<SynoraClip[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const results = await Promise.allSettled([getIncidents(20, signal), getClips(20, signal)]);
    if (signal?.aborted) return;
    const incidentResult = results[0];
    const clipResult = results[1];
    if (incidentResult.status === "fulfilled") setIncidents(incidentResult.value);
    if (clipResult.status === "fulfilled") setClips(clipResult.value);
    const failed = results.find((result) => result.status === "rejected");
    setError(failed ? "Les preuves V1 sont momentanément indisponibles." : null);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [refresh]);

  async function changeStatus(incident: SynoraIncident, action: "view" | "acknowledge" | "resolve") {
    setBusy(`${incident.id}:${action}`);
    try {
      const updated = await updateIncidentStatus(incident.id, action);
      setIncidents((items) => items.map((item) => item.id === updated.id ? updated : item));
      setError(null);
    } catch {
      setError("La mise à jour de l’incident a échoué.");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="dashboard-evidence-grid card-full">
      <Panel title="Incidents V1" action={<CircleAlert size={17} />}>
        {error && <div className="auth-error" role="alert">{error} <button type="button" className="text-button" onClick={() => void refresh()}>Réessayer</button></div>}
        <div className="incident-list">
          {incidents.length === 0 ? <div className="empty-state"><Check size={20} /><strong>Aucun incident réel ouvert.</strong><span>Les incidents persistants apparaîtront ici avec leur cause et leurs preuves.</span></div> : incidents.slice(0, 5).map((incident) => (
            <div className="incident-row" key={incident.id}>
              <div className="incident-row-main"><strong>{incident.severity || "Incident"} · {identityLabel(incident.identity_kind)}</strong><span>{incident.cause?.reason || incident.cause?.event_type || incident.security_state} · {formatDateTime(incident.last_event_at)}</span></div>
              <span className={`badge ${incident.status === "new" ? "danger" : incident.status === "resolved" ? "success" : "warning"}`}>{statusLabel(incident.status)}</span>
              {auth.isAdmin && incident.status !== "resolved" && <div className="incident-row-actions">
                {incident.status === "new" && <button type="button" className="icon-button" title="Marquer comme vu" disabled={busy !== null} onClick={() => void changeStatus(incident, "view")}><Eye size={15} /></button>}
                {incident.status !== "acknowledged" && <button type="button" className="icon-button" title="Acquitter" disabled={busy !== null} onClick={() => void changeStatus(incident, "acknowledge")}><Check size={15} /></button>}
                <button type="button" className="icon-button" title="Résoudre" disabled={busy !== null} onClick={() => void changeStatus(incident, "resolve")}><RefreshCw size={15} /></button>
              </div>}
            </div>
          ))}
        </div>
      </Panel>
      <Panel title="Clips et preuves" action={<Film size={17} />}>
        <div className="clip-list">
          {clips.length === 0 ? <div className="empty-state"><Film size={20} /><strong>Aucun clip conservé.</strong><span>Les clips associés aux incidents seront listés ici.</span></div> : clips.slice(0, 6).map((clip) => (
            <div className="clip-row" key={clip.id}><div><strong>{clip.id}</strong><span>{clip.camera_id} · {formatDateTime(clip.updated_at)}</span></div><span className={`badge ${clip.status === "processed" || clip.status === "ready" ? "success" : clip.status === "failed" || clip.status === "missing" ? "danger" : "warning"}`}>{clipStatusLabel(clip.status)}</span></div>
          ))}
        </div>
      </Panel>
    </div>
  );
}
