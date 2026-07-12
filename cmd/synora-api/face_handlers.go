package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const (
	defaultFaceDataRoot = "services/vision-worker/data/face"
	maxFaceUploadSize   = 5 << 20
)

type faceConfigurationProvider interface {
	Resident(string) (map[string]any, error)
	UpdateResident(string, json.RawMessage) (map[string]any, error)
}

type faceStore struct {
	root string
}

func newFaceStore(root string) *faceStore {
	if strings.TrimSpace(root) == "" {
		root = defaultFaceDataRoot
	}
	return &faceStore{root: filepath.Clean(root)}
}

func handleResidentRoute(core residentConfigurationProvider, faces *faceStore) http.HandlerFunc {
	itemHandler := handleResidentItem(core, faces)
	faceHandler := handleResidentFaceRoute(core, faces)

	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/residents/")
		parts := splitPath(rest)
		if len(parts) >= 2 && parts[1] == "face" {
			faceHandler.ServeHTTP(w, r)
			return
		}
		itemHandler.ServeHTTP(w, r)
	}
}

func handleResidentFaceRoute(core faceConfigurationProvider, faces *faceStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/residents/"))
		if len(parts) < 2 || parts[1] != "face" {
			writeRouteNotFound(w, "resident face")
			return
		}
		residentID, ok := decodePathPart(parts[0])
		if !ok || !safeStorageSegment(residentID) {
			writeRouteNotFound(w, "resident")
			return
		}

		if len(parts) == 2 {
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, http.MethodGet)
				return
			}
			profile, err := faces.profile(core, residentID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, profile)
			return
		}

		switch parts[2] {
		case "base":
			faces.handleBase(w, r, core, residentID, parts[3:])
		case "review", "pending": // pending is kept as a compatibility alias.
			faces.handleReview(w, r, core, residentID, parts[3:])
		case "rebuild":
			faces.handleRebuild(w, r, core, residentID)
		default:
			writeRouteNotFound(w, "resident face")
		}
	}
}

func (s *faceStore) handleBase(w http.ResponseWriter, r *http.Request, core faceConfigurationProvider, residentID string, parts []string) {
	if len(parts) == 0 {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		photo, err := s.upload(w, core, residentID, r)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, photo)
		return
	}

	photoID, ok := decodePathPart(parts[0])
	if !ok {
		writeRouteNotFound(w, "face photo")
		return
	}
	if len(parts) == 2 && parts[1] == "image" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.serveImage(w, r, core, residentID, photoID)
		return
	}
	if len(parts) == 2 && parts[1] == "replace" {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		photo, err := s.replace(w, core, residentID, photoID, r)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, photo)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			writeMethodNotAllowed(w, http.MethodDelete)
			return
		}
		if err := s.remove(core, residentID, photoID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeRouteNotFound(w, "face photo")
}

func (s *faceStore) handleRebuild(w http.ResponseWriter, r *http.Request, core faceConfigurationProvider, residentID string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	profile, err := s.profile(core, residentID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(profile.BasePhotos) == 0 {
		profile.Status = "empty"
	} else {
		profile.Status = "ready"
	}
	if _, err := s.updateProfile(core, residentID, profile); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *faceStore) handleReview(w http.ResponseWriter, r *http.Request, core faceConfigurationProvider, residentID string, parts []string) {
	if _, err := core.Resident(residentID); err != nil {
		writeError(w, err)
		return
	}
	photos, err := s.listPhotos(residentID, "review")
	if err != nil {
		writeError(w, err)
		return
	}
	if len(parts) == 0 {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, photos)
		return
	}
	cropID, ok := decodePathPart(parts[0])
	if !ok {
		writeRouteNotFound(w, "review crop")
		return
	}
	if len(parts) == 2 && parts[1] == "image" {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.serveReviewImage(w, r, residentID, cropID, photos)
		return
	}
	if len(parts) != 2 || parts[1] != "accept" || r.Method != http.MethodPost {
		if len(parts) == 1 && r.Method == http.MethodDelete {
			if err := s.removeReview(residentID, cropID, photos); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeMethodNotAllowed(w, http.MethodPost, http.MethodDelete, http.MethodGet)
		return
	}
	photo, err := s.acceptReview(residentID, cropID, photos)
	if err != nil {
		writeError(w, err)
		return
	}
	profile, err := s.profile(core, residentID)
	if err != nil {
		writeError(w, err)
		return
	}
	profile.Status = "needs_rebuild"
	if _, err := s.updateProfile(core, residentID, profile); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, photo)
}

