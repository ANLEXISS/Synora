import { useMemo } from "react";
import { useSynoraSnapshot } from "./useSynoraSnapshot";

function toArray<T>(value: T[] | Record<string, T> | undefined): T[] {
  if (!value) return [];
  if (Array.isArray(value)) return value;

  return Object.values(value);
}

export function useSynoraData() {
  const state = useSynoraSnapshot();

  const devices = useMemo(
    () => toArray(state.snapshot?.devices),
    [state.snapshot?.devices]
  );

  const residents = useMemo(
    () => toArray(state.snapshot?.residents),
    [state.snapshot?.residents]
  );

  const automations = useMemo(
    () => toArray(state.snapshot?.automations),
    [state.snapshot?.automations]
  );

  const events = useMemo(
    () => state.snapshot?.events ?? [],
    [state.snapshot?.events]
  );

  const topology = useMemo(
    () => state.snapshot?.topology ?? [],
    [state.snapshot?.topology]
  );

  return {
    ...state,
    devices,
    residents,
    automations,
    events,
    topology,
    systemState: state.snapshot?.system_state ?? state.snapshot?.state ?? "unknown",
    dangerScore: state.snapshot?.danger_score ?? 0,
  };
}