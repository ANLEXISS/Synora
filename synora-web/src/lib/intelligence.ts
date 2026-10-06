import { useEffect, useState } from "react";
import { synoraFetch } from "./api";
import { buildWsUrl } from "./config";

export type IntelligenceLayer = { id: string; input_size: number; output_size: number; activation: string };
export type IntelligenceTopology = { schema_version: string; model_version: string; heads: Array<{ name: string; layers: IntelligenceLayer[] }> };
export type IntelligenceTrace = {
  schema_version: string;
  inference_id: string;
  model_version: string;
  provenance?: string;
  test?: boolean;
  topology: IntelligenceTopology;
  activations: Array<{ layer_id: string; count?: number; minimum?: number; maximum?: number; mean?: number; active_nodes: Array<{ node_id: string; activation?: number }> }>;
  active_paths: Array<{ from: string; to: string; strength?: number }>;
  proposed_output?: string;
  duration_ms?: number;
  redacted: boolean;
  runtime_mode?: string;
  inference_status?: string;
  live: boolean;
};
export type IntelligenceEvent = {
  schema_version: string;
  event_type: "inference" | string;
  timestamp: string;
  inference_id: string;
  model_version: string;
  provenance?: string;
  test?: boolean;
  proposed_output?: string;
  runtime_mode?: string;
  inference_status?: string;
  live: boolean;
};

export function getIntelligenceTopology(signal?: AbortSignal) { return synoraFetch<IntelligenceTopology>("/api/intelligence/topology", { signal }); }
export function getIntelligenceTraces(signal?: AbortSignal) { return synoraFetch<IntelligenceTrace[]>("/api/intelligence/traces", { signal }); }
export function getIntelligenceEvents(signal?: AbortSignal) { return synoraFetch<IntelligenceEvent[]>("/api/intelligence/events", { signal }); }

export function useIntelligenceTelemetry() {
  const [topology, setTopology] = useState<IntelligenceTopology | null>(null);
  const [traces, setTraces] = useState<IntelligenceTrace[]>([]);
  const [events, setEvents] = useState<IntelligenceEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([getIntelligenceTopology(controller.signal), getIntelligenceTraces(controller.signal), getIntelligenceEvents(controller.signal)]).then(([nextTopology, nextTraces, nextEvents]) => {
      setTopology(nextTopology?.heads?.length ? nextTopology : null);
      setTraces(nextTraces.slice(-24));
      setEvents(nextEvents.slice(-64));
      setError(null);
    }).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Les traces Intelligence sont indisponibles."); });
    const socket = new WebSocket(buildWsUrl());
    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as { type?: string; data?: { trace?: IntelligenceTrace; event?: IntelligenceEvent; events?: IntelligenceEvent[] } };
        if (message.type === "snapshot.initial" && message.data?.events) {
          setEvents(message.data.events.slice(-64));
          return;
        }
        if (message.type !== "intelligence.inference" || !message.data?.trace?.inference_id) return;
        const trace = message.data.trace;
        setTopology((current) => current ?? trace.topology);
        setTraces((current) => [...current, trace].slice(-24));
        if (message.data.event?.inference_id) setEvents((current) => [...current, message.data!.event!].slice(-64));
        setError(null);
      } catch { /* Other bus events are intentionally ignored. */ }
    };
    socket.onerror = () => setError((current) => current ?? "Le flux temps réel est indisponible.");
    return () => { controller.abort(); socket.close(); };
  }, []);
  return { topology, traces, events, latest: traces.at(-1) ?? null, error };
}
