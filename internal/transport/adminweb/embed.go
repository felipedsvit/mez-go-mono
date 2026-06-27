// Package adminweb — embedded static assets.
//
// The htmx.min.js placeholder is a no-op stub in Phase 0. The real bundle
// (with extensions) is delivered in Phase 5 (D13 — htmx for the panel).
package adminweb

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticAssets embed.FS

// Assets returns the fs with the static files (CSS/JS) for the admin web UI.
func Assets() fs.FS { return staticAssets }
