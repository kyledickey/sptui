// Package demo is an in-memory stand-in for Spotify. It implements the same
// methods as *spotify.Client, so the UI can run offline with `sptui -demo`
// and tests can drive the UI without network access.
package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/sptui/internal/spotify"
)

const pageSize = 50

// Backend is a fake Spotify account with a library and a simulated player.
type Backend struct {
	// Latency is added to every call to make loading states visible.
	Latency time.Duration

	mu        sync.Mutex
	me        spotify.User
	artists   []spotify.Artist
	albums    []spotify.Album
	tracks    []spotify.Track // every track on every album
	liked     []spotify.Track
	library   map[string]bool // saved/followed URIs
	playlists []spotify.Playlist
	plTracks  map[string][]spotify.Track
	devices   []spotify.Device
	recent    []spotify.Track
	shows     []spotify.Show
	episodes  []spotify.Track // every episode of every show, newest first per show

	// Player state.
	active  int // index into devices, -1 when nothing is active
	context string
	list    []spotify.Track
	index   int
	queue   []spotify.Track
	playing bool
	posMS   int
	at      time.Time
	shuffle bool
	repeat  string
}

// New returns a demo account filled with generated music.
func New() *Backend {
	b := &Backend{
		me:       spotify.User{ID: "demo", DisplayName: "Demo Listener", URI: "spotify:user:demo"},
		library:  map[string]bool{},
		plTracks: map[string][]spotify.Track{},
		active:   -1,
		repeat:   spotify.RepeatOff,
	}
	b.generate()
	return b
}

var (
	artistNames = []string{"Neon Harbor", "Velvet Static", "Paper Moons", "Low Orbit", "Juniper Fox",
		"Glasshouse Club", "Midnight Arcade", "Sora Blue", "The Quiet Hours", "Copper & Lune"}
	showNames = []string{"Signal & Noise", "The Long Take", "Small Hours Radio", "Field Notes"}
	genres    = []string{"indie pop", "dream pop", "synthwave", "lo-fi", "alt rock", "chillwave", "shoegaze", "electronica"}
	words1    = []string{"Golden", "Electric", "Silent", "Paper", "Neon", "Wild", "Velvet", "Hollow", "Crystal", "Endless", "Summer", "Midnight"}
	words2    = []string{"Hearts", "Skies", "Rivers", "Signals", "Dreams", "Lights", "Waves", "Cities", "Echoes", "Roads", "Gardens", "Machines"}
	devices   = []spotify.Device{
		{ID: "dev-laptop", Name: "Demo Laptop", Type: "Computer", SupportsVolume: true, VolumePercent: ptr(64)},
		{ID: "dev-phone", Name: "Pocket Phone", Type: "Smartphone", SupportsVolume: true, VolumePercent: ptr(40)},
		{ID: "dev-kitchen", Name: "Kitchen Speaker", Type: "Speaker", SupportsVolume: true, VolumePercent: ptr(25)},
	}
)

func ptr[T any](v T) *T { return &v }

