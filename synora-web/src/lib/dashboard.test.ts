import {
  filterDashboardEvents,
  normalizeDashboardDanger,
  normalizeDashboardResidents,
  normalizeDashboardSystemState,
} from "./dashboard";

export function dashboardNormalizationFixtureTest() {
  const runtime = {
    current_state: "suspicious",
    danger_level: "high",
    danger_score: 0.75,
    danger_source: "manual",
    manual_risk_active: true,
  };
  if (normalizeDashboardSystemState(runtime, {}) !== "Suspect") {
    throw new Error("runtime suspicious state should be displayed as Suspect");
  }
  const danger = normalizeDashboardDanger(runtime, {});
  if (danger.level !== "high" || danger.score !== 0.75 || danger.source !== "manual" || !danger.manualRiskActive) {
    throw new Error("manual high runtime danger was not normalized");
  }
}

export function dashboardResidentsFixtureTest() {
  const result = normalizeDashboardResidents(
    [{ id: "alexis" }, { id: "sam" }, { id: "lee" }],
    { residents: [] },
  );
  if (result.known !== 3 || result.present !== 0) {
    throw new Error("dashboard should distinguish three known residents from zero present");
  }
}

export function dashboardEventsFixtureTest() {
  const events = filterDashboardEvents([
    { id: "crashed", type: "discovery.worker.crashed", timestamp: "2026-07-13T10:03:00Z" },
    { id: "action", type: "action.result", timestamp: "2026-07-13T10:02:00Z" },
    { id: "manual", type: "manual.risk", timestamp: "2026-07-13T10:01:00Z" },
  ]);
  const types = events.map((event) => event.type);
  if (types.includes("discovery.worker.crashed") || !types.includes("manual.risk") || !types.includes("action.result")) {
    throw new Error("dashboard event filter did not prioritize significant events");
  }
}

export function dashboardCgeRiskFixtureTest() {
  const danger = normalizeDashboardDanger({ danger_level: "high", danger_score: 0.75 }, {});
  if (danger.level === "none" || danger.score !== 0.75) {
    throw new Error("CGE risk should remain active when runtime danger is high");
  }
}
