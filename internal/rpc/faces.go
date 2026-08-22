package rpc

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

func (s *Server) facePhotoList(msg contract.Message) (any, error) {
	residentID := ""
	if len(msg.Payload) > 0 {
		var req struct {
			ResidentID string `json:"resident_id"`
		}
		if err := decodePayload(msg.Payload, &req); err != nil {
			return nil, err
		}
		residentID = strings.TrimSpace(req.ResidentID)
	}
	if residentID != "" {
		if _, ok := s.residentByID(residentID); !ok {
			return nil, contract.NewAPIError(contract.ErrorNotFound, "resident not found")
		}
	}
	items := s.state.FacePhotosList(residentID, 100)
	return items, nil
}

func (s *Server) facePhotoGet(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	value, ok := s.state.FacePhoto(req.ID)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	return *value, nil
}

func (s *Server) facePhotoRegister(msg contract.Message) (any, error) {
	var value contract.FacePhoto
	if err := decodePayload(msg.Payload, &value); err != nil {
		return nil, err
	}
	if _, ok := s.residentByID(value.ResidentID); !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "resident not found")
	}
	if value.Status == "" {
		value.Status = string(contract.FacePhotoStored)
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	if _, created, err := s.state.RegisterFacePhoto(&value); err != nil {
		return nil, err
	} else if created {
		s.notifyMutation("resident.face_photo.updated", value.ID)
	}
	result, _ := s.state.FacePhoto(value.ID)
	return result, nil
}

func (s *Server) facePhotoDelete(msg contract.Message) (any, error) {
	var req struct {
		ID         string `json:"id"`
		ResidentID string `json:"resident_id"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	value, ok := s.state.FacePhoto(req.ID)
	if !ok || (strings.TrimSpace(req.ResidentID) != "" && value.ResidentID != strings.TrimSpace(req.ResidentID)) {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	updated, _, err := s.state.TransitionFacePhoto(value.ID, contract.FacePhotoRemovalPending, "")
	if err != nil {
		return nil, err
	}
	s.notifyMutation("resident.face_photo.removal_pending", updated.ID)
	return updated, nil
}

func (s *Server) facePhotoActivate(msg contract.Message) (any, error) {
	var req struct {
		ID             string `json:"id"`
		DatasetVersion string `json:"dataset_version"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	value, ok := s.state.FacePhoto(req.ID)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	if strings.TrimSpace(req.DatasetVersion) != "" {
		value.DatasetVersion = strings.TrimSpace(req.DatasetVersion)
	}
	if value.Status != string(contract.FacePhotoActive) {
		if value.Status == string(contract.FacePhotoStored) || value.Status == string(contract.FacePhotoMissing) {
			if _, _, err := s.state.TransitionFacePhoto(value.ID, contract.FacePhotoValidating, ""); err != nil {
				return nil, err
			}
		}
		if _, _, err := s.state.TransitionFacePhoto(value.ID, contract.FacePhotoActive, ""); err != nil {
			return nil, err
		}
	}
	result, _ := s.state.FacePhoto(value.ID)
	return result, nil
}

func (s *Server) faceDatasetSnapshot(_ contract.Message) (any, error) {
	items := s.state.FacePhotosList("", 200)
	type entry struct {
		Photo      contract.FacePhoto `json:"photo"`
		StorageKey string             `json:"storage_key"`
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		if item.Status == string(contract.FacePhotoRemoved) || item.Status == string(contract.FacePhotoRejected) {
			continue
		}
		entries = append(entries, entry{Photo: item, StorageKey: item.StorageKey})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Photo.ResidentID == entries[j].Photo.ResidentID {
			return entries[i].Photo.ID < entries[j].Photo.ID
		}
		return entries[i].Photo.ResidentID < entries[j].Photo.ResidentID
	})
	return map[string]any{"desired_revision": s.state.FaceDatasetState().DesiredRevision, "photos": entries}, nil
}

func (s *Server) faceDatasetStatus(_ contract.Message) (any, error) {
	return s.state.FaceDatasetState(), nil
}

