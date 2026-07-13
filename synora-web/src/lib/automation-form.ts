import type {
  AutomationActionKind,
  AutomationConditionDefinition,
  AutomationConditionKind,
  AutomationOperator,
} from "../data/demo";
import type {
  SynoraAutomation,
  SynoraAutomationAction,
  SynoraAutomationCondition,
  SynoraAutomationTrigger,
} from "./synora-types";

export type AutomationConditionJoin = "AND" | "OR";

export type AutomationFormCondition = {
  id: string;
  join: AutomationConditionJoin;
  kind: AutomationConditionKind;
  operator: AutomationOperator;
  value: string;
  source?: SynoraAutomationCondition;
};

export type AutomationFormAction = {
  id: string;
  kind: AutomationActionKind;
  target: string;
  command?: string;
  enabled: boolean;
  source?: SynoraAutomationAction;
};

export type AutomationFormState = {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  trigger: SynoraAutomationTrigger;
  conditionMode: AutomationConditionJoin;
  conditions: AutomationFormCondition[];
  actions: AutomationFormAction[];
  schedule: "always" | { start: string; end: string };
  metadata: {
    cooldown_ms?: number;
    timeout_ms?: number;
    retry_count?: number;
    dry_run?: boolean;
    requires_validation?: boolean;
    status?: string;
  };
};

export type AutomationMutationPayload = {
  name: string;
  enabled: boolean;
  trigger: SynoraAutomationTrigger;
  condition_logic: "all" | "any";
  conditions: SynoraAutomationCondition[];
  actions: SynoraAutomationAction[];
};

const knownConditionKinds: AutomationConditionKind[] = [
  "event.type",
  "system.state",
  "node.id",
  "danger.level",
  "security.mode",
  "security.armed",
  "occupancy.expected",
  "manual_risk.active",
  "device.id",
];

const knownActionKinds: AutomationActionKind[] = [
  "device.command",
  "record.clip",
  "notify",
  "siren",
];

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function text(value: unknown, fallback = "") {
  return typeof value === "string" ? value : value == null ? fallback : String(value);
}

function number(value: unknown, fallback = 0) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function sourceID(prefix: string, index: number, value: unknown) {
  const id = text(value).trim();
  return id || `${prefix}_${index + 1}`;
}

function conditionKind(
  condition: SynoraAutomationCondition,
  catalog: AutomationConditionDefinition[]
): AutomationConditionKind {
  const raw = text(condition.field || (condition as Record<string, unknown>).kind);
  const catalogKind = catalog.find((item) => item.kind === raw)?.kind;
  if (catalogKind) return catalogKind;
  return knownConditionKinds.includes(raw as AutomationConditionKind)
    ? raw as AutomationConditionKind
    : "event.type";
}

function conditionOperator(condition: SynoraAutomationCondition): AutomationOperator {
  const raw = text(condition.op || (condition as Record<string, unknown>).operator, "==");
  return raw === "!=" || raw === ">" || raw === ">=" || raw === "<" || raw === "<=" || raw === "==" ? raw : "==";
}

function actionKind(action: SynoraAutomationAction): AutomationActionKind {
  const raw = text(action.type || (action as Record<string, unknown>).kind);
  if (knownActionKinds.includes(raw as AutomationActionKind)) {
    return raw as AutomationActionKind;
  }
  if (raw === "record_clip" || raw === "record.clip") return "record.clip";
  if (raw === "siren.turn_on" || raw === "siren.on") return "siren";
  if (raw === "notify.push" || raw === "notification") return "notify";
  return "device.command";
}

function actionCommand(action: SynoraAutomationAction) {
  const data = asRecord(action.data);
  return text(data.command || action.command || data.action || "", "");
}

function actionTarget(action: SynoraAutomationAction) {
  const data = asRecord(action.data);
  return text(action.target || action.device || action.channel || data.target || "", "");
}

function scheduleState(value: unknown): AutomationFormState["schedule"] {
  if (!value || typeof value !== "object") return "always";
  const schedule = asRecord(value);
  const start = text(schedule.start);
  const end = text(schedule.end);
  return start || end ? { start, end } : "always";
}

function triggerState(automation: SynoraAutomation): SynoraAutomationTrigger {
  return {
    ...(automation.trigger ?? {}),
    event_type: text(automation.trigger?.event_type || automation.event_type),
    ...(text(automation.trigger?.node_id || automation.node_id)
      ? { node_id: text(automation.trigger?.node_id || automation.node_id) }
      : {}),
    ...(automation.trigger?.min_score !== undefined || typeof automation["min_score"] === "number"
      ? { min_score: number(automation.trigger?.min_score ?? automation["min_score"]) }
      : {}),
    ...(text(automation.trigger?.state || automation.state)
      ? { state: text(automation.trigger?.state || automation.state) }
      : {}),
  };
}

