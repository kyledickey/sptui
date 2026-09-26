package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
	"github.com/kyledickey/sptui/internal/spotify"
)

// counting wraps the demo library, counting calls and optionally failing
// them like a rate-limited Spotify.
type counting struct {
	*demo.Backend
	calls int
	fail  bool
}

func (c *counting) SavedTracks(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	c.calls++
	if c.fail {
		return spotify.Page[spotify.Track]{}, &spotify.Error{Status: 429}
	}
	return c.Backend.SavedTracks(ctx, offset)
}

func (c *counting) PlaylistTracks(ctx context.Context, id string, offset int) (spotify.Page[spotify.Track], error) {
	c.calls++
	return c.Backend.PlaylistTracks(ctx, id, offset)
}

func newCache(t *testing.T) (*Library, *counting) {
	t.Helper()
	inner := &counting{Backend: demo.New()}
	return New(inner, t.TempDir(), logging.Discard()), inner
}

func TestServesFromCache(t *testing.T) {
	c, inner := newCache(t)
	ctx := context.Background()
	first, err := c.SavedTracks(ctx, 0)
	if err != nil || len(first.Items) == 0 {
		t.Fatal(err)
	}
	second, _ := c.SavedTracks(ctx, 0)
	if inner.calls != 1 || second.Items[0].URI != first.Items[0].URI {
		t.Fatalf("second read hit Spotify (%d calls)", inner.calls)
	}
	// A different page is a different answer.
	c.SavedTracks(ctx, 50)
	if inner.calls != 2 {
		t.Fatalf("page 2 wasn't fetched (%d calls)", inner.calls)
	}
	// A reload asks Spotify again.
	c.SavedTracks(spotify.WithFresh(ctx), 0)
	if inner.calls != 3 {
		t.Fatalf("fresh read didn't hit Spotify (%d calls)", inner.calls)
	}
}

func TestKeysDontCollide(t *testing.T) {
	c, inner := newCache(t)
	ctx := context.Background()
	c.PlaylistTracks(ctx, "pl1", 0)
	c.PlaylistTracks(ctx, "pl", 10)
	if inner.calls != 2 {
		t.Fatalf("different playlists shared a cache entry (%d calls)", inner.calls)
	}
}

func TestServesStaleCopyWhenSpotifyRefuses(t *testing.T) {
	c, inner := newCache(t)
	ctx := context.Background()
	want, _ := c.SavedTracks(ctx, 0)
	inner.fail = true
	got, err := c.SavedTracks(spotify.WithFresh(ctx), 0)
	if err != nil || len(got.Items) != len(want.Items) {
		t.Fatalf("got %d items, %v; want the saved copy", len(got.Items), err)
	}
	// Nothing saved yet: the error comes through.
	var apiErr *spotify.Error
	if _, err := c.SavedTracks(ctx, 100); !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want the 429", err)
	}
}

func TestLikingClearsLikedSongs(t *testing.T) {
	c, inner := newCache(t)
	ctx := context.Background()
	c.SavedTracks(ctx, 0)
	uri := "spotify:track:tr0_0_0"
	if err := c.SaveToLibrary(ctx, []string{uri}); err != nil {
		t.Fatal(err)
	}
	c.SavedTracks(ctx, 0)
	if inner.calls != 2 {
		t.Fatalf("liked songs weren't refetched after a like (%d calls)", inner.calls)
	}
	saved, _ := c.InLibrary(ctx, []string{uri})
	if !saved[0] {
		t.Fatal("InLibrary doesn't know about the like")
	}
}

func TestNotesOrigin(t *testing.T) {
	c, inner := newCache(t)
	var o spotify.Origin
	c.SavedTracks(spotify.WithOrigin(context.Background(), &o), 0)
	if !o.CachedAt.IsZero() {
		t.Fatal("a live answer was marked as cached")
	}
	o = spotify.Origin{}
	c.SavedTracks(spotify.WithOrigin(context.Background(), &o), 0)
	if o.CachedAt.IsZero() || o.Stale {
		t.Fatalf("cached answer origin = %+v", o)
	}
	inner.fail = true
	o = spotify.Origin{}
	c.SavedTracks(spotify.WithOrigin(spotify.WithFresh(context.Background()), &o), 0)
	if o.CachedAt.IsZero() || !o.Stale {
		t.Fatalf("saved copy served on error: origin = %+v", o)
	}
}
