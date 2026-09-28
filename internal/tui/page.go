package tui

import (
	"context"
	"slices"
	"strings"

	"github.com/kyledickey/sptui/internal/spotify"
)

// rowKind is what a row in a list represents.
type rowKind int

const (
	kindHeader rowKind = iota // a non-selectable section title
	kindTrack
	kindAlbum
	kindArtist
	kindPlaylist
	kindShow
)

// row is one line in a list. Only the field matching kind is set.
type row struct {
	kind     rowKind
	header   string
	track    spotify.Track
	album    spotify.Album
	artist   spotify.Artist
	playlist spotify.Playlist
	show     spotify.Show
}

func trackRow(t spotify.Track) row       { return row{kind: kindTrack, track: t} }
func albumRow(a spotify.Album) row       { return row{kind: kindAlbum, album: a} }
func artistRow(a spotify.Artist) row     { return row{kind: kindArtist, artist: a} }
func playlistRow(p spotify.Playlist) row { return row{kind: kindPlaylist, playlist: p} }
func showRow(s spotify.Show) row         { return row{kind: kindShow, show: s} }
func headerRow(title string) row         { return row{kind: kindHeader, header: title} }

// uri returns the Spotify URI of the row's entity.
func (r row) uri() string {
	switch r.kind {
	case kindTrack:
		return r.track.URI
	case kindAlbum:
		return r.album.URI
	case kindArtist:
		return r.artist.URI
	case kindPlaylist:
		return r.playlist.URI
	case kindShow:
		return r.show.URI
	}
	return ""
}

// name returns the row's primary label.
func (r row) name() string {
	switch r.kind {
	case kindTrack:
		return r.track.Name
	case kindAlbum:
		return r.album.Name
	case kindArtist:
		return r.artist.Name
	case kindPlaylist:
		return r.playlist.Name
	case kindShow:
		return r.show.Name
	}
	return r.header
}

// cover returns the URL of the row's artwork, or "" if it has none.
func (r row) cover() string {
	var images []spotify.Image
	switch r.kind {
	case kindTrack:
		images = r.track.Cover()
	case kindAlbum:
		images = r.album.Images
	case kindArtist:
		images = r.artist.Images
	case kindPlaylist:
		images = r.playlist.Images
	case kindShow:
		images = r.show.Images
	}
	return spotify.CoverURL(images, coverSource)
}

// matches reports whether the row contains every word of filter.
func (r row) matches(filter string) bool {
	if r.kind == kindHeader {
		return false
	}
	var hay string
	switch r.kind {
	case kindTrack:
		hay = r.track.Name + " " + r.track.ArtistNames() + " " + r.track.Album.Name
	case kindAlbum:
		hay = r.album.Name + " " + spotify.JoinArtists(r.album.Artists)
	case kindArtist:
		hay = r.artist.Name + " " + strings.Join(r.artist.Genres, " ")
	case kindPlaylist:
		hay = r.playlist.Name + " " + r.playlist.Owner.Name()
	case kindShow:
		hay = r.show.Name + " " + r.show.Publisher
	}
	hay = strings.ToLower(hay)
	for word := range strings.FieldsSeq(strings.ToLower(filter)) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}

// chunk is one batch of rows from a loader. next is the offset to request
// the following batch from, or -1 when everything is loaded.
type chunk struct {
	rows        []row
	next        int
	total       int
	homeData    *homeData       // the home page's extras
	artistPlays *int            // an artist page's: plays of them among recent plays
	artist      *spotify.Artist // and the artist in full, when the page only had a name
}

// loadFunc fetches rows starting at offset.
type loadFunc func(ctx context.Context, offset int) (chunk, error)

// fromPage adapts a spotify.Page to a chunk.
func fromPage[T any](p spotify.Page[T], err error, toRow func(T) row) (chunk, error) {
	if err != nil {
		return chunk{}, err
	}
	c := chunk{rows: mapRows(p.Items, toRow), next: -1, total: p.Total}
	if p.HasMore() {
		c.next = p.Offset + max(p.Limit, len(p.Items))
	}
	return c, nil
}

// fromSlice turns a fully loaded slice into a chunk.
func fromSlice[T any](items []T, err error, toRow func(T) row) (chunk, error) {
	return fromPage(spotify.Page[T]{Items: items, Total: len(items)}, err, toRow)
}

// page is one screen in the main pane: a title and a list of rows, loaded
// lazily as the user scrolls.
type page struct {
	id       int
	title    string
	subtitle string
	kind     rowKind // column layout; kindHeader means mixed rows (search)
	context  string  // playback context URI for tracks, "" to play rows as a list
	empty    string  // shown when there are no rows

	load    loadFunc
	next    int // offset of the next chunk, -1 when done
	total   int
	loading bool
	err     error

	rows    []row
	visible []int // indexes into rows that pass the filter
	filter  string
	cursor  int // index into visible
	scroll  int // first visible line

	self       *row   // the album, artist or playlist this page shows, if any
	cover      string // cover image URL
	about      string // a line under the subtitle, e.g. a playlist description
	noAlbum    bool   // hide the album column (album pages)
	episodes   bool   // rows are podcast episodes: label columns for them
	kicker     string // a small heading over the title, e.g. "ALBUM · 2016"
	lengths    bool   // draw each song's length as a bar (album pages)
	grid       bool   // tiles in a grid, newest first (an artist's releases)
	strip      bool   // a strip of every song's cover color (playlists)
	home       bool   // the home page
	homeData   *homeData
	tabs       []searchTab // kinds of row to show, picked with 1-9
	tab        int         // which of tabs is showing; 0 shows all
	plays      int         // artist pages: plays among recent plays, -1 if unknown
	isSearch   bool
	query      string
	live       bool           // reload when the playing track changes (the queue)
	fresh      bool           // skip caches: the user asked for a reload
	origin     spotify.Origin // where the rows came from, for the cache marker
	nowPlaying bool           // the big now-playing view
	settings   bool           // the settings screen
	hits       settingsHits   // settings: what's where on screen, for clicks
}

