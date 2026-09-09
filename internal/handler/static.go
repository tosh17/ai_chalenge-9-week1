package handler

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

//go:embed web
var webFS embed.FS

var indexTemplate = func() *template.Template {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		log.Fatal(err)
	}
	return template.Must(template.New("index").Parse(string(data)))
}()

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	staticFS, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic("static fs: " + err.Error())
	}

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /{$}", h.Index)
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /api/providers", h.Providers)
	mux.HandleFunc("GET /api/history", h.History)
	mux.HandleFunc("DELETE /api/history", h.ClearHistory)
	mux.HandleFunc("POST /api/chat", h.Chat)
	mux.HandleFunc("POST /api/design", h.Design)
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, map[string]string{"Model": h.model}); err != nil {
		http.Error(w, "page render error", http.StatusInternalServerError)
	}
}
