import { BrainCircuit, CircleDot, Info, Radio, ShieldCheck } from "lucide-react";
import { type CSSProperties, useEffect, useMemo, useState } from "react";
import { type IntelligenceEvent, type IntelligenceTopology, type IntelligenceTrace, useIntelligenceTelemetry } from "../lib/intelligence";

const headLabels: Record<string, string> = { danger: "Ce que la maison perçoit", incident: "Les indices se croisent", task: "Le sens se précise", action: "Réponse proposée" };

export function Intelligence() {
  const { topology, latest, traces, events, error } = useIntelligenceTelemetry();
  const [selectedLayer, setSelectedLayer] = useState<string | null>(null);
  const selected = latest?.activations.find((item) => item.layer_id === selectedLayer);
  const dryRun = latest?.runtime_mode === "active_dry_run";
  return <div className="intelligence-page">
    <section className="intelligence-intro"><div><div className="intelligence-kicker"><BrainCircuit size={16} /> Intelligence locale</div><h2>Réseau de compréhension</h2><p>Seules les traces réelles et protégées reçues par Synora sont affichées.</p></div><div className={`intelligence-live-pill ${latest?.live ? "is-live" : ""}`}><Radio size={15} />{latest?.test ? "Simulation contrôlée" : latest?.live ? "Inférence en cours" : dryRun ? "Calcul local" : "Aucune inférence récente"}</div></section>
    {error && <div className="intelligence-notice"><Info size={17} />{error}</div>}
    {!topology ? <section className="intelligence-empty"><CircleDot size={32} /><h3>Aucune inférence MLP récente</h3><p>Synora n’invente aucune donnée pour remplir cette vue. Les scénarios sont exécutés par le harnais central local.</p></section> : <><NetworkGraph topology={topology} trace={latest} selectedLayer={selectedLayer} onSelect={setSelectedLayer} /><div className="intelligence-footer"><div className="intelligence-status"><ShieldCheck size={17} /><span>{latest?.test ? "Simulation contrôlée · aucune action physique" : latest?.live ? `Trace live · ${(latest.duration_ms ?? 0).toFixed(1)} ms` : dryRun ? "Calcul local · aucune action physique" : "Aucune inférence récente"}</span>{latest && <small>Les sorties restent consultatives et protégées.</small>}</div>{selected && <div className="intelligence-explanation"><strong>Les indices se rejoignent ici</strong><span>Cette étape relie les signaux reçus.</span></div>}</div><details className="intelligence-admin-details"><summary>Détails techniques réservés à l’administration</summary><div><span>Version modèle : {latest?.model_version ?? (topology.model_version || "indisponible")}</span><span>Traces conservées : {traces.length}/24</span><span>{latest?.test ? "Provenance : test-harness · trace de validation" : "Provenance : flux runtime"}</span><span>Poids, valeurs brutes, embeddings, médias, identités et chemins matériels exclus.</span></div></details></>}
    <RecentEvents events={events} />
  </div>;
}

function RecentEvents({ events }: { events: IntelligenceEvent[] }) {
  return <section className="intelligence-events" aria-labelledby="intelligence-events-title">
    <div className="intelligence-events-head"><div><span className="intelligence-events-kicker">Journal protégé</span><h3 id="intelligence-events-title">Événements récents</h3></div><small>{events.length}/64 conservés</small></div>
    {events.length === 0 ? <p className="intelligence-events-empty">Aucun événement récent.</p> : <ol className="intelligence-events-list">{events.slice().reverse().map((event) => <li key={`${event.inference_id}-${event.timestamp}`}><div><strong>{event.test ? "Simulation contrôlée" : event.live ? "Inférence live" : "Calcul local"}</strong><span>{event.proposed_output ? `Sortie proposée : ${event.proposed_output}` : "Trace reçue"}</span></div><time dateTime={event.timestamp}>{formatEventTime(event.timestamp)}</time></li>)}</ol>}
  </section>;
}

function formatEventTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Heure indisponible" : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function NetworkGraph({ topology, trace, selectedLayer, onSelect }: { topology: IntelligenceTopology; trace: IntelligenceTrace | null; selectedLayer: string | null; onSelect: (layer: string) => void }) {
  const [pulseId, setPulseId] = useState<string | null>(null);
  const [pulseRevision, setPulseRevision] = useState(0);
  const graph = useMemo(() => buildNetworkGraph(topology, trace), [topology, trace]);

  useEffect(() => {
    if (!trace?.inference_id) return;
    setPulseId(trace.inference_id);
    setPulseRevision((revision) => revision + 1);
    const timeout = window.setTimeout(() => setPulseId(null), 1250);
    return () => window.clearTimeout(timeout);
  }, [trace?.inference_id]);

  const statusLabel = trace?.test ? "Simulation contrôlée" : trace ? "Données observées" : "Aucune donnée récente";
  const pulsing = pulseId === trace?.inference_id;
  return <section className="intelligence-graph" aria-label="Réseau vivant de compréhension"><div className="intelligence-graph-head"><span>Les indices se relient</span><small>{statusLabel}</small></div><svg className="intelligence-network-svg" viewBox={`0 0 ${GRAPH_WIDTH} ${GRAPH_HEIGHT}`} role="img" aria-labelledby="intelligence-network-title intelligence-network-description" preserveAspectRatio="xMidYMid meet"><title id="intelligence-network-title">Réseau de compréhension Synora</title><desc id="intelligence-network-description">Quatre colonnes de nœuds actifs reliées uniquement par les chemins reçus avec la dernière trace.</desc><style>{networkAnimationStyles}</style>{graph.paths.map((path, index) => <NetworkPath key={`${path.from}-${path.to}-${index}-${pulseRevision}`} path={path} pulsing={pulsing} />)}{graph.nodes.map((node) => <NetworkNode key={node.id} node={node} pulsing={pulsing && graph.activePathNodeIds.has(node.id)} selected={selectedLayer === node.layerId} onSelect={onSelect} />)}{graph.columns.map((column) => <text key={column.name} x={column.x} y={32} textAnchor="middle" fill="#d9e7f5" style={{ fill: "#d9e7f5" }} className="intelligence-network-label">{column.label}</text>)}</svg>{trace && graph.paths.length === 0 && <div className="intelligence-network-empty" role="status">Aucune connexion active reçue pour cette trace.</div>}</section>;
}

const GRAPH_WIDTH = 960;
const GRAPH_HEIGHT = 390;
const GRAPH_TOP = 76;
const GRAPH_BOTTOM = 340;
const networkColors = ["#63e6d1", "#b77aff"];
const networkAnimationStyles = `
  .intelligence-network-line { stroke-width: var(--line-width); opacity: var(--line-opacity); }
  .intelligence-network-line.is-pulsing { animation: intelligence-network-pulse 1250ms ease-out both; }
  .intelligence-network-impulse { animation: intelligence-network-impulse 1250ms linear both; }
  .intelligence-network-node-pulse { animation: intelligence-network-node-pulse 1250ms ease-out both; transform-box: fill-box; transform-origin: center; }
  @keyframes intelligence-network-pulse {
    0% { opacity: .35; stroke-width: var(--line-width); }
    24% { opacity: 1; stroke-width: calc(var(--line-width) + 1.8px); }
    100% { opacity: var(--line-opacity); stroke-width: var(--line-width); }
  }
  @keyframes intelligence-network-impulse {
    0% { stroke-dashoffset: 1000; opacity: .05; }
    18% { opacity: .98; }
    100% { stroke-dashoffset: 0; opacity: .9; }
  }
  @keyframes intelligence-network-node-pulse {
    0% { opacity: .15; transform: scale(.72); }
    24% { opacity: .9; transform: scale(1.25); }
    100% { opacity: .15; transform: scale(1); }
  }
  @media (prefers-reduced-motion: reduce) {
    .intelligence-network-line.is-pulsing, .intelligence-network-impulse, .intelligence-network-node-pulse { animation: none; }
    .intelligence-network-impulse, .intelligence-network-node-pulse { opacity: .9; stroke-dashoffset: 0; transform: scale(1); }
  }
`;

type NetworkColumn = { name: string; label: string; x: number };
type NetworkNodeData = { id: string; layerId: string; x: number; y: number; activation: number; color: string; radius: number };
type NetworkPathData = { from: string; to: string; fromPoint: NetworkNodeData; toPoint: NetworkNodeData; strength: number; color: string; width: number; opacity: number; glow: number };
type NetworkGraphData = { columns: NetworkColumn[]; nodes: NetworkNodeData[]; paths: NetworkPathData[]; activePathNodeIds: Set<string> };

