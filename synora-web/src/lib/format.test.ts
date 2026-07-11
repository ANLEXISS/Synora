import { formatDateTime, normalizeSystemState } from "./format";
import { mergeResidentRuntime } from "./useSynoraData";

// Framework-free checks kept callable by any frontend test runner.
export function residentRuntimeUiTest() {
  if (formatDateTime(null) !== "Pas encore vu") {
    throw new Error("null last_seen should be displayed as Pas encore vu");
  }

  const merged = mergeResidentRuntime(
    [{ id: "alexis", state: "unknown", last_seen: "2026-07-11T17:03:56Z" }],
    [{ id: "alexis", state: "absent", last_seen: null }]
  );
  if (merged[0]?.last_seen !== "2026-07-11T17:03:56Z") {
    throw new Error("runtime merge should preserve a non-null last_seen");
  }

  if (normalizeSystemState({ system_state: "unknown" }) !== "—") {
    throw new Error("unknown system state should be displayed as an em dash");
  }
}