func (s *faceStore) profile(core faceConfigurationProvider, residentID string) (contract.FaceProfile, error) {
	if err := s.ensureResidentDirs(residentID); err != nil {
		return contract.FaceProfile{}, err
	}
	item, err := core.Resident(residentID)
	if err != nil {
		return contract.FaceProfile{}, err
	}
	var profile contract.FaceProfile
	if raw, ok := item["face_profile"]; ok && raw != nil {
		body, marshalErr := json.Marshal(raw)
		if marshalErr != nil {
			return profile, contract.NewAPIError(contract.ErrorInternal, "decode face profile")
		}
		if err := json.Unmarshal(body, &profile); err != nil {
			return profile, contract.NewAPIError(contract.ErrorInternal, "decode face profile")
		}
	}
	if err := s.reconcileProfile(&profile, residentID); err != nil {
		return contract.FaceProfile{}, err
	}
	normalizeFaceProfileForAPI(&profile)
	return profile, nil
}

func (s *faceStore) updateProfile(core faceConfigurationProvider, residentID string, profile contract.FaceProfile) (map[string]any, error) {
	normalizeFaceProfileForAPI(&profile)
	body, err := json.Marshal(map[string]any{"face_profile": profile})
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "encode face profile")
	}
	return core.UpdateResident(residentID, body)
}

func (s *faceStore) ensureResidentDirs(residentID string) error {
	if !safeStorageSegment(residentID) {
		return contract.NewAPIError(contract.ErrorValidationFailed, "invalid resident id")
	}
	for _, name := range []string{"base", "auto", "review"} {
		if err := os.MkdirAll(filepath.Join(s.root, residentID, name), 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (s *faceStore) reconcileProfile(profile *contract.FaceProfile, residentID string) error {
	if profile == nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(s.root, residentID, "base"))
	if err != nil {
		return err
	}
	metadata := make(map[string]contract.FacePhoto, len(profile.BasePhotos))
	for _, photo := range profile.BasePhotos {
		metadata[photo.Filename] = photo
	}
	photos := make([]contract.FacePhoto, 0, len(entries))
	usedViews := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !isFaceImageName(entry.Name()) {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		photo, ok := metadata[entry.Name()]
		if !ok {
			photo = contract.FacePhoto{ID: stableFacePhotoID("base", entry.Name()), Filename: entry.Name(), Source: "manual_upload", CreatedAt: info.ModTime().UTC()}
		}
		if photo.ID == "" {
			photo.ID = stableFacePhotoID("base", entry.Name())
		}
		if !validFaceView(photo.View) || usedViews[photo.View] {
			for _, candidate := range []string{"face", "up", "left", "right"} {
				if !usedViews[candidate] {
					photo.View = candidate
					break
				}
			}
		}
		usedViews[photo.View] = true
		if photo.CreatedAt.IsZero() {
			photo.CreatedAt = info.ModTime().UTC()
		}
		photo.UpdatedAt = info.ModTime().UTC()
		photo.Path = filepath.Join(s.root, residentID, "base", entry.Name())
		photos = append(photos, photo)
	}
	profile.BasePhotos = photos
	profile.AutoCount, err = s.countImages(residentID, "auto")
	if err != nil {
		return err
	}
	profile.ReviewCount, err = s.countImages(residentID, "review")
	if err != nil {
		return err
	}
	profile.PendingCount = profile.ReviewCount
	return nil
}

func (s *faceStore) countImages(residentID, directory string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, residentID, directory))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && isFaceImageName(entry.Name()) {
			count++
		}
	}
	return count, nil
}

