import { Activity, CircleHelp, CircleDot, ShieldAlert, UserRoundCheck, WifiOff } from "lucide-react";
import { useState } from "react";
import {
  chainSourceTone,
  dangerTone,
  formatChainEventLabel,
  formatChainEventLocation,
  formatDangerLevel,
  getChainEventKind,
  getChainRailItems,
  getChainSourceBadge,
} from "../lib/event-chains";
import type { ApiTopologyNode, EventChain } from "../lib/synora-types";

export type EventChainRailProps = {
  chain: EventChain;
  topology?: ApiTopologyNode[];
  selectedEventId?: string;
  onSelectEvent?: (eventId: string) => void;
  compact?: boolean;
};

export function EventChainRail({
  chain,
  topology = [],
  selectedEventId,
  onSelectEvent,
  compact = false,
}: EventChainRailProps) {
  const [internalSelectedEventId, setInternalSelectedEventId] = useState<string | undefined>();
  const items = getChainRailItems(chain);
  const activeEventId = selectedEventId ?? internalSelectedEventId;
  const sourceTone = chainSourceTone(chain);
  const sourceLabel = getChainSourceBadge(chain);

  function selectEvent(eventId: string) {
    setInternalSelectedEventId(eventId);
    onSelectEvent?.(eventId);
  }

  return (
    <section className={`event-chain-rail ${compact ? "event-chain-rail--compact" : ""}`} aria-label="Frise visuelle de la chaîne CGE">
      <div className="event-chain-rail-heading">
        <div className="event-chain-rail-caption"><span className="event-chain-rail-kicker">Frise de chaîne</span><span>{items.length} maillon{items.length === 1 ? "" : "s"}</span></div>
        <div className="event-chain-rail-badges">
          <span className={`badge ${chain.status === "open" ? "warning" : "neutral"}`}>{chain.status === "open" ? "En cours" : "Clôturée"}</span>
          <span className={`badge ${sourceTone}`}>{sourceLabel}</span>
        </div>
      </div>
      <div className="event-chain-rail-scroll" role="list" aria-label="Maillons événementiels">
        <div className="event-chain-rail-endpoint event-chain-rail-start" aria-label="Départ"><CircleDot size={compact ? 16 : 18} /><span>Départ</span></div>
        {items.length === 0 && <div className="event-chain-rail-empty">Aucun événement récent</div>}
        {items.map((item) => {
          const label = formatChainEventLabel(item.event);
          const location = formatChainEventLocation(item.event, topology);
          const kind = getChainEventKind(item.event);
          const tone = dangerTone(item.dangerLevel);
          const dangerClass = `event-chain-rail-danger--${item.dangerLevel}`;
          const selected = activeEventId === item.eventId;
          return (
            <div className="event-chain-rail-segment" role="presentation" key={item.eventId}>
              <span className="event-chain-rail-connector" aria-hidden="true" />
              <button
                type="button"
                role="listitem"
                className={`event-chain-rail-item event-chain-rail-item--${kind} event-chain-rail-item--${tone} ${dangerClass} ${selected ? "is-selected" : ""}`}
                aria-label={`${label}, ${kind === "contextual" ? "événement contextuel" : "événement significatif"}, ${location}, danger ${formatDangerLevel(item.dangerLevel)}`}
                aria-pressed={selected}
                onClick={() => selectEvent(item.eventId)}
              >
                <span className="event-chain-rail-item-icon" aria-hidden="true"><RailEventIcon type={item.event.type} /></span>
                <span className="event-chain-rail-item-copy"><strong>{label}</strong><small>{location}</small></span>
                <span className="event-chain-rail-item-meta"><span className={`badge ${tone} ${dangerClass}-badge`}>{formatDangerLevel(item.dangerLevel)}</span><time dateTime={item.event.timestamp}>{formatShortTime(item.event.timestamp)}</time></span>
                {item.event.validation && <span className="badge validation">Validation</span>}
                {item.event.simulated && <span className="badge simulation">Simulation</span>}
              </button>
            </div>
          );
        })}
        <span className="event-chain-rail-connector event-chain-rail-final-connector" aria-hidden="true" />
        <div className={`event-chain-rail-endpoint event-chain-rail-finish ${chain.status === "open" ? "event-chain-rail-finish--open" : ""}`} aria-label={chain.status === "open" ? "Chaîne en cours" : "Fin de chaîne"}>
          <CircleDot size={compact ? 16 : 18} /><span>{chain.status === "open" ? "En cours" : "Fin"}</span>
        </div>
      </div>
    </section>
  );
}

function RailEventIcon({ type }: { type: string }) {
  const normalized = type.toLowerCase();
  if (normalized.includes("identity")) return <UserRoundCheck size={compactIconSize} />;
  if (normalized.includes("unknown") || normalized.includes("uncertain")) return <CircleHelp size={compactIconSize} />;
  if (normalized.includes("weapon") || normalized.includes("tamper") || normalized.includes("risk")) return <ShieldAlert size={compactIconSize} />;
  if (normalized.includes("offline")) return <WifiOff size={compactIconSize} />;
  if (normalized.includes("motion")) return <Activity size={compactIconSize} />;
  return <Activity size={compactIconSize} />;
}

const compactIconSize = 15;

function formatShortTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
}
