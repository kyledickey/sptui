package web

import (
	"fmt"
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

func TestData(t *testing.T) {
	h := Handler(fstest.MapFS{})
	for _, tt := range []struct {
		path   string
		status int
	}{
		{"/intros.json", 200},
		{"/intros/vinyl.json", 200},
		{"/intros/nope.json", 404},
		{"/intros/vinyl", 404},
		{"/screens/home.json", 200},
		{"/screens/playing.json", 200},
		{"/screens/nope.json", 404},
	} {
		req := httptest.NewRequest("GET", tt.path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.path, rec.Code, tt.status)
		}
		if tt.status == 200 && rec.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("%s: not gzipped", tt.path)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/intros.json", nil))
	if !strings.HasPrefix(rec.Body.String(), `["vinyl"`) {
		t.Errorf("/intros.json without gzip: %q", rec.Body.String())
	}
}

func TestEmbeddedSite(t *testing.T) {
	for _, name := range []string{"index.html", "404.html", "style.css", "screen.js", "install.js"} {
		if !isFile(Site, name) {
			t.Errorf("embedded site is missing %s", name)
		}
	}
}

func TestParseScreen(t *testing.T) {
	s := parseScreen("\x1b[1;38;2;30;215;96mhi\x1b[m \x1b[48;2;16;18;22m \x1b[m\n█")
	if s.W != 4 || s.H != 2 {
		t.Fatalf("size %d×%d, want 4×2", s.W, s.H)
	}
	want := [][6]any{{0, 0, "h", 1, 0, true}, {1, 0, "i", 1, 0, true}, {3, 0, " ", 0, 2, false}, {0, 1, "█", 0, 0, false}}
	if fmt.Sprint(s.Cells) != fmt.Sprint(want) || s.Colors[0] != "#1ed760" {
		t.Errorf("cells %v colours %v, want %v", s.Cells, s.Colors, want)
	}
}