func newPage(title string, kind rowKind, load loadFunc) *page {
	return &page{title: title, kind: kind, load: load, plays: -1}
}

// append adds a loaded chunk.
func (p *page) append(c chunk) {
	if c.homeData != nil {
		p.homeData = c.homeData
	}
	if c.artistPlays != nil {
		p.plays = *c.artistPlays
	}
	if a := c.artist; a != nil && p.self != nil {
		p.self.artist = *a
		p.cover = spotify.CoverURL(a.Images, coverSource)
		p.about = strings.Join(a.Genres, ", ")
	}
	p.rows = append(p.rows, c.rows...)
	if p.grid {
		// Newest first. Spotify lists albums, then singles, a page at a time.
		slices.SortStableFunc(p.rows, func(a, b row) int { return strings.Compare(b.album.ReleaseDate, a.album.ReleaseDate) })
	}
	p.next = c.next
	p.total = max(c.total, len(p.rows))
	p.refilter()
}

// remove drops rows with the given URI.
func (p *page) remove(uri string) {
	before := len(p.rows)
	p.rows = slices.DeleteFunc(p.rows, func(r row) bool { return r.uri() == uri })
	removed := before - len(p.rows)
	p.total -= removed
	if p.next > 0 {
		p.next = max(0, p.next-removed) // the server's list shifted too
	}
	p.refilter()
}

// noteOrigin folds in where a chunk came from: a page is as old as its
// oldest chunk, and stale if any chunk is.
func (p *page) noteOrigin(o spotify.Origin) {
	p.origin = mergeOrigin(p.origin, o)
}

func mergeOrigin(a, b spotify.Origin) spotify.Origin {
	if !b.CachedAt.IsZero() && (a.CachedAt.IsZero() || b.CachedAt.Before(a.CachedAt)) {
		a.CachedAt = b.CachedAt
	}
	a.Stale = a.Stale || b.Stale
	return a
}

// reset clears loaded rows so the page can be loaded again.
func (p *page) reset() {
	p.origin = spotify.Origin{}
	p.rows, p.visible = nil, nil
	p.next, p.total, p.err = 0, 0, nil
	p.cursor, p.scroll = 0, 0
}

// setFilter changes the filter and recomputes visible rows.
func (p *page) setFilter(f string) {
	p.filter = f
	p.cursor, p.scroll = 0, 0
	p.refilter()
}

func (p *page) refilter() {
	p.visible = p.visible[:0]
	for i, r := range p.rows {
		if (p.filter == "" || r.matches(p.filter)) && (p.tab == 0 || p.tabs[p.tab].shows(r)) {
			p.visible = append(p.visible, i)
		}
	}
	p.cursor = p.selectable(p.cursor, 1)
}

// needsMore reports whether another chunk should be fetched: the user is
// close to the end of what's loaded, or a filter wants to see everything.
func (p *page) needsMore() bool {
	if p.load == nil || p.loading || p.next < 0 || p.err != nil {
		return false
	}
	return p.filter != "" || len(p.rows) == 0 || p.cursor >= len(p.visible)-20
}

// selected returns the row under the cursor.
func (p *page) selected() (row, bool) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return row{}, false
	}
	r := p.rows[p.visible[p.cursor]]
	return r, r.kind != kindHeader
}

// move shifts the cursor by delta rows, skipping section headers.
func (p *page) move(delta int) {
	dir := 1
	if delta < 0 {
		dir = -1
	}
	p.cursor = p.selectable(p.cursor+delta, dir)
}

// selectable returns the nearest non-header row to start, preferring
// direction dir.
func (p *page) selectable(start, dir int) int {
	if len(p.visible) == 0 {
		return 0
	}
	start = min(max(start, 0), len(p.visible)-1)
	for _, step := range []int{dir, -dir} {
		for i := start; i >= 0 && i < len(p.visible); i += step {
			if p.rows[p.visible[i]].kind != kindHeader {
				return i
			}
		}
	}
	return start
}

// scrollTo keeps the cursor inside a viewport of height lines.
func (p *page) scrollTo(height int) {
	if height > 0 {
		p.scroll = keepInView(p.scroll, p.cursor, height, len(p.visible))
	}
}

// keepInView returns the first of n lines to show in a viewport of height
// lines so that cursor is in it, moving on from scroll as little as needed.
func keepInView(scroll, cursor, height, n int) int {
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+height {
		scroll = cursor - height + 1
	}
	return max(0, min(scroll, n-height))
}

// tracks returns every track row and the index of target among them.
func (p *page) tracks(target string) (uris []string, index int) {
	index = -1
	for _, r := range p.rows {
		if r.kind != kindTrack || r.track.IsLocal {
			continue
		}
		if r.track.URI == target && index < 0 {
			index = len(uris)
		}
		uris = append(uris, r.track.URI)
	}
	return uris, max(index, 0)
}