function buildNetworkGraph(topology: IntelligenceTopology, trace: IntelligenceTrace | null): NetworkGraphData {
  const columns = topology.heads.slice(0, 4).map((head, index) => ({ name: head.name, label: headLabels[head.name] ?? "Étape Synora", x: 120 + index * 240 }));
  const nodes: NetworkNodeData[] = [];
  const nodeById = new Map<string, NetworkNodeData>();
  const summaries = trace?.activations ?? [];
  columns.forEach((column, columnIndex) => {
    const head = topology.heads[columnIndex];
    const layers = head?.layers ?? [];
    layers.forEach((layer, layerIndex) => {
      const summary = summaries.find((item) => item.layer_id === layer.id);
      const activeNodes = summary?.active_nodes?.slice(0, 5) ?? [];
      const x = layers.length === 1 ? column.x : column.x - 38 + (layerIndex / Math.max(1, layers.length - 1)) * 76;
      activeNodes.forEach((activeNode, nodeIndex) => {
        const spread = activeNodes.length > 1 ? (GRAPH_BOTTOM - GRAPH_TOP) / (activeNodes.length - 1) : 0;
        const y = activeNodes.length > 1 ? GRAPH_TOP + nodeIndex * spread : (GRAPH_TOP + GRAPH_BOTTOM) / 2;
        const activation = clamp01(Math.abs(activeNode.activation ?? 0));
        const node: NetworkNodeData = { id: activeNode.node_id, layerId: layer.id, x, y, activation, color: networkColors[(layerIndex + columnIndex) % networkColors.length], radius: 3.5 + activation * 3.5 };
        nodes.push(node);
        nodeById.set(node.id, node);
      });
    });
  });
  const receivedPaths = trace?.active_paths?.slice(0, 64) ?? [];
  const maximumStrength = receivedPaths.reduce((maximum, path) => Math.max(maximum, Math.abs(path.strength ?? 0)), 0);
  const paths = receivedPaths.flatMap((path, index) => {
    const fromPoint = nodeById.get(path.from);
    const toPoint = nodeById.get(path.to);
    if (!fromPoint || !toPoint) return [];
    const strength = maximumStrength > 0 ? clamp01(Math.abs(path.strength ?? 0) / maximumStrength) : 0;
    return [{ from: path.from, to: path.to, fromPoint, toPoint, strength, color: networkColors[index % networkColors.length], width: 0.8 + strength * 2.8, opacity: 0.28 + strength * 0.68, glow: 1 + strength * 4 }];
  });
  const activePathNodeIds = new Set(paths.flatMap((path) => [path.from, path.to]));
  return { columns, nodes, paths, activePathNodeIds };
}

function NetworkPath({ path, pulsing }: { path: NetworkPathData; pulsing: boolean }) {
  const curve = bezierPath(path.fromPoint, path.toPoint);
  const style = { "--line-width": `${path.width}px`, "--line-opacity": path.opacity, filter: `drop-shadow(0 0 ${path.glow}px ${path.color})` } as CSSProperties;
  return <g role="img" aria-label={`Connexion active de ${path.from} vers ${path.to}`}><path d={curve} className={`intelligence-network-line ${pulsing ? "is-pulsing" : ""}`} style={style} stroke={path.color} fill="none" vectorEffect="non-scaling-stroke"><title>Connexion active</title></path>{pulsing && <path d={curve} className="intelligence-network-impulse" pathLength={1000} stroke="#f5ffff" strokeWidth={path.width + 1.5} strokeDasharray="24 976" strokeDashoffset={1000} opacity={.9} fill="none" vectorEffect="non-scaling-stroke" style={{ filter: `drop-shadow(0 0 ${path.glow + 2}px ${path.color})` }} aria-hidden="true" />}</g>;
}

function NetworkNode({ node, pulsing, selected, onSelect }: { node: NetworkNodeData; pulsing: boolean; selected: boolean; onSelect: (layer: string) => void }) {
  const style = { filter: node.activation > 0 ? `drop-shadow(0 0 ${2 + node.activation * 6}px ${node.color})` : undefined };
  const activate = () => onSelect(node.layerId);
  return <g className={`intelligence-network-node ${selected ? "is-selected" : ""}`} transform={`translate(${node.x} ${node.y})`} role="button" tabIndex={0} aria-label={`Nœud actif de ${node.layerId}`} onClick={activate} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); activate(); } }}><circle r={node.radius + 4} fill={node.color} opacity={0.08 + node.activation * 0.2} /><circle r={node.radius} fill={node.color} opacity={0.45 + node.activation * 0.55} style={style} />{pulsing && <circle className="intelligence-network-node-pulse" r={node.radius + 3} fill="none" stroke={node.color} strokeWidth="1.5" opacity=".8" aria-hidden="true" />}<title>Nœud actif · {node.layerId}</title></g>;
}

function bezierPath(from: NetworkNodeData, to: NetworkNodeData) {
  const bend = Math.max(30, Math.abs(to.x - from.x) * 0.62);
  return `M ${from.x} ${from.y} C ${from.x + bend} ${from.y}, ${to.x - bend} ${to.y}, ${to.x} ${to.y}`;
}

function clamp01(value: number) { return Number.isFinite(value) ? Math.min(1, Math.max(0, value)) : 0; }