func (s *faceStore) listPhotos(residentID, directory string) ([]contract.FacePhoto, error) {
	if err := s.ensureResidentDirs(residentID); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, residentID, directory))
	if err != nil {
		return nil, err
	}
	photos := make([]contract.FacePhoto, 0)
	for _, entry := range entries {
		if entry.IsDir() || !isFaceImageName(entry.Name()) {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return nil, statErr
		}
		photos = append(photos, contract.FacePhoto{
			ID: stableFacePhotoID(directory, entry.Name()), Filename: entry.Name(),
			Path:      filepath.Join(s.root, residentID, directory, entry.Name()),
			CreatedAt: info.ModTime().UTC(), UpdatedAt: info.ModTime().UTC(), Source: directory,
		})
	}
	return photos, nil
}

func (s *faceStore) acceptReview(residentID, cropID string, photos []contract.FacePhoto) (contract.FacePhoto, error) {
	for _, photo := range photos {
		if photo.ID != cropID {
			continue
		}
		from, ok := s.safeFacePath(residentID, "review", photo.Path)
		if !ok {
			return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorValidationFailed, "invalid review crop path")
		}
		if _, err := os.Stat(from); err != nil {
			return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorNotFound, "review crop not found")
		}
		if err := s.ensureResidentDirs(residentID); err != nil {
			return contract.FacePhoto{}, err
		}
		filename := "auto-" + cropID + filepath.Ext(photo.Filename)
		to := filepath.Join(s.root, residentID, "auto", filename)
		if err := os.Rename(from, to); err != nil {
			return contract.FacePhoto{}, err
		}
		photo.Filename, photo.Path, photo.Source = filename, to, "review_accept"
		return photo, nil
	}
	return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorNotFound, "review crop not found")
}

func (s *faceStore) removeReview(residentID, cropID string, photos []contract.FacePhoto) error {
	for _, photo := range photos {
		if photo.ID != cropID {
			continue
		}
		path, ok := s.safeFacePath(residentID, "review", photo.Path)
		if !ok {
			return contract.NewAPIError(contract.ErrorValidationFailed, "invalid review crop path")
		}
		if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
			return contract.NewAPIError(contract.ErrorNotFound, "review crop not found")
		} else {
			return err
		}
	}
	return contract.NewAPIError(contract.ErrorNotFound, "review crop not found")
}

func (s *faceStore) serveReviewImage(w http.ResponseWriter, r *http.Request, residentID, cropID string, photos []contract.FacePhoto) {
	for _, photo := range photos {
		if photo.ID != cropID {
			continue
		}
		path, ok := s.safeFacePath(residentID, "review", photo.Path)
		if !ok {
			break
		}
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			writeError(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, filepath.Base(path), photo.UpdatedAt, file)
		return
	}
	writeError(w, contract.NewAPIError(contract.ErrorNotFound, "review crop not found"))
}

