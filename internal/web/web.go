// Package web serves sptui's website: the HTML pages and stylesheet in site/,
// embedded into the binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed site
var embedded embed.FS

// Site is the embedded site.
var Site, _ = fs.Sub(embedded, "site")

// Handler serves site, and sptui's startup animations for it to play (see
// intros). Pages are addressed without their .html, so
// site/about.html is /about, site/docs/index.html is /docs and
// site/index.html is /; other files, like
// style.css, are served as is. Anything else gets site/404.html.
func Handler(site fs.FS) http.Handler {
	files := http.FileServerFS(site)
	mux := http.NewServeMux()
	var anims intros
	mux.HandleFunc("GET /intros.json", anims.serveIndex)
	mux.HandleFunc("GET /intros/{file}", anims.serveAnim)
	var shots screens
	mux.HandleFunc("GET /screens/{file}", shots.serve)
	go anims.load() // draw them now, not on the first visit
	go shots.load()
	mux.HandleFunc("GET /{path...}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("path")
		switch {
		case name == "":
			name = "index.html"
		case strings.HasSuffix(name, ".html"):
			http.Redirect(w, r, "/"+strings.TrimSuffix(strings.TrimSuffix(name, ".html"), "index"), http.StatusMovedPermanently)
			return
		case path.Ext(name) == "":
			name = strings.TrimSuffix(name, "/")
			if isFile(site, name+"/index.html") {
				name += "/index"
			}
			name += ".html"
		default:
			if isFile(site, name) {
				if ext := path.Ext(name); ext == ".sh" || ext == ".ps1" {
					// Readable in a browser, not downloaded.
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if !isFile(site, name) {
			notFound(w, site)
			return
		}
		http.ServeFileFS(w, r, site, name)
	})
	return mux
}

func isFile(site fs.FS, name string) bool {
	info, err := fs.Stat(site, name)
	return err == nil && !info.IsDir()
}

func notFound(w http.ResponseWriter, site fs.FS) {
	page, err := fs.ReadFile(site, "404.html")
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write(page)
}
