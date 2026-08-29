import { useEffect, useRef, useState } from "react";
import { buildWsUrl } from "../lib/config";
import { SynoraApiError } from "./api";
import { getState } from "../lib/synora-api";
import { acceptRealtimeMessage, type RealtimeCursor } from "./realtime";
import type { SynoraSnapshot, SynoraWsMessage } from "../lib/synora-types";

type SynoraConnectionState = "connecting" | "connected" | "disconnected" | "error";

type UseSynoraSnapshotResult = {
  snapshot: SynoraSnapshot | null;
  loading: boolean;
  error: string | null;
  connection: SynoraConnectionState;
  lastMessageAt: Date | null;
  refresh: () => Promise<void>;
  apiStatus: "connected" | "unauthenticated" | "unavailable";
};

export function useSynoraSnapshot(): UseSynoraSnapshotResult {
  const [snapshot, setSnapshot] = useState<SynoraSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connection, setConnection] =
    useState<SynoraConnectionState>("connecting");
  const [lastMessageAt, setLastMessageAt] = useState<Date | null>(null);
  const [apiStatus, setApiStatus] = useState<UseSynoraSnapshotResult["apiStatus"]>("unavailable");

  const abortRef = useRef<AbortController | null>(null);
  const cursorRef = useRef<RealtimeCursor | null>(null);

  async function refresh() {
    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      setError(null);

      const state = await getState(controller.signal);

      setSnapshot(state);
      cursorRef.current = null;
      setLoading(false);
      setApiStatus("connected");
    } catch (err) {
      if (controller.signal.aborted) return;

      setError(err instanceof Error ? err.message : "Erreur API inconnue");
      setLoading(false);
      setApiStatus(err instanceof SynoraApiError && err.status === 401 ? "unauthenticated" : "unavailable");
    }
  }

  useEffect(() => {
    void refresh();

    return () => {
      abortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let closedByComponent = false;
    let reconnectTimer: number | null = null;
    let pollingTimer: number | null = null;
    let reconnectDelay = 1000;

    function startPolling() {
      if (pollingTimer !== null) return;
      pollingTimer = window.setInterval(() => void refresh(), 5000);
    }

    function stopPolling() {
      if (pollingTimer !== null) window.clearInterval(pollingTimer);
      pollingTimer = null;
    }

    function connect() {
      if (typeof WebSocket === "undefined") {
        setConnection("disconnected");
        startPolling();
        return;
      }
      setConnection("connecting");
      try {
        ws = new WebSocket(buildWsUrl("/api/ws"));
      } catch {
        setConnection("disconnected");
        startPolling();
        return;
      }

      ws.onopen = () => {
        setConnection("connected");
        stopPolling();
        setError(null);
        reconnectDelay = 1000;
      };

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as SynoraWsMessage;
          const decision = acceptRealtimeMessage(message, cursorRef.current);

          if (decision.kind === "resync") {
            cursorRef.current = null;
            setError(decision.reason);
            void refresh();
            return;
          }
          if (decision.kind === "ignore") return;
          cursorRef.current = decision.cursor;

          if (message.type === "security.mode.changed" || message.topic === "security.mode.changed") {
            void refresh();
          }

          setLastMessageAt(new Date());

          if (decision.snapshot) {
            setSnapshot(decision.snapshot);
            setLoading(false);
            setError(null);
            setApiStatus("connected");
          }
        } catch (err) {
          console.warn("Invalid Synora WS message", err);
        }
      };

      ws.onerror = () => {
        setConnection("error");
        startPolling();
      };

      ws.onclose = () => {
        if (closedByComponent) return;

        setConnection("disconnected");
        void refresh();
        startPolling();

        reconnectTimer = window.setTimeout(() => {
          connect();
        }, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      };
    }

    connect();

    return () => {
      closedByComponent = true;

      if (reconnectTimer !== null) {
        window.clearTimeout(reconnectTimer);
      }
      stopPolling();

      ws?.close();
    };
  }, []);

  return {
    snapshot,
    loading,
    error,
    connection,
    lastMessageAt,
    refresh,
    apiStatus,
  };
}
