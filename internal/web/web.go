package web

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*
var files embed.FS

var pages = template.Must(template.ParseFS(files, "templates/*.html"))

func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, "home", nil)
	})
	return mux
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		panic(err)
	}
}
