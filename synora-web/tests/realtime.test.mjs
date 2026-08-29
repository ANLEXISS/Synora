import assert from "node:assert/strict";
import test from "node:test";
import { acceptRealtimeMessage, extractSnapshot } from "../src/lib/realtime-core.mjs";

const snapshot = { revision: 2, system_state: "activity" };

test("accepts a snapshot and the next delta", () => {
  const first = acceptRealtimeMessage({ type: "snapshot", epoch: "epoch-1", sequence: 1, revision: 2, payload: { snapshot } }, null);
  assert.equal(first.kind, "accept");
  assert.equal(first.snapshot.revision, 2);
  const next = acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 2, revision: 3, payload: {} }, first.cursor);
  assert.equal(next.kind, "accept");
  assert.equal(next.cursor.revision, 3);
});

test("ignores duplicate and old messages", () => {
  const current = { epoch: "epoch-1", sequence: 4, revision: 4 };
  assert.equal(acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 4 }, current).kind, "ignore");
  assert.equal(acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 3 }, current).kind, "ignore");
});

test("requests resync on a gap or an epoch change without snapshot", () => {
  const current = { epoch: "epoch-1", sequence: 4, revision: 4 };
  assert.equal(acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-1", sequence: 6 }, current).kind, "resync");
  assert.equal(acceptRealtimeMessage({ type: "incident.created", epoch: "epoch-2", sequence: 1 }, current).kind, "resync");
});

test("resets on a new epoch only with a snapshot", () => {
  const result = acceptRealtimeMessage({ type: "snapshot", epoch: "epoch-2", sequence: 1, revision: 1, payload: { snapshot } }, { epoch: "epoch-1", sequence: 40, revision: 40 });
  assert.equal(result.kind, "accept");
  assert.equal(result.cursor.epoch, "epoch-2");
  assert.deepEqual(extractSnapshot({ type: "snapshot", payload: { snapshot } }), snapshot);
});
