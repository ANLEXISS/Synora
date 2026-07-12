import { Brain, CheckCircle2, ChevronRight, Lock, RefreshCw, ShieldAlert, SlidersHorizontal, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useAuth } from "../hooks/useAuth";
import { useSynoraData } from "../hooks/useSynoraData";
import { getTopologyRooms } from "../lib/topology";
import {
  getCgeFeedback,
  getCgeSecurityProfile,
  getCriticalChain,
  getCriticalChains,
  updateCgeSecurityProfile,
} from "../lib/synora-api";
import { formatDangerLevel } from "../lib/event-chains";
import { buildSecurityProfilePayload, formatSecurityMode } from "../lib/cge";
import type {
  CgeChainFeedback,
  CgeEvaluationFeedback,
  CgeSecurityProfile,
  CriticalChainMemory,
} from "../lib/synora-types";
import { LiveEvents } from "./LiveEvents";

type CgeTab = "live" | "known" | "settings" | "corrections";

export function Cge() {
  const auth = useAuth();
  const [tab, setTab] = useState<CgeTab>("live");

  return (
    <div className="cge-page">
      <div className="cge-page-heading">
        <div className="cge-page-heading-icon"><Brain size={22} /></div>
        <div><h2>CGE — Cognitive Guard Engine</h2><p>Chaînes d’événements, raisonnement moteur et réglages de sécurité.</p></div>
      </div>
      <nav className="cge-tabs" aria-label="Sections CGE">
        <CgeTabButton active={tab === "live"} onClick={() => setTab("live")}>Live</CgeTabButton>
        <CgeTabButton active={tab === "known"} onClick={() => setTab("known")}>Chaînes connues</CgeTabButton>
        <CgeTabButton active={tab === "settings"} onClick={() => setTab("settings")}>Réglages sécurité</CgeTabButton>
        <CgeTabButton active={tab === "corrections"} onClick={() => setTab("corrections")}>Corrections</CgeTabButton>
      </nav>
      {tab === "live" && <LiveEvents />}
      {tab === "known" && <KnownChains />}
      {tab === "settings" && <SecuritySettings isAdmin={auth.isAdmin} />}
      {tab === "corrections" && <CgeCorrections isAdmin={auth.isAdmin} />}
    </div>
  );
}

function CgeTabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: string }) {
  return <button type="button" className={`cge-tab ${active ? "active" : ""}`} aria-selected={active} onClick={onClick}>{children}</button>;
}

