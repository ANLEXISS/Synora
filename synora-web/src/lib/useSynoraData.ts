import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getAutomations, getDevices, getResidents, getRuntimeStatus, getSecurityMode, getTopology } from "./synora-api";
import { normalizeSecurityMode, type SecurityModeState } from "./security-mode";
import {
  isRecognizedTopologyResponse,
  normalizeTopologyResponse,
  type TopologySource,
} from "./topology";
import { useSynoraSnapshot } from "./useSynoraSnapshot";
import type { DashboardRuntimeStatus } from "./dashboard";
import type {
  ApiTopologyNode,
  SynoraAutomation,
  SynoraDevice,
  SynoraResident,
} from "./synora-types";

function toArray<T>(value: T[] | Record<string, T> | undefined): T[] {
  if (!value) return [];
  if (Array.isArray(value)) return value;

  return Object.values(value);
}

function mergeRuntimeCollection<T extends { id: string }>(
  configured: T[],
  runtime: T[]
): T[] {
  if (runtime.length === 0) return configured;

  const runtimeByID = new Map(runtime.map((item) => [item.id, item]));
  const merged = configured.map((item) => ({
    ...item,
    ...(runtimeByID.get(item.id) ?? {}),
  }));
  const configuredIDs = new Set(configured.map((item) => item.id));

  return [
    ...merged,
    ...runtime.filter((item) => !configuredIDs.has(item.id)),
  ];
}

function mergeConfiguredRuntimeCollection<T extends { id: string }>(
  configured: T[],
  runtime: T[],
): T[] {
  if (configured.length === 0) return runtime;
  if (runtime.length === 0) return configured;

  const runtimeByID = new Map(runtime.map((item) => [item.id, item]));
  return configured.map((item) => ({
    ...item,
    ...(runtimeByID.get(item.id) ?? {}),
  }));
}

export function mergeResidentRuntime(
  configured: SynoraResident[],
  runtime: SynoraResident[]
): SynoraResident[] {
  if (runtime.length === 0) return configured;

  const runtimeByID = new Map(runtime.map((item) => [item.id, item]));
  const merged = configured.map((item) => {
    const runtimeItem = runtimeByID.get(item.id);
    if (!runtimeItem) return item;

    const result = { ...item, ...runtimeItem };
    if (runtimeItem.last_seen == null && item.last_seen != null) {
      result.last_seen = item.last_seen;
    }
    return result;
  });
  const configuredIDs = new Set(configured.map((item) => item.id));

  return [
    ...merged,
    ...runtime.filter((item) => !configuredIDs.has(item.id)),
  ];
}

