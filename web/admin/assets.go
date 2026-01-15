// Package adminassets embeds the admin SPA static files.
// This package exists in web/admin/ so that the //go:embed directive
// can reference the dist/ directory (Go requires embedded files to be
// in or below the package directory).
package adminassets

import "embed"

// StaticFS contains the built React SPA files.
// The dist/ directory is created by `npm run build` in web/admin/.
//
//go:embed all:dist
var StaticFS embed.FS
