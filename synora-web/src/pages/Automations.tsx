import {
  Activity,
  Bell,
  CalendarClock,
  CheckCircle2,
  CirclePause,
  Clock,
  Cpu,
  Lightbulb,
  Pencil,
  Play,
  Plus,
  Radio,
  Save,
  Search,
  ShieldAlert,
  SlidersHorizontal,
  Trash2,
  Workflow,
  X,
  Zap,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Panel } from "../components/Panel";
import { useSynoraData } from "../hooks/useSynoraData";
import { useAuth } from "../hooks/useAuth";
import {
  createAutomation,
  deleteAutomation,
  getAutomations,
  updateAutomation,
} from "../lib/synora-api";
import {
  automationPayloadMatches,
  automationToFormState,
  formStateToAutomationPayload,
  type AutomationFormAction,
  type AutomationFormCondition,
  type AutomationFormState,
} from "../lib/automation-form";
import {
  automationActionCommandCatalog,
  automationActionTypeCatalog,
  automationDangerLevelCatalog,
  automationEventTypeCatalog,
  automationNotifyTargetCatalog,
  automationOperatorLabels,
  automationSystemStateCatalog,
  demoApiTopology,
  demoAutomations,
  demoTopologyDevices,
  prettyTopologyName,
  type AutomationActionKind,
  type AutomationCatalogOption,
  type AutomationConditionDefinition,
  type AutomationConditionKind,
  type AutomationOperator,
  type DemoAutomation,
  type DemoAutomationAction,
  type DemoAutomationCondition,
} from "../data/demo";

type AutomationFilter = "all" | "enabled" | "disabled";
type StateFilter = "all" | DemoAutomation["state"];
type JoinOperator = "AND" | "OR";

function normalizeAutomationState(value: string | undefined): DemoAutomation["state"] {
  return value === "idle" || value === "activity" || value === "suspicious" ||
    value === "intrusion" || value === "break-in"
    ? value
    : "";
}

type BuilderCondition = AutomationFormCondition;
type BuilderAction = AutomationFormAction;

function uid(prefix: string) {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return `${prefix}_${crypto.randomUUID()}`;
  }

  return `${prefix}_${Math.random().toString(36).slice(2)}`;
}

function flattenRooms() {
  return demoApiTopology.flatMap((zone) =>
    (zone.children ?? []).flatMap((floor) =>
      (floor.children ?? [])
        .filter((node) => node.type === "room")
        .map((room) => ({
          id: room.id,
          name: prettyTopologyName(room.name),
          floor: floor.name,
          zone: prettyTopologyName(zone.name),
        }))
    )
  );
}

function roomOptions(): AutomationCatalogOption[] {
  return flattenRooms().map((room) => ({
    value: room.id,
    label: `${room.name} · ${room.floor}`,
    category: room.zone,
  }));
}

function deviceOptions(filter?: "camera" | "siren"): AutomationCatalogOption[] {
  const devices = filter
    ? demoTopologyDevices.filter((device) => device.type === filter)
    : demoTopologyDevices;

  if (filter === "siren" && devices.length === 0) {
    return [{ value: "siren_main", label: "Sirène principale" }];
  }

  return devices.map((device) => ({
    value: device.id,
    label: device.name,
    category: prettyTopologyName(device.type),
  }));
}

function buildConditionCatalog(): AutomationConditionDefinition[] {
  return [
    {
      kind: "event.type",
      label: "Événement",
      description: "Type d’événement détecté par Synora.",
      operators: ["==", "!="],
      values: automationEventTypeCatalog,
    },
    {
      kind: "system.state",
      label: "État système",
      description: "État global de sécurité.",
      operators: ["==", "!="],
      values: automationSystemStateCatalog,
    },
    {
      kind: "node.id",
      label: "Pièce",
      description: "Pièce ou zone concernée.",
      operators: ["==", "!="],
      values: roomOptions(),
    },
    {
      kind: "danger.level",
      label: "Niveau de danger",
      description: "Niveau de risque estimé.",
      operators: ["==", "!=", ">", "<"],
      values: automationDangerLevelCatalog,
    },
    {
      kind: "device.id",
      label: "Périphérique",
      description: "Périphérique concerné.",
      operators: ["==", "!="],
      values: deviceOptions(),
    },
  ];
}

