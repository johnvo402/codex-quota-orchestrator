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
			links := `<div style="position:fixed;right:18px;top:18px;z-index:100;display:flex;gap:8px"><a href="/queue.html" style="background:#2563eb;color:white;text-decoration:none;padding:9px 13px;border-radius:10px;font:600 13px system-ui">Task Queue</a><a href="/settings.html" style="background:#172036;border:1px solid #31415f;color:white;text-decoration:none;padding:9px 13px;border-radius:10px;font:600 13px system-ui">Settings</a></div>`
			html = strings.Replace(html, "</body>", links+"</body>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		files.ServeHTTP(w, r)
	})
}
