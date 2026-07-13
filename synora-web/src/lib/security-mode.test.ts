import { normalizeSecurityMode } from "./security-mode";

export function securityModeNormalizationFixtureTest() {
  const high = normalizeSecurityMode({ mode: "high_security", armed: true, expected_occupancy: "empty" });
  if (high.mode !== "high_security" || !high.armed || high.expected_occupancy !== "empty") {
    throw new Error("high_security mode was not normalized");
  }
  const home = normalizeSecurityMode({ mode: "home", armed: true });
  if (home.mode !== "home" || home.armed || home.expected_occupancy !== "unknown") {
    throw new Error("home mode should always be disarmed");
  }
}
