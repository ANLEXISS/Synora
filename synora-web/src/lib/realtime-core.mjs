export function extractSnapshot(message) {
  if (message.snapshot) return message.snapshot;
  if (message.state) return message.state;

  if (message.payload && typeof message.payload === "object" && !Array.isArray(message.payload)) {
    const payload = message.payload;
    if (payload.snapshot && typeof payload.snapshot === "object") return payload.snapshot;
    if (payload.state && typeof payload.state === "object") return payload.state;
    if (message.type === "snapshot" || message.type === "snapshot.initial" || message.type === "snapshot.updated") {
      return payload;
    }
  }

  if (message.data && typeof message.data === "object" && !Array.isArray(message.data)) {
    return message.data;
  }
  return null;
}

function numeric(value) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function messageCursor(message) {
  if (typeof message.epoch !== "string" || numeric(message.sequence) <= 0) return null;
  return { epoch: message.epoch, sequence: numeric(message.sequence), revision: numeric(message.revision) };
}

export function acceptRealtimeMessage(message, current) {
  if (message.type === "resync_required") {
    return { kind: "resync", reason: "Le serveur demande une resynchronisation." };
  }
  if (message.type === "connection.ready") {
    const cursor = messageCursor(message);
    if (!cursor) return { kind: "ignore" };
    if (current && current.epoch === cursor.epoch && cursor.sequence < current.sequence) return { kind: "ignore" };
    return { kind: "accept", snapshot: null, cursor };
  }

  const cursor = messageCursor(message);
  const snapshot = extractSnapshot(message);
  if (current && cursor) {
    if (cursor.epoch !== current.epoch) {
      if (!snapshot) return { kind: "resync", reason: "L’époque WebSocket a changé sans snapshot." };
    } else if (cursor.sequence <= current.sequence) {
      return { kind: "ignore" };
    } else if (cursor.sequence !== current.sequence + 1) {
      return { kind: "resync", reason: "Un trou de séquence WebSocket a été détecté." };
    }
    if (cursor.epoch === current.epoch && cursor.revision > 0 && current.revision > 0 && cursor.revision < current.revision) {
      return { kind: "ignore" };
    }
  }
  return { kind: "accept", snapshot, cursor: cursor ?? current };
}
