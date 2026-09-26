// Package cache keeps Spotify library responses on disk, in front of the
// Web API. Spotify rate-limits that API per app, and the shared app sptui
// uses by default is often over its limit, so the fewer calls the better:
//
//   - answers younger than their kind's freshness are served from disk;
//   - older ones are refetched, but if Spotify refuses (rate limited, or
//     offline) the saved copy is served instead of an error;
//   - changes made through sptui clear what they affect;
//   - spotify.WithFresh on a request's context skips the cache (reload).
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/sptui/internal/spotify"
	"github.com/kyledickey/sptui/internal/tui"
)

// How long each kind of answer stays fresh.
const (
	queue    = 15 * time.Second // changes with every song; cached only as a fallback
	recent   = 2 * time.Minute  // recently played changes with every song
	changing = 10 * time.Minute // playlists, liked songs, search
	settled  = 24 * time.Hour   // top tracks, an artist's releases
	fixed    = 30 * 24 * time.Hour
)

// Library caches a tui.Library. Methods it doesn't define (the queue,
// cover art) go straight to the wrapped one.
type Library struct {
	tui.Library
	dir string
	log *slog.Logger

	mu    sync.Mutex
	saved map[string]savedAt // InLibrary answers, by URI
}

type savedAt struct {
	saved bool
	at    time.Time
}

// New wraps lib, keeping answers in dir.
func New(lib tui.Library, dir string, log *slog.Logger) *Library {
	return &Library{Library: lib, dir: dir, log: log, saved: map[string]savedAt{}}
}

// Clear deletes everything cached in dir, e.g. when logging out.
func Clear(dir string) error {
	return os.RemoveAll(dir)
}

// entry is one cached answer on disk.
type entry struct {
	Key   string          `json:"key"`
	Saved time.Time       `json:"saved"`
	Value json.RawMessage `json:"value"`
}

// get returns the cached answer for kind+args if it's fresh, else fetches
// it, falling back to a stale copy if fetching fails.
func get[T any](ctx context.Context, c *Library, maxAge time.Duration, kind string, args []any, fetch func() (T, error)) (T, error) {
	key := kind + fmt.Sprintf("%v", args) // e.g. playlist[abc 50]
	path := c.path(kind, key)
	cached, savedAt := c.load(path, key)
	if cached != nil && time.Since(savedAt) <= maxAge && !spotify.WantsFresh(ctx) {
		var v T
		if err := json.Unmarshal(cached, &v); err == nil {
			spotify.NoteOrigin(ctx, savedAt, false)
			return v, nil
		}
	}

	v, err := fetch()
	if err != nil {
		var old T
		if cached != nil && json.Unmarshal(cached, &old) == nil {
			c.log.Warn("serving saved copy", "what", key, "because", err)
			spotify.NoteOrigin(ctx, savedAt, true)
			return old, nil
		}
		return v, err
	}
	c.store(path, key, v)
	return v, nil
}

// load reads a cached answer and when it was saved.
func (c *Library) load(path, key string) (value json.RawMessage, saved time.Time) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}
	}
	var e entry
	if json.Unmarshal(data, &e) != nil || e.Key != key {
		return nil, time.Time{}
	}
	return e.Value, e.Saved
}

func (c *Library) store(path, key string, v any) {
	value, err := json.Marshal(v)
	if err == nil {
		var data []byte
		if data, err = json.Marshal(entry{Key: key, Saved: time.Now(), Value: value}); err == nil {
			err = writeFile(path, data)
		}
	}
	if err != nil {
		c.log.Warn("cache write failed", "what", key, "err", err)
	}
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// path names the file for key. Files are grouped by kind so a whole kind can
// be cleared at once.
func (c *Library) path(kind, key string) string {
	h := fnv.New64a()
	h.Write([]byte(key))
	return filepath.Join(c.dir, fmt.Sprintf("%s-%x.json", kind, h.Sum64()))
}

// forget clears every cached answer of the given kinds.
func (c *Library) forget(kinds ...string) {
	for _, kind := range kinds {
		files, _ := filepath.Glob(filepath.Join(c.dir, kind+"-*.json"))
		for _, f := range files {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
				c.log.Warn("cache clear failed", "file", f, "err", err)
			}
		}
	}
}

// --- cached reads ---

func (c *Library) Me(ctx context.Context) (spotify.User, error) {
	return get(ctx, c, fixed, "me", nil, func() (spotify.User, error) { return c.Library.Me(ctx) })
}

func (c *Library) Playlists(ctx context.Context, offset int) (spotify.Page[spotify.Playlist], error) {
	return get(ctx, c, changing, "playlists", []any{offset}, func() (spotify.Page[spotify.Playlist], error) {
		return c.Library.Playlists(ctx, offset)
	})
}

func (c *Library) SavedTracks(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	return get(ctx, c, changing, "liked", []any{offset}, func() (spotify.Page[spotify.Track], error) {
		return c.Library.SavedTracks(ctx, offset)
	})
}

func (c *Library) SavedAlbums(ctx context.Context, offset int) (spotify.Page[spotify.Album], error) {
	return get(ctx, c, changing, "albums", []any{offset}, func() (spotify.Page[spotify.Album], error) {
		return c.Library.SavedAlbums(ctx, offset)
	})
}

func (c *Library) FollowedArtists(ctx context.Context) ([]spotify.Artist, error) {
	return get(ctx, c, changing, "artists", nil, func() ([]spotify.Artist, error) { return c.Library.FollowedArtists(ctx) })
}

