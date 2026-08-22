import { ArrowRight, CheckCircle2, CircleHelp } from "lucide-react";
import { useMemo } from "react";
import { useSynoraData } from "../hooks/useSynoraData";
import { getTopologyRooms } from "../lib/topology";
import type { PageId } from "../app/App";

type OnboardingProps = {
  navigate: (page: PageId) => void;
};

type Step = {
  id: string;
  title: string;
  detail: string;
  page: PageId;
  done: boolean;
};

export function V1Onboarding({ navigate }: OnboardingProps) {
  const data = useSynoraData();
  const steps = useMemo<Step[]>(() => {
    const rooms = getTopologyRooms(data.topology);
    const cameras = data.devices.filter((device) => device.type === "camera");
    const residentsWithFace = data.residents.filter((resident) => resident.face_profile?.status === "ready");

    return [
      {
        id: "access",
        title: "Centrale accessible",
        detail: "Compte local authentifié et services Synora joignables.",
        page: "dashboard",
        done: !data.error,
      },
      {
        id: "topology",
        title: "Créer la topologie",
        detail: rooms.length ? `${rooms.length} pièce${rooms.length > 1 ? "s" : ""} configurée${rooms.length > 1 ? "s" : ""}.` : "Définissez au moins une pièce de la maison.",
        page: "home",
        done: rooms.length > 0,
      },
      {
        id: "camera",
        title: "Ajouter une caméra",
        detail: cameras.length ? `${cameras.length} caméra${cameras.length > 1 ? "s" : ""} enregistrée${cameras.length > 1 ? "s" : ""}.` : "Appairez une caméra Synora depuis Périphériques.",
        page: "devices",
        done: cameras.length > 0,
      },
      {
        id: "resident",
        title: "Créer un résident",
        detail: data.residents.length ? `${data.residents.length} résident${data.residents.length > 1 ? "s" : ""} configuré${data.residents.length > 1 ? "s" : ""}.` : "Ajoutez le premier résident de confiance.",
        page: "residents",
        done: data.residents.length > 0,
      },
      {
        id: "face",
        title: "Valider une référence faciale",
        detail: residentsWithFace.length ? "Le profil facial est prêt pour la reconnaissance locale." : "Ajoutez des photos puis lancez la reconstruction du profil.",
        page: "residents",
        done: residentsWithFace.length > 0,
      },
    ];
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
