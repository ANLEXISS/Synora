const dateFormatter = new Intl.DateTimeFormat("fr-FR", {
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});

const timeFormatter = new Intl.DateTimeFormat("fr-FR", {
  hour: "2-digit",
  minute: "2-digit",
});

function parseDate(value: string | null | undefined): Date | null {
  if (!value || !value.trim()) return null;

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDateTime(value: string | null | undefined): string {
  if (!value || !value.trim()) return "Pas encore vu";

  const date = parseDate(value);
  return date
    ? `${dateFormatter.format(date)} · ${timeFormatter.format(date)}`
    : "Date invalide";
}

export function formatRelativeDateTime(value: string | null | undefined): string {
  if (!value || !value.trim()) return "Pas encore vu";

  const date = parseDate(value);
  if (!date) return "Date invalide";

  const now = new Date();
  if (date.toLocaleDateString("fr-FR") === now.toLocaleDateString("fr-FR")) {
    return `Aujourd’hui · ${timeFormatter.format(date)}`;
  }

  return `${dateFormatter.format(date)} · ${timeFormatter.format(date)}`;
}

export function normalizeSystemState(
  snapshot: Record<string, unknown> | null | undefined
): string {
  const system = snapshot?.system;
  const systemRecord =
    system && typeof system === "object" && !Array.isArray(system)
      ? (system as Record<string, unknown>)
      : undefined;
  const raw = [
    snapshot?.system_state,
    snapshot?.state,
    snapshot?.house_state,
    systemRecord?.state,
    systemRecord?.status,
    snapshot?.status,
  ].find((value) => typeof value === "string" && value.trim().length > 0);
  const state = typeof raw === "string" ? raw.trim().toLowerCase() : "";

  switch (state) {
    case "idle":
      return "Repos";
    case "activity":
      return "Activité";
    case "suspicious":
      return "Suspect";
    case "intrusion":
      return "Intrusion";
    case "break-in":
      return "Effraction";
    default:
      return "—";
  }
}

export function normalizeDangerScore(
  snapshot: Record<string, unknown> | null | undefined
): number {
  const value = [snapshot?.danger_score, snapshot?.danger, snapshot?.risk_score].find(
    (candidate) => typeof candidate === "number"
  );

  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
