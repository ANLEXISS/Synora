import { Check, CircleAlert, Eye, Film, RefreshCw, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Panel } from "./Panel";
import { useAuth } from "../hooks/useAuth";
import { getClipMediaUrl, getClips, getIncidents, updateIncidentStatus } from "../lib/synora-api";
import { formatDateTime } from "../lib/format";
import type { SynoraClip, SynoraIncident } from "../lib/synora-types";

type IncidentEvidencePanelProps = { refreshKey?: number };

function statusLabel(status: string) {
  return { new: "Nouveau", viewed: "Vu", acknowledged: "Acquitté", resolved: "Résolu" }[status] ?? status;
}

function identityLabel(kind: string) {
  return { resident: "Résident", unknown: "Inconnu", uncertain: "Incertain", none: "Non identifié" }[kind] ?? kind;
}

function clipStatusLabel(status: string) {
  return {
    receiving: "Réception",
    processing: "Analyse",
    processed: "Analysé",
    ready: "Prêt",
    failed: "Échec",
    missing: "Manquant",
    expired: "Expiré",
  }[status] ?? status;
}

function clipTone(status: string) {
  if (status === "processed" || status === "ready") return "success";
  if (status === "failed" || status === "missing" || status === "expired") return "danger";
  return "warning";
}

export function IncidentEvidencePanel({ refreshKey = 0 }: IncidentEvidencePanelProps) {
  const auth = useAuth();
  const [incidents, setIncidents] = useState<SynoraIncident[]>([]);
  const [clips, setClips] = useState<SynoraClip[]>([]);
  const [selectedIncidentID, setSelectedIncidentID] = useState<string | null>(null);
  const [selectedClipID, setSelectedClipID] = useState<string | null>(null);
  const [mediaError, setMediaError] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const refresh = useCallback(async (signal?: AbortSignal) => {
    const results = await Promise.allSettled([getIncidents(100, signal), getClips(100, signal)]);
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
    const timer = window.setInterval(() => void refresh(controller.signal), 5000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [refresh, refreshKey]);

  const selectedIncident = useMemo(
    () => incidents.find((incident) => incident.id === selectedIncidentID) ?? null,
    [incidents, selectedIncidentID],
  );
  const selectedClip = useMemo(
    () => clips.find((clip) => clip.id === selectedClipID) ?? null,
    [clips, selectedClipID],
  );
  const incidentClips = useMemo(() => {
    if (!selectedIncident) return clips;
    const ids = new Set(selectedIncident.clip_ids ?? []);
    return ids.size > 0 ? clips.filter((clip) => ids.has(clip.id)) : clips;
  }, [clips, selectedIncident]);

  function selectIncident(incident: SynoraIncident) {
    setSelectedIncidentID(incident.id);
    setSelectedClipID(null);
    setMediaError(false);
  }

  async function changeStatus(incident: SynoraIncident, action: "view" | "acknowledge" | "resolve") {
    const key = `${incident.id}:${action}`;
    if (busy) return;
    setBusy(key);
    try {
      const updated = await updateIncidentStatus(incident.id, action);
      setIncidents((items) => items.map((item) => item.id === updated.id ? updated : item));
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "La mise à jour de l’incident a échoué.");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="dashboard-evidence-grid card-full">
      <Panel title="Incidents V1" action={<CircleAlert size={17} />}>
        {error && <div className="auth-error" role="alert">{error} <button type="button" className="text-button" onClick={() => void refresh()}>Réessayer</button></div>}
        <div className="incident-list" aria-label="Liste des incidents">
          {incidents.length === 0 ? <div className="empty-state"><Check size={20} /><strong>Aucun incident réel ouvert.</strong><span>Les incidents persistants apparaîtront ici avec leur cause et leurs preuves.</span></div> : incidents.map((incident) => (
            <div className={`incident-row incident-row-button${selectedIncidentID === incident.id ? " selected" : ""}`} key={incident.id} onClick={() => selectIncident(incident)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); selectIncident(incident); } }} role="button" tabIndex={0} aria-pressed={selectedIncidentID === incident.id}>
              <span className="incident-row-main"><strong>{incident.severity || "Incident"} · {identityLabel(incident.identity_kind)}</strong><span>{incident.cause?.reason || incident.cause?.event_type || incident.security_state} · {formatDateTime(incident.last_event_at)}</span></span>
              <span className={`badge ${incident.status === "new" ? "danger" : incident.status === "resolved" ? "success" : "warning"}`}>{statusLabel(incident.status)}</span>
              {auth.isAdmin && incident.status !== "resolved" && <span className="incident-row-actions" onClick={(event) => event.stopPropagation()}>
                {incident.status === "new" && <button type="button" className="icon-button" title="Marquer comme vu" aria-label={`Marquer ${incident.id} comme vu`} disabled={busy !== null} onClick={() => void changeStatus(incident, "view")}><Eye size={15} /></button>}
                {incident.status !== "acknowledged" && <button type="button" className="icon-button" title="Acquitter" aria-label={`Acquitter ${incident.id}`} disabled={busy !== null} onClick={() => void changeStatus(incident, "acknowledge")}><Check size={15} /></button>}
                <button type="button" className="icon-button" title="Résoudre" aria-label={`Résoudre ${incident.id}`} disabled={busy !== null} onClick={() => void changeStatus(incident, "resolve")}><RefreshCw size={15} /></button>
              </span>}
            </div>
          ))}
        </div>
      </Panel>

      <Panel title="Clips et preuves" action={<Film size={17} />}>
        {selectedIncident && <div className="evidence-detail" role="region" aria-label={`Détail de l’incident ${selectedIncident.id}`}>
          <div className="evidence-detail-header"><div><strong>{selectedIncident.id}</strong><span>{selectedIncident.security_state} · {identityLabel(selectedIncident.identity_kind)}</span></div><button type="button" className="icon-button" aria-label="Fermer le détail" onClick={() => { setSelectedIncidentID(null); setSelectedClipID(null); }}><X size={15} /></button></div>
          <p>{selectedIncident.cause?.reason || selectedIncident.cause?.event_type || "Cause non précisée."}</p>
          <span className="evidence-detail-time">Dernier événement : {formatDateTime(selectedIncident.last_event_at)}</span>
          {auth.isAdmin && selectedIncident.status !== "resolved" && <div className="evidence-detail-actions">
            {selectedIncident.status === "new" && <button type="button" className="secondary-button" disabled={busy !== null} onClick={() => void changeStatus(selectedIncident, "view")}><Eye size={15} /> Marquer vu</button>}
            {selectedIncident.status !== "acknowledged" && <button type="button" className="secondary-button" disabled={busy !== null} onClick={() => void changeStatus(selectedIncident, "acknowledge")}><Check size={15} /> Acquitter</button>}
          </div>}
        </div>}
        <div className="clip-list" aria-label="Liste des clips">
          {clips.length === 0 ? <div className="empty-state"><Film size={20} /><strong>Aucun clip conservé.</strong><span>Les clips associés aux incidents seront listés ici.</span></div> : incidentClips.map((clip) => (
            <button type="button" className={`clip-row clip-row-button${selectedClipID === clip.id ? " selected" : ""}`} key={clip.id} onClick={() => { setSelectedClipID(clip.id); setMediaError(false); }} disabled={clip.status !== "ready" && clip.status !== "processed"}>
              <span><strong>{clip.id}</strong><span>{clip.camera_id} · {formatDateTime(clip.updated_at)}</span></span><span className={`badge ${clipTone(clip.status)}`}>{clipStatusLabel(clip.status)}</span>
            </button>
          ))}
        </div>
        {selectedClip && (selectedClip.status === "ready" || selectedClip.status === "processed") && <div className="clip-player" role="region" aria-label={`Lecture du clip ${selectedClip.id}`}>
          {mediaError ? <div className="empty-state"><Film size={20} /><strong>Vidéo indisponible.</strong><span>Le média n’est pas lisible pour le moment ; les métadonnées restent disponibles.</span></div> : <video controls preload="metadata" src={getClipMediaUrl(selectedClip.id)} onError={() => setMediaError(true)} />}
        </div>}
      </Panel>
    </div>
  );
}