func (b *Backend) generate() {
	r := rand.New(rand.NewPCG(7, 11))
	title := func() string { return words1[r.IntN(len(words1))] + " " + words2[r.IntN(len(words2))] }

	for i, name := range artistNames {
		ar := spotify.Artist{
			ID: fmt.Sprintf("ar%d", i), Name: name, URI: fmt.Sprintf("spotify:artist:ar%d", i),
			Genres:    []string{genres[i%len(genres)], genres[(i+3)%len(genres)]},
			Images:    coverImages(fmt.Sprintf("ar%d", i)),
			Followers: &spotify.Count{Total: 1000 + r.IntN(2_000_000)},
		}
		b.artists = append(b.artists, ar)
		for j := range 3 + r.IntN(8) {
			id := fmt.Sprintf("al%d_%d", i, j)
			al := spotify.Album{
				ID: id, Name: title(), URI: "spotify:album:" + id, Artists: []spotify.Artist{ar},
				AlbumType: []string{"album", "single"}[r.IntN(2)], ReleaseDate: fmt.Sprintf("%d-03-14", 2008+r.IntN(18)),
				Images: coverImages(id),
			}
			n := 4 + r.IntN(9)
			al.TotalTracks = n
			var tracks []spotify.Track
			for k := range n {
				tid := fmt.Sprintf("tr%d_%d_%d", i, j, k)
				tracks = append(tracks, spotify.Track{
					ID: tid, Name: title(), URI: "spotify:track:" + tid, Type: "track",
					Artists: []spotify.Artist{ar}, DurationMS: (120 + r.IntN(200)) * 1000, TrackNumber: k + 1,
				})
			}
			for k := range tracks {
				tracks[k].Album = al
			}
			b.albums = append(b.albums, al)
			b.tracks = append(b.tracks, tracks...)
		}
	}

	for _, i := range r.Perm(len(b.tracks))[:min(130, len(b.tracks))] {
		b.liked = append(b.liked, b.tracks[i])
		b.library[b.tracks[i].URI] = true
	}
	for _, i := range r.Perm(len(b.albums))[:12] {
		b.library[b.albums[i].URI] = true
	}
	for _, ar := range b.artists[:6] {
		b.library[ar.URI] = true
	}

	names := []string{"Morning Coffee", "Deep Focus", "Night Drive", "Running Mix", "Sunday Slow", "Throwbacks", "Rainy Day"}
	for i, name := range names {
		b.addPlaylist(fmt.Sprintf("pl%d", i), name, b.me, r.Perm(len(b.tracks))[:15+r.IntN(60)])
	}
	editorial := spotify.User{ID: "spotify", DisplayName: "Spotify"}
	b.addPlaylist("pl-top50", "Today's Top Hits", editorial, r.Perm(len(b.tracks))[:50])
	b.addPlaylist("pl-chill", "Chill Vibes", editorial, r.Perm(len(b.tracks))[:40])

	b.devices = slices.Clone(devices)
	// Recent plays over the last week, some songs on repeat, played from
	// playlists or their albums.
	favourites := r.Perm(len(b.tracks))[:4]
	played := time.Now().Add(-10 * time.Minute)
	for i := range 50 {
		t := b.tracks[r.IntN(len(b.tracks))]
		if i%3 == 0 {
			t = b.tracks[favourites[r.IntN(len(favourites))]]
		}
		t.PlayedAt, t.PlayedFrom = played, t.Album.URI
		if i%4 == 1 {
			t.PlayedFrom = b.playlists[r.IntN(len(b.playlists))].URI
		}
		b.recent = append(b.recent, t)
		played = played.Add(-time.Duration(20+r.IntN(300)) * time.Minute)
	}

	for i, name := range showNames {
		id := fmt.Sprintf("sh%d", i)
		sh := spotify.Show{
			ID: id, Name: name, URI: "spotify:show:" + id, Publisher: artistNames[i],
			Description: "Conversations about " + genres[i] + " and the people who make it.",
			Images:      coverImages(id),
		}
		n := 8 + r.IntN(20)
		sh.TotalEpisodes = n
		released := time.Now().AddDate(0, 0, -1-2*i)
		for k := range n {
			eid := fmt.Sprintf("ep%d_%d", i, k)
			b.episodes = append(b.episodes, spotify.Track{
				ID: eid, Name: fmt.Sprintf("#%d: %s", n-k, title()), URI: "spotify:episode:" + eid, Type: "episode",
				Show:       &spotify.Show{ID: sh.ID, Name: sh.Name, URI: sh.URI},
				DurationMS: (20 + r.IntN(70)) * 60 * 1000, ReleaseDate: released.AddDate(0, 0, -7*k).Format(time.DateOnly),
				Images: sh.Images,
			})
			switch k {
			case 1: // half-listened
				b.episodes[len(b.episodes)-1].ResumePoint = &spotify.ResumePoint{ResumePositionMS: (10 + r.IntN(15)) * 60 * 1000}
			case 2, 3:
				b.episodes[len(b.episodes)-1].ResumePoint = &spotify.ResumePoint{FullyPlayed: true}
			}
		}
		b.shows = append(b.shows, sh)
		if i < 3 {
			b.library[sh.URI] = true
		}
	}
	for _, i := range r.Perm(len(b.episodes))[:5] {
		b.library[b.episodes[i].URI] = true
	}
}

