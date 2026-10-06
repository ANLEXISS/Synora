import { Activity, CircleHelp, MapPin, UserRound } from "lucide-react";
import { usePilotState } from "../lib/pilotState";

export function PilotStateCard() {
  const { state, error } = usePilotState();
  return <section className="pilot-state-card" aria-labelledby="pilot-state-title">
    <div className="pilot-state-heading"><div><span><Activity size={15} /> État observé</span><strong id="pilot-state-title">État du pilote</strong></div><small>{state ? `Révision ${state.revision}` : "Lecture seule"}</small></div>
    {error && <p className="pilot-state-message is-warning">{error}</p>}
    {!state ? <p className="pilot-state-message"><CircleHelp size={16} /> Aucun état Core observé pour le moment.</p> : <div className="pilot-state-grid">
      <div><UserRound size={16} /><span>Présence</span><strong>{state.presence?.human_present ? "Humaine observée" : "Aucune présence observée"}</strong></div>
      <div><MapPin size={16} /><span>Zone</span><strong>{label(state.topology)}</strong></div>
      <div><Activity size={16} /><span>Épisode</span><strong>{label(state.episode?.phase)}</strong></div>
      <div><Activity size={16} /><span>Santé caméra</span><strong>{label(state.vision?.camera_health_status)}</strong></div>
    </div>}
  </section>;
}

function label(value: string | undefined) {
  if (!value) return "Non observé";
  return value.replaceAll("_", " ");
}
