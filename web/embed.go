package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded Svelte SPA assets
// with history-API fallback to index.html for client-side routes.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Never serve SPA HTML for unmatched API endpoints.
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			http.NotFound(w, r)
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if cleanPath == "" || cleanPath == "." {
			serveIndexHTML(w, r, sub)
			return
		}

		// Serve concrete static files when present.
		if info, err := fs.Stat(sub, cleanPath); err == nil && !info.IsDir() {
			if strings.HasPrefix(cleanPath, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if cleanPath == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if ext := filepath.Ext(cleanPath); ext != "" {
				if ct := mime.TypeByExtension(ext); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Missing files with an explicit asset extension should 404 cleanly.
		if filepath.Ext(cleanPath) != "" {
			http.NotFound(w, r)
			return
		}

		// Client-side SPA route fallback.
		serveIndexHTML(w, r, sub)
	})
}

func serveIndexHTML(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