export function useSynoraData() {
  const state = useSynoraSnapshot();
  const residentLastSeen = useRef(new Map<string, string>());
  const [remote, setRemote] = useState<{
    devices: SynoraDevice[];
    residents: SynoraResident[];
    automations: SynoraAutomation[];
    topology: ApiTopologyNode[] | null;
    topologySource: Exclude<TopologySource, "snapshot" | "loading">;
  } | null>(null);
  const [remoteError, setRemoteError] = useState<string | null>(null);
  const [runtimeStatus, setRuntimeStatus] = useState<DashboardRuntimeStatus | null>(null);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);
  const [securityMode, setSecurityMode] = useState<SecurityModeState>(normalizeSecurityMode(null));

  const loadRuntimeStatus = useCallback(async (signal?: AbortSignal) => {
    try {
      const [value, mode] = await Promise.all([getRuntimeStatus(signal), getSecurityMode(signal)]);
      if (signal?.aborted) return;
      setRuntimeStatus(value);
      setSecurityMode(mode);
      setRuntimeError(null);
    } catch (cause) {
      if (signal?.aborted) return;
      setRuntimeStatus(null);
      setRuntimeError(cause instanceof Error ? cause.message : "Impossible de charger le statut runtime du CGE.");
    }
  }, []);

  const loadRemote = useCallback(async (signal?: AbortSignal) => {
    const results = await Promise.allSettled([
      getDevices(signal),
      getResidents(signal),
      getAutomations(signal),
      getTopology(signal),
      getRuntimeStatus(signal),
      getSecurityMode(signal),
    ]);
    if (signal?.aborted) return;

    const [devices, residents, automations, topology, runtime, security] = results;
    const failed = results.slice(0, 4).find((result) => result.status === "rejected");
    setRemoteError(failed?.status === "rejected"
      ? failed.reason instanceof Error ? failed.reason.message : "Impossible de charger les données de configuration."
      : null);
    const topologyValue = topology.status === "fulfilled" ? topology.value : {};
    const topologySource: Exclude<TopologySource, "snapshot" | "loading"> =
      topology.status !== "fulfilled"
        ? "unavailable"
        : !isRecognizedTopologyResponse(topologyValue)
          ? "unrecognized"
          : normalizeTopologyResponse(topologyValue).length === 0
            ? "empty"
            : "api";
    setRemote({
      devices: devices.status === "fulfilled" ? devices.value : [],
      residents: residents.status === "fulfilled" ? residents.value : [],
      automations: automations.status === "fulfilled" ? automations.value : [],
      topology: topology.status === "fulfilled" ? normalizeTopologyResponse(topologyValue) : null,
      topologySource,
    });
    if (runtime.status === "fulfilled") {
      setRuntimeStatus(runtime.value);
      setRuntimeError(null);
    } else {
      setRuntimeStatus(null);
      setRuntimeError(runtime.reason instanceof Error ? runtime.reason.message : "Impossible de charger le statut runtime du CGE.");
    }
    if (security.status === "fulfilled") setSecurityMode(security.value);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void loadRemote(controller.signal);

    const refreshTimer = window.setInterval(() => void loadRemote(), 5000);

    return () => {
      controller.abort();
      window.clearInterval(refreshTimer);
    };
  }, [loadRemote, loadRuntimeStatus]);

  useEffect(() => {
    if (state.lastMessageAt) void loadRuntimeStatus();
  }, [state.lastMessageAt, loadRuntimeStatus]);

  const refreshData = useCallback(async () => {
    await Promise.all([state.refresh(), loadRemote()]);
  }, [loadRemote, state.refresh]);

  const devices = useMemo(
    () => mergeConfiguredRuntimeCollection(
      remote?.devices ?? [],
      toArray(state.snapshot?.devices)
    ),
    [remote?.devices, state.snapshot?.devices]
  );

  const residents = useMemo(
    () => {
      const runtime = toArray(state.snapshot?.residents).map((resident) => {
        const currentLastSeen =
          typeof resident.last_seen === "string" && resident.last_seen.trim()
            ? resident.last_seen
            : null;
        const previousLastSeen = residentLastSeen.current.get(resident.id);
        const lastSeen = currentLastSeen ?? previousLastSeen;

        if (lastSeen) {
          residentLastSeen.current.set(resident.id, lastSeen);
        }

        return lastSeen && lastSeen !== resident.last_seen
          ? { ...resident, last_seen: lastSeen }
          : resident;
      });

      return mergeResidentRuntime(remote?.residents ?? [], runtime);
    },
    [remote?.residents, state.snapshot?.residents]
  );

  const automations = useMemo(
    () => mergeRuntimeCollection(
      remote?.automations ?? [],
      toArray(state.snapshot?.automations)
    ),
    [remote?.automations, state.snapshot?.automations]
  );

  const events = useMemo(
    () => state.snapshot?.events ?? state.snapshot?.recent_events ?? [],
    [state.snapshot?.events, state.snapshot?.recent_events]
  );

  const snapshotTopology = useMemo(
    () => normalizeTopologyResponse(state.snapshot?.nodes ?? []),
    [state.snapshot?.nodes]
  );
  const topology = useMemo(
    () => (remote?.topology?.length ? remote.topology : snapshotTopology),
    [remote?.topology, snapshotTopology]
  );
  const topologySource: TopologySource = remote?.topology?.length
    ? "api"
    : snapshotTopology.length > 0
      ? "snapshot"
      : remote?.topologySource ?? "loading";

  return {
    ...state,
    error: state.error ?? remoteError ?? runtimeError,
    refresh: refreshData,
    devices,
    residents,
    automations,
    events,
    topology,
    topologySource,
    runtimeStatus,
    securityMode,
  };
}
