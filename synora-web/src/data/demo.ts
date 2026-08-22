import type { NormalizedTopologyDevice } from "../lib/topology";
import type { ApiTopologyNode, SynoraAutomation, SynoraResident } from "../lib/synora-types";
export type { ApiTopologyNode } from "../lib/synora-types";

export type TopologyDevice = NormalizedTopologyDevice;

export type DemoResident = SynoraResident & {
  name: string;
  role: "owner" | "resident" | "guest" | "child";
  state: "present" | "away" | "unknown";
  presence_score: number;
  node_id: string | null;
  reference_node_id: string | null;
};

export type AutomationActionKind = "device.command" | "record.clip" | "notify" | "siren";
export type AutomationConditionKind = "event.type" | "system.state" | "security.mode" | "security.armed" | "occupancy.expected" | "manual_risk.active" | "node.id" | "danger.level" | "device.id";
export type AutomationOperator = "==" | "!=" | ">" | ">=" | "<" | "<=";

export type AutomationCatalogOption = { value: string; label: string; category?: string };
export type AutomationConditionDefinition = {
  kind: AutomationConditionKind;
  label: string;
  description: string;
  operators: AutomationOperator[];
  values: AutomationCatalogOption[];
};

export type DemoAutomationCondition = {
  kind: AutomationConditionKind;
  operator: AutomationOperator;
  value: string;
};

export type DemoAutomationAction = {
  kind: AutomationActionKind;
  target: string;
  command?: string;
};

export type DemoAutomation = Omit<SynoraAutomation, "name" | "description" | "enabled"> & {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  state: "" | "idle" | "activity" | "suspicious" | "intrusion" | "break-in";
  event_type?: string;
  node_id?: string;
  min_score: number;
  schedule: "always" | { start: string; end: string };
  conditions: DemoAutomationCondition[];
  actions: DemoAutomationAction[];
  last_triggered: string | null;
};

export const demoApiTopology: ApiTopologyNode[] = [];

export function prettyTopologyName(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

const values = (items: Array<[string, string]>): AutomationCatalogOption[] => items.map(([value, label]) => ({ value, label }));

export const automationEventTypeCatalog = values([
  ["vision.motion", "Mouvement"], ["vision.identity", "Identité reconnue"], ["vision.unknown", "Personne inconnue"],
  ["vision.weapon", "Objet dangereux"], ["vision.tamper", "Sabotage"], ["device.offline", "Périphérique hors ligne"],
]);
export const automationSystemStateCatalog = values([["idle", "Repos"], ["activity", "Activité"], ["suspicious", "Suspect"], ["intrusion", "Intrusion"]]);
export const automationSecurityModeCatalog = values([["home", "Maison"], ["night", "Nuit"], ["away", "Absent"], ["high_security", "Haute sécurité"]]);
export const automationSecurityArmedCatalog = values([["true", "Armée"], ["false", "Désarmée"]]);
export const automationExpectedOccupancyCatalog = values([["occupied", "Occupée"], ["empty", "Vide"], ["unknown", "Inconnue"]]);
export const automationManualRiskCatalog = values([["true", "Actif"], ["false", "Inactif"]]);
export const automationDangerLevelCatalog = values([["low", "Faible"], ["medium", "Moyen"], ["high", "Élevé"], ["critical", "Critique"]]);
export const automationNotifyTargetCatalog = values([["admins", "Administrateurs"], ["residents", "Résidents"], ["emergency_contact", "Contact d’urgence"]]);
export const automationOperatorLabels: Record<AutomationOperator, string> = { "==": "est", "!=": "n’est pas", ">": ">", ">=": "≥", "<": "<", "<=": "≤" };

export const automationActionTypeCatalog: Array<{ kind: AutomationActionKind; label: string }> = [
  { kind: "device.command", label: "Commander un périphérique" },
  { kind: "record.clip", label: "Enregistrer un clip" },
  { kind: "notify", label: "Notifier" },
  { kind: "siren", label: "Déclencher la sirène" },
];
export const automationActionCommandCatalog: Record<AutomationActionKind, AutomationCatalogOption[]> = {
  "device.command": values([["on", "Allumer"], ["off", "Éteindre"], ["lock", "Verrouiller"]]),
  "record.clip": values([["record", "Enregistrer"]]),
  notify: values([["notify", "Notifier"]]),
  siren: values([["on", "Activer"], ["off", "Désactiver"]]),
};
