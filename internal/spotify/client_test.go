package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/sptui/internal/logging"
)

// newTestClient returns a client whose requests are served by handler.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.Client(), logging.Discard()).WithBaseURL(srv.URL)
}

func TestPlaybackNothingPlaying(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	st, err := c.Playback(context.Background())
	if err != nil || st != nil {
		t.Fatalf("Playback() = %v, %v; want nil, nil", st, err)
	}
}

func TestPlaybackDecodes(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/player" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"device":{"id":"d1","name":"Laptop","volume_percent":40},
			"is_playing":true,"progress_ms":1500,"shuffle_state":true,"repeat_state":"context",
			"item":{"name":"Song","uri":"spotify:track:1","duration_ms":200000,"artists":[{"name":"A"},{"name":"B"}]}}`)
	})
	st, err := c.Playback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsPlaying || st.Device.Name != "Laptop" || *st.Device.VolumePercent != 40 || st.Item.ArtistNames() != "A, B" {
		t.Fatalf("unexpected state: %+v", st)
	}
}

func TestNoActiveDevice(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`)
	})
	err := c.Play(context.Background(), PlayOptions{})
	if !errors.Is(err, ErrNoActiveDevice) {
		t.Fatalf("err = %v, want ErrNoActiveDevice", err)
	}
}

func TestAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"status":403,"message":"Forbidden"}}`)
	})
	_, err := c.PlaylistTracks(context.Background(), "abc", 0)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Message != "Forbidden" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRateLimitRetriesOnce(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"id":"me","display_name":"Me"}`)
	})
	u, err := c.Me(context.Background())
	if err != nil || u.Name() != "Me" || calls != 2 {
		t.Fatalf("Me() = %+v, %v after %d calls", u, err, calls)
	}
}

func TestPlaySendsBody(t *testing.T) {
	var got map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Query().Get("device_id") != "dev" {
			t.Errorf("got %s %s", r.Method, r.URL)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	})
	err := c.Play(context.Background(), PlayOptions{DeviceID: "dev", ContextURI: "spotify:album:1", OffsetURI: "spotify:track:2"})
	if err != nil {
		t.Fatal(err)
	}
	if got["context_uri"] != "spotify:album:1" || got["offset"].(map[string]any)["uri"] != "spotify:track:2" {
		t.Fatalf("body = %v", got)
	}
}

func TestPlaylistTracksUsesItems(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlists/p1/items" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"total":3,"limit":50,"offset":0,"next":"x","items":[
			{"item":{"name":"One","uri":"spotify:track:1"}},
			{"item":null},
			{"item":{"name":"Ep","uri":"spotify:episode:2","type":"episode","show":{"name":"Pod"}}}]}`)
	})
	pg, err := c.PlaylistTracks(context.Background(), "p1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 2 || !pg.HasMore() || pg.Items[1].ArtistNames() != "Pod" {
		t.Fatalf("page = %+v", pg)
	}
}

func TestLibraryChunksURIs(t *testing.T) {
	var sizes []int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := len(strings.Split(r.URL.Query().Get("uris"), ","))
		sizes = append(sizes, n)
		out := make([]bool, n)
		json.NewEncoder(w).Encode(out)
	})
	uris := make([]string, 90)
	for i := range uris {
		uris[i] = "spotify:track:x"
	}
	saved, err := c.InLibrary(context.Background(), uris)
	if err != nil || len(saved) != 90 {
		t.Fatalf("InLibrary = %d results, %v", len(saved), err)
	}
	if len(sizes) != 3 || sizes[0] != 40 || sizes[2] != 10 {
		t.Fatalf("chunk sizes = %v", sizes)
	}
}

func TestID(t *testing.T) {
	if got := ID("spotify:track:abc"); got != "abc" {
		t.Fatalf("ID = %q", got)
	}
}

func TestCoverURL(t *testing.T) {
	imgs := []Image{{URL: "640", Width: 640}, {URL: "300", Width: 300}, {URL: "64", Width: 64}}
	for _, c := range []struct {
		min  int
		want string
	}{{200, "300"}, {300, "300"}, {301, "640"}, {1000, "640"}, {10, "64"}} {
		if got := CoverURL(imgs, c.min); got != c.want {
			t.Errorf("CoverURL(%d) = %s, want %s", c.min, got, c.want)
		}
	}
	if CoverURL(nil, 100) != "" {
		t.Error("no images should give no URL")
	}
	if got := CoverURL([]Image{{URL: "mosaic"}}, 300); got != "mosaic" {
		t.Errorf("unknown size = %q", got)
	}
}

func TestCoverArt(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		png.Encode(w, img)
	})
	img, err := c.CoverArt(context.Background(), c.baseURL+"/cover.png")
	if err != nil || img.Bounds().Dx() != 2 {
		t.Fatalf("CoverArt = %v, %v", img, err)
	}
}

func TestRateLimitBlocksLaterRequests(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Me(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || apiErr.RetryAfter != time.Minute {
		t.Fatalf("err = %v", err)
	}
	// While the limit lasts, requests fail without reaching Spotify.
	if _, err := c.Playlists(context.Background(), 0); !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("second request: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Spotify was called %d times during the rate limit", calls)
	}
}

func TestRateLimitNeverRetriesWrites(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if err := c.SaveToLibrary(context.Background(), []string{"spotify:track:1"}); err == nil || calls != 1 {
		t.Fatalf("err = %v after %d calls", err, calls)
	}
}

func TestYear(t *testing.T) {
	for in, want := range map[string]string{"2002-11-11": "2002", "1999": "1999", "year:2002 month:11 day:11": "2002", "": ""} {
		if got := (Album{ReleaseDate: in}).Year(); got != want {
			t.Errorf("Year(%q) = %q, want %q", in, got, want)
		}
	}
}
