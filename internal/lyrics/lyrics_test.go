package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kyledickey/sptui/internal/logging"
	"github.com/kyledickey/sptui/internal/spotify"
)

func TestParseLRC(t *testing.T) {
	lines := parseLRC("[00:31.48] Like the legend\n[00:35.40] All ends\n\n[01:02.5][00:10.00] Again\nnot a line\n")
	want := []Line{
		{10 * time.Second, "Again"},
		{31480 * time.Millisecond, "Like the legend"},
		{35400 * time.Millisecond, "All ends"},
		{62500 * time.Millisecond, "Again"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines: %+v", len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func TestCurrent(t *testing.T) {
	l := Lyrics{Synced: true, Lines: []Line{{10 * time.Second, "a"}, {20 * time.Second, "b"}}}
	for pos, want := range map[time.Duration]int{5 * time.Second: -1, 10 * time.Second: 0, 19 * time.Second: 0, time.Minute: 1} {
		if got := l.Current(pos); got != want {
			t.Errorf("Current(%s) = %d, want %d", pos, got, want)
		}
	}
	if (Lyrics{Lines: []Line{{0, "plain"}}}).Current(time.Minute) != -1 {
		t.Error("plain lyrics have no current line")
	}
}

var song = spotify.Track{
	Name: "Get Lucky", Artists: []spotify.Artist{{Name: "Daft Punk"}},
	Album: spotify.Album{Name: "Random Access Memories"}, DurationMS: 369000,
}

func TestExactMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/get" || q.Get("artist_name") != "Daft Punk" || q.Get("duration") != "369" || r.UserAgent() != "test" {
			t.Errorf("unexpected request %s", r.URL)
		}
		json.NewEncoder(w).Encode(record{SyncedLyrics: "[00:01.00] hi"})
	}))
	defer srv.Close()
	l, err := New("test", logging.Discard()).WithBaseURL(srv.URL).Lyrics(context.Background(), song)
	if err != nil || !l.Synced || l.Lines[0].Text != "hi" {
		t.Fatalf("Lyrics = %+v, %v", l, err)
	}
}

func TestSearchFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]record{
			{Duration: 30, SyncedLyrics: "[00:01.00] short edit"},
			{Duration: 371, PlainLyrics: "plain version"},
			{Duration: 367, SyncedLyrics: "[00:01.00] album version"},
		})
	}))
	defer srv.Close()
	l, err := New("test", logging.Discard()).WithBaseURL(srv.URL).Lyrics(context.Background(), song)
	if err != nil || l.Lines[0].Text != "album version" {
		t.Fatalf("Lyrics = %+v, %v", l, err)
	}
}

func TestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if _, err := New("test", logging.Discard()).WithBaseURL(srv.URL).Lyrics(context.Background(), song); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"Song - Remastered 2011":     "Song",
		"Song (feat. Someone)":       "Song",
		"Song (Live) - 2004 Version": "Song",
		"Plain":                      "Plain",
	} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	c := New("test", logging.Discard()).WithBaseURL(srv.URL)
	if _, err := c.Lyrics(context.Background(), song); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("503: err = %v, want ErrUnavailable", err)
	}
	srv.Close() // now unreachable
	if _, err := c.Lyrics(context.Background(), song); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable: err = %v, want ErrUnavailable", err)
	}
}
