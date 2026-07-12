import {
  Activity,
  CheckCircle2,
  Clock3,
  Cpu,
  Eye,
  Radio,
  ShieldAlert,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Panel } from "../components/Panel";
import { EventChainTimeline } from "../components/EventChainTimeline";
import { useAuth } from "../hooks/useAuth";
import { useSynoraData } from "../hooks/useSynoraData";
import { buildWsUrl } from "../lib/config";
import { getCgeFeedback, getEventChain, getEventChains, submitCgeChainFeedback, submitCgeEvaluationFeedback } from "../lib/synora-api";
import { buildChainFeedbackPayload, buildEvaluationFeedbackPayload } from "../lib/cge";
import {
  dangerTone,
  formatChainDuration,
  formatClosedReason,
  formatDangerLevel,
  getChainRoomLabel,
  mergeChainUpdate,
  sortEventChains,
  type EventChainUpdate,
} from "../lib/event-chains";
import type { ApiTopologyNode, CgeChainFeedback, CgeEvaluationFeedback, ChainEvaluation, EventChain, EventChainEvent, SynoraDevice, SynoraWsMessage } from "../lib/synora-types";

type TransportStatus = "connecting" | "live" | "reconnecting" | "polling";
type CorrectionTarget =
  | { kind: "evaluation"; chain: EventChain; event: EventChainEvent; evaluation: ChainEvaluation }
  | { kind: "chain"; chain: EventChain };

function stateLabel(state: string | undefined) {
  switch (state) {
    case "suspicious": return "Suspect";
    case "activity": return "Activité";
    case "intrusion": return "Intrusion";
    case "break-in": return "Effraction";
    case "idle": return "Inactif";
    default: return state?.trim() || "État inconnu";
  }
}

function statusLabel(status: TransportStatus) {
  switch (status) {
    case "live": return "Live connecté";
    case "polling": return "Polling";
    case "reconnecting": return "Reconnexion";
    default: return "Connexion";
  }
}

