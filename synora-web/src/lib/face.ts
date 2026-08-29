import type { SynoraFacePhoto, SynoraFaceProfile } from "./synora-types";

export type BaseFaceView = "face" | "up" | "left" | "right";

export const MAX_FACE_UPLOAD_BYTES = 5 * 1024 * 1024;

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

export function validateFaceFile(file: File): string | null {
  if (file.size <= 0) return "La photo est vide.";
  if (file.size > MAX_FACE_UPLOAD_BYTES) return "La photo dépasse la limite de 5 Mo.";
  if (file.type !== "image/jpeg" && file.type !== "image/png") {
    return "Seules les images JPEG ou PNG sont acceptées.";
  }
  return null;
}
