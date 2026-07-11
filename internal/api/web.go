package api

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

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
