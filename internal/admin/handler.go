// Package admin provides HTTP handlers for serving the admin SPA.
package admin

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	adminassets "github.com/m0rjc/OsmDeviceAdapter/web/admin"
)

// buildTime is injected at build time via ldflags
var buildTime = "dev"

// NewSPAHandler returns an http.Handler that serves the admin SPA.
// It handles client-side routing by returning index.html for paths
// that don't match a static file.
//
// The handler expects to be mounted at /admin/ and will strip that
// prefix when looking up files from the embedded filesystem.
//
// ETags are based on build time since embedded files never change at runtime.
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
				serveIndexWithCacheHeaders(w, r, distFS)
				return
			}
		} else {
			// Root path - serve index.html
			serveIndexWithCacheHeaders(w, r, distFS)
			return
		}

		// File exists - set cache headers and ETag based on build time
		etag := fmt.Sprintf(`"%s"`, buildTime)
		setCacheHeaders(w, cleanPath, etag)

		// Check If-None-Match header for 304 Not Modified
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Serve the file
		r.URL.Path = path
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndexWithCacheHeaders serves index.html with appropriate cache headers
func serveIndexWithCacheHeaders(w http.ResponseWriter, r *http.Request, distFS fs.FS) {
	indexContent, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}

	// ETag based on build time
	etag := fmt.Sprintf(`"%s"`, buildTime)

	// Set no-cache for index.html (always revalidate)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("ETag", etag)

	// Check If-None-Match for 304
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexContent)
}

// setCacheHeaders sets appropriate cache control headers based on file type
func setCacheHeaders(w http.ResponseWriter, path string, etag string) {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}

	// Set cache duration based on file type
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
		// Vite adds content hashes to filenames, so these can be cached immutably
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".png") ||
		strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".ico") ||
		strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2") {
		// Static assets can be cached for a day
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		// Other files should revalidate
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
}
