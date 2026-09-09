package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed *.html
var assets embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			html := string(b)
			link := `<a href="/queue.html" style="position:fixed;right:18px;top:18px;z-index:100;background:#2563eb;color:white;text-decoration:none;padding:9px 13px;border-radius:10px;font:600 13px system-ui">Task Queue</a>`
			html = strings.Replace(html, "</body>", link+"</body>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		files.ServeHTTP(w, r)
	})
}
