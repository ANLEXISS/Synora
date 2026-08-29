package main

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"synora/pkg/contract"
)

type residentPhotoProvider interface {
	ResidentPhotos(string, int) ([]contract.FacePhoto, error)
	ResidentPhoto(string) (*contract.FacePhoto, error)
	RegisterResidentPhoto(contract.FacePhoto) (*contract.FacePhoto, error)
	DeleteResidentPhoto(string, string) (*contract.FacePhoto, error)
}

func handleResidentPhotoRoute(core residentConfigurationProvider, faces *faceStore) http.HandlerFunc {
	photos, _ := core.(residentPhotoProvider)
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/residents/"))
		if len(parts) < 2 || parts[1] != "photos" {
			writeRouteNotFound(w, "resident photos")
			return
		}
		residentID, ok := decodePathPart(parts[0])
		if !ok || !safeStorageSegment(residentID) {
			writeRouteNotFound(w, "resident")
			return
		}
		if photos == nil {
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "resident photo RPC unavailable"))
			return
		}
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			items, err := photos.ResidentPhotos(residentID, 100)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case len(parts) == 2 && r.Method == http.MethodPost:
			photo, err := receiveResidentPhoto(w, r, faces, core, residentID, photos)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, photo)
		case len(parts) == 3:
			photoID, valid := decodePathPart(parts[2])
			if !valid || !safeStorageSegment(photoID) {
				writeRouteNotFound(w, "face photo")
				return
			}
			if r.Method == http.MethodGet {
				photo, err := photos.ResidentPhoto(photoID)
				if err != nil || photo.ResidentID != residentID {
					if err == nil {
						err = contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
					}
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, photo)
				return
			}
			if r.Method == http.MethodDelete {
				photo, err := photos.DeleteResidentPhoto(residentID, photoID)
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusAccepted, photo)
				return
			}
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost, http.MethodDelete)
		}
	}
}

func receiveResidentPhoto(w http.ResponseWriter, r *http.Request, faces *faceStore, core faceConfigurationProvider, residentID string, photos residentPhotoProvider) (*contract.FacePhoto, error) {
	if _, err := core.Resident(residentID); err != nil {
		return nil, err
	}
	if faces == nil || faces.physical == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "face storage unavailable")
	}
	if r.ContentLength > faces.physical.Limits.MaxUploadSize && r.ContentLength >= 0 {
		return nil, contract.NewAPIError(contract.ErrorPayloadTooLarge, "face photo exceeds upload limit")
	}
	r.Body = http.MaxBytesReader(w, r.Body, faces.physical.Limits.MaxUploadSize+64*1024)
	multipartReader, err := r.MultipartReader()
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "multipart upload required")
	}
	var received *contract.FacePhoto
	view := ""
	for {
		part, nextErr := multipartReader.NextPart()
		if errors.Is(nextErr, io.EOF) || nextErr == nil && part == nil {
			break
		}
		if nextErr != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "invalid multipart upload")
		}
		if part.FormName() == "view" {
			data, readErr := io.ReadAll(io.LimitReader(part, 32))
			_ = part.Close()
			if readErr != nil {
				return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "invalid face view")
			}
			view = strings.ToLower(strings.TrimSpace(string(data)))
			if view != "" && !validFaceView(view) {
				return nil, contract.NewAPIError(contract.ErrorValidationFailed, "view must be face, up, left or right")
			}
			continue
		}
		if part.FormName() != "file" && part.FormName() != "photo" {
			_ = part.Close()
			continue
		}
		if received != nil {
			_ = part.Close()
			return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "one photo per request")
		}
		result, receiveErr := faces.physical.Receive(residentID, part)
		_ = part.Close()
		if receiveErr != nil {
			return nil, receiveErr
		}
		received = &result.Photo
		received.View = view
	}
	if received == nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "multipart field file is required")
	}
	registered, err := photos.RegisterResidentPhoto(*received)
	if err != nil {
		_ = faces.physical.RemoveSource(*received)
		return nil, err
	}
	if registered.ID != received.ID {
		_ = faces.physical.RemoveSource(*received)
	}
	return registered, nil
}
