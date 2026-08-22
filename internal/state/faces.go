package state

import (
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	DefaultFacePhotoListLimit = 50
	MaxFacePhotoListLimit     = 200
)

func cloneFacePhoto(value *contract.FacePhoto) *contract.FacePhoto {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFaceDataset(value *contract.FaceDatasetState) *contract.FaceDatasetState {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.ResidentIDs = append([]string(nil), value.ResidentIDs...)
	cloned.PhotoIDs = append([]string(nil), value.PhotoIDs...)
	return &cloned
}

func sameFacePhotoContent(left, right *contract.FacePhoto) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.ResidentID == right.ResidentID &&
		left.SizeBytes == right.SizeBytes &&
		strings.EqualFold(strings.TrimSpace(left.Checksum), strings.TrimSpace(right.Checksum)) &&
		strings.EqualFold(strings.TrimSpace(left.MediaType), strings.TrimSpace(right.MediaType))
}

// RegisterFacePhoto is idempotent for a retry of the same source. The stored
// path is deliberately a relative StorageKey; an absolute local path never
// crosses the Core/RPC boundary.
func (s *Store) RegisterFacePhoto(value *contract.FacePhoto) (contract.FacePhoto, bool, error) {
	if s == nil || value == nil {
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorInternal, "state store unavailable")
	}
	cloned := cloneFacePhoto(value)
	cloned.ID = strings.TrimSpace(cloned.ID)
	cloned.ResidentID = strings.TrimSpace(cloned.ResidentID)
	cloned.Checksum = strings.TrimSpace(cloned.Checksum)
	if cloned.ID == "" || cloned.ResidentID == "" || cloned.SizeBytes < 0 || cloned.Checksum == "" {
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "face photo id, resident_id, size_bytes and checksum are required")
	}
	if cloned.Status == "" {
		cloned.Status = string(contract.FacePhotoStored)
	}
	if err := contract.FacePhotoStatus(strings.TrimSpace(cloned.Status)).Validate(); err != nil {
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = time.Now().UTC()
	}
	cloned.CreatedAt = cloned.CreatedAt.UTC()
	cloned.UpdatedAt = cloned.UpdatedAt.UTC()
	if cloned.UpdatedAt.IsZero() {
		cloned.UpdatedAt = cloned.CreatedAt
	}
	s.mu.Lock()
	if existing := s.FacePhotos[cloned.ID]; existing != nil {
		if !sameFacePhotoContent(existing, cloned) {
			s.mu.Unlock()
			return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorConflict, "face photo id collision")
		}
		result := *cloneFacePhoto(existing)
		s.mu.Unlock()
		return result, false, nil
	}
	for _, existing := range s.FacePhotos {
		if existing != nil && existing.ResidentID != cloned.ResidentID &&
			existing.SizeBytes == cloned.SizeBytes && existing.Checksum == cloned.Checksum &&
			existing.MediaType == cloned.MediaType && existing.Status != string(contract.FacePhotoRemoved) {
			s.mu.Unlock()
			return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorConflict, "face sample is already registered to another resident")
		}
		if existing != nil && existing.ResidentID == cloned.ResidentID &&
			existing.SizeBytes == cloned.SizeBytes && existing.Checksum == cloned.Checksum &&
			existing.MediaType == cloned.MediaType && existing.Status != string(contract.FacePhotoRemoved) {
			result := *cloneFacePhoto(existing)
			s.mu.Unlock()
			return result, false, nil
		}
	}
	if cloned.Revision == 0 {
		cloned.Revision = 1
	}
	s.FacePhotos[cloned.ID] = cloned
	if s.FaceDataset == nil {
		s.FaceDataset = &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetIdle}
	}
	s.FaceDataset.DesiredRevision++
	s.revision.Add(1)
	result := *cloneFacePhoto(cloned)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (s *Store) FacePhoto(id string) (*contract.FacePhoto, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.FacePhotos[strings.TrimSpace(id)]
	if !ok || value == nil {
		return nil, false
	}
	return cloneFacePhoto(value), true
}

func (s *Store) FacePhotosList(residentID string, limit int) []contract.FacePhoto {
	if limit <= 0 {
		limit = DefaultFacePhotoListLimit
	}
	if limit > MaxFacePhotoListLimit {
		limit = MaxFacePhotoListLimit
	}
	residentID = strings.TrimSpace(residentID)
	s.mu.RLock()
	items := make([]contract.FacePhoto, 0, len(s.FacePhotos))
	for _, value := range s.FacePhotos {
		if value != nil && (residentID == "" || value.ResidentID == residentID) {
			items = append(items, *cloneFacePhoto(value))
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *Store) TransitionFacePhoto(id string, target contract.FacePhotoStatus, failureCode string) (contract.FacePhoto, bool, error) {
	if err := target.Validate(); err != nil {
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}
	s.mu.Lock()
	value := s.FacePhotos[strings.TrimSpace(id)]
	if value == nil {
		s.mu.Unlock()
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
	}
	if value.Status == string(target) {
		result := *cloneFacePhoto(value)
		s.mu.Unlock()
		return result, false, nil
	}
	from := contract.FacePhotoStatus(value.Status)
	if !contract.ValidFacePhotoTransition(from, target) {
		s.mu.Unlock()
		return contract.FacePhoto{}, false, contract.NewAPIError(contract.ErrorConflict, "face photo transition from %s to %s is not allowed", from, target)
	}
	now := time.Now().UTC()
	value.Status = string(target)
	value.FailureCode = strings.TrimSpace(failureCode)
	value.UpdatedAt = now
	value.Revision++
	if target == contract.FacePhotoActive {
		value.ValidatedAt = &now
		value.ActivatedAt = &now
	}
	if target == contract.FacePhotoRemoved {
		value.RemovedAt = &now
	}
	if s.FaceDataset == nil {
		s.FaceDataset = &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetIdle}
	}
	if target == contract.FacePhotoRemovalPending || target == contract.FacePhotoStored || target == contract.FacePhotoRejected || target == contract.FacePhotoMissing {
		s.FaceDataset.DesiredRevision++
	}
	s.revision.Add(1)
	result := *cloneFacePhoto(value)
	s.mu.Unlock()
	if err := s.SaveNow(); err != nil {
		return result, true, err
	}
	return result, true, nil
}

func (s *Store) SetFaceDataset(value *contract.FaceDatasetState) error {
	if s == nil || value == nil {
		return contract.NewAPIError(contract.ErrorInvalidRequest, "face dataset is required")
	}
	cloned := cloneFaceDataset(value)
	if err := cloned.Validate(); err != nil {
		return contract.NewAPIError(contract.ErrorInvalidRequest, "%v", err)
	}
	s.mu.Lock()
	s.FaceDataset = cloned
	s.revision.Add(1)
	s.mu.Unlock()
	return s.SaveNow()
}

func (s *Store) FaceDatasetState() *contract.FaceDatasetState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFaceDataset(s.FaceDataset)
}
