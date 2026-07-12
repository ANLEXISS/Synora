import { buildFaceUploadFormData, getBasePhotoByView, isBaseComplete } from "./face";

export function residentFaceSetupUiTest() {
  const profile = {
    status: "needs_rebuild",
    base_photos: [
      { id: "face", filename: "face.jpg", path: "base/face.jpg", view: "face", created_at: "", updated_at: "", source: "manual_upload" },
      { id: "up", filename: "up.jpg", path: "base/up.jpg", view: "up", created_at: "", updated_at: "", source: "manual_upload" },
      { id: "left", filename: "left.jpg", path: "base/left.jpg", view: "left", created_at: "", updated_at: "", source: "manual_upload" },
      { id: "right", filename: "right.jpg", path: "base/right.jpg", view: "right", created_at: "", updated_at: "", source: "manual_upload" },
    ],
    auto_count: 0,
    review_count: 0,
    pending_count: 0,
  };
  if (!getBasePhotoByView(profile, "face") || !isBaseComplete(profile)) {
    throw new Error("four base views should complete the face setup");
  }
  const form = buildFaceUploadFormData("face", new File(["face"], "face.jpg", { type: "image/jpeg" }));
  if (form.get("view") !== "face" || !(form.get("file") instanceof File)) {
    throw new Error("face upload form should contain view and file");
  }
}
