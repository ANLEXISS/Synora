import { buildFaceUploadFormData, getBasePhotoByView, isBaseComplete, validateFaceFile } from "./face";

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

export function residentFaceUploadValidationTest() {
  const valid = { size: 12, type: "image/jpeg" } as File;
  if (validateFaceFile(valid) !== null) throw new Error("JPEG should be accepted");
  if (!validateFaceFile({ size: 0, type: "image/jpeg" } as File)) throw new Error("empty image should be rejected");
  if (!validateFaceFile({ size: 12, type: "image/gif" } as File)) throw new Error("unsupported image type should be rejected");
  if (!validateFaceFile({ size: 5 * 1024 * 1024 + 1, type: "image/png" } as File)) throw new Error("oversized image should be rejected");
}
