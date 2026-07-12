import {
  Activity,
  CircleHelp,
  PersonStanding,
  ShieldAlert,
  UserRoundCheck,
  WifiOff,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { getRoomLabel } from "../lib/topology";
import {
  dangerTone,
  formatDangerLevel,
  formatEventType,
  getEvaluationForEvent,
  getEventKey,
  getLatestEventId,
  isContextualEvent,
  isSignificantEvent,
} from "../lib/event-chains";
import type {
  ApiTopologyNode,
  ChainEvaluation,
  EventChain,
  EventChainEvent,
  SynoraDevice,
  CgeEvaluationFeedback,
} from "../lib/synora-types";

export type EventChainTimelineProps = {
  chain: EventChain;
  topology?: ApiTopologyNode[];
  devices?: SynoraDevice[];
  initialExpandedEventId?: string;
  isAdmin?: boolean;
  onCorrectEvaluation?: (event: EventChainEvent, evaluation: ChainEvaluation) => void;
  feedback?: CgeEvaluationFeedback[];
};

export function EventChainTimeline({
  chain,
  topology = [],
  devices = [],
  initialExpandedEventId,
  isAdmin = false,
  onCorrectEvaluation,
  feedback = [],
}: EventChainTimelineProps) {
  const events = useMemo(
    () => (chain.recent_events ?? [])
      .map((event, index) => ({ event, index }))
      .sort((left, right) => Date.parse(left.event.timestamp) - Date.parse(right.event.timestamp)),
    [chain.recent_events],
  );
  const latestEventId = getLatestEventId(chain);
  const [expandedEventId, setExpandedEventId] = useState(
    initialExpandedEventId ?? latestEventId,
  );
  const [userSelectedEvent, setUserSelectedEvent] = useState(false);
  const [newEventId, setNewEventId] = useState<string | undefined>();
  const previousLatestEventId = useRef(latestEventId);

  useEffect(() => {
    setExpandedEventId(initialExpandedEventId ?? latestEventId);
    setUserSelectedEvent(false);
    setNewEventId(undefined);
    previousLatestEventId.current = latestEventId;
    // A new modal chain starts with its latest event expanded.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chain.id]);

  useEffect(() => {
    if (latestEventId && latestEventId !== previousLatestEventId.current) {
      if (userSelectedEvent) {
        setNewEventId(latestEventId);
      } else {
        setExpandedEventId(latestEventId);
        setNewEventId(undefined);
      }
    }
    previousLatestEventId.current = latestEventId;
  }, [latestEventId, userSelectedEvent]);

  if (events.length === 0) {
    return <div className="event-chain-empty-detail">Aucun maillon récent conservé.</div>;
  }

  function selectEvent(eventId: string) {
    setExpandedEventId(eventId);
    setUserSelectedEvent(true);
    if (eventId === latestEventId) setNewEventId(undefined);
  }

  return (
    <div className="event-chain-timeline" aria-label="Maillons de la chaîne">
      {events.map(({ event, index }) => {
        const eventId = getEventKey(event, index);
        const evaluation = event.id ? getEvaluationForEvent(chain, event.id) : undefined;
        return (
          <EventStepCard
            key={eventId}
            event={event}
            eventId={eventId}
            evaluation={evaluation}
            expanded={expandedEventId === eventId}
            isNew={newEventId === eventId}
            topology={topology}
            devices={devices}
            isAdmin={isAdmin}
            onCorrectEvaluation={onCorrectEvaluation}
            hasFeedback={Boolean(event.id && feedback.some((item) => item.event_id === event.id))}
            onSelect={selectEvent}
          />
        );
      })}
    </div>
  );
}

function EventStepCard({
  event,
  eventId,
  evaluation,
  expanded,
  isNew,
  topology,
  devices,
  isAdmin,
  onCorrectEvaluation,
  hasFeedback,
  onSelect,
}: {
  event: EventChainEvent;
  eventId: string;
  evaluation?: ChainEvaluation;
  expanded: boolean;
  isNew: boolean;
  topology: ApiTopologyNode[];
  devices: SynoraDevice[];
  isAdmin: boolean;
  onCorrectEvaluation?: (event: EventChainEvent, evaluation: ChainEvaluation) => void;
  hasFeedback: boolean;
  onSelect: (eventId: string) => void;
}) {
  const contextual = isContextualEvent(event);
  const significant = isSignificantEvent(event);
  const tone = evaluation ? dangerTone(evaluation.danger_level) : contextual ? "neutral" : "warning";
  const device = devices.find((item) => item.id === event.device_id);
  const deviceName = device && typeof device.name === "string" ? device.name : undefined;
  const room = event.node_id ? getRoomLabel(event.node_id, topology) : "Pièce inconnue";
  const ariaLabel = `${event.type}, ${significant ? "événement significatif" : "événement contextuel"}, ${formatFullTimestamp(event.timestamp)}`;

  return (
    <article className={`event-step-card event-step-card--${expanded ? "expanded" : "compact"} event-step-card--${significant ? "significant" : "contextual"} event-step-card--${tone}`}>
      <button
        type="button"
        className="event-step-header"
        aria-expanded={expanded}
        aria-label={ariaLabel}
        onClick={() => onSelect(eventId)}
      >
        <span className="event-step-icon" aria-hidden="true"><EventTypeIcon type={event.type} /></span>
        <span className="event-step-heading">
          <strong>{event.type}</strong>
          {expanded && <small>{formatEventType(event.type)}</small>}
        </span>
        <span className="event-step-badges">
          <span className={`badge ${significant ? "warning" : "neutral"}`}>{significant ? "Significatif" : "Contexte"}</span>
          {evaluation && <span className={`badge ${dangerTone(evaluation.danger_level)}`}>{formatDangerLevel(evaluation.danger_level)}</span>}
          {event.simulated && <span className="badge simulation">Simulation</span>}
          {isNew && <span className="event-step-new-badge">Nouveau</span>}
        </span>
        <time dateTime={event.timestamp}>{expanded ? formatFullTimestamp(event.timestamp) : formatShortTime(event.timestamp)}</time>
      </button>

      {expanded && (
        <div className="event-step-details">
          <dl className="event-step-facts">
            <Fact label="Périphérique" value={deviceName ? `${deviceName} (${event.device_id || "—"})` : event.device_id || "—"} />
            <Fact label="Pièce" value={event.node_id ? `${room} (${event.node_id})` : room} />
            <Fact label="Clip" value={event.clip_id} />
            <Fact label="Track" value={event.track_id} />
            <Fact label="Confiance" value={formatConfidence(event.payload?.confidence)} />
            <Fact label="Identité" value={formatPayloadValue(event.payload?.identity)} />
          </dl>

          <EventEvaluation evaluation={evaluation} />
          {hasFeedback && <span className="event-feedback-applied">Correction existante</span>}
          {isAdmin && evaluation && onCorrectEvaluation && <button type="button" className="event-correction-button" onClick={() => onCorrectEvaluation(event, evaluation)}>Ajouter une correction</button>}
          <TechnicalPayload payload={event.payload} />
        </div>
      )}
    </article>
  );
}

function EventTypeIcon({ type }: { type: string }) {
  const normalized = type.toLowerCase();
  if (normalized.includes("identity")) return <UserRoundCheck size={17} />;
  if (normalized.includes("unknown") || normalized.includes("uncertain")) return <CircleHelp size={17} />;
  if (normalized.includes("motion")) return <Activity size={17} />;
  if (normalized.includes("weapon") || normalized.includes("tamper")) return <ShieldAlert size={17} />;
  if (normalized.includes("fall")) return <PersonStanding size={17} />;
  if (normalized.includes("offline")) return <WifiOff size={17} />;
  return <Activity size={17} />;
}

function Fact({ label, value }: { label: string; value?: string }) {
  if (!value || value === "—") return null;
  return <div><dt>{label}</dt><dd>{value}</dd></div>;
}

function EventEvaluation({ evaluation }: { evaluation?: ChainEvaluation }) {
  if (!evaluation) {
    return <div className="event-evaluation-panel event-evaluation-panel--contextual">Événement contextuel — pas d’évaluation CGE complète</div>;
  }

  return (
    <section className="event-evaluation-panel" aria-label="Évaluation CGE liée">
      <div className="event-evaluation-panel-header"><strong>Évaluation CGE</strong><span className={`badge ${dangerTone(evaluation.danger_level)}`}>{formatDangerLevel(evaluation.danger_level)} · {evaluation.danger_score.toFixed(2)}</span></div>
      <div className="event-evaluation-state"><span>État</span><strong>{stateLabel(evaluation.state)}</strong></div>
      {evaluation.reasons && evaluation.reasons.length > 0 && <div><h4>Raisons moteur</h4><ul>{evaluation.reasons.map((reason) => <li key={reason}>{reason}</li>)}</ul></div>}
      {evaluation.recommended_actions && evaluation.recommended_actions.length > 0 && <div><h4>Actions recommandées</h4><ul>{evaluation.recommended_actions.map((action) => <li key={action}>{action}</li>)}</ul></div>}
    </section>
  );
}

function TechnicalPayload({ payload }: { payload?: Record<string, unknown> }) {
  if (!payload || Object.keys(payload).length === 0) return null;
  const sensitive = Object.entries(payload).filter(([key]) => isSensitiveKey(key));
  const safe = Object.entries(payload).filter(([key, value]) => !isSensitiveKey(key) && value !== undefined).slice(0, 12);

  return (
    <details className="event-step-technical">
      <summary>Données techniques</summary>
      <div className="event-step-technical-content">
        {safe.map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{formatPayloadValue(value) || "—"}</dd></div>)}
        {sensitive.length > 0 && <p>{sensitive.length} champ(s) sensible(s) masqué(s).</p>}
        {safe.length === 0 && sensitive.length === 0 && <p>Aucune donnée technique affichable.</p>}
      </div>
    </details>
  );
}

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

function formatShortTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function formatFullTimestamp(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Horodatage inconnu" : date.toLocaleString("fr-FR");
}

function formatConfidence(value: unknown) {
  if (typeof value !== "number") return undefined;
  return `${Math.round(value <= 1 ? value * 100 : value)} %`;
}

function formatPayloadValue(value: unknown): string | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value)) return value.length > 0 ? `[${value.length} éléments]` : "[]";
  return "[objet]";
}

function isSensitiveKey(key: string) {
  return /token|secret|credential|password|setup|face|absolute_path|clip_path/i.test(key);
}