func (s *faceStore) safeFacePath(residentID, directory, path string) (string, bool) {
	if !safeStorageSegment(residentID) || !safeStorageSegment(directory) {
		return "", false
	}
	base := filepath.Join(s.root, residentID, directory)
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(base, clean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return clean, true
}

func stableFacePhotoID(directory, filename string) string {
	sum := sha256.Sum256([]byte(directory + ":" + filename))
	return directory + "-" + hex.EncodeToString(sum[:])[:16]
}

func isFaceImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

func (s *faceStore) upload(w http.ResponseWriter, core faceConfigurationProvider, residentID string, r *http.Request) (contract.FacePhoto, error) {
	profile, err := s.profile(core, residentID)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	baseCount, countErr := s.countImages(residentID, "base")
	if countErr != nil {
		return contract.FacePhoto{}, countErr
	}
	if baseCount >= 4 || len(profile.BasePhotos) >= 4 {
		return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorValidationFailed, "maximum of 4 base photos reached")
	}
	data, extension, view, err := readFaceUpload(w, r)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	if view == "" {
		view = nextFaceView(profile)
	}
	if view != "" && facePhotoViewIndex(profile, view) >= 0 {
		return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorValidationFailed, "a base photo already uses this view")
	}
	photoID := idgen.New("face")
	now := time.Now().UTC()
	filename := photoID + "." + extension
	path, err := s.writeFile(residentID, filename, data)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	photo := contract.FacePhoto{ID: photoID, Filename: filename, Path: path, View: view, CreatedAt: now, UpdatedAt: now, Source: "manual_upload"}
	profile.BasePhotos = append(profile.BasePhotos, photo)
	profile.Status = "needs_rebuild"
	if _, err := s.updateProfile(core, residentID, profile); err != nil {
		_ = os.Remove(path)
		return contract.FacePhoto{}, err
	}
	return photo, nil
}

func (s *faceStore) replace(w http.ResponseWriter, core faceConfigurationProvider, residentID, photoID string, r *http.Request) (contract.FacePhoto, error) {
	profile, err := s.profile(core, residentID)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	index := facePhotoIndex(profile, photoID)
	if index < 0 {
		return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	data, extension, view, err := readFaceUpload(w, r)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	now := time.Now().UTC()
	filename := photoID + "-" + idgen.New("version") + "." + extension
	path, err := s.writeFile(residentID, filename, data)
	if err != nil {
		return contract.FacePhoto{}, err
	}
	old := profile.BasePhotos[index]
	if view == "" {
		view = old.View
	}
	if view != old.View && view != "" {
		if other := facePhotoViewIndex(profile, view); other >= 0 && other != index {
			return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorValidationFailed, "a base photo already uses this view")
		}
	}
	if _, ok := s.safeBasePath(residentID, old.Path); !ok {
		_ = os.Remove(path)
		return contract.FacePhoto{}, contract.NewAPIError(contract.ErrorValidationFailed, "invalid existing face photo path")
	}
	photo := old
	photo.Filename, photo.Path, photo.View, photo.UpdatedAt = filename, path, view, now
	profile.BasePhotos[index] = photo
	profile.Status = "needs_rebuild"
	if _, err := s.updateProfile(core, residentID, profile); err != nil {
		_ = os.Remove(path)
		return contract.FacePhoto{}, err
	}
	_ = s.archive(old.Path, residentID, old.Filename)
	return photo, nil
}

func (s *faceStore) remove(core faceConfigurationProvider, residentID, photoID string) error {
	profile, err := s.profile(core, residentID)
	if err != nil {
		return err
	}
	index := facePhotoIndex(profile, photoID)
	if index < 0 {
		return contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	photo := profile.BasePhotos[index]
	if _, ok := s.safeBasePath(residentID, photo.Path); !ok {
		return contract.NewAPIError(contract.ErrorValidationFailed, "invalid existing face photo path")
	}
	profile.BasePhotos = append(profile.BasePhotos[:index], profile.BasePhotos[index+1:]...)
	if len(profile.BasePhotos) == 0 {
		profile.Status = "empty"
	} else {
		profile.Status = "needs_rebuild"
	}
	if _, err := s.updateProfile(core, residentID, profile); err != nil {
		return err
	}
	return s.archive(photo.Path, residentID, photo.Filename)
}

func (s *faceStore) serveImage(w http.ResponseWriter, r *http.Request, core faceConfigurationProvider, residentID, photoID string) {
	profile, err := s.profile(core, residentID)
	if err != nil {
		writeError(w, err)
		return
	}
	index := facePhotoIndex(profile, photoID)
	if index < 0 {
		writeError(w, contract.NewAPIError(contract.ErrorNotFound, "face photo not found"))
		return
	}
	path, ok := s.safeBasePath(residentID, profile.BasePhotos[index].Path)
	if !ok {
		writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "invalid face photo path"))
		return
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, contract.NewAPIError(contract.ErrorNotFound, "face photo file not found"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, filepath.Base(path), time.Time{}, file)
}

