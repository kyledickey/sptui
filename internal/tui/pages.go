package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/kyledickey/sptui/internal/spotify"
)

// Page constructors. Each returns a page wired to load its rows from b.

func likedPage(b Backend, me spotify.User) *page {
	p := newPage("Liked Songs", kindTrack, func(ctx context.Context, off int) (chunk, error) {
		pg, err := b.SavedTracks(ctx, off)
		return fromPage(pg, err, trackRow)
	})
	p.context = spotify.LikedSongsURI(me.ID)
	p.subtitle = "Songs you've liked"
	p.empty = "No liked songs yet. Press l on any track to like it."
	return p
}

func recentPage(b Backend) *page {
	p := newPage("Recently Played", kindTrack, func(ctx context.Context, _ int) (chunk, error) {
		tracks, err := b.RecentlyPlayed(ctx)
		return fromSlice(tracks, err, trackRow)
	})
	p.subtitle = "Your last 50 plays"
	return p
}

func topPage(b Backend) *page {
	p := newPage("Top Tracks", kindTrack, func(ctx context.Context, _ int) (chunk, error) {
		tracks, err := b.TopTracks(ctx)
		return fromSlice(tracks, err, trackRow)
	})
	p.subtitle = "Your most played, last 6 months"
	return p
}

func albumsPage(b Backend) *page {
	p := newPage("Albums", kindAlbum, func(ctx context.Context, off int) (chunk, error) {
		pg, err := b.SavedAlbums(ctx, off)
		return fromPage(pg, err, albumRow)
	})
	p.empty = "No saved albums. Press l on an album to save it."
	return p
}

func artistsPage(b Backend) *page {
	p := newPage("Artists", kindArtist, func(ctx context.Context, _ int) (chunk, error) {
		artists, err := b.FollowedArtists(ctx)
		return fromSlice(artists, err, artistRow)
	})
	p.empty = "You don't follow any artists yet. Press l on an artist to follow."
	return p
}

// nowPlayingPage is the big now-playing view. Its rows are the queue.
func nowPlayingPage(b Backend) *page {
	p := newPage("Now Playing", kindTrack, func(ctx context.Context, _ int) (chunk, error) {
		q, err := b.Queue(ctx)
		return fromSlice(q.Queue, err, trackRow)
	})
	p.empty = "Nothing queued. Press a on a song to add it."
	p.live = true
	p.nowPlaying = true
	return p
}

func playlistPage(b Backend, pl spotify.Playlist) *page {
	p := newPage(pl.Name, kindTrack, func(ctx context.Context, off int) (chunk, error) {
		pg, err := b.PlaylistTracks(ctx, pl.ID, off)
		return fromPage(pg, err, trackRow)
	})
	p.context = pl.URI
	p.self = ptr(playlistRow(pl))
	p.cover = spotify.CoverURL(pl.Images, coverSource)
	p.about = cleanDescription(pl.Description)
	p.subtitle = fmt.Sprintf("Playlist · %s", pl.Owner.Name())
	p.empty = "This playlist is empty."
	return p
}

func albumPage(b Backend, al spotify.Album) *page {
	p := newPage(al.Name, kindTrack, func(ctx context.Context, off int) (chunk, error) {
		pg, err := b.AlbumTracks(ctx, al, off)
		return fromPage(pg, err, trackRow)
	})
	p.context = al.URI
	p.self = ptr(albumRow(al))
	p.cover = spotify.CoverURL(al.Images, coverSource)
	p.noAlbum = true
	p.subtitle = joinNonEmpty(" · ", "Album", spotify.JoinArtists(al.Artists), al.Year())
	return p
}

func artistPage(b Backend, ar spotify.Artist) *page {
	p := newPage(ar.Name, kindAlbum, func(ctx context.Context, off int) (chunk, error) {
		pg, err := b.ArtistAlbums(ctx, ar.ID, off)
		return fromPage(pg, err, albumRow)
	})
	p.context = ar.URI
	p.self = ptr(artistRow(ar))
	p.cover = spotify.CoverURL(ar.Images, coverSource)
	p.about = strings.Join(ar.Genres, ", ")
	p.subtitle = "Artist · albums and singles"
	return p
}

func searchPage() *page {
	p := newPage("Search", kindHeader, nil)
	p.isSearch = true
	p.next = -1
	p.empty = "Type to search songs, artists, albums and playlists."
	return p
}

// myPlaylists finds the user's playlists (from the sidebar) matching query,
// as a search section.
func (m *Model) myPlaylists(query string) []row {
	var rows []row
	for _, pl := range m.sidebar.playlists() {
		if r := playlistRow(pl); r.matches(query) {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return append([]row{headerRow("Your playlists")}, rows...)
}

// searchRows lays out search results as sections.
func searchRows(res spotify.SearchResults) []row {
	var rows []row
	section := func(title string, items []row) {
		if len(items) > 0 {
			rows = append(append(rows, headerRow(title)), items...)
		}
	}
	section("Songs", mapRows(res.Tracks, trackRow))
	section("Artists", mapRows(res.Artists, artistRow))
	section("Albums", mapRows(res.Albums, albumRow))
	section("Playlists", mapRows(res.Playlists, playlistRow))
	return rows
}

func ptr[T any](v T) *T { return &v }

func mapRows[T any](items []T, toRow func(T) row) []row {
	rows := make([]row, len(items))
	for i, it := range items {
		rows[i] = toRow(it)
	}
	return rows
}

// openRow returns the page to show when a non-track row is opened.
func openRow(b Backend, r row) *page {
	switch r.kind {
	case kindAlbum:
		return albumPage(b, r.album)
	case kindArtist:
		return artistPage(b, r.artist)
	case kindPlaylist:
		return playlistPage(b, r.playlist)
	}
	return nil
}
