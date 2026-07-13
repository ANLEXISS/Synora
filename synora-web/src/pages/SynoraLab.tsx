import { ArrowDown, ArrowUp, Bot, CheckCircle2, Eraser, History, Lock, Play, Plus, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Panel } from "../components/Panel";
import { useAuth } from "../hooks/useAuth";
import { useSynoraData } from "../hooks/useSynoraData";
import {
  clearCgeValidationHistory,
  getCgeValidationHistory,
  getEventChains,
  getRuntimeStatus,
  injectCgeValidationEvent,
  injectCgeValidationSequence,
} from "../lib/synora-api";
import { formatDangerLevel, formatEventType } from "../lib/event-chains";
import { getTopologyRooms } from "../lib/topology";
import type { CgeValidationEventPayload, CgeValidationHistoryItem, EventChain } from "../lib/synora-types";

type LabEvent = {
  id: string;
  event_type: string;
  device_id: string;
  node_id: string;
  confidence: string;
  danger_level_hint: string;
  identity: string;
};

type LabResult = {
  response: Record<string, unknown>;
  runtime: Record<string, unknown> | null;
  chains: EventChain[];
};

const eventOptions = [
  ["vision.unknown", "Inconnu détecté"],
  ["vision.identity", "Résident reconnu"],
  ["vision.uncertain", "Identité incertaine"],
  ["vision.motion", "Mouvement contextuel"],
  ["vision.weapon", "Arme détectée"],
  ["vision.fall", "Chute détectée"],
  ["camera.offline", "Caméra hors ligne"],
  ["camera.tampered", "Caméra manipulée"],
  ["device.offline", "Périphérique hors ligne"],
  ["manual.risk", "Danger manuel"],
] as const;

const dangerOptions = [
  ["", "Automatique"],
  ["low", "Faible"],
  ["medium", "Moyen"],
  ["medium_high", "Moyen élevé"],
  ["high", "Élevé"],
  ["critical", "Critique"],
] as const;

const presetDefinitions: Array<{ id: string; label: string; events: Array<Partial<LabEvent> & Pick<LabEvent, "event_type">> }> = [
  { id: "unknown-entry", label: "Inconnu à l’entrée", events: [{ event_type: "vision.unknown", node_id: "zoneA.L0.entree", confidence: "0.78", danger_level_hint: "medium" }] },
  { id: "persistent-unknown", label: "Inconnu persistant", events: [{ event_type: "vision.unknown", node_id: "zoneA.L0.entree", confidence: "0.82", danger_level_hint: "medium" }, { event_type: "vision.motion", node_id: "zoneA.L0.entree" }, { event_type: "vision.unknown", node_id: "zoneA.L0.salon", confidence: "0.84", danger_level_hint: "medium_high" }] },
  { id: "unknown-motion-weapon", label: "Inconnu + mouvement + arme", events: [{ event_type: "vision.unknown", node_id: "zoneA.L0.entree", confidence: "0.82", danger_level_hint: "medium" }, { event_type: "vision.motion", node_id: "zoneA.L0.entree" }, { event_type: "vision.weapon", node_id: "zoneA.L0.salon", confidence: "0.91", danger_level_hint: "critical" }] },
  { id: "fall", label: "Chute détectée", events: [{ event_type: "vision.identity", identity: "resident-test", node_id: "zoneA.L0.salon", confidence: "0.96", danger_level_hint: "low" }, { event_type: "vision.motion", node_id: "zoneA.L0.salon" }, { event_type: "vision.fall", node_id: "zoneA.L0.salon", confidence: "0.93", danger_level_hint: "high" }] },
  { id: "offline", label: "Caméra hors ligne", events: [{ event_type: "camera.offline", node_id: "zoneA.L0.entree", danger_level_hint: "medium" }] },
  { id: "resident", label: "Résident reconnu normal", events: [{ event_type: "vision.identity", identity: "resident-test", node_id: "zoneA.L0.entree", confidence: "0.97", danger_level_hint: "low" }] },
  { id: "intrusion", label: "Intrusion probable", events: [{ event_type: "vision.unknown", node_id: "zoneA.L0.entree", confidence: "0.81", danger_level_hint: "medium" }, { event_type: "vision.motion", node_id: "zoneA.L0.entree" }, { event_type: "camera.tampered", node_id: "zoneA.L0.entree", danger_level_hint: "high" }] },
];