func (s *faceStore) writeFile(residentID, filename string, data []byte) (string, error) {
	if !safeStorageSegment(residentID) || !safeStorageSegment(filename) {
		return "", contract.NewAPIError(contract.ErrorValidationFailed, "invalid identity path")
	}
	dir := filepath.Join(s.root, residentID, "base")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filename)
	tmp, err := os.CreateTemp(dir, ".face-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *faceStore) archive(path, residentID, filename string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	dir := filepath.Join(s.root, residentID, "archive")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	archiveName := idgen.New("archived") + "-" + filepath.Base(filename)
	return os.Rename(path, filepath.Join(dir, archiveName))
}

func (s *faceStore) safeBasePath(residentID, path string) (string, bool) {
	if !safeStorageSegment(residentID) {
		return "", false
	}
	base := filepath.Join(s.root, residentID, "base")
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(base, clean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return clean, true
}

func safeStorageSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`) && filepath.Base(value) == value
}

func readFaceUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFaceUploadSize+1024)
	if err := r.ParseMultipartForm(maxFaceUploadSize + 1024); err != nil {
		return nil, "", "", contract.NewAPIError(contract.ErrorValidationFailed, "invalid multipart upload")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "", "", contract.NewAPIError(contract.ErrorValidationFailed, "multipart field file is required")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFaceUploadSize+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) > maxFaceUploadSize {
		return nil, "", "", contract.NewAPIError(contract.ErrorValidationFailed, "face photo exceeds 5 MB")
	}
	contentType := http.DetectContentType(data)
	extension := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[contentType]
	if extension == "" {
		return nil, "", "", contract.NewAPIError(contract.ErrorValidationFailed, "supported formats are jpeg, png and webp")
	}
	view := strings.ToLower(strings.TrimSpace(r.FormValue("view")))
	if view != "" && !validFaceView(view) {
		return nil, "", "", contract.NewAPIError(contract.ErrorValidationFailed, "view must be face, up, left or right")
	}
	return data, extension, view, nil
}

func facePhotoIndex(profile contract.FaceProfile, id string) int {
	for index, photo := range profile.BasePhotos {
		if photo.ID == id {
			return index
		}
	}
	return -1
}

func facePhotoViewIndex(profile contract.FaceProfile, view string) int {
	for index, photo := range profile.BasePhotos {
		if photo.View == view {
			return index
		}
	}
	return -1
}

func nextFaceView(profile contract.FaceProfile) string {
	for _, view := range []string{"face", "up", "left", "right"} {
		if facePhotoViewIndex(profile, view) < 0 {
			return view
		}
	}
	return ""
}

func validFaceView(view string) bool {
	return view == "face" || view == "up" || view == "left" || view == "right"
}

func normalizeFaceProfileForAPI(profile *contract.FaceProfile) {
	if profile == nil {
		return
	}
	if profile.Status != "ready" && profile.Status != "needs_rebuild" && profile.Status != "error" {
		profile.Status = "empty"
	}
	if len(profile.BasePhotos) == 0 && profile.Status == "ready" {
		profile.Status = "empty"
	}
	if len(profile.BasePhotos) > 0 && profile.Status == "empty" {
		profile.Status = "needs_rebuild"
	}
	if profile.AutoCount < 0 {
		profile.AutoCount = 0
	}
	if profile.ReviewCount < 0 {
		profile.ReviewCount = 0
	}
	profile.PendingCount = profile.ReviewCount
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func decodePathPart(value string) (string, bool) {
	decoded, err := url.PathUnescape(value)
	if err != nil || decoded == "" || strings.Contains(decoded, "/") || decoded == "." || decoded == ".." {
		return "", false
	}
	return decoded, true
}
