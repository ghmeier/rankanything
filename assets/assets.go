package assets

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
