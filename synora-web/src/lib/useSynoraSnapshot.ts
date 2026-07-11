import { useEffect, useRef, useState } from "react";
import { buildWsUrl } from "../lib/config";
import { SynoraApiError } from "./api";
import { getState } from "../lib/synora-api";
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

function extractSnapshot(message: SynoraWsMessage): SynoraSnapshot | null {
  if (message.snapshot) return message.snapshot;
  if (message.state) return message.state;

  if (
    message.payload &&
    typeof message.payload === "object" &&
    !Array.isArray(message.payload)
  ) {
    const payload = message.payload as Record<string, unknown>;

    if (payload.snapshot && typeof payload.snapshot === "object") {
      return payload.snapshot as SynoraSnapshot;
    }

    if (payload.state && typeof payload.state === "object") {
      return payload.state as SynoraSnapshot;
    }

    if (
      message.type === "snapshot.initial" ||
      message.type === "snapshot.updated" ||
      message.topic === "state.snapshot"
    ) {
      return payload as SynoraSnapshot;
    }
  }

  if (message.data && typeof message.data === "object" && !Array.isArray(message.data)) {
    return message.data as SynoraSnapshot;
  }

  if (
    message.type === "snapshot.initial" ||
    message.type === "snapshot.updated" ||
    message.topic === "state.snapshot"
  ) {
    return message as SynoraSnapshot;
  }

  return null;
}

export function useSynoraSnapshot(): UseSynoraSnapshotResult {
  const [snapshot, setSnapshot] = useState<SynoraSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connection, setConnection] =
    useState<SynoraConnectionState>("connecting");
  const [lastMessageAt, setLastMessageAt] = useState<Date | null>(null);
  const [apiStatus, setApiStatus] = useState<UseSynoraSnapshotResult["apiStatus"]>("unavailable");

  const abortRef = useRef<AbortController | null>(null);

  async function refresh() {
    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      setError(null);

      const state = await getState(controller.signal);

      setSnapshot(state);
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
    let reconnectDelay = 1000;

    function connect() {
      setConnection("connecting");

      ws = new WebSocket(buildWsUrl("/api/ws"));

      ws.onopen = () => {
        setConnection("connected");
        setError(null);
        reconnectDelay = 1000;
      };

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as SynoraWsMessage;
          const nextSnapshot = extractSnapshot(message);

          setLastMessageAt(new Date());

          if (nextSnapshot) {
            setSnapshot(nextSnapshot);
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
      };

      ws.onclose = () => {
        if (closedByComponent) return;

        setConnection("disconnected");
        void refresh();

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
