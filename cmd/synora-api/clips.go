package main

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"synora/internal/clipstore"
	"synora/pkg/contract"
)

type clipProvider interface {
	Clips(int) ([]contract.Clip, error)
	Clip(string) (*contract.Clip, error)
}

func handleClipCollection(core clipProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		limit := contract.DefaultClipListLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > contract.MaxClipListLimit {
				writeError(w, contract.NewAPIError(contract.ErrorInvalidRequest, "clip limit must be between 1 and 100"))
				return
			}
			limit = parsed
		}
		items, err := core.Clips(limit)
		if err != nil {
			writeError(w, err)
			return
		}
		for index := range items {
			items[index].Path = ""
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleClipRoute(core clipProvider) http.HandlerFunc {
	return handleClipRouteWithRoot(core, clipMediaRoot())
}

func handleClipRouteWithRoot(core clipProvider, root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/clips/")
		if strings.HasSuffix(raw, "/media") {
			handleClipMedia(core, root).ServeHTTP(w, r)
			return
		}
		id, err := url.PathUnescape(strings.TrimSpace(raw))
		if err != nil || id == "" || strings.Contains(id, "/") {
			writeRouteNotFound(w, "clip")
			return
		}
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		item, err := core.Clip(id)
		if err != nil {
			writeError(w, err)
			return
		}
		item.Path = ""
		writeJSON(w, http.StatusOK, item)
	}
}

func handleClipMedia(core clipProvider, root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := mediaClipID(r.URL.Path)
		if err != nil {
			writeRouteNotFound(w, "clip")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		clip, err := core.Clip(id)
		if err != nil {
			writeError(w, err)
			return
		}
		if clip == nil {
			writeRouteNotFound(w, "clip")
			return
		}
		if clip.Status == contract.ClipStatusMissing || clip.Status == contract.ClipStatusExpired {
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "clip media is unavailable"))
			return
		}
		if clip.Status != contract.ClipStatusReady && clip.Status != contract.ClipStatusProcessed {
			writeError(w, contract.NewAPIError(contract.ErrorConflict, "clip media is not ready"))
			return
		}
		path, err := clipstore.FinalPath(root, clip.CameraID, clip.ID)
		if err != nil {
			writeRouteNotFound(w, "clip")
			return
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "clip media is unavailable"))
			return
		}
		valid, err := clipstore.VerifyRegularFile(path, clip.SizeBytes, clip.Checksum)
		if err != nil || !valid {
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "clip media checksum is invalid"))
			return
		}
		file, err := os.Open(path)
		if err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "clip media is unavailable"))
			return
		}
		defer file.Close()
		serveName := filepath.Base(path)
		modtime := clip.UpdatedAt
		if modtime.IsZero() {
			modtime = time.Unix(0, 0).UTC()
		}
		http.ServeContent(w, r, serveName, modtime, file)
	}
}

func mediaClipID(path string) (string, error) {
	raw := strings.TrimPrefix(path, "/api/clips/")
	if !strings.HasSuffix(raw, "/media") {
		return "", errors.New("invalid media route")
	}
	id, err := url.PathUnescape(strings.TrimSuffix(raw, "/media"))
	if err != nil || id == "" || strings.Contains(id, "/") {
		return "", errors.New("invalid media route")
	}
	return id, nil
}

func clipMediaRoot() string {
	if value := strings.TrimSpace(os.Getenv("SYNORA_CLIP_DIR")); value != "" {
		return value
	}
	return "/var/lib/synora/clips"
}
