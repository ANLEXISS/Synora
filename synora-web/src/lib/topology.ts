import type {
  ApiTopologyNode,
  SynoraDevice,
} from "./synora-types";

type FlatTopologyNode = {
  id: string;
  name?: string;
  type?: string;
  parent?: string;
  neighbors?: string[];
  connect?: string[];
  children?: unknown[];
  dynamic_score?: number;
};

export type TopologySource =
  | "api"
  | "snapshot"
  | "empty"
  | "unrecognized"
  | "unavailable"
  | "loading";

export function normalizeTopologyResponse(input: unknown): ApiTopologyNode[] {
  if (Array.isArray(input)) {
    return normalizeTree(input);
  }

  if (!isRecord(input)) return [];

  if (input.topology !== undefined) {
    return normalizeTopologyResponse(input.topology);
  }

  if (Array.isArray(input.nodes)) {
    return normalizeFlat(input.nodes, Array.isArray(input.links) ? input.links : []);
  }

  return [];
}

export function isRecognizedTopologyResponse(input: unknown): boolean {
  if (Array.isArray(input)) return true;
  if (!isRecord(input)) return false;
  if (input.topology !== undefined) return isRecognizedTopologyResponse(input.topology);
  return Array.isArray(input.nodes);
}

function normalizeTree(input: unknown[]): ApiTopologyNode[] {
  return input.flatMap((value) => {
    if (!isRecord(value) || typeof value.id !== "string") return [];
    const children = Array.isArray(value.children) ? normalizeTree(value.children) : [];
    return [{
      id: value.id,
      name: typeof value.name === "string" ? value.name : value.id,
      type: typeof value.type === "string" ? value.type : "room",
      connect: stringArray(value.connect ?? value.neighbors),
      children,
      dynamic_score: numberValue(value.dynamic_score),
    }];
  });
}

function normalizeFlat(nodesInput: unknown[], linksInput: unknown[]): ApiTopologyNode[] {
  const flatNodes = nodesInput.filter(isRecord).filter(
    (node): node is FlatTopologyNode => typeof node.id === "string"
  );
  const byID = new Map<string, ApiTopologyNode>();
  const parents = new Map<string, string>();

  for (const node of flatNodes) {
    byID.set(node.id, {
      id: node.id,
      name: typeof node.name === "string" ? node.name : node.id,
      type: typeof node.type === "string" ? node.type : "room",
      connect: uniqueStrings(stringArray(node.neighbors ?? node.connect)),
      children: [],
      dynamic_score: numberValue(node.dynamic_score),
    });
    const inferredParent = typeof node.parent === "string"
      ? node.parent
      : inferParent(node.id, node.type);
    if (inferredParent) parents.set(node.id, inferredParent);
  }

  for (const linkValue of linksInput) {
    if (!isRecord(linkValue)) continue;
    const from = typeof linkValue.from === "string" ? linkValue.from : "";
    const to = typeof linkValue.to === "string" ? linkValue.to : "";
    if (!from || !to) continue;
    const fromNode = byID.get(from);
    const toNode = byID.get(to);
    if (fromNode) fromNode.connect = [...(fromNode.connect ?? []), to];
    if (toNode) toNode.connect = [...(toNode.connect ?? []), from];
  }

  const roots: ApiTopologyNode[] = [];
  for (const node of byID.values()) {
    node.connect = uniqueStrings(node.connect ?? []).filter((id) => id !== node.id);
    const parentID = parents.get(node.id);
    const parent = parentID ? byID.get(parentID) : undefined;
    if (parent && parent.id !== node.id) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  return roots;
}

function inferParent(id: string, type: string | undefined): string | null {
  if (type === "zone") return null;
  const separator = id.lastIndexOf(".");
  return separator > 0 ? id.slice(0, separator) : null;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function uniqueStrings(values: string[]): string[] {
  return [...new Set(values)];
}

function numberValue(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function isRecord(value: unknown): value is Record<string, any> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export type NormalizedTopologyDevice = {
  id: string;
  name: string;
  type: "camera" | "light" | "sensor" | "siren" | "lock" | "unknown";
  status: "online" | "offline";
  node_id: string | null;
  role?: string;
  trusted?: boolean;
};

export function normalizeTopologyDevices(
  devices: SynoraDevice[],
  topology: ApiTopologyNode[]
): NormalizedTopologyDevice[] {
  const nodeIDs = new Set(flattenTopology(topology).map((node) => node.id));
  const roomIDs = flattenTopology(topology)
    .filter((node) => node.type === "room")
    .map((node) => node.id);

  return devices.map((device) => {
    const rawNodeID = typeof device.node_id === "string"
      ? device.node_id
      : typeof device.room === "string" ? device.room : "";
    const resolvedNodeID = resolveNodeID(rawNodeID, nodeIDs, roomIDs);
    const rawType = String(device["type"] ?? "unknown");
    const type = ["camera", "light", "sensor", "siren", "lock"].includes(rawType)
      ? rawType as NormalizedTopologyDevice["type"]
      : "unknown";

    return {
      id: device.id,
      name: String(device["name"] ?? device.id),
      type,
      status: device.enabled === false ? "offline" : "online",
      node_id: resolvedNodeID,
      role: typeof device["role"] === "string" ? device["role"] : undefined,
      trusted: typeof device["trusted"] === "boolean" ? device["trusted"] : undefined,
    };
  });
}

function resolveNodeID(raw: string, nodeIDs: Set<string>, roomIDs: string[]): string | null {
  if (nodeIDs.has(raw)) return raw;
  const matches = roomIDs.filter((id) => id.endsWith(`.${raw}`));
  return matches.length === 1 ? matches[0] : null;
}

function flattenTopology(nodes: ApiTopologyNode[]): ApiTopologyNode[] {
  return nodes.flatMap((node) => [node, ...flattenTopology(node.children)]);
}
