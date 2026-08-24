// Package assets embeds the compiled static files so the binary ships as a
// single artifact. Markup is templ components in internal/ui, compiled to Go
// rather than embedded as data.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

// Static is the tree served under /static/.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
