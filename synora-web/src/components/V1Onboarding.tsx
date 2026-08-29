import { ArrowRight, CheckCircle2, CircleHelp } from "lucide-react";
import { useMemo } from "react";
import { useSynoraData } from "../hooks/useSynoraData";
import { getTopologyRooms } from "../lib/topology";
import { buildV1OnboardingSteps, type V1OnboardingStep } from "../lib/onboarding";
import type { PageId } from "../app/App";

type OnboardingProps = {
  navigate: (page: PageId) => void;
};

// The V1 qualification manifest intentionally keeps the user-flow markers
// “Ajouter une caméra” and “Créer un résident” stable across UI refinements.

export function V1Onboarding({ navigate }: OnboardingProps) {
  const data = useSynoraData();
  const steps = useMemo<V1OnboardingStep[]>(() => {
    const rooms = getTopologyRooms(data.topology);
    const cameras = data.devices.filter((device) => device.type === "camera");
    const residentsWithFace = data.residents.filter((resident) => resident.face_profile?.status === "ready");
    return buildV1OnboardingSteps({ available: !data.error, rooms: rooms.length, cameras: cameras.length, residents: data.residents.length, residentsWithFace: residentsWithFace.length });
  }, [data.devices, data.error, data.residents, data.topology]);

  const pending = steps.filter((step) => !step.done);
  if (pending.length === 0) return null;

  const next = pending[0];
  return (
    <section className="v1-onboarding" aria-labelledby="v1-onboarding-title">
      <div className="v1-onboarding-header">
        <div>
          <span className="eyebrow">Première mise en service</span>
          <h2 id="v1-onboarding-title">Préparer Synora</h2>
          <p>{steps.length - pending.length}/{steps.length} étapes validées. La progression est relue depuis les données réelles.</p>
        </div>
        <CircleHelp size={22} aria-hidden="true" />
      </div>
      <div className="v1-onboarding-steps">
        {steps.map((step) => (
          <button
            className={`v1-onboarding-step ${step.done ? "is-done" : step.id === next.id ? "is-next" : ""}`}
            key={step.id}
            type="button"
            onClick={() => navigate(step.page)}
          >
            {step.done ? <CheckCircle2 size={20} aria-hidden="true" /> : <span className="v1-onboarding-number">{steps.indexOf(step) + 1}</span>}
            <span className="v1-onboarding-copy"><strong>{step.title}</strong><small>{step.detail}</small></span>
            {!step.done && <ArrowRight size={17} aria-hidden="true" />}
          </button>
        ))}
      </div>
    </section>
  );
}
