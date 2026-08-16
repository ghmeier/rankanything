// Package assets embeds the HTML templates and compiled static files so the
// binary ships as a single artifact.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates is the template tree rooted at layout.html.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// Static is the tree served under /static/.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