function automationTone(automation: DemoAutomation) {
  if (!automation.enabled) return "neutral";
  if (automation.state === "break-in" || automation.state === "intrusion") {
    return "danger";
  }
  if (automation.state === "suspicious") return "warning";
  return "success";
}

function stateLabel(state: DemoAutomation["state"]) {
  if (state === "") return "Tous états";
  if (state === "idle") return "Repos";
  if (state === "activity") return "Activité";
  if (state === "suspicious") return "Suspect";
  if (state === "intrusion") return "Intrusion";
  if (state === "break-in") return "Effraction";

  return state;
}

function scheduleLabel(schedule: DemoAutomation["schedule"]) {
  if (schedule === "always") return "Toujours actif";

  return `${schedule.start} → ${schedule.end}`;
}

function findOptionLabel(options: AutomationCatalogOption[], value: string) {
  return options.find((option) => option.value === value)?.label ?? value;
}

function conditionLabel(
  condition: DemoAutomationCondition,
  catalog: AutomationConditionDefinition[]
) {
  const definition = catalog.find((item) => item.kind === condition.kind);
  const operator = automationOperatorLabels[condition.operator];
  const value = definition
    ? findOptionLabel(definition.values, condition.value)
    : condition.value;

  return `${definition?.label ?? condition.kind} ${operator} ${value}`;
}

function actionLabel(action: DemoAutomationAction) {
  const definition = automationActionTypeCatalog.find(
    (item) => item.kind === action.kind
  );

  const targetOptions =
    action.kind === "notify"
      ? automationNotifyTargetCatalog
      : action.kind === "record.clip"
        ? deviceOptions("camera")
        : action.kind === "siren"
          ? deviceOptions("siren")
          : deviceOptions();

  const commandOptions = automationActionCommandCatalog[action.kind];

  const target = findOptionLabel(targetOptions, action.target);
  const command = action.command
    ? findOptionLabel(commandOptions, action.command)
    : "";

  return {
    type: definition?.label ?? action.kind,
    target,
    command,
  };
}

function actionIcon(type: AutomationActionKind) {
  if (type === "device.command") return Lightbulb;
  if (type === "record.clip") return Radio;
  if (type === "notify") return Bell;
  if (type === "siren") return ShieldAlert;

  return Zap;
}

function nodeLabel(nodeId?: string) {
  if (!nodeId) return "Global";

  return findOptionLabel(roomOptions(), nodeId);
}

function filterMatches(automation: DemoAutomation, filter: AutomationFilter) {
  if (filter === "all") return true;
  if (filter === "enabled") return automation.enabled;
  if (filter === "disabled") return !automation.enabled;

  return true;
}

function createCondition(
  catalog: AutomationConditionDefinition[],
  kind: AutomationConditionKind = "event.type",
  join: JoinOperator = "AND"
): BuilderCondition {
  const definition = catalog.find((item) => item.kind === kind) ?? catalog[0];

  return {
    id: uid("condition"),
    join,
    kind: definition.kind,
    operator: definition.operators[0],
    value: definition.values[0]?.value ?? "",
  };
}

function createAction(kind: AutomationActionKind = "notify"): BuilderAction {
  const target =
    kind === "notify"
      ? automationNotifyTargetCatalog[0]?.value
      : kind === "record.clip"
        ? deviceOptions("camera")[0]?.value
        : kind === "siren"
          ? deviceOptions("siren")[0]?.value
          : deviceOptions()[0]?.value;

  const command = automationActionCommandCatalog[kind][0]?.value;

  return {
    id: uid("action"),
    kind,
    target: target ?? "",
    command,
    enabled: true,
  };
}

function automationForList(
  automation: import("../lib/synora-types").SynoraAutomation,
  catalog: AutomationConditionDefinition[]
): DemoAutomation {
  const form = automationToFormState(automation, catalog);
  return {
    id: form.id,
    name: form.name,
    description: form.description,
    enabled: form.enabled,
    state: normalizeAutomationState(form.trigger.state),
    event_type: form.trigger.event_type,
    node_id: form.trigger.node_id,
    min_score: typeof form.trigger.min_score === "number" ? form.trigger.min_score : 0,
    schedule: form.schedule,
    conditions: form.conditions.map(({ id: _id, join: _join, source: _source, ...condition }) => condition),
    actions: form.actions.map(({ id: _id, enabled: _enabled, source: _source, ...action }) => action),
    last_triggered: typeof automation["last_triggered"] === "string" ? automation["last_triggered"] : null,
  };
}

