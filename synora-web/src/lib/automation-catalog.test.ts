import {
  automationExpectedOccupancyCatalog,
  automationManualRiskCatalog,
  automationSecurityArmedCatalog,
  automationSecurityModeCatalog,
} from "../data/demo";

export function securityAutomationCatalogFixtureTest() {
  if (automationSecurityModeCatalog.length !== 4 || !automationSecurityModeCatalog.some((item) => item.value === "high_security")) {
    throw new Error("security mode catalog is incomplete");
  }
  if (!automationSecurityArmedCatalog.some((item) => item.value === "true") || !automationExpectedOccupancyCatalog.some((item) => item.value === "empty") || !automationManualRiskCatalog.some((item) => item.value === "true")) {
    throw new Error("security automation boolean/occupancy catalog is incomplete");
  }
}
