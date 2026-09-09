package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
var assets embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
