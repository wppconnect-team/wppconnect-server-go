package httpapi

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed manager/index.html manager/styles.css manager/app.js
var managerFiles embed.FS

func (s *Server) managerRoutes(r chi.Router) {
	for _, path := range []string{"/manager", "/manager/", "/manager/styles.css", "/manager/app.js"} {
		r.Get(path, s.managerAsset)
	}
}
func (s *Server) managerAsset(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.ManagerEnabled {
		http.NotFound(w, r)
		return
	}
	name, mime := "index.html", "text/html; charset=utf-8"
	switch r.URL.Path {
	case "/manager/styles.css":
		name, mime = "styles.css", "text/css; charset=utf-8"
	case "/manager/app.js":
		name, mime = "app.js", "text/javascript; charset=utf-8"
	}
	data, err := managerFiles.ReadFile("manager/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Write(data)
}
