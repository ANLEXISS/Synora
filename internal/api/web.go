package api

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// WebHealth describes the state of the static web build served by synora-api.
type WebHealth struct {
	Enabled       bool   `json:"enabled"`
	Root          string `json:"root"`
	IndexPresent  bool   `json:"index_present"`
	IndexPath     string `json:"index_path"`
	AssetsPresent bool   `json:"assets_present"`
	AssetsCount   int    `json:"assets_count"`
	LastModified  string `json:"last_modified,omitempty"`
	Status        string `json:"status"`
}

// Health inspects the static web root without making its availability a
// startup requirement for synora-api.
func (s *Server) Health() WebHealth {
	root := strings.TrimSpace(s.WebRoot)
	indexPath := filepath.Join(root, "index.html")

	health := WebHealth{
		Enabled:   s.WebEnabled,
		Root:      root,
		IndexPath: indexPath,
		Status:    "disabled",
	}

	if info, err := os.Stat(indexPath); err == nil && !info.IsDir() {
		health.IndexPresent = true
		health.LastModified = info.ModTime().UTC().Format(time.RFC3339Nano)
	}

	assetsPath := filepath.Join(root, "assets")
	if info, err := os.Stat(assetsPath); err == nil && info.IsDir() {
		health.AssetsCount = countWebAssets(assetsPath)
		health.AssetsPresent = health.AssetsCount > 0
	}

	if !s.WebEnabled {
		return health
	}

	health.Status = "degraded"
	if health.IndexPresent && health.AssetsPresent {
		health.Status = "ok"
	}
	return health
}

func countWebAssets(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	return count
}

func (s *Server) WebHandler() http.Handler {
	root := strings.TrimSpace(s.WebRoot)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.WebEnabled || root == "" {
			http.NotFound(w, r)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		// Ne jamais renvoyer index.html pour une route API cassée.
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/ws") {
			http.NotFound(w, r)
			return
		}

		cleanURLPath := path.Clean("/" + r.URL.Path)
		rel := strings.TrimPrefix(cleanURLPath, "/")

		if rel == "" {
			serveWebIndex(w, r, root)
			return
		}

		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		rootClean := filepath.Clean(root)

		if fullPath != rootClean && !strings.HasPrefix(fullPath, rootClean+string(os.PathSeparator)) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			setWebCacheHeaders(w, cleanURLPath)
			http.ServeFile(w, r, fullPath)
			return
		}

		// SPA fallback: /settings, /automations, etc. -> index.html
		serveWebIndex(w, r, root)
	})
}

func serveWebIndex(w http.ResponseWriter, r *http.Request, root string) {
	indexPath := filepath.Join(root, "index.html")

	if _, err := os.Stat(indexPath); err != nil {
		log.Printf("web index missing: %s", indexPath)
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, indexPath)
}

func setWebCacheHeaders(w http.ResponseWriter, requestPath string) {
	if strings.HasPrefix(requestPath, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
}