func (c *Library) RecentlyPlayed(ctx context.Context) ([]spotify.Track, error) {
	return get(ctx, c, recent, "recent", nil, func() ([]spotify.Track, error) { return c.Library.RecentlyPlayed(ctx) })
}

func (c *Library) TopTracks(ctx context.Context) ([]spotify.Track, error) {
	return get(ctx, c, settled, "top", nil, func() ([]spotify.Track, error) { return c.Library.TopTracks(ctx) })
}

func (c *Library) PlaylistTracks(ctx context.Context, id string, offset int) (spotify.Page[spotify.Track], error) {
	return get(ctx, c, changing, "playlist", []any{id, offset}, func() (spotify.Page[spotify.Track], error) {
		return c.Library.PlaylistTracks(ctx, id, offset)
	})
}

func (c *Library) AlbumTracks(ctx context.Context, album spotify.Album, offset int) (spotify.Page[spotify.Track], error) {
	return get(ctx, c, fixed, "album", []any{album.ID, offset}, func() (spotify.Page[spotify.Track], error) {
		return c.Library.AlbumTracks(ctx, album, offset)
	})
}

func (c *Library) ArtistAlbums(ctx context.Context, id string, offset int) (spotify.Page[spotify.Album], error) {
	return get(ctx, c, settled, "artist", []any{id, offset}, func() (spotify.Page[spotify.Album], error) {
		return c.Library.ArtistAlbums(ctx, id, offset)
	})
}

func (c *Library) SavedShows(ctx context.Context, offset int) (spotify.Page[spotify.Show], error) {
	return get(ctx, c, changing, "shows", []any{offset}, func() (spotify.Page[spotify.Show], error) {
		return c.Library.SavedShows(ctx, offset)
	})
}

func (c *Library) SavedEpisodes(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	return get(ctx, c, changing, "episodes", []any{offset}, func() (spotify.Page[spotify.Track], error) {
		return c.Library.SavedEpisodes(ctx, offset)
	})
}

// ShowEpisodes stays fresh as long as a playlist: new episodes come often.
func (c *Library) ShowEpisodes(ctx context.Context, show spotify.Show, offset int) (spotify.Page[spotify.Track], error) {
	return get(ctx, c, changing, "show", []any{show.ID, offset}, func() (spotify.Page[spotify.Track], error) {
		return c.Library.ShowEpisodes(ctx, show, offset)
	})
}

func (c *Library) Search(ctx context.Context, query string) (spotify.SearchResults, error) {
	return get(ctx, c, changing, "search", []any{strings.ToLower(query)}, func() (spotify.SearchResults, error) {
		return c.Library.Search(ctx, query)
	})
}

// Queue is refetched almost every time, but a recent copy beats an error
// when Spotify is busy.
func (c *Library) Queue(ctx context.Context) (spotify.Queue, error) {
	return get(ctx, c, queue, "queue", nil, func() (spotify.Queue, error) { return c.Library.Queue(ctx) })
}

// InLibrary remembers answers in memory; they're checked for every new song.
func (c *Library) InLibrary(ctx context.Context, uris []string) ([]bool, error) {
	out := make([]bool, len(uris))
	c.mu.Lock()
	known := !spotify.WantsFresh(ctx)
	for i, u := range uris {
		s, ok := c.saved[u]
		known = known && ok && time.Since(s.at) < changing
		out[i] = s.saved
	}
	c.mu.Unlock()
	if known {
		return out, nil
	}
	saved, err := c.Library.InLibrary(ctx, uris)
	if err != nil {
		return nil, err
	}
	c.remember(uris, saved...)
	return saved, nil
}

func (c *Library) remember(uris []string, saved ...bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, u := range uris {
		c.saved[u] = savedAt{saved: saved[min(i, len(saved)-1)], at: time.Now()}
	}
}

// --- writes, which clear what they change ---

func (c *Library) SaveToLibrary(ctx context.Context, uris []string) error {
	if err := c.Library.SaveToLibrary(ctx, uris); err != nil {
		return err
	}
	c.remember(uris, true)
	c.forget(kindsFor(uris)...)
	return nil
}

func (c *Library) RemoveFromLibrary(ctx context.Context, uris []string) error {
	if err := c.Library.RemoveFromLibrary(ctx, uris); err != nil {
		return err
	}
	c.remember(uris, false)
	c.forget(kindsFor(uris)...)
	return nil
}

func (c *Library) AddToPlaylist(ctx context.Context, id string, uris []string) error {
	if err := c.Library.AddToPlaylist(ctx, id, uris); err != nil {
		return err
	}
	c.forget("playlist", "playlists")
	return nil
}

// kindsFor lists the cached kinds that saving or removing uris changes.
func kindsFor(uris []string) []string {
	var kinds []string
	for _, u := range uris {
		switch {
		case strings.HasPrefix(u, "spotify:track:"):
			kinds = append(kinds, "liked")
		case strings.HasPrefix(u, "spotify:album:"):
			kinds = append(kinds, "albums")
		case strings.HasPrefix(u, "spotify:artist:"):
			kinds = append(kinds, "artists")
		case strings.HasPrefix(u, "spotify:playlist:"):
			kinds = append(kinds, "playlists")
		case strings.HasPrefix(u, "spotify:show:"):
			kinds = append(kinds, "shows")
		case strings.HasPrefix(u, "spotify:episode:"):
			kinds = append(kinds, "episodes")
		}
	}
	return kinds
}
