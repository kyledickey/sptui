package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// offline is a latest that never finds out.
func offline() *latest {
	return &latest{fetch: func(context.Context) (string, error) { return "", errors.New("offline") }}
}

func TestLatest(t *testing.T) {
	asked := make(chan struct{}, 10)
	rel := &latest{fetch: func(context.Context) (string, error) {
		asked <- struct{}{}
		return "v1.2.3", nil
	}}
	h := handler(fstest.MapFS{
		"index.html": {Data: []byte(`<a class="version"><!--latest--></a>`)},
	}, rel)
	<-asked // the handler looks it up as soon as it's made

	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		return rec.Body.String()
	}
	deadline := time.Now().Add(5 * time.Second)
	for get() != `<a class="version">v1.2.3</a>` {
		if time.Now().After(deadline) {
			t.Fatalf("page = %q, want the version in it", get())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-asked:
		t.Error("asked GitHub again within latestEvery")
	default:
	}
}

func TestLatestUnknown(t *testing.T) {
	h := handler(fstest.MapFS{"index.html": {Data: []byte(`<a><!--latest--></a>`)}}, offline())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Body.String(); got != `<a><!--latest--></a>` {
		t.Errorf("page = %q, want the marker left alone", got)
	}
}

func TestHandler(t *testing.T) {
	site := fstest.MapFS{
		"index.html":      {Data: []byte("home")},
		"about.html":      {Data: []byte("about")},
		"docs/index.html": {Data: []byte("docs")},
		"style.css":       {Data: []byte("body{}")},
		"install.sh":      {Data: []byte("#!/bin/sh")},
		"install.ps1":     {Data: []byte("& {}")},
		"404.html":        {Data: []byte("lost")},
	}
	h := handler(site, offline())
	for _, tt := range []struct {
		path, body, location string
		status               int
	}{
		{path: "/", status: 200, body: "home"},
		{path: "/about", status: 200, body: "about"},
		{path: "/about/", status: 200, body: "about"},
		{path: "/style.css", status: 200, body: "body{}"},
		{path: "/install.sh", status: 200, body: "#!/bin/sh"},
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

	for _, script := range []string{"/install.sh", "/install.ps1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", script, nil))
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
			t.Errorf("%s: Content-Type %q, want text/plain", script, got)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: status %d, want 405", rec.Code)
	}
}

func TestData(t *testing.T) {
	h := handler(fstest.MapFS{}, offline())
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
	for _, name := range []string{"index.html", "404.html", "style.css", "screen.js", "install.js", "install.sh", "install.ps1"} {
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
		t.Errorf("cells %v colors %v, want %v", s.Cells, s.Colors, want)
	}
}