export function automationToFormState(
  automation: SynoraAutomation,
  catalog: AutomationConditionDefinition[] = []
): AutomationFormState {
  const rawConditions = Array.isArray(automation.conditions) ? automation.conditions : [];
  const rawActions = Array.isArray(automation.actions) ? automation.actions : [];
  const conditionMode: AutomationConditionJoin =
    text(automation.condition_logic, "all").toLowerCase() === "any" ? "OR" : "AND";

  return {
    id: automation.id,
    name: text(automation.name || automation.title, automation.id),
    description: text(automation.description, "Automatisation Synora"),
    enabled: automation.enabled !== false,
    trigger: triggerState(automation),
    conditionMode,
    conditions: rawConditions.map((condition, index) => ({
      id: sourceID("condition", index, condition.id),
      join: index === 0 ? "AND" : conditionMode,
      kind: conditionKind(condition, catalog),
      operator: conditionOperator(condition),
      value: text(condition.value),
      source: condition,
    })),
    actions: rawActions.map((action, index) => ({
      id: sourceID("action", index, action.id),
      kind: actionKind(action),
      target: actionTarget(action),
      command: actionCommand(action) || undefined,
      enabled: action.enabled !== false,
      source: action,
    })),
    schedule: scheduleState(automation.schedule),
    metadata: {
      cooldown_ms: automation.cooldown_ms,
      timeout_ms: automation.timeout_ms,
      retry_count: automation.retry_count,
      dry_run: automation.dry_run,
      requires_validation: automation.requires_validation,
      status: text(automation.status) || undefined,
    },
  };
}

function triggerFromForm(state: AutomationFormState): SynoraAutomationTrigger {
  const eventType = state.conditions.find((condition) => condition.kind === "event.type")?.value;
  const nodeID = state.conditions.find((condition) => condition.kind === "node.id")?.value;
  const systemState = state.conditions.find((condition) => condition.kind === "system.state")?.value;
  const dangerLevel = state.conditions.find((condition) => condition.kind === "danger.level")?.value;
  const minScore = dangerLevel === "critical" ? 0.9 : dangerLevel === "high" ? 0.7 : dangerLevel === "medium" ? 0.45 : dangerLevel === "low" ? 0.2 : undefined;

  return {
    ...state.trigger,
    ...(eventType ? { event_type: eventType } : {}),
    ...(nodeID ? { node_id: nodeID } : { node_id: undefined }),
    ...(systemState ? { state: systemState } : { state: undefined }),
    ...(minScore === undefined ? {} : { min_score: minScore }),
  };
}

function actionPayload(action: AutomationFormAction): SynoraAutomationAction {
  const source = { ...asRecord(action.source) } as SynoraAutomationAction;
  const sourceData = asRecord(source.data);
  const command = action.command?.trim();
  const sourceType = text(source.type);
  const legacy = !sourceType && (source.device !== undefined || source.command !== undefined);

  if (legacy) {
    return {
      ...source,
      id: action.id,
      device: action.target,
      command: command || undefined,
      enabled: action.enabled,
    };
  }

  return {
    ...source,
    id: action.id,
    type: sourceType || action.kind,
    target: action.target,
    data: command ? { ...sourceData, command } : sourceData,
    enabled: action.enabled,
  };
}

export function formStateToAutomationPayload(
  state: AutomationFormState
): AutomationMutationPayload {
  return {
    name: state.name.trim(),
    enabled: state.enabled,
    trigger: triggerFromForm(state),
    condition_logic: state.conditions.some((condition) => condition.join === "OR") ? "any" : "all",
    conditions: state.conditions.map((condition) => ({
      ...asRecord(condition.source),
      id: condition.id,
      field: condition.kind,
      op: condition.operator,
      value: ["security.armed", "manual_risk.active"].includes(condition.kind)
        ? condition.value === "true"
        : condition.value,
    })),
    actions: state.actions.map(actionPayload),
  };
}

function sortJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortJSON);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, item]) => [key, sortJSON(item)])
  );
}

export function automationPayloadMatches(
  state: AutomationFormState,
  automation: SynoraAutomation,
  catalog: AutomationConditionDefinition[] = []
) {
  return JSON.stringify(sortJSON(formStateToAutomationPayload(state))) ===
    JSON.stringify(sortJSON(formStateToAutomationPayload(automationToFormState(automation, catalog))));
}
