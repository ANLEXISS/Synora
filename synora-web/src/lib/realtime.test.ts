import { acceptRealtimeMessage, extractSnapshot, type RealtimeCursor } from "./realtime";

const snapshot = { revision: 2, system_state: "activity" };

export function realtimeAcceptsSnapshotAndNextDelta() {
  const first = acceptRealtimeMessage({
    type: "snapshot",
    epoch: "epoch-1",
    sequence: 1,
    revision: 2,
    payload: { snapshot },
  }, null);
  if (first.kind !== "accept" || first.snapshot?.revision !== 2 || first.cursor?.sequence !== 1) {
    throw new Error("initial snapshot should be accepted with its cursor");
  }
  const next = acceptRealtimeMessage({
    type: "incident.created",
    epoch: "epoch-1",
    sequence: 2,
    revision: 3,
    payload: {},
  }, first.cursor);
  if (next.kind !== "accept" || next.cursor?.revision !== 3) {
    throw new Error("next sequence should be accepted");
  }
}

export function realtimeRejectsDuplicateAndOutOfOrderMessages() {
  const current: RealtimeCursor = { epoch: "epoch-1", sequence: 4, revision: 4 };
  if (acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 4 }, current).kind !== "ignore") {
    throw new Error("duplicate sequence should be ignored");
  }
  if (acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 3 }, current).kind !== "ignore") {
    throw new Error("out-of-order sequence should be ignored");
  }
}

export function realtimeRequestsResyncOnGapOrEpochChange() {
  const current: RealtimeCursor = { epoch: "epoch-1", sequence: 4, revision: 4 };
  if (acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 6 }, current).kind !== "resync") {
    throw new Error("sequence gap should request resync");
  }
  if (acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-2", sequence: 1 }, current).kind !== "resync") {
    throw new Error("epoch change without snapshot should request resync");
  }
}

export function realtimeAcceptsNewEpochOnlyWithSnapshot() {
  const result = acceptRealtimeMessage({
    type: "snapshot",
    epoch: "epoch-2",
    sequence: 1,
    revision: 1,
    payload: { snapshot },
  }, { epoch: "epoch-1", sequence: 40, revision: 40 });
  if (result.kind !== "accept" || result.cursor?.epoch !== "epoch-2" || !result.snapshot) {
    throw new Error("a new epoch should reset from a snapshot");
  }
}

export function realtimeExtractsTypedSnapshotPayload() {
  if (extractSnapshot({ type: "snapshot", payload: { snapshot } })?.revision !== 2) {
    throw new Error("typed snapshot payload should be extracted");
  }
}
