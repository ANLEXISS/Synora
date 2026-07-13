export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function normalizeArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? value as T[] : [];
}

export function normalizeCollection<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[];
  return isRecord(value) ? Object.values(value) as T[] : [];
}

export function normalizeStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

export function normalizeString(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

export function normalizeNumber(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function normalizeBoolean(value: unknown, fallback = false): boolean {
  return typeof value === "boolean" ? value : fallback;
}

export function normalizeDateString(value: unknown, fallback = ""): string {
  if (typeof value !== "string" || !value.trim() || !Number.isFinite(Date.parse(value))) return fallback;
  return value;
}

export function normalizeDangerLevel(value: unknown): "none" | "low" | "medium" | "medium_high" | "high" | "critical" {
  switch (value) {
    case "low":
	case "medium":
	case "medium_high":
    case "high":
    case "critical":
    case "none":
      return value;
    default:
      return "none";
  }
}