function newEvent(overrides: Partial<LabEvent> = {}): LabEvent {
  return {
    id: typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
      ? crypto.randomUUID()
      : `lab-${Date.now()}-${Math.random().toString(36).slice(2)}`,
    event_type: "vision.unknown",
    device_id: "",
    node_id: "",
    confidence: "0.78",
    danger_level_hint: "medium",
    identity: "",
    ...overrides,
  };
}

function payloadFromEvent(event: LabEvent, learn: boolean, reason: string): CgeValidationEventPayload {
  const payload: CgeValidationEventPayload = {
    event_type: event.event_type,
    device_id: event.device_id.trim() || undefined,
    node_id: event.node_id.trim() || undefined,
    identity: event.identity.trim() || undefined,
    danger_level_hint: event.danger_level_hint || undefined,
    learn,
    reason: reason.trim() || "synora_lab_validation",
  };
  if (event.confidence.trim()) {
    const confidence = Number(event.confidence);
    if (Number.isFinite(confidence)) payload.confidence = confidence;
  }
  return payload;
}

function valueOf(value: unknown, fallback = "—") {
  if (typeof value === "string" && value.trim()) return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function formatHistoryDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Date indisponible" : date.toLocaleString("fr-FR");
}

export function SynoraLab() {
  const auth = useAuth();
  const data = useSynoraData();
  const rooms = useMemo(() => getTopologyRooms(data.topology), [data.topology]);
  const [draft, setDraft] = useState<LabEvent[]>([newEvent()]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [injectedCount, setInjectedCount] = useState(0);
  const [learn, setLearn] = useState(false);
  const [reason, setReason] = useState("synora_lab_validation");
  const [history, setHistory] = useState<CgeValidationHistoryItem[]>([]);
  const [result, setResult] = useState<LabResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  async function refreshHistory() {
    if (!auth.isAdmin) return;
    try {
      setHistory(await getCgeValidationHistory());
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Historique de validation indisponible.");
    }
  }

  useEffect(() => { void refreshHistory(); }, [auth.isAdmin]);

  function replaceDraft(events: LabEvent[]) {
    setDraft(events);
    setSelectedIndex(0);
    setInjectedCount(0);
    setResult(null);
    setMessage(null);
  }

  function updateEvent(index: number, changes: Partial<LabEvent>) {
    setDraft((current) => current.map((event, eventIndex) => eventIndex === index ? { ...event, ...changes } : event));
    setInjectedCount(0);
  }

  function addEvent() {
    setDraft((current) => [...current, newEvent({ device_id: current[selectedIndex]?.device_id ?? "", node_id: current[selectedIndex]?.node_id ?? "" })]);
    setSelectedIndex(draft.length);
    setInjectedCount(0);
  }

  function removeEvent(index: number) {
    if (draft.length === 1) return;
    setDraft((current) => current.filter((_, eventIndex) => eventIndex !== index));
    setSelectedIndex((current) => Math.min(current, Math.max(0, draft.length - 2)));
    setInjectedCount(0);
  }

  function moveEvent(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= draft.length) return;
    setDraft((current) => {
      const next = [...current];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
    setSelectedIndex(target);
    setInjectedCount(0);
  }

  async function refreshResult(response: Record<string, unknown>) {
    const [runtime, chains] = await Promise.allSettled([getRuntimeStatus(), getEventChains({ status: "all", limit: 8 })]);
    setResult({
      response,
      runtime: runtime.status === "fulfilled" ? runtime.value as Record<string, unknown> : null,
      chains: chains.status === "fulfilled" ? chains.value.chains : [],
    });
  }

  async function injectNext() {
    if (!auth.isAdmin || busy || injectedCount >= draft.length) return;
    setBusy(true); setError(null); setMessage(null);
    try {
      const response = await injectCgeValidationEvent(payloadFromEvent(draft[injectedCount], learn, reason));
      setInjectedCount((current) => current + 1);
      setSelectedIndex(Math.min(injectedCount + 1, draft.length - 1));
      setMessage(`Maillon ${injectedCount + 1}/${draft.length} placé dans le pipeline CGE${learn ? " · apprentissage actif" : " · sans apprentissage"}.`);
      await refreshResult(response);
      await refreshHistory();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Injection du maillon impossible.");
    } finally { setBusy(false); }
  }

  async function injectAll() {
    if (!auth.isAdmin || busy || draft.length === 0) return;
    setBusy(true); setError(null); setMessage(null);
    try {
      const response = await injectCgeValidationSequence({ events: draft.map((event) => payloadFromEvent(event, learn, reason)), learn, reason: reason.trim() || "synora_lab_validation" });
      setInjectedCount(draft.length);
      setMessage(`Chaîne de ${draft.length} maillon${draft.length > 1 ? "s" : ""} placée dans le pipeline CGE${learn ? " · apprentissage actif" : " · sans apprentissage"}.`);
      await refreshResult(response);
      await refreshHistory();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Injection de la chaîne impossible.");
    } finally { setBusy(false); }
  }

  async function clearHistory() {
    if (!auth.isAdmin || busy || !window.confirm("Effacer uniquement l’historique des validations Synora Lab ?")) return;
    setBusy(true); setError(null);
    try {
      await clearCgeValidationHistory();
      setHistory([]);
      setMessage("Historique de validation nettoyé. L’historique réel reste intact.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nettoyage de l’historique impossible.");
    } finally { setBusy(false); }
  }

  const deviceIDs = Array.from(new Set(data.devices.map((device) => device.id).filter(Boolean)));
  const nodeIDs = Array.from(new Set([...rooms.map((room) => room.id), ...draft.map((event) => event.node_id).filter(Boolean)]));
  const runtime = result?.runtime;
  const nextAvailable = auth.isAdmin && injectedCount < draft.length;

  return <div className="lab-page">
    <div className="lab-heading">
      <div className="lab-heading-icon"><Bot size={22} /></div>
      <div><h2>Synora Lab</h2><p>Construisez une chaîne contrôlée et observez sa réaction dans le pipeline CGE.</p></div>
      <span className="badge validation">Validation admin</span>
    </div>

    {!auth.isAdmin && <div className="readonly-label"><Lock size={14} /> Le constructeur et les injections sont réservés aux administrateurs.</div>}
    {error && <div className="auth-error" role="alert">{error}</div>}
    {message && <div className="cge-success" role="status"><CheckCircle2 size={16} /> {message}</div>}

    <Panel title="Constructeur de chaîne" className="lab-builder-panel" action={<span className="lab-builder-count">{draft.length} maillon{draft.length > 1 ? "s" : ""}</span>}>
      <div className="lab-rail" aria-label="Chaîne en préparation">
        <span className="lab-rail-end">Départ</span>
        {draft.map((event, index) => <button key={event.id} type="button" className={`lab-rail-item ${index === selectedIndex ? "selected" : ""} ${event.event_type === "vision.motion" ? "contextual" : ""}`} onClick={() => setSelectedIndex(index)} aria-label={`Sélectionner le maillon ${index + 1}, ${formatEventType(event.event_type)}`} aria-pressed={index === selectedIndex}><span>{index + 1}</span><strong>{formatEventType(event.event_type)}</strong></button>)}
        <span className="lab-rail-end">Ouvert</span>
      </div>

      <div className="lab-chain-list">
        {draft.map((event, index) => <article key={event.id} className={`lab-event-card ${index === selectedIndex ? "selected" : ""} ${event.event_type === "vision.motion" ? "contextual" : ""}`}>
          <button type="button" className="lab-event-card-heading" onClick={() => setSelectedIndex(index)} aria-expanded={index === selectedIndex}><span className="lab-event-index">{index + 1}</span><span><strong>{formatEventType(event.event_type)}</strong><small>{event.node_id || "Nœud non précisé"} · {event.device_id || "Périphérique non précisé"}</small></span><span className="badge neutral">{index < injectedCount ? "Injecté" : "Brouillon"}</span></button>
          <div className="lab-event-actions"><button type="button" className="icon-button" onClick={() => moveEvent(index, -1)} disabled={!auth.isAdmin || index === 0 || busy} aria-label="Déplacer le maillon vers le haut"><ArrowUp size={15} /></button><button type="button" className="icon-button" onClick={() => moveEvent(index, 1)} disabled={!auth.isAdmin || index === draft.length - 1 || busy} aria-label="Déplacer le maillon vers le bas"><ArrowDown size={15} /></button><button type="button" className="icon-button danger-icon" onClick={() => removeEvent(index)} disabled={!auth.isAdmin || draft.length === 1 || busy} aria-label="Supprimer le maillon"><Trash2 size={15} /></button></div>
          {index === selectedIndex && <div className="lab-event-form">
            <label>Type d’événement<select disabled={!auth.isAdmin || busy} value={event.event_type} onChange={(input) => updateEvent(index, { event_type: input.target.value })}>{eventOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
            <label>Périphérique<input disabled={!auth.isAdmin || busy} list="lab-device-options" value={event.device_id} onChange={(input) => updateEvent(index, { device_id: input.target.value })} placeholder="cam_03" /></label>
            <label>Pièce / nœud<input disabled={!auth.isAdmin || busy} list="lab-node-options" value={event.node_id} onChange={(input) => updateEvent(index, { node_id: input.target.value })} placeholder="zoneA.L0.entree" /></label>
            <label>Confiance<input disabled={!auth.isAdmin || busy} type="number" min="0" max="1" step="0.01" value={event.confidence} onChange={(input) => updateEvent(index, { confidence: input.target.value })} placeholder="optionnelle" /></label>
            <label>Indice danger<select disabled={!auth.isAdmin || busy} value={event.danger_level_hint} onChange={(input) => updateEvent(index, { danger_level_hint: input.target.value })}>{dangerOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
            {(event.event_type === "vision.identity" || event.event_type === "vision.uncertain") && <label>Identité / résident<input disabled={!auth.isAdmin || busy} value={event.identity} onChange={(input) => updateEvent(index, { identity: input.target.value })} placeholder="resident-test" /></label>}
          </div>}
        </article>)}
      </div>

      <div className="lab-builder-actions"><button type="button" className="secondary-button" onClick={addEvent} disabled={!auth.isAdmin || busy}><Plus size={15} /> Ajouter un maillon</button><button type="button" className="secondary-button" onClick={() => replaceDraft([])} disabled={!auth.isAdmin || busy || draft.length === 0}><Eraser size={15} /> Vider la chaîne</button><button type="button" className="secondary-button" onClick={() => replaceDraft([newEvent()])} disabled={!auth.isAdmin || busy}><RotateCcw size={15} /> Réinitialiser</button></div>
    </Panel>

    <datalist id="lab-device-options">{deviceIDs.map((id) => <option key={id} value={id} />)}</datalist>
    <datalist id="lab-node-options">{nodeIDs.map((id) => <option key={id} value={id} />)}</datalist>

    <div className="lab-columns">
      <Panel title="Presets rapides" className="lab-presets-panel"><div className="lab-preset-grid">{presetDefinitions.map((preset) => <button type="button" key={preset.id} className="lab-preset" disabled={!auth.isAdmin || busy} onClick={() => replaceDraft(preset.events.map((event) => newEvent(event)))}><strong>{preset.label}</strong><small>{preset.events.length} maillon{preset.events.length > 1 ? "s" : ""}</small></button>)}</div></Panel>
      <Panel title="Exécution contrôlée" className="lab-execution-panel"><label className="lab-reason">Motif du test<input disabled={!auth.isAdmin || busy} value={reason} onChange={(event) => setReason(event.target.value)} placeholder="synora_lab_validation" /></label><label className="lab-learning-toggle"><input disabled={!auth.isAdmin || busy} type="checkbox" checked={learn} onChange={(event) => setLearn(event.target.checked)} /><span><strong>Autoriser l’apprentissage CGE pour ce test</strong><small>{learn ? "La mémoire critique peut apprendre de cette validation." : "Par défaut : la mémoire critique réelle ne sera pas renforcée."}</small></span></label><div className="lab-injection-actions"><button type="button" className="primary-button" disabled={!nextAvailable || busy} onClick={() => void injectNext()}><Play size={15} /> Injecter le prochain maillon</button><button type="button" className="primary-button" disabled={!auth.isAdmin || busy || draft.length === 0} onClick={() => void injectAll()}><Play size={15} /> Injecter toute la chaîne</button></div><small className="lab-source-note">Chaque injection porte les marqueurs <code>source_type=validation</code> et <code>test_mode=controlled_real_test</code>.</small></Panel>
    </div>

    {result && <Panel title="Résultat du dernier test" className="lab-result-panel"><div className="lab-result-grid"><div><span>État courant</span><strong>{valueOf(runtime?.current_state ?? result.response.current_state)}</strong></div><div><span>Danger</span><strong>{formatDangerLevel(valueOf(runtime?.danger_level ?? result.response.danger_level, "none"))}</strong></div><div><span>Score</span><strong>{valueOf(runtime?.danger_score ?? result.response.danger_score)}</strong></div><div><span>Source</span><strong>{valueOf(runtime?.danger_source ?? result.response.source_type, "Validation")}</strong></div></div><div className="lab-result-details"><div><strong>Actions / blocages</strong><p>{valueOf(runtime?.blocking_reasons ?? runtime?.last_action_result ?? result.response.message, "Aucune information supplémentaire.")}</p></div><div><strong>Chaîne générée</strong>{result.chains.length === 0 ? <p>La chaîne est en file d’ingestion ou n’est pas encore disponible.</p> : <div className="lab-result-chains">{result.chains.slice(0, 3).map((chain) => <button type="button" key={chain.id} onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "cge" }))}><span className="badge validation">Validation</span><strong>{chain.title || chain.id}</strong><small>{chain.events_count} événements · {formatDangerLevel(chain.danger_level)}</small></button>)}</div>}</div></div><button type="button" className="secondary-button" onClick={() => window.dispatchEvent(new CustomEvent("synora:navigate", { detail: "cge" }))}>Ouvrir le CGE</button></Panel>}

    <Panel title="Historique des validations" className="lab-history-panel" action={<div className="lab-history-actions"><button type="button" className="secondary-button" disabled={!auth.isAdmin || busy} onClick={() => void refreshHistory()}><RefreshCw size={14} /> Actualiser</button><button type="button" className="secondary-button" disabled={!auth.isAdmin || busy || history.length === 0} onClick={() => void clearHistory()}><Trash2 size={14} /> Effacer</button></div>}>
      {!auth.isAdmin ? <div className="readonly-label"><Lock size={14} /> Historique réservé aux administrateurs.</div> : history.length === 0 ? <div className="lab-empty-history"><History size={20} /><span>Aucune validation récente.</span></div> : <div className="lab-history-list">{history.slice().reverse().slice(0, 20).map((item, index) => <div className="lab-history-row" key={`${item.validation_id}-${item.event_id}-${index}`}><div><strong>{formatEventType(item.event_type)}</strong><small>{item.node_id || item.device_id || "Emplacement non précisé"} · {formatHistoryDate(item.timestamp)}</small></div><div><span className="badge validation">Validation</span><span className="badge neutral">{item.learn ? "Apprentissage actif" : "Sans apprentissage"}</span></div></div>)}</div>}
    </Panel>
  </div>;
}