export function Automations() {
  const data = useSynoraData();
  const auth = useAuth();
  const conditionCatalog = useMemo(() => buildConditionCatalog(), []);

  const [automations, setAutomations] =
    useState<DemoAutomation[]>(demoAutomations);

  useEffect(() => {
    if (data.automations.length === 0) return;
    setAutomations(
      data.automations.map((automation) => automationForList(automation, conditionCatalog))
    );
  }, [conditionCatalog, data.automations]);

  const [builderOpen, setBuilderOpen] = useState(false);
  const [editingAutomationId, setEditingAutomationId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const [search, setSearch] = useState("");
  const [automationFilter, setAutomationFilter] =
    useState<AutomationFilter>("all");
  const [stateFilter, setStateFilter] = useState<StateFilter>("all");

  const [ruleName, setRuleName] = useState("Nouvelle règle");
  const [ruleDescription, setRuleDescription] = useState(
    "Décrire ce que cette automatisation doit faire."
  );
  const [enabled, setEnabled] = useState(true);
  const [conditionMode, setConditionMode] = useState<JoinOperator>("AND");
  const [scheduleMode, setScheduleMode] = useState<"always" | "range">("always");
  const [scheduleStart, setScheduleStart] = useState("22:00");
  const [scheduleEnd, setScheduleEnd] = useState("06:00");

  const [conditions, setConditions] = useState<BuilderCondition[]>([
    createCondition(conditionCatalog, "event.type"),
    createCondition(conditionCatalog, "node.id"),
    createCondition(conditionCatalog, "danger.level"),
  ]);

  const [actions, setActions] = useState<BuilderAction[]>([
    createAction("notify"),
    createAction("record.clip"),
  ]);

  const filteredAutomations = useMemo(() => {
    const query = search.trim().toLowerCase();

    return automations.filter((automation) => {
      const readableConditions = automation.conditions
        .map((condition) => conditionLabel(condition, conditionCatalog))
        .join(" ")
        .toLowerCase();

      const readableActions = automation.actions
        .map((action) => {
          const labels = actionLabel(action);
          return `${labels.type} ${labels.target} ${labels.command}`;
        })
        .join(" ")
        .toLowerCase();

      const matchSearch =
        query.length === 0 ||
        automation.id.toLowerCase().includes(query) ||
        automation.name.toLowerCase().includes(query) ||
        automation.description.toLowerCase().includes(query) ||
        readableConditions.includes(query) ||
        readableActions.includes(query) ||
        nodeLabel(automation.node_id).toLowerCase().includes(query);

      const matchAutomationFilter = filterMatches(automation, automationFilter);
      const matchState =
        stateFilter === "all" || automation.state === stateFilter;

      return matchSearch && matchAutomationFilter && matchState;
    });
  }, [automations, automationFilter, conditionCatalog, search, stateFilter]);

  const activeCount = automations.filter((automation) => automation.enabled).length;
  const disabledCount = automations.filter(
    (automation) => !automation.enabled
  ).length;
  const criticalCount = automations.filter(
    (automation) =>
      automation.enabled &&
      (automation.state === "intrusion" || automation.state === "break-in")
  ).length;
  const scheduledCount = automations.filter(
    (automation) => automation.schedule !== "always"
  ).length;

  function updateCondition(id: string, patch: Partial<BuilderCondition>) {
    setConditions((items) =>
      items.map((item) => (item.id === id ? { ...item, ...patch } : item))
    );
  }

  function changeConditionKind(id: string, kind: AutomationConditionKind) {
    const definition = conditionCatalog.find((item) => item.kind === kind);

    if (!definition) return;

    updateCondition(id, {
      kind,
      operator: definition.operators[0],
      value: definition.values[0]?.value ?? "",
    });
  }

  function updateAction(id: string, patch: Partial<BuilderAction>) {
    setActions((items) =>
      items.map((item) => (item.id === id ? { ...item, ...patch } : item))
    );
  }

  function changeActionKind(id: string, kind: AutomationActionKind) {
    const next = createAction(kind);

    updateAction(id, {
      kind,
      target: next.target,
      command: next.command,
    });
  }

function resetBuilder() {
    setEditingAutomationId(null);
    setRuleName("Nouvelle règle");
    setRuleDescription("Décrire ce que cette automatisation doit faire.");
    setEnabled(true);
    setConditionMode("AND");
    setScheduleMode("always");
    setScheduleStart("22:00");
    setScheduleEnd("06:00");

    setConditions([
      createCondition(conditionCatalog, "event.type"),
      createCondition(conditionCatalog, "node.id"),
      createCondition(conditionCatalog, "danger.level"),
    ]);

    setActions([
      createAction("notify"),
      createAction("record.clip"),
    ]);
  }

function openCreateBuilder() {
  resetBuilder();
  setBuilderOpen(true);
}

function closeBuilder() {
  setBuilderOpen(false);
  setEditingAutomationId(null);
}

function openEditBuilder(automation: DemoAutomation) {
  const selectedAutomation = data.automations.find((item) => item.id === automation.id);
  const initialState: AutomationFormState = selectedAutomation
    ? automationToFormState(selectedAutomation, conditionCatalog)
    : {
        id: automation.id,
        name: automation.name,
        description: automation.description,
        enabled: automation.enabled,
        trigger: {
          event_type: automation.event_type,
          node_id: automation.node_id,
          min_score: automation.min_score,
          state: automation.state,
        },
        conditionMode: "AND",
        conditions: automation.conditions.map((condition, index) => ({
          id: uid("condition"),
          join: index === 0 ? "AND" : "AND",
          kind: condition.kind,
          operator: condition.operator,
          value: condition.value,
        })),
        actions: automation.actions.map((action) => ({
          id: uid("action"),
          kind: action.kind,
          target: action.target,
          command: action.command,
          enabled: true,
        })),
        schedule: automation.schedule,
        metadata: {},
      };

  setEditingAutomationId(initialState.id);
  setRuleName(initialState.name);
  setRuleDescription(initialState.description);
  setEnabled(initialState.enabled);
  setConditionMode(initialState.conditionMode);
  setConditions(initialState.conditions);
  setActions(initialState.actions);

  if (initialState.schedule === "always") {
    setScheduleMode("always");
    setScheduleStart("22:00");
    setScheduleEnd("06:00");
  } else {
    setScheduleMode("range");
    setScheduleStart(initialState.schedule.start);
    setScheduleEnd(initialState.schedule.end);
  }

  setBuilderOpen(true);
}

async function createAutomationFromBuilder() {
  const safeName = ruleName.trim() || "Nouvelle règle";
  const safeId = safeName
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_|_$/g, "");

  const automationId = editingAutomationId ?? safeId ?? uid("automation");
  const formState: AutomationFormState = {
    id: automationId,
    name: safeName,
    description: ruleDescription,
    enabled,
    trigger: {},
    conditionMode,
    conditions,
    actions,
    schedule:
      scheduleMode === "always"
        ? "always"
        : {
            start: scheduleStart,
            end: scheduleEnd,
          },
    metadata: {},
  };
  const payload = formStateToAutomationPayload(formState);

  try {
    if (editingAutomationId) {
      await updateAutomation(editingAutomationId, payload);
    } else {
      await createAutomation({ id: automationId, ...payload, description: ruleDescription });
    }
  } catch {
    setNotice("Action non disponible côté backend : automatisation non sauvegardée.");
    return;
  }

  let refreshed;
  try {
    refreshed = await getAutomations();
    await data.refresh();
  } catch {
    setNotice("La sauvegarde de l’automatisation n’a pas été confirmée par le backend.");
    return;
  }

  const confirmed = refreshed.find((item) => item.id === automationId);
  if (!confirmed || (editingAutomationId && !automationPayloadMatches(formState, confirmed, conditionCatalog))) {
    setNotice("La sauvegarde de l’automatisation n’a pas été confirmée par le backend.");
    return;
  }

  setAutomations(refreshed.map((item) => automationForList(item, conditionCatalog)));

  setBuilderOpen(false);
  setEditingAutomationId(null);
  setNotice(editingAutomationId ? "Automatisation mise à jour côté API." : "Automatisation créée côté API.");
}

async function handleDeleteAutomation(id: string) {
  try {
    await deleteAutomation(id);
    await data.refresh();
    setAutomations((items) => items.filter((item) => item.id !== id));
    setNotice("Automatisation supprimée côté API.");
  } catch {
    setNotice("Action non disponible côté backend : automatisation conservée.");
  }
}

  return (
    <div className="automations-layout">
      <div className="automations-stats">
        <Panel className="automation-stat-card">
          <div className="automation-stat-content">
            <div className="automation-stat-icon success">
              <CheckCircle2 size={18} />
            </div>
            <div>
              <strong>{activeCount}</strong>
              <span>Actives</span>
            </div>
          </div>
        </Panel>

        <Panel className="automation-stat-card">
          <div className="automation-stat-content">
            <div className="automation-stat-icon neutral">
              <CirclePause size={18} />
            </div>
            <div>
              <strong>{disabledCount}</strong>
              <span>Désactivées</span>
            </div>
          </div>
        </Panel>

        <Panel className="automation-stat-card">
          <div className="automation-stat-content">
            <div className="automation-stat-icon danger">
              <ShieldAlert size={18} />
            </div>
            <div>
              <strong>{criticalCount}</strong>
              <span>Critiques</span>
            </div>
          </div>
        </Panel>

        <Panel className="automation-stat-card">
          <div className="automation-stat-content">
            <div className="automation-stat-icon warning">
              <CalendarClock size={18} />
            </div>
            <div>
              <strong>{scheduledCount}</strong>
              <span>Planifiées</span>
            </div>
          </div>
        </Panel>
      </div>

      <Panel
        title="Automatisations"
        className="automations-main-panel"
        action={auth.can("automations:write") ? (
          <button
            className="primary-button automations-add-button"
            onClick={openCreateBuilder}
          >
            <Plus size={16} />
            Nouvelle règle
          </button>
        ) : undefined}
      >
        {notice && <div className="auth-error">{notice}</div>}
        <div className="automations-toolbar">
          <label className="automation-search">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Rechercher une règle, une pièce, une action..."
            />
          </label>

          <div className="automation-filters">
            <button
              className={automationFilter === "all" ? "active" : ""}
              onClick={() => setAutomationFilter("all")}
            >
              Toutes
            </button>
            <button
              className={automationFilter === "enabled" ? "active" : ""}
              onClick={() => setAutomationFilter("enabled")}
            >
              Actives
            </button>
            <button
              className={automationFilter === "disabled" ? "active" : ""}
              onClick={() => setAutomationFilter("disabled")}
            >
              Off
            </button>
          </div>

          <div className="automation-state-filter">
            <select
              value={stateFilter}
              onChange={(event) =>
                setStateFilter(event.target.value as StateFilter)
              }
            >
              <option value="all">Tous les états</option>
              <option value="idle">Repos</option>
              <option value="activity">Activité</option>
              <option value="suspicious">Suspect</option>
              <option value="intrusion">Intrusion</option>
              <option value="break-in">Effraction</option>
            </select>
          </div>
        </div>

        <div className="automations-grid">
          {filteredAutomations.map((automation) => {
            const tone = automationTone(automation);
            const score = Math.round(automation.min_score * 100);

            return (
              <article
                className={`automation-card automation-${tone}`}
                key={automation.id}
              >
                <div className="automation-card-header">
                  <div className={`automation-card-icon ${tone}`}>
                    {automation.enabled ? (
                      <Play size={19} />
                    ) : (
                      <CirclePause size={19} />
                    )}
                  </div>

                  <div className="automation-title">
                    <strong>{automation.name}</strong>
                    <span>{automation.id}</span>
                  </div>

                  <div className="automation-badges">
                    <span className={`badge ${automation.enabled ? "success" : ""}`}>
                      {automation.enabled ? "active" : "désactivée"}
                    </span>
                  </div>
                </div>

                <p className="automation-summary">{automation.description}</p>

                <div className="automation-trigger-grid">
                  <div>
                    <span>
                      <Activity size={13} />
                      État
                    </span>
                    <strong>{stateLabel(automation.state)}</strong>
                  </div>

                  <div>
                    <span>
                      <Cpu size={13} />
                      Pièce
                    </span>
                    <strong>{nodeLabel(automation.node_id)}</strong>
                  </div>

                  <div>
                    <span>
                      <CalendarClock size={13} />
                      Horaire
                    </span>
                    <strong>{scheduleLabel(automation.schedule)}</strong>
                  </div>

                  <div>
                    <span>
                      <ShieldAlert size={13} />
                      Danger min.
                    </span>
                    <strong>{score}%</strong>
                  </div>
                </div>

                <div className="automation-conditions">
                  <div className="automation-section-title">
                    <SlidersHorizontal size={14} />
                    Conditions
                  </div>

                  <div className="automation-chip-list">
                    {automation.conditions.map((condition, index) => (
                      <span
                        className="automation-condition-chip"
                        key={`${automation.id}-condition-${index}`}
                      >
                        {conditionLabel(condition, conditionCatalog)}
                      </span>
                    ))}
                  </div>
                </div>

                <div className="automation-actions-list">
                  <div className="automation-section-title">
                    <Zap size={14} />
                    Actions
                  </div>

                  <div className="automation-action-grid">
                    {automation.actions.map((action, index) => {
                      const Icon = actionIcon(action.kind);
                      const labels = actionLabel(action);

                      return (
                        <div
                          className="automation-action-card"
                          key={`${automation.id}-action-${index}`}
                        >
                          <Icon size={15} />
                          <div>
                            <strong>{labels.type}</strong>
                            <span>
                              {labels.target}
                              {labels.command ? ` · ${labels.command}` : ""}
                            </span>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>

                <div className="automation-card-footer">
                  <span className="automation-small-info">
                    <Clock size={14} />
                    {automation.last_triggered ?? "Jamais déclenchée"}
                  </span>

                  {auth.can("automations:write") && (
                    <div className="automation-actions">
                      <button title="Modifier" onClick={() => openEditBuilder(automation)}>
                        <Pencil size={15} />
                      </button>
                      <button title="Supprimer" onClick={() => void handleDeleteAutomation(automation.id)}>
                        <Trash2 size={15} />
                      </button>
                    </div>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      </Panel>

      {builderOpen && (
        <div
          className="automation-modal-backdrop"
          onClick={closeBuilder}
        >
          <section
            className="automation-modal"
            role="dialog"
            aria-modal="true"
            aria-label="Créer une automatisation"
            onClick={(event) => event.stopPropagation()}
          >
            <header className="automation-modal-header">
              <div>
                <strong>
                  {editingAutomationId ? "Modifier l’automatisation" : "Nouvelle automatisation"}
                </strong>
                <span>Construis une règle avec des briques IF / AND / OR / THEN.</span>
              </div>

              <button onClick={() => setBuilderOpen(false)} title="Fermer">
                <X size={18} />
              </button>
            </header>

            <div className="automation-builder">
              <div className="builder-config">
                <label>
                  <span>Nom</span>
                  <input
                    value={ruleName}
                    onChange={(event) => setRuleName(event.target.value)}
                  />
                </label>

                <label>
                  <span>Description</span>
                  <input
                    value={ruleDescription}
                    onChange={(event) => setRuleDescription(event.target.value)}
                  />
                </label>

                <label>
                  <span>Statut</span>
                  <select
                    value={enabled ? "enabled" : "disabled"}
                    onChange={(event) =>
                      setEnabled(event.target.value === "enabled")
                    }
                  >
                    <option value="enabled">Activée</option>
                    <option value="disabled">Désactivée</option>
                  </select>
                </label>
              </div>

              <div className="builder-flow">
                <section className="builder-stage">
                  <div className="builder-stage-header">
                    <div className="builder-stage-badge if">IF</div>
                    <div>
                      <strong>Conditions</strong>
                      <span>Choisis uniquement parmi les capacités Synora.</span>
                    </div>
                  </div>

                  <div className="builder-brick-stack">
                    {conditions.map((condition, index) => {
                      const definition =
                        conditionCatalog.find(
                          (item) => item.kind === condition.kind
                        ) ?? conditionCatalog[0];

                      return (
                        <div
                          className="builder-condition-block"
                          key={condition.id}
                        >
                          {index > 0 && (
                            <div className="logic-switch">
                              <button
                                className={condition.join === "AND" ? "active" : ""}
                                onClick={() =>
                                  updateCondition(condition.id, { join: "AND" })
                                }
                              >
                                AND
                              </button>
                              <button
                                className={condition.join === "OR" ? "active" : ""}
                                onClick={() =>
                                  updateCondition(condition.id, { join: "OR" })
                                }
                              >
                                OR
                              </button>
                            </div>
                          )}

                          <div className="builder-brick condition">
                            <select
                              value={condition.kind}
                              onChange={(event) =>
                                changeConditionKind(
                                  condition.id,
                                  event.target.value as AutomationConditionKind
                                )
                              }
                            >
                              {conditionCatalog.map((item) => (
                                <option key={item.kind} value={item.kind}>
                                  {item.label}
                                </option>
                              ))}
                            </select>

                            <select
                              value={condition.operator}
                              onChange={(event) =>
                                updateCondition(condition.id, {
                                  operator: event.target.value as AutomationOperator,
                                })
                              }
                            >
                              {definition.operators.map((operator) => (
                                <option key={operator} value={operator}>
                                  {automationOperatorLabels[operator]}
                                </option>
                              ))}
                            </select>

                            <select
                              value={condition.value}
                              onChange={(event) =>
                                updateCondition(condition.id, {
                                  value: event.target.value,
                                })
                              }
                            >
                              {definition.values.map((option) => (
                                <option key={option.value} value={option.value}>
                                  {option.label}
                                </option>
                              ))}
                            </select>

                            <button
                              className="brick-remove"
                              onClick={() =>
                                setConditions((items) =>
                                  items.filter((item) => item.id !== condition.id)
                                )
                              }
                              title="Supprimer"
                            >
                              <X size={15} />
                            </button>
                          </div>
                        </div>
                      );
                    })}
                  </div>

                  <button
                    className="builder-add-brick"
                    onClick={() =>
                      setConditions((items) => [
                        ...items,
                        createCondition(conditionCatalog),
                      ])
                    }
                  >
                    <Plus size={15} />
                    Ajouter une condition
                  </button>
                </section>

                <div className="builder-then-arrow">
                  <Workflow size={20} />
                  THEN
                </div>

                <section className="builder-stage">
                  <div className="builder-stage-header">
                    <div className="builder-stage-badge then">THEN</div>
                    <div>
                      <strong>Actions</strong>
                      <span>Ce que Synora doit exécuter.</span>
                    </div>
                  </div>

                  <div className="builder-brick-stack">
                    {actions.map((action) => {
                      const targetOptions =
                        action.kind === "notify"
                          ? automationNotifyTargetCatalog
                          : action.kind === "record.clip"
                            ? deviceOptions("camera")
                            : action.kind === "siren"
                              ? deviceOptions("siren")
                              : deviceOptions();

                      const commandOptions =
                        automationActionCommandCatalog[action.kind];

                      return (
                        <div className="builder-brick action" key={action.id}>
                          <select
                            value={action.kind}
                            onChange={(event) =>
                              changeActionKind(
                                action.id,
                                event.target.value as AutomationActionKind
                              )
                            }
                          >
                            {automationActionTypeCatalog.map((item) => (
                              <option key={item.kind} value={item.kind}>
                                {item.label}
                              </option>
                            ))}
                          </select>

                          <select
                            value={action.target}
                            onChange={(event) =>
                              updateAction(action.id, {
                                target: event.target.value,
                              })
                            }
                          >
                            {targetOptions.map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </select>

                          <select
                            value={action.command ?? ""}
                            onChange={(event) =>
                              updateAction(action.id, {
                                command: event.target.value,
                              })
                            }
                          >
                            {commandOptions.map((option) => (
                              <option key={option.value} value={option.value}>
                                {option.label}
                              </option>
                            ))}
                          </select>

                          <button
                            className="brick-remove"
                            onClick={() =>
                              setActions((items) =>
                                items.filter((item) => item.id !== action.id)
                              )
                            }
                            title="Supprimer"
                          >
                            <X size={15} />
                          </button>
                        </div>
                      );
                    })}
                  </div>

                  <button
                    className="builder-add-brick"
                    onClick={() =>
                      setActions((items) => [...items, createAction()])
                    }
                  >
                    <Plus size={15} />
                    Ajouter une action
                  </button>
                </section>
              </div>

              <div className="builder-footer">
                <div className="builder-schedule">
                  <select
                    value={scheduleMode}
                    onChange={(event) =>
                      setScheduleMode(event.target.value as "always" | "range")
                    }
                  >
                    <option value="always">Toujours actif</option>
                    <option value="range">Plage horaire</option>
                  </select>

                  {scheduleMode === "range" && (
                    <>
                      <input
                        type="time"
                        value={scheduleStart}
                        onChange={(event) => setScheduleStart(event.target.value)}
                      />
                      <input
                        type="time"
                        value={scheduleEnd}
                        onChange={(event) => setScheduleEnd(event.target.value)}
                      />
                    </>
                  )}
                </div>

                <button
                  className="primary-button builder-save-button"
                  onClick={createAutomationFromBuilder}
                >
                  <Save size={16} />
                  {editingAutomationId ? "Enregistrer" : "Créer la règle"}
                </button>
              </div>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
