import { acceptRealtimeMessage as acceptCore, extractSnapshot as extractCore } from "./realtime-core.mjs";
import type { SynoraSnapshot, SynoraWsMessage } from "./synora-types";

export type RealtimeCursor = {
  epoch: string;
  sequence: number;
  revision: number;
};

export type RealtimeDecision =
  | { kind: "ignore" }
  | { kind: "resync"; reason: string }
  | { kind: "accept"; snapshot: SynoraSnapshot | null; cursor: RealtimeCursor | null };

export function extractSnapshot(message: SynoraWsMessage): SynoraSnapshot | null {
  return extractCore(message) as SynoraSnapshot | null;
}

export function acceptRealtimeMessage(message: SynoraWsMessage, current: RealtimeCursor | null): RealtimeDecision {
  return acceptCore(message, current) as RealtimeDecision;
}
