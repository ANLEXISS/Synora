import { useEffect, useState } from "react";
import { buildWsUrl } from "./config";

export type PilotState = {
  schema_version: string;
  source_schema: string;
  revision: number;
  captured_at?: string;
  topology?: string;
  security?: { armed?: boolean; degraded?: boolean; known?: boolean };
  presence?: { human_present?: boolean; known_residents_present?: boolean; known_resident_count?: number; track_count?: number; track_confirmed?: boolean };
  sensors?: { movement?: boolean; access_state?: string; sensor_evidence?: boolean; alarm_state?: string; observation_count?: number; confidence?: number };
  episode?: { phase?: string; segment_count?: number; gap_count?: number; seconds_since_first?: number; seconds_since_last?: number; calm_seconds?: number };
  vision?: { pose_status?: string; posture?: string; fall_state?: string; risk_status?: string; camera_health_status?: string; camera_integrity_status?: string; camera_uncertainty?: boolean; real_detection?: boolean; replay_simulation?: boolean; aggregate_confidence?: number };
};

export async function getPilotState(signal?: AbortSignal): Promise<PilotState | null> {
  const response = await fetch("/api/system/state", { signal, cache: "no-store", credentials: "same-origin", headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => null);
  if (response.status === 503 && body?.status === "unknown") return null;
  if (!response.ok) throw new Error("L’état du pilote est indisponible.");
  return body as PilotState;
}

export function usePilotState() {
  const [state, setState] = useState<PilotState | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void getPilotState(controller.signal).then((nextState) => { setState(nextState); setError(null); }).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "L’état du pilote est indisponible."); });
    const socket = new WebSocket(buildWsUrl());
    socket.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as { type?: string; data?: { state?: PilotState } };
        if ((message.type === "snapshot.initial" || message.type === "system.state") && message.data?.state?.schema_version) {
          setState(message.data.state);
          setError(null);
        }
      } catch { /* Other bus events are intentionally ignored. */ }
    };
    socket.onerror = () => setError((current) => current ?? "Le flux temps réel de l’état est indisponible.");
    return () => { controller.abort(); socket.close(); };
  }, []);
  return { state, error };
}
