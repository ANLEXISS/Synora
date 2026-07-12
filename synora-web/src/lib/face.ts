import type { SynoraFacePhoto, SynoraFaceProfile } from "./synora-types";

export type BaseFaceView = "face" | "up" | "left" | "right";

export const BASE_FACE_VIEWS: { id: BaseFaceView; label: string; help: string }[] = [
  { id: "face", label: "Face", help: "Visage face caméra" },
  { id: "up", label: "Haut", help: "Visage légèrement levé" },
  { id: "left", label: "Gauche", help: "Visage tourné à gauche" },
  { id: "right", label: "Droite", help: "Visage tourné à droite" },
];

export function getBasePhotoByView(profile: SynoraFaceProfile | null | undefined, view: BaseFaceView): SynoraFacePhoto | undefined {
  return profile?.base_photos?.find((photo) => photo.view === view);
}

export function isBaseComplete(profile: SynoraFaceProfile | null | undefined): boolean {
  return BASE_FACE_VIEWS.every(({ id }) => Boolean(getBasePhotoByView(profile, id)));
}

export function buildFaceUploadFormData(view: BaseFaceView, file: File): FormData {
  const body = new FormData();
  body.append("view", view);
  body.append("file", file);
  return body;
}
