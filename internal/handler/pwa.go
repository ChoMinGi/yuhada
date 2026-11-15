package handler

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// registerPWARoutes keeps the worker at the origin root so its scope covers v1,
// v2 and existing customer links. Files are public; no session data is returned.
func registerPWARoutes(r chi.Router) {
	for path, asset := range map[string]struct{ file, contentType string }{
		"/sw.js":                {"sw.js", "text/javascript; charset=utf-8"},
		"/manifest.webmanifest": {"manifest.webmanifest", "application/manifest+json; charset=utf-8"},
		"/favicon.svg":          {"icons/favicon.svg", "image/svg+xml"},
		"/favicon.ico":          {"icons/favicon.ico", "image/x-icon"},
	} {
		file, contentType := asset.file, asset.contentType
		r.Method(http.MethodGet, path, publicPWAFile(file, contentType))
		r.Method(http.MethodHead, path, publicPWAFile(file, contentType))
	}
}

func publicPWAFile(file, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if file == "sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
		}
		http.ServeFile(w, r, filepath.Join("static", file))
	})
}