function relativeTime(value: string | undefined, now = Date.now()) {
  if (!value) return "—";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "—";
  const seconds = Math.max(0, Math.floor((now - timestamp) / 1000));
  if (seconds < 5) return "à l’instant";
  if (seconds < 60) return `il y a ${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `il y a ${minutes} min`;
  return `il y a ${Math.floor(minutes / 60)} h`;
}

export function LiveEvents() {
  const auth = useAuth();
  const topologyData = useSynoraData();
  const [chainsByID, setChainsByID] = useState<Record<string, EventChain>>({});
  const [transport, setTransport] = useState<TransportStatus>("connecting");
  const [lastUpdateAt, setLastUpdateAt] = useState<Date | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedChainID, setSelectedChainID] = useState<string | null>(null);
  const [selectedChain, setSelectedChain] = useState<EventChain | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [selectedFeedback, setSelectedFeedback] = useState<CgeEvaluationFeedback[]>([]);
  const [correctionTarget, setCorrectionTarget] = useState<CorrectionTarget | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const selectedChainIDRef = useRef<string | null>(null);

  useEffect(() => {
    let active = true;
    let socket: WebSocket | null = null;
    let reconnectTimer: number | null = null;
    let pollingTimer: number | null = null;
    let reconnectDelay = 1000;

    const merge = (update: EventChainUpdate) => {
      const id = update.id ?? update.chain_id;
      if (!id || !active) return;
      setChainsByID((current) => ({
        ...current,
        [id]: mergeChainUpdate(current[id], update),
      }));
      setSelectedChain((current) => current?.id === id ? mergeChainUpdate(current, update) : current);
      if (selectedChainIDRef.current === id) {
        void getEventChain(id).then((detail) => {
          if (active && selectedChainIDRef.current === id) setSelectedChain(detail);
        }).catch(() => undefined);
      }
      setLastUpdateAt(new Date());
    };

    const refresh = async () => {
      try {
        const response = await getEventChains({ status: "all", limit: 100 });
        if (!active) return;
        const nextChains = Object.fromEntries(response.chains.map((chain) => [chain.id, chain]));
        setChainsByID(nextChains);
        setSelectedChain((current) => current && nextChains[current.id] ? nextChains[current.id] : current);
        setLastUpdateAt(new Date());
        setError(null);
        setTransport((current) => current === "live" ? current : "polling");
      } catch (cause) {
        if (!active) return;
        setError(cause instanceof Error ? cause.message : "Impossible de charger les chaînes.");
        setTransport((current) => current === "live" ? current : "polling");
      }
    };

    const stopPolling = () => {
      if (pollingTimer !== null) window.clearInterval(pollingTimer);
      pollingTimer = null;
    };

    const startPolling = () => {
      if (pollingTimer !== null) return;
      void refresh();
      pollingTimer = window.setInterval(() => void refresh(), 5000);
    };

    const connect = () => {
      if (!active || typeof WebSocket === "undefined") {
        setTransport("polling");
        startPolling();
        return;
      }
      setTransport((current) => current === "live" ? current : "reconnecting");
      socket = new WebSocket(buildWsUrl("/api/ws"));
      socket.onopen = () => {
        if (!active) return;
        reconnectDelay = 1000;
        stopPolling();
        setTransport("live");
        setError(null);
      };
      socket.onmessage = (message) => {
        try {
          const parsed = JSON.parse(message.data) as SynoraWsMessage;
          if (!parsed.type?.startsWith("event.chain.") && parsed.type !== "engine.evaluation.updated") return;
          const raw = parsed.data ?? parsed.payload;
          if (!raw || typeof raw !== "object" || Array.isArray(raw)) return;
          merge(raw as EventChainUpdate);
        } catch {
          setError("Message WebSocket invalide.");
        }
      };
      socket.onerror = () => {
        if (!active) return;
        setTransport("reconnecting");
        startPolling();
      };
      socket.onclose = () => {
        if (!active) return;
        setTransport("reconnecting");
        startPolling();
        reconnectTimer = window.setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      };
    };

    void refresh();
    connect();
    return () => {
      active = false;
      stopPolling();
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
      socket?.close();
    };
    // The transport lifecycle must be mounted once per page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshNonce]);

  const chains = useMemo(() => Object.values(chainsByID), [chainsByID]);
  const openChains = useMemo(() => sortEventChains(chains, "open"), [chains]);
  const closedChains = useMemo(() => sortEventChains(chains, "closed"), [chains]);

  async function showDetails(chain: EventChain) {
    selectedChainIDRef.current = chain.id;
    setSelectedChainID(chain.id);
    setSelectedChain(chain);
    setDetailLoading(true);
    try {
      const [detail, feedback] = await Promise.all([getEventChain(chain.id), getCgeFeedback({ chain_id: chain.id })]);
      setSelectedChain(detail);
      setSelectedFeedback(feedback.filter((item): item is CgeEvaluationFeedback => "event_id" in item));
    } catch {
      setError("Les détails de cette chaîne sont indisponibles.");
    } finally {
      setDetailLoading(false);
    }
  }

  function closeDetails() {
    selectedChainIDRef.current = null;
    setSelectedChainID(null);
    setSelectedChain(null);
    setSelectedFeedback([]);
    setCorrectionTarget(null);
  }

  async function submitCorrection(payload: CgeEvaluationFeedback | CgeChainFeedback) {
    if (payload && "event_id" in payload) {
      await submitCgeEvaluationFeedback(payload);
    } else {
      await submitCgeChainFeedback(payload);
    }
    setCorrectionTarget(null);
    setNotice("Correction enregistrée. Elle influencera les futures évaluations ; l’événement brut reste inchangé.");
    if (selectedChainIDRef.current) {
      const feedback = await getCgeFeedback({ chain_id: selectedChainIDRef.current });
      setSelectedFeedback(feedback.filter((item): item is CgeEvaluationFeedback => "event_id" in item));
    }
  }

  return (
    <div className="live-events-layout">
      <div className="live-events-status-row">
        <span className={`live-events-transport ${transport}`}>
          {transport === "live" ? <Radio size={14} /> : transport === "polling" ? <Clock3 size={14} /> : <Activity size={14} />}
          {statusLabel(transport)}
        </span>
        <span className="live-events-last-update">
          Dernière mise à jour {relativeTime(lastUpdateAt?.toISOString())}
        </span>
      </div>

      {error && <div className="auth-error" role="alert">{error} <button type="button" className="secondary-button" onClick={() => setRefreshNonce((value) => value + 1)}>Réessayer</button></div>}
      {notice && <div className="cge-success" role="status">{notice}</div>}

      <ChainSection
        title="En cours"
        icon={<ShieldAlert size={18} />}
        chains={openChains}
        devices={topologyData.devices}
        topology={topologyData.topology}
        empty="Aucune chaîne en cours. Lance une simulation ou attends un événement réel."
        onDetails={showDetails}
      />

      <ChainSection
        title="Récemment clôturées"
        icon={<CheckCircle2 size={18} />}
        chains={closedChains}
        devices={topologyData.devices}
        topology={topologyData.topology}
        empty="Aucune chaîne clôturée récente."
        closed
        onDetails={showDetails}
      />

      {selectedChainID && (
        <ChainDetail
          chain={selectedChain}
          devices={topologyData.devices}
          topology={topologyData.topology}
          loading={detailLoading}
          isAdmin={auth.isAdmin}
          feedback={selectedFeedback}
          onCorrectEvaluation={(event, evaluation) => selectedChain && setCorrectionTarget({ kind: "evaluation", chain: selectedChain, event, evaluation })}
          onCorrectChain={(chain) => setCorrectionTarget({ kind: "chain", chain })}
          onClose={closeDetails}
        />
      )}
      {correctionTarget && <CgeCorrectionModal target={correctionTarget} onClose={() => setCorrectionTarget(null)} onSubmit={submitCorrection} />}
    </div>
  );
}

function ChainSection({
  title,
  icon,
  chains,
  devices,
  topology,
  empty,
  closed = false,
  onDetails,
}: {
  title: string;
  icon: ReactNode;
  chains: EventChain[];
  devices: SynoraDevice[];
  topology: ApiTopologyNode[];
  empty: string;
  closed?: boolean;
  onDetails: (chain: EventChain) => void;
}) {
  return (
    <Panel title={title} className="live-events-panel" action={<span className="live-events-section-count">{chains.length}</span>}>
      <div className="live-events-section-heading">{icon}<span>{closed ? "Historique récent" : "Évaluation continue"}</span></div>
      {chains.length === 0 ? (
        <div className="live-events-empty"><Eye size={22} /><span>{empty}</span></div>
      ) : (
        <div className="event-chains-grid">
          {chains.map((chain) => (
            <ChainCard key={chain.id} chain={chain} devices={devices} topology={topology} closed={closed} onDetails={onDetails} />
          ))}
        </div>
      )}
    </Panel>
  );
}

function ChainCard({ chain, devices, topology, closed, onDetails }: { chain: EventChain; devices: SynoraDevice[]; topology: ApiTopologyNode[]; closed: boolean; onDetails: (chain: EventChain) => void }) {
  const tone = dangerTone(chain.danger_level);
  const primaryDevice = devices.find((device) => device.id === chain.primary_device_id);
  const deviceLabel = primaryDevice && typeof primaryDevice.name === "string"
    ? `${primaryDevice.name} (${chain.primary_device_id})`
    : chain.primary_device_id || "Device inconnu";
  return (
    <article className={`event-chain-card ${tone}`}>
      <header className="event-chain-card-header">
        <div className="event-chain-card-title">
          <strong>{chain.title || "Chaîne d’événements"}</strong>
          <span>{chain.id}</span>
        </div>
        <div className="event-chain-badges">
          <span className={`badge ${tone}`}>{formatDangerLevel(chain.danger_level)}</span>
          <span className="badge neutral">{stateLabel(chain.current_state)}</span>
          <span className="badge neutral">{chain.status === "open" ? "En cours" : "Clôturée"}</span>
          {chain.simulated && <span className="badge simulation">Simulation</span>}
        </div>
      </header>

      {chain.summary && <p className="event-chain-summary">{chain.summary}</p>}
      <div className="event-chain-location">
        <span><Cpu size={14} /> {deviceLabel}</span>
        <span>{getChainRoomLabel(chain, topology)}</span>
      </div>
      <div className="event-chain-metrics">
        <span><strong>{chain.events_count}</strong> events</span>
        <span><strong>{chain.significant_events_count}</strong> significatifs</span>
        <span><strong>{chain.motion_count}</strong> motions</span>
      </div>
      <div className="event-chain-card-footer">
        <div>
          {closed ? <span>{formatClosedReason(chain.closed_reason)}</span> : <span>Mis à jour {relativeTime(chain.updated_at)}</span>}
          <small>{closed ? `Durée ${formatChainDuration(chain)}` : `Dernier signal ${relativeTime(chain.last_significant_event_at)}`}</small>
          {chain.simulated && <small>{chain.test_run_id || chain.scenario_id || "Simulation"}</small>}
        </div>
        <button type="button" className="secondary-button event-chain-details-button" onClick={() => onDetails(chain)}>Voir la chaîne</button>
      </div>
    </article>
  );
}

function ChainDetail({ chain, devices, topology, loading, isAdmin, feedback, onCorrectEvaluation, onCorrectChain, onClose }: { chain: EventChain | null; devices: SynoraDevice[]; topology: ApiTopologyNode[]; loading: boolean; isAdmin: boolean; feedback: CgeEvaluationFeedback[]; onCorrectEvaluation: (event: EventChainEvent, evaluation: ChainEvaluation) => void; onCorrectChain: (chain: EventChain) => void; onClose: () => void }) {
  return (
    <div className="event-chain-detail-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="event-chain-detail" role="dialog" aria-modal="true" aria-labelledby="event-chain-detail-title">
        <header className="event-chain-detail-header">
          <div><span>Event Chain</span><h2 id="event-chain-detail-title">{chain?.title || "Détails de la chaîne"}</h2></div>
          <button type="button" className="icon-button" onClick={onClose} aria-label="Fermer"><X size={19} /></button>
        </header>
        {loading && <div className="event-chain-detail-loading">Chargement des détails…</div>}
        {chain && <div className="event-chain-detail-content">
          <div className="event-chain-detail-overview">
            <span className={`badge ${dangerTone(chain.danger_level)}`}>{formatDangerLevel(chain.danger_level)} · {chain.danger_score.toFixed(2)}</span>
            <span className="badge neutral">{stateLabel(chain.current_state)}</span>
            <span className="badge neutral">{chain.status === "open" ? "En cours" : "Clôturée"}</span>
            <span>{chain.primary_device_id || "Device inconnu"} · {getChainRoomLabel(chain, topology)}</span>
          </div>
          <div className="event-chain-detail-metrics">
            <span><strong>{chain.events_count}</strong> événements</span>
            <span><strong>{chain.significant_events_count}</strong> significatifs</span>
            <span><strong>{chain.motion_count}</strong> mouvements</span>
            {chain.closed_reason && <span>{formatClosedReason(chain.closed_reason)}</span>}
          </div>
          {chain.summary && <p className="event-chain-detail-summary">{chain.summary}</p>}
          {chain.simulated && <div className="event-chain-simulation-meta">Simulation · test_run_id={chain.test_run_id || "—"} · scenario_id={chain.scenario_id || "—"}</div>}
          <h3>Maillons de la chaîne</h3>
          <EventChainTimeline chain={chain} devices={devices} topology={topology} isAdmin={isAdmin} feedback={feedback} onCorrectEvaluation={onCorrectEvaluation} />
          {isAdmin && <button type="button" className="secondary-button chain-correction-button" onClick={() => onCorrectChain(chain)}>Corriger la fin de chaîne</button>}
          <h3>Historique des évaluations CGE</h3>
          <EvaluationList evaluations={chain.evaluations ?? []} />
        </div>}
      </section>
    </div>
  );
}

function EvaluationList({ evaluations }: { evaluations: ChainEvaluation[] }) {
  if (evaluations.length === 0) return <div className="event-chain-empty-detail">Aucune évaluation conservée.</div>;
  return <div className="event-chain-evaluations">{evaluations.slice().reverse().map((evaluation) => (
    <article className="event-chain-evaluation" key={`${evaluation.event_id}-${evaluation.index}`}>
      <div className="event-chain-evaluation-header"><strong>Évaluation #{evaluation.index}</strong><span>{new Date(evaluation.timestamp).toLocaleString("fr-FR")}</span></div>
      <div className="event-chain-evaluation-badges"><span className="badge neutral">{stateLabel(evaluation.state)}</span><span className={`badge ${dangerTone(evaluation.danger_level)}`}>{formatDangerLevel(evaluation.danger_level)} · {evaluation.danger_score.toFixed(2)}</span></div>
      <small>Maillon évalué : {evaluation.event_id || "—"}</small>
    </article>
  ))}</div>;
}

function CgeCorrectionModal({ target, onClose, onSubmit }: { target: CorrectionTarget; onClose: () => void; onSubmit: (payload: CgeEvaluationFeedback | CgeChainFeedback) => Promise<void> }) {
  const [correctionType, setCorrectionType] = useState<CgeEvaluationFeedback["correction_type"]>("false_positive");
  const [outcome, setOutcome] = useState<CgeChainFeedback["final_outcome"]>("uncertain");
  const [state, setState] = useState("suspicious");
  const [danger, setDanger] = useState<CgeEvaluationFeedback["corrected_danger_level"]>("medium");
  const [note, setNote] = useState("");
  const [actions, setActions] = useState("");
  const [applySimilar, setApplySimilar] = useState(false);
  const [saving, setSaving] = useState(false);
  const evaluationTarget = target.kind === "evaluation";

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    try {
      if (evaluationTarget) {
        await onSubmit(buildEvaluationFeedbackPayload(target.chain, target.evaluation, {
          correction_type: correctionType,
          corrected_state: correctionType === "wrong_state" ? state : undefined,
          corrected_danger_level: ["too_low", "too_high"].includes(correctionType) ? danger : undefined,
          preferred_actions: actions.split(",").map((value) => value.trim()).filter(Boolean),
          note: note.trim() || undefined,
        }));
      } else {
        await onSubmit(buildChainFeedbackPayload(target.chain, {
          final_outcome: outcome,
          corrected_final_danger_level: danger,
          apply_to_similar_future_chains: applySimilar,
          note: note.trim() || undefined,
        }));
      }
    } finally { setSaving(false); }
  }

  return <div className="cge-modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="cge-modal cge-correction-modal" role="dialog" aria-modal="true" aria-labelledby="cge-correction-title">
      <header><div><span>{evaluationTarget ? "Correction d’évaluation" : "Correction de fin de chaîne"}</span><h2 id="cge-correction-title">Ajouter une correction</h2></div><button className="icon-button" type="button" onClick={onClose} aria-label="Fermer"><X size={18} /></button></header>
      <form className="cge-modal-content cge-correction-form" onSubmit={(event) => void submit(event)}><p>Cette correction influencera les futures évaluations. L’événement brut reste inchangé.</p>{evaluationTarget ? <><label>Type de correction<select value={correctionType} onChange={(event) => setCorrectionType(event.target.value as CgeEvaluationFeedback["correction_type"])}><option value="false_positive">Faux positif</option><option value="too_low">Danger trop faible</option><option value="too_high">Danger trop élevé</option><option value="wrong_state">Mauvais état</option><option value="wrong_action">Action recommandée incorrecte</option><option value="mark_normal">Ce pattern est normal</option><option value="mark_critical">Ce pattern est critique</option></select></label>{correctionType === "wrong_state" && <label>État corrigé<select value={state} onChange={(event) => setState(event.target.value)}><option value="idle">Inactif</option><option value="activity">Activité</option><option value="suspicious">Suspect</option><option value="intrusion">Intrusion</option><option value="break-in">Effraction</option></select></label>}<label>Niveau corrigé<select value={danger} onChange={(event) => setDanger(event.target.value as CgeEvaluationFeedback["corrected_danger_level"])}>{dangerOptions()}</select></label><label>Actions préférées<input value={actions} onChange={(event) => setActions(event.target.value)} placeholder="observe, notify, record_clip" /></label></> : <><label>Résultat final<select value={outcome} onChange={(event) => setOutcome(event.target.value as CgeChainFeedback["final_outcome"])}><option value="normal">Normal</option><option value="false_positive">Faux positif</option><option value="real_incident">Incident réel</option><option value="uncertain">Incertain</option></select></label><label>Niveau final corrigé<select value={danger} onChange={(event) => setDanger(event.target.value as CgeEvaluationFeedback["corrected_danger_level"])}>{dangerOptions()}</select></label><label className="checkbox-line"><input type="checkbox" checked={applySimilar} onChange={(event) => setApplySimilar(event.target.checked)} /> Appliquer aux chaînes futures similaires</label></>}<label>Note admin<textarea value={note} onChange={(event) => setNote(event.target.value)} rows={4} placeholder="Expliquez la correction…" /></label><div className="cge-correction-actions"><button type="button" className="secondary-button" onClick={onClose}>Annuler</button><button type="submit" className="primary-button" disabled={saving}>{saving ? "Enregistrement…" : "Enregistrer la correction"}</button></div></form>
    </section>
  </div>;
}

function dangerOptions() {
  return [<option key="none" value="none">Aucun</option>, <option key="low" value="low">Faible</option>, <option key="medium" value="medium">Moyen</option>, <option key="high" value="high">Élevé</option>, <option key="critical" value="critical">Critique</option>];
}