function KnownChains() {
  const [chains, setChains] = useState<CriticalChainMemory[]>([]);
  const [selected, setSelected] = useState<CriticalChainMemory | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    setLoading(true);
    try { setChains(await getCriticalChains()); setError(null); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Impossible de charger les chaînes connues."); }
    finally { setLoading(false); }
  }

  useEffect(() => { void refresh(); }, []);

  async function showDetails(chain: CriticalChainMemory) {
    setSelected(chain);
    try { setSelected(await getCriticalChain(chain.id)); } catch { setError("Les détails de cette chaîne connue sont indisponibles."); }
  }

  return (
    <div className="cge-tab-content">
      <div className="cge-section-toolbar"><div><h3>Chaînes critiques connues</h3><p>Motifs mémorisés par le moteur pour accélérer la reconnaissance des situations critiques.</p></div><button className="secondary-button" type="button" onClick={() => void refresh()}><RefreshCw size={15} /> Actualiser</button></div>
      {error && <div className="auth-error" role="alert">{error}</div>}
      {loading ? <div className="cge-empty">Chargement des chaînes connues…</div> : chains.length === 0 ? <div className="cge-empty"><Brain size={22} /><span>Aucune chaîne critique connue pour l’instant.</span></div> : (
        <div className="critical-chains-grid">{chains.map((chain) => <CriticalChainCard key={chain.id} chain={chain} onDetails={showDetails} />)}</div>
      )}
      {selected && <CriticalChainDetail chain={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function CriticalChainCard({ chain, onDetails }: { chain: CriticalChainMemory; onDetails: (chain: CriticalChainMemory) => void }) {
  return <article className="critical-chain-card">
    <header><div><strong>{chain.summary || "Chaîne critique mémorisée"}</strong><small>{chain.template_id || chain.id}</small></div><span className="badge danger">{formatDangerLevel(chain.max_danger_level)}</span></header>
    <p>{chain.learned_reason || "Motif critique appris à partir de chaînes observées."}</p>
    <div className="critical-chain-stats"><span><strong>{chain.occurrences}</strong> occurrences</span><span><strong>{Math.round(chain.confidence * 100)} %</strong> confiance</span><span><strong>{chain.max_danger_score.toFixed(2)}</strong> score max</span></div>
    <div className="critical-chain-tags">{chain.significant_event_types.slice(0, 5).map((type) => <span key={type}>{type}</span>)}</div>
    <footer><small>Dernière occurrence : {formatDate(chain.last_seen)}</small><button type="button" className="secondary-button" onClick={() => onDetails(chain)}>Détails <ChevronRight size={15} /></button></footer>
  </article>;
}

function CriticalChainDetail({ chain, onClose }: { chain: CriticalChainMemory; onClose: () => void }) {
  return <div className="cge-modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="cge-modal" role="dialog" aria-modal="true" aria-labelledby="critical-chain-title">
      <header><div><span>Chaîne critique connue</span><h2 id="critical-chain-title">{chain.summary || chain.template_id}</h2></div><button className="icon-button" type="button" onClick={onClose} aria-label="Fermer"><X size={18} /></button></header>
      <div className="cge-modal-content"><div className="critical-chain-detail-grid"><DetailList title="Trajectoire d’état" values={chain.typical_state_path} /><DetailList title="Trajectoire de danger" values={chain.typical_danger_path.map(formatDangerLevel)} /><DetailList title="Nœuds" values={chain.node_pattern} /><DetailList title="Types d’événements" values={chain.significant_event_types} /></div><DetailList title="Chaînes récentes" values={chain.recent_chain_ids} /><DetailList title="Actions recommandées" values={chain.recommended_actions} /><DetailList title="Actions prises" values={chain.actions_taken} /><DetailList title="Résultats" values={chain.outcomes} /><p className="cge-feedback-note">Chaîne représentative : {chain.representative_chain_id || "—"} · Apprentissage : {chain.learned_reason || "—"}</p></div>
    </section>
  </div>;
}

function DetailList({ title, values }: { title: string; values: string[] }) {
  if (!values.length) return null;
  return <div className="cge-detail-list"><h4>{title}</h4><div>{values.map((value) => <span key={value}>{value}</span>)}</div></div>;
}

function SecuritySettings({ isAdmin }: { isAdmin: boolean }) {
  const data = useSynoraData();
  const rooms = useMemo(() => getTopologyRooms(data.topology), [data.topology]);
  const [profile, setProfile] = useState<CgeSecurityProfile | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { getCgeSecurityProfile().then(setProfile).catch((cause) => setError(cause instanceof Error ? cause.message : "Profil sécurité indisponible.")).finally(() => setLoading(false)); }, []);

  if (loading || !profile) return <div className="cge-tab-content"><div className="cge-empty">Chargement du profil sécurité…</div>{error && <div className="auth-error">{error}</div>}</div>;
  const currentProfile = profile;

  function update<K extends keyof CgeSecurityProfile>(key: K, value: CgeSecurityProfile[K]) { setProfile((current) => current ? { ...current, [key]: value } : current); setMessage(null); }
  function toggleRoom(key: "critical_rooms" | "ignored_motion_rooms", roomID: string) { const values = currentProfile[key].includes(roomID) ? currentProfile[key].filter((value) => value !== roomID) : [...currentProfile[key], roomID]; update(key, values); }
  async function save() { setSaving(true); setError(null); try { setProfile(await updateCgeSecurityProfile(buildSecurityProfilePayload(currentProfile))); setMessage("Profil sécurité enregistré. Les prochaines évaluations utiliseront ces réglages."); } catch (cause) { setError(cause instanceof Error ? cause.message : "Impossible d’enregistrer le profil."); } finally { setSaving(false); } }

  return <div className="cge-tab-content">
    <div className="cge-section-toolbar"><div><h3>Réglages sécurité CGE</h3><p>Le profil influence les futures évaluations. Les chaînes historiques restent inchangées.</p></div>{!isAdmin && <span className="readonly-label"><Lock size={14} /> Lecture seule</span>}</div>
    {error && <div className="auth-error" role="alert">{error}</div>}{message && <div className="cge-success" role="status"><CheckCircle2 size={16} /> {message}</div>}
    <div className="security-mode-grid">{(["relaxed", "balanced", "strict", "paranoid"] as const).map((mode) => <button key={mode} type="button" disabled={!isAdmin} className={`security-mode-card ${profile.mode === mode ? "selected" : ""}`} onClick={() => update("mode", mode)}><strong>{formatSecurityMode(mode)}</strong><span>{mode === "relaxed" ? "Moins d’alertes, plus de tolérance." : mode === "balanced" ? "Équilibre recommandé." : mode === "strict" ? "Sensibilité renforcée." : "Réponse maximale aux signaux."}</span></button>)}</div>
    {(profile.mode === "strict" || profile.mode === "paranoid") && <div className="cge-warning"><ShieldAlert size={17} /> Ce mode peut augmenter les faux positifs et les notifications.</div>}
    <div className="security-settings-grid">
      <label>Sensibilité globale <output>{Math.round(profile.global_sensitivity * 100)} %</output><input disabled={!isAdmin} type="range" min="0" max="1" step="0.05" value={profile.global_sensitivity} onChange={(event) => update("global_sensitivity", Number(event.target.value))} /></label>
      <label>Multiplicateur nuit <output>{profile.night_sensitivity_multiplier.toFixed(1)}×</output><input disabled={!isAdmin} type="number" min="0.1" max="5" step="0.1" value={profile.night_sensitivity_multiplier} onChange={(event) => update("night_sensitivity_multiplier", Number(event.target.value))} /></label>
      <label>Multiplicateur système armé <output>{profile.armed_sensitivity_multiplier.toFixed(1)}×</output><input disabled={!isAdmin} type="number" min="0.1" max="5" step="0.1" value={profile.armed_sensitivity_multiplier} onChange={(event) => update("armed_sensitivity_multiplier", Number(event.target.value))} /></label>
      <label>Tolérance personne inconnue<select disabled={!isAdmin} value={profile.unknown_person_tolerance} onChange={(event) => update("unknown_person_tolerance", event.target.value as CgeSecurityProfile["unknown_person_tolerance"])}><option value="low">Faible</option><option value="medium">Moyenne</option><option value="high">Élevée</option></select></label>
      <label>Danger minimum notification<select disabled={!isAdmin} value={profile.minimum_notify_danger_level} onChange={(event) => update("minimum_notify_danger_level", event.target.value as CgeSecurityProfile["minimum_notify_danger_level"])}>{dangerOptions()}</select></label>
      <label>Danger minimum action automatique<select disabled={!isAdmin} value={profile.minimum_auto_action_danger_level} onChange={(event) => update("minimum_auto_action_danger_level", event.target.value as CgeSecurityProfile["minimum_auto_action_danger_level"])}>{dangerOptions()}</select></label>
      <label>Persistance personne inconnue (secondes)<input disabled={!isAdmin} type="number" min="1" max="86400" value={profile.unknown_persistence_seconds} onChange={(event) => update("unknown_persistence_seconds", Number(event.target.value))} /></label>
      <label>Inactivité significative (secondes)<input disabled={!isAdmin} type="number" min="1" max="86400" value={profile.significant_inactivity_timeout_seconds} onChange={(event) => update("significant_inactivity_timeout_seconds", Number(event.target.value))} /></label>
    </div>
    <div className="security-rooms-grid"><RoomSelector title="Pièces critiques" rooms={rooms} values={profile.critical_rooms} disabled={!isAdmin} onToggle={(id) => toggleRoom("critical_rooms", id)} /><RoomSelector title="Ignorer les mouvements dans" rooms={rooms} values={profile.ignored_motion_rooms} disabled={!isAdmin} onToggle={(id) => toggleRoom("ignored_motion_rooms", id)} /></div>
    <div className="security-switches">{(["require_human_confirmation_for_siren", "allow_automatic_lights", "allow_automatic_recording", "allow_automatic_notifications"] as const).map((key) => <label key={key}><input disabled={!isAdmin} type="checkbox" checked={profile[key]} onChange={(event) => update(key, event.target.checked)} /> {securityLabel(key)}</label>)}</div>
    {isAdmin && <button type="button" className="primary-button" disabled={saving} onClick={() => void save()}>{saving ? "Enregistrement…" : "Enregistrer le profil sécurité"}</button>}
  </div>;
}

function RoomSelector({ title, rooms, values, disabled, onToggle }: { title: string; rooms: Array<{ id: string; name: string }>; values: string[]; disabled: boolean; onToggle: (id: string) => void }) {
  return <fieldset className="security-room-selector"><legend>{title}</legend>{rooms.length === 0 ? <small>Aucune pièce configurée.</small> : rooms.map((room) => <label key={room.id}><input disabled={disabled} type="checkbox" checked={values.includes(room.id)} onChange={() => onToggle(room.id)} /> {room.name} <small>{room.id}</small></label>)}</fieldset>;
}

function CgeCorrections({ isAdmin }: { isAdmin: boolean }) {
  const [feedback, setFeedback] = useState<Array<CgeEvaluationFeedback | CgeChainFeedback>>([]);
  const [loading, setLoading] = useState(true);
  useEffect(() => { getCgeFeedback().then(setFeedback).catch(() => undefined).finally(() => setLoading(false)); }, []);
  return <div className="cge-tab-content"><div className="cge-section-toolbar"><div><h3>Corrections moteur</h3><p>Les corrections sont versionnées et influencent les futures évaluations ; l’événement brut reste inchangé.</p></div>{!isAdmin && <span className="readonly-label"><Lock size={14} /> Lecture seule</span>}</div>{loading ? <div className="cge-empty">Chargement des corrections…</div> : feedback.length === 0 ? <div className="cge-empty"><SlidersHorizontal size={22} /><span>Aucune correction enregistrée.</span></div> : <div className="cge-feedback-list">{feedback.slice().reverse().map((item, index) => <article key={item.id || index}><span className="badge neutral">{"correction_type" in item ? item.correction_type : item.final_outcome}</span><strong>Chaîne {item.chain_id}</strong><p>{item.note || "Aucune note."}</p><small>{item.created_by || "admin"} · {item.created_at ? formatDate(item.created_at) : "—"}</small></article>)}</div>}</div>;
}

function dangerOptions() { return [<option key="none" value="none">Aucun</option>, <option key="low" value="low">Faible</option>, <option key="medium" value="medium">Moyen</option>, <option key="high" value="high">Élevé</option>, <option key="critical" value="critical">Critique</option>]; }
function securityLabel(key: string) { return { require_human_confirmation_for_siren: "Confirmer humainement la sirène", allow_automatic_lights: "Autoriser les lumières automatiques", allow_automatic_recording: "Autoriser l’enregistrement automatique", allow_automatic_notifications: "Autoriser les notifications automatiques" }[key] || key; }
function formatDate(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("fr-FR"); }