func (b *Backend) addPlaylist(id, name string, owner spotify.User, idx []int) {
	var tracks []spotify.Track
	for _, i := range idx {
		tracks = append(tracks, b.tracks[i])
	}
	b.playlists = append(b.playlists, spotify.Playlist{
		ID: id, Name: name, URI: "spotify:playlist:" + id, Owner: owner,
		Items: &spotify.Count{Total: len(tracks)}, Images: coverImages(id),
	})
	b.plTracks[id] = tracks
}

// wait simulates network latency and locks the backend. Callers must unlock.
func (b *Backend) wait(ctx context.Context) error {
	if b.Latency > 0 {
		select {
		case <-time.After(b.Latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	b.advance(time.Now())
	return nil
}

func page[T any](items []T, offset int) spotify.Page[T] {
	offset = min(max(offset, 0), len(items))
	end := min(offset+pageSize, len(items))
	p := spotify.Page[T]{Items: slices.Clone(items[offset:end]), Total: len(items), Limit: pageSize, Offset: offset}
	if end < len(items) {
		p.Next = "more"
	}
	return p
}

// --- Library ---

func (b *Backend) Me(ctx context.Context) (spotify.User, error) {
	if err := b.wait(ctx); err != nil {
		return spotify.User{}, err
	}
	defer b.mu.Unlock()
	return b.me, nil
}

func (b *Backend) Playlists(ctx context.Context, offset int) (spotify.Page[spotify.Playlist], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Playlist]{}, err
	}
	defer b.mu.Unlock()
	return page(b.playlists, offset), nil
}

func (b *Backend) SavedTracks(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Track]{}, err
	}
	defer b.mu.Unlock()
	return page(b.liked, offset), nil
}

func (b *Backend) SavedAlbums(ctx context.Context, offset int) (spotify.Page[spotify.Album], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Album]{}, err
	}
	defer b.mu.Unlock()
	return page(filter(b.albums, func(a spotify.Album) bool { return b.library[a.URI] }), offset), nil
}

func (b *Backend) FollowedArtists(ctx context.Context) ([]spotify.Artist, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	return filter(b.artists, func(a spotify.Artist) bool { return b.library[a.URI] }), nil
}

func (b *Backend) RecentlyPlayed(ctx context.Context) ([]spotify.Track, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	return slices.Clone(b.recent), nil
}

func (b *Backend) TopTracks(ctx context.Context) ([]spotify.Track, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	return slices.Clone(b.liked[:25]), nil
}

func (b *Backend) PlaylistTracks(ctx context.Context, id string, offset int) (spotify.Page[spotify.Track], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Track]{}, err
	}
	defer b.mu.Unlock()
	for _, pl := range b.playlists {
		if pl.ID == id && !pl.EditableBy(b.me.ID) {
			return spotify.Page[spotify.Track]{}, &spotify.Error{Status: http.StatusForbidden, Message: "Forbidden"}
		}
	}
	return page(b.plTracks[id], offset), nil
}

func (b *Backend) AlbumTracks(ctx context.Context, album spotify.Album, offset int) (spotify.Page[spotify.Track], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Track]{}, err
	}
	defer b.mu.Unlock()
	return page(b.albumTracks(album.URI), offset), nil
}

func (b *Backend) albumTracks(uri string) []spotify.Track {
	return filter(b.tracks, func(t spotify.Track) bool { return t.Album.URI == uri })
}

func (b *Backend) ArtistAlbums(ctx context.Context, id string, offset int) (spotify.Page[spotify.Album], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Album]{}, err
	}
	defer b.mu.Unlock()
	return page(b.artistAlbums(id), offset), nil
}

func (b *Backend) artistAlbums(id string) []spotify.Album {
	return filter(b.albums, func(a spotify.Album) bool { return a.Artists[0].ID == id })
}

func (b *Backend) SavedShows(ctx context.Context, offset int) (spotify.Page[spotify.Show], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Show]{}, err
	}
	defer b.mu.Unlock()
	return page(filter(b.shows, func(s spotify.Show) bool { return b.library[s.URI] }), offset), nil
}