func (s *Server) faceDatasetFailure(msg contract.Message) (any, error) {
	var req struct {
		FailureCode string `json:"failure_code"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	current := s.state.FaceDatasetState()
	if current == nil {
		current = &contract.FaceDatasetState{SchemaVersion: 1}
	}
	current.Status = contract.FaceDatasetFailed
	current.FailureCode = strings.TrimSpace(req.FailureCode)
	if current.FailureCode == "" {
		current.FailureCode = "dataset_sync_failed"
	}
	if err := s.state.SetFaceDataset(current); err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Server) faceDatasetMarkMissing(msg contract.Message) (any, error) {
	var req struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	updated := []contract.FacePhoto{}
	for _, id := range req.PhotoIDs {
		value, ok := s.state.FacePhoto(id)
		if !ok {
			continue
		}
		if value.Status == string(contract.FacePhotoMissing) {
			updated = append(updated, *value)
			continue
		}
		if _, _, err := s.state.TransitionFacePhoto(id, contract.FacePhotoMissing, "source_missing"); err != nil {
			return nil, err
		}
		if value, ok := s.state.FacePhoto(id); ok {
			updated = append(updated, *value)
			s.notifyMutation("resident.face_photo.updated", value.ID)
		}
	}
	return updated, nil
}

func (s *Server) faceDatasetActivate(msg contract.Message) (any, error) {
	var req struct {
		Version            string   `json:"version"`
		DesiredRevision    uint64   `json:"desired_revision"`
		ManifestChecksum   string   `json:"manifest_checksum"`
		ModelFingerprint   string   `json:"model_fingerprint"`
		EmbeddingDimension int      `json:"embedding_dimension"`
		ResidentIDs        []string `json:"resident_ids"`
		PhotoIDs           []string `json:"photo_ids"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	current := s.state.FaceDatasetState()
	if current != nil && req.DesiredRevision != current.DesiredRevision {
		return nil, contract.NewAPIError(contract.ErrorConflict, "face dataset build is obsolete")
	}
	newPhotos := make(map[string]bool, len(req.PhotoIDs))
	for _, id := range req.PhotoIDs {
		newPhotos[id] = true
	}
	removed := []string{}
	if current != nil {
		for _, id := range current.PhotoIDs {
			if newPhotos[id] {
				continue
			}
			value, ok := s.state.FacePhoto(id)
			if !ok {
				continue
			}
			if value.Status == string(contract.FacePhotoActive) {
				if _, _, err := s.state.TransitionFacePhoto(id, contract.FacePhotoRemovalPending, ""); err != nil {
					return nil, err
				}
			}
			removed = append(removed, id)
		}
	}
	for _, value := range s.state.FacePhotosList("", 200) {
		if value.Status == string(contract.FacePhotoRemovalPending) && !newPhotos[value.ID] {
			seen := false
			for _, id := range removed {
				if id == value.ID {
					seen = true
					break
				}
			}
			if !seen {
				removed = append(removed, value.ID)
			}
		}
	}
	for _, id := range req.PhotoIDs {
		value, ok := s.state.FacePhoto(id)
		if !ok {
			return nil, contract.NewAPIError(contract.ErrorConflict, "face dataset references unknown photo")
		}
		if value.Status == string(contract.FacePhotoStored) || value.Status == string(contract.FacePhotoMissing) {
			if _, _, err := s.state.TransitionFacePhoto(id, contract.FacePhotoValidating, ""); err != nil {
				return nil, err
			}
		}
		if _, _, err := s.state.TransitionFacePhoto(id, contract.FacePhotoActive, ""); err != nil {
			return nil, err
		}
	}
	updated := &contract.FaceDatasetState{SchemaVersion: 1, DesiredRevision: req.DesiredRevision, ActiveVersion: strings.TrimSpace(req.Version), ActiveRevision: req.DesiredRevision, BuiltAt: time.Now().UTC(), ActivatedAt: time.Now().UTC(), ResidentIDs: append([]string(nil), req.ResidentIDs...), PhotoIDs: append([]string(nil), req.PhotoIDs...), ManifestChecksum: strings.TrimSpace(req.ManifestChecksum), ModelFingerprint: strings.TrimSpace(req.ModelFingerprint), EmbeddingDimension: req.EmbeddingDimension, Status: contract.FaceDatasetActive}
	if err := s.state.SetFaceDataset(updated); err != nil {
		return nil, err
	}
	s.notifyMutation("resident.face_dataset.activated", updated.ActiveVersion)
	return map[string]any{"dataset": updated, "removed_photo_ids": removed}, nil
}

func (s *Server) facePhotoRemoveConfirmed(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	updated, _, err := s.state.TransitionFacePhoto(req.ID, contract.FacePhotoRemoved, "")
	if err != nil {
		return nil, err
	}
	s.notifyMutation("resident.face_photo.removed", updated.ID)
	return updated, nil
}

func (s *Server) facePhotoReject(msg contract.Message) (any, error) {
	var req struct {
		ID          string `json:"id"`
		FailureCode string `json:"failure_code"`
	}
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	updated, _, err := s.state.TransitionFacePhoto(req.ID, contract.FacePhotoRejected, strings.TrimSpace(req.FailureCode))
	if err != nil {
		return nil, err
	}
	s.notifyMutation("resident.face_photo.rejected", updated.ID)
	return updated, nil
}

func (s *Server) residentByID(id string) (any, bool) {
	if s == nil || s.snapshot == nil {
		return nil, false
	}
	s.snapshot.Mu.RLock()
	defer s.snapshot.Mu.RUnlock()
	value, ok := s.snapshot.Residents[strings.TrimSpace(id)]
	return value, ok && value != nil
}

// facePhotoPublicJSON is intentionally kept here as a regression guard for
// future contract additions: no source path, storage key or embedding may
// cross the public RPC/API projection.
func facePhotoPublicJSON(value contract.FacePhoto) map[string]any {
	data, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	delete(result, "path")
	delete(result, "storage_key")
	delete(result, "embedding")
	return result
}
