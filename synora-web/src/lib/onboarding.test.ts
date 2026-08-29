import { buildV1OnboardingSteps } from "./onboarding";

export function v1OnboardingFixtureTest() {
  const fresh = buildV1OnboardingSteps({ available: true, rooms: 1, cameras: 0, residents: 0, residentsWithFace: 0 });
  if (!fresh.find((step) => step.id === "camera" && !step.done)?.detail.includes("0/3")) {
    throw new Error("fresh onboarding should signal the missing camera configuration");
  }
  if (!fresh.find((step) => step.id === "resident")?.done || !fresh.find((step) => step.id === "face")?.done) {
    throw new Error("zero residents should not block onboarding");
  }

  const complete = buildV1OnboardingSteps({ available: true, rooms: 3, cameras: 3, residents: 1, residentsWithFace: 1 });
  if (complete.some((step) => !step.done)) throw new Error("complete onboarding should have no pending step");

  const unavailable = buildV1OnboardingSteps({ available: false, rooms: 0, cameras: 1, residents: 0, residentsWithFace: 0 });
  if (unavailable[0]?.done || unavailable[1]?.done) throw new Error("unavailable installation should remain resumable");
}
