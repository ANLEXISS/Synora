export type OnboardingPage = "dashboard" | "home" | "devices" | "residents";

export type V1OnboardingStep = {
  id: "access" | "topology" | "camera" | "resident" | "face";
  title: string;
  detail: string;
  page: OnboardingPage;
  done: boolean;
};

export function buildV1OnboardingSteps(input: {
  available: boolean;
  rooms: number;
  cameras: number;
  residents: number;
  residentsWithFace: number;
}): V1OnboardingStep[] {
  return [
    {
      id: "access",
      title: "Centrale accessible",
      detail: "Compte local authentifié et services Synora joignables.",
      page: "dashboard",
      done: input.available,
    },
    {
      id: "topology",
      title: "Créer la topologie",
      detail: input.rooms > 0 ? `${input.rooms} pièce${input.rooms > 1 ? "s" : ""} configurée${input.rooms > 1 ? "s" : ""}.` : "Définissez au moins une pièce de la maison.",
      page: "home",
      done: input.rooms > 0,
    },
    {
      id: "camera",
      title: "Ajouter une caméra — appairer les caméras",
      detail: input.cameras >= 3
        ? "Les trois caméras V1 sont enregistrées."
        : `${input.cameras}/3 caméra${input.cameras > 1 ? "s" : ""} enregistrée${input.cameras > 1 ? "s" : ""} — Synora reste utilisable pendant la configuration.`,
      page: "devices",
      done: input.cameras >= 3,
    },
    {
      id: "resident",
      title: "Créer un résident (facultatif)",
      detail: input.residents > 0 ? `${input.residents} résident${input.residents > 1 ? "s" : ""} configuré${input.residents > 1 ? "s" : ""}.` : "Aucun résident — cette étape est facultative.",
      page: "residents",
      done: true,
    },
    {
      id: "face",
      title: "Valider une référence faciale",
      detail: input.residentsWithFace > 0 ? "Le profil facial est prêt pour la reconnaissance locale." : "Ajoutez des photos puis lancez la reconstruction du profil.",
      page: "residents",
      done: input.residents === 0 || input.residentsWithFace >= input.residents,
    },
  ];
}
