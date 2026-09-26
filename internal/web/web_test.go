package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	site := fstest.MapFS{
		"index.html":      {Data: []byte("home")},
		"about.html":      {Data: []byte("about")},
		"docs/index.html": {Data: []byte("docs")},
		"style.css":       {Data: []byte("body{}")},
		"404.html":        {Data: []byte("lost")},
	}
	h := Handler(site)
	for _, tt := range []struct {
		path, body, location string
		status               int
	}{
		{path: "/", status: 200, body: "home"},
		{path: "/about", status: 200, body: "about"},
		{path: "/about/", status: 200, body: "about"},
		{path: "/style.css", status: 200, body: "body{}"},
		{path: "/about.html", status: 301, location: "/about"},
		{path: "/index.html", status: 301, location: "/"},
		{path: "/nope", status: 404, body: "lost"},
		{path: "/nope.css", status: 404, body: "lost"},
		{path: "/docs", status: 200, body: "docs"},
		{path: "/docs/", status: 200, body: "docs"},
		{path: "/docs/index.html", status: 301, location: "/docs/"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.path, rec.Code, tt.status)
		}
		if tt.body != "" && !strings.Contains(rec.Body.String(), tt.body) {
			t.Errorf("%s: body %q, want %q", tt.path, rec.Body.String(), tt.body)
		}
		if got := rec.Header().Get("Location"); got != tt.location {
			t.Errorf("%s: Location %q, want %q", tt.path, got, tt.location)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: status %d, want 405", rec.Code)
	}
}

func TestEmbeddedSite(t *testing.T) {
	for _, name := range []string{"index.html", "404.html", "style.css"} {
		if !isFile(Site, name) {
			t.Errorf("embedded site is missing %s", name)
		}
	}
}
