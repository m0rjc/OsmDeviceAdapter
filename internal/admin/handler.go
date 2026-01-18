// Package admin provides HTTP handlers for serving the admin SPA.
package admin

import (
	"io/fs"
	"net/http"
	"strings"

	adminassets "github.com/m0rjc/OsmDeviceAdapter/web/admin"
)

// NewSPAHandler returns an http.Handler that serves the admin SPA.
// It handles client-side routing by returning index.html for paths
// that don't match a static file.
//
// The handler expects to be mounted at /admin/ and will strip that
// prefix when looking up files from the embedded filesystem.
func NewSPAHandler() http.Handler {
	// Strip "dist" prefix from embedded filesystem
	distFS, err := fs.Sub(adminassets.StaticFS, "dist")
	if err != nil {
		panic("failed to access embedded admin dist: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /admin prefix for file lookup
		path := strings.TrimPrefix(r.URL.Path, "/admin")
		if path == "" {
			path = "/"
		}

		// Check if the file exists (for non-root paths)
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath != "" {
			_, err := fs.Stat(distFS, cleanPath)
			if err != nil {
				// File not found - serve index.html for SPA client-side routing
				// Use "/" so FileServer serves index.html without redirect
				// (FileServer redirects /index.html to ./ which causes loops)
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// File exists or root path - serve it
		r.URL.Path = path
		fileServer.ServeHTTP(w, r)
	})
}