func (b *Backend) SavedEpisodes(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Track]{}, err
	}
	defer b.mu.Unlock()
	return page(b.savedEpisodes(), offset), nil
}

func (b *Backend) savedEpisodes() []spotify.Track {
	return filter(b.episodes, func(t spotify.Track) bool { return b.library[t.URI] })
}

func (b *Backend) ShowEpisodes(ctx context.Context, show spotify.Show, offset int) (spotify.Page[spotify.Track], error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Page[spotify.Track]{}, err
	}
	defer b.mu.Unlock()
	return page(b.showEpisodes(show.URI), offset), nil
}

func (b *Backend) showEpisodes(uri string) []spotify.Track {
	return filter(b.episodes, func(t spotify.Track) bool { return t.Show.URI == uri })
}

func (b *Backend) Search(ctx context.Context, query string) (spotify.SearchResults, error) {
	if err := b.wait(ctx); err != nil {
		return spotify.SearchResults{}, err
	}
	defer b.mu.Unlock()
	q := strings.ToLower(query)
	has := func(s ...string) bool { return strings.Contains(strings.ToLower(strings.Join(s, " ")), q) }
	first10 := func(n int) int { return min(n, 10) }
	tracks := filter(b.tracks, func(t spotify.Track) bool { return has(t.Name, t.ArtistNames()) })
	albums := filter(b.albums, func(a spotify.Album) bool { return has(a.Name, spotify.JoinArtists(a.Artists)) })
	artists := filter(b.artists, func(a spotify.Artist) bool { return has(a.Name) })
	pls := filter(b.playlists, func(p spotify.Playlist) bool { return has(p.Name) })
	shows := filter(b.shows, func(s spotify.Show) bool { return has(s.Name, s.Publisher) })
	episodes := filter(b.episodes, func(t spotify.Track) bool { return has(t.Name) })
	return spotify.SearchResults{
		Tracks:    tracks[:first10(len(tracks))],
		Albums:    albums[:first10(len(albums))],
		Artists:   artists[:first10(len(artists))],
		Playlists: pls[:first10(len(pls))],
		Shows:     shows[:first10(len(shows))],
		Episodes:  episodes[:first10(len(episodes))],
	}, nil
}

func (b *Backend) InLibrary(ctx context.Context, uris []string) ([]bool, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	out := make([]bool, len(uris))
	for i, u := range uris {
		out[i] = b.library[u]
	}
	return out, nil
}

func (b *Backend) SaveToLibrary(ctx context.Context, uris []string) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	defer b.mu.Unlock()
	for _, u := range uris {
		b.library[u] = true
		if t, ok := b.findTrack(u); ok && !t.IsEpisode() && !slices.ContainsFunc(b.liked, func(l spotify.Track) bool { return l.URI == u }) {
			b.liked = append([]spotify.Track{t}, b.liked...)
		}
	}
	return nil
}

func (b *Backend) RemoveFromLibrary(ctx context.Context, uris []string) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	defer b.mu.Unlock()
	for _, u := range uris {
		delete(b.library, u)
		b.liked = slices.DeleteFunc(b.liked, func(t spotify.Track) bool { return t.URI == u })
	}
	return nil
}

func (b *Backend) AddToPlaylist(ctx context.Context, id string, uris []string) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	defer b.mu.Unlock()
	for i, pl := range b.playlists {
		if pl.ID != id {
			continue
		}
		for _, u := range uris {
			if t, ok := b.findTrack(u); ok {
				b.plTracks[id] = append(b.plTracks[id], t)
			}
		}
		b.playlists[i].Items = &spotify.Count{Total: len(b.plTracks[id])}
		return nil
	}
	return &spotify.Error{Status: http.StatusNotFound, Message: "playlist not found"}
}

func (b *Backend) findTrack(uri string) (spotify.Track, bool) {
	for _, list := range [][]spotify.Track{b.tracks, b.episodes} {
		if i := slices.IndexFunc(list, func(t spotify.Track) bool { return t.URI == uri }); i >= 0 {
			return list[i], true
		}
	}
	return spotify.Track{}, false
}

func filter[T any](items []T, keep func(T) bool) []T {
	var out []T
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}
