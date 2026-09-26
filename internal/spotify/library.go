package spotify

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Per-endpoint page size limits set by Spotify.
const (
	pageLimit         = 50
	artistAlbumsLimit = 10
	searchLimit       = 10
	libraryURIsLimit  = 40
)

// Wrappers Spotify puts around items in some collections.
type (
	savedTrack struct {
		Track *Track `json:"track"`
	}
	savedAlbum struct {
		Album *Album `json:"album"`
	}
	playlistItem struct {
		Item *Track `json:"item"`
	}
	playHistory struct {
		Track    *Track           `json:"track"`
		PlayedAt time.Time        `json:"played_at"`
		Context  *PlaybackContext `json:"context"`
	}
	savedShow struct {
		Show *Show `json:"show"`
	}
	savedEpisode struct {
		Episode *Track `json:"episode"`
	}
)

// Me returns the current user.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	err := c.get(ctx, "/me", nil, &u)
	return u, err
}

// Playlists returns a page of the current user's playlists.
func (c *Client) Playlists(ctx context.Context, offset int) (Page[Playlist], error) {
	var page Page[*Playlist]
	if err := c.get(ctx, "/me/playlists", pageQuery(offset, pageLimit), &page); err != nil {
		return Page[Playlist]{}, err
	}
	return derefPage(page), nil
}

// SavedTracks returns a page of the user's liked songs.
func (c *Client) SavedTracks(ctx context.Context, offset int) (Page[Track], error) {
	var page Page[savedTrack]
	err := c.get(ctx, "/me/tracks", pageQuery(offset, pageLimit), &page)
	return mapPage(page, func(it savedTrack) *Track { return it.Track }), err
}

// SavedAlbums returns a page of the user's saved albums.
func (c *Client) SavedAlbums(ctx context.Context, offset int) (Page[Album], error) {
	var page Page[savedAlbum]
	err := c.get(ctx, "/me/albums", pageQuery(offset, pageLimit), &page)
	return mapPage(page, func(it savedAlbum) *Album {
		if it.Album != nil {
			it.Album.Tracks = nil // not needed and can be large
		}
		return it.Album
	}), err
}

// FollowedArtists returns every artist the user follows.
func (c *Client) FollowedArtists(ctx context.Context) ([]Artist, error) {
	var all []Artist
	q := url.Values{"type": {"artist"}, "limit": {strconv.Itoa(pageLimit)}}
	for {
		var resp struct {
			Artists struct {
				Items   []Artist `json:"items"`
				Next    string   `json:"next"`
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
			} `json:"artists"`
		}
		if err := c.get(ctx, "/me/following", q, &resp); err != nil {
			return all, err
		}
		all = append(all, resp.Artists.Items...)
		if resp.Artists.Next == "" || resp.Artists.Cursors.After == "" {
			return all, nil
		}
		q.Set("after", resp.Artists.Cursors.After)
	}
}

// RecentlyPlayed returns the user's last 50 played tracks, newest first,
// with PlayedAt and PlayedFrom set.
func (c *Client) RecentlyPlayed(ctx context.Context) ([]Track, error) {
	var page Page[playHistory]
	if err := c.get(ctx, "/me/player/recently-played", url.Values{"limit": {strconv.Itoa(pageLimit)}}, &page); err != nil {
		return nil, err
	}
	return mapPage(page, func(it playHistory) *Track {
		if it.Track != nil {
			it.Track.PlayedAt = it.PlayedAt
			if it.Context != nil {
				it.Track.PlayedFrom = it.Context.URI
			}
		}
		return it.Track
	}).Items, nil
}

// TopTracks returns the user's most played tracks over the last ~6 months.
func (c *Client) TopTracks(ctx context.Context) ([]Track, error) {
	var page Page[*Track]
	q := url.Values{"limit": {strconv.Itoa(pageLimit)}, "time_range": {"medium_term"}}
	if err := c.get(ctx, "/me/top/tracks", q, &page); err != nil {
		return nil, err
	}
	return derefPage(page).Items, nil
}

// PlaylistTracks returns a page of a playlist's tracks. Spotify only allows
// this for playlists the user owns or collaborates on; others return 403.
func (c *Client) PlaylistTracks(ctx context.Context, playlistID string, offset int) (Page[Track], error) {
	var page Page[playlistItem]
	q := pageQuery(offset, pageLimit)
	q.Set("additional_types", "track,episode")
	err := c.get(ctx, "/playlists/"+playlistID+"/items", q, &page)
	return mapPage(page, func(it playlistItem) *Track { return it.Item }), err
}

// AlbumTracks returns a page of an album's tracks.
func (c *Client) AlbumTracks(ctx context.Context, album Album, offset int) (Page[Track], error) {
	var page Page[Track]
	err := c.get(ctx, "/albums/"+album.ID+"/tracks", pageQuery(offset, pageLimit), &page)
	fillAlbum(page.Items, album)
	return page, err
}

// fillAlbum sets the album on simplified track objects, which omit it.
func fillAlbum(tracks []Track, album Album) {
	album.Tracks = nil
	for i := range tracks {
		tracks[i].Album = album
	}
}

// ArtistAlbums returns a page of an artist's albums and singles.
func (c *Client) ArtistAlbums(ctx context.Context, artistID string, offset int) (Page[Album], error) {
	var page Page[*Album]
	q := pageQuery(offset, artistAlbumsLimit)
	q.Set("include_groups", "album,single,compilation")
	if err := c.get(ctx, "/artists/"+artistID+"/albums", q, &page); err != nil {
		return Page[Album]{}, err
	}
	return derefPage(page), nil
}

// SavedShows returns a page of the podcasts the user follows.
func (c *Client) SavedShows(ctx context.Context, offset int) (Page[Show], error) {
	var page Page[savedShow]
	err := c.get(ctx, "/me/shows", pageQuery(offset, pageLimit), &page)
	return mapPage(page, func(it savedShow) *Show { return it.Show }), err
}

// SavedEpisodes returns a page of the user's saved podcast episodes.
func (c *Client) SavedEpisodes(ctx context.Context, offset int) (Page[Track], error) {
	var page Page[savedEpisode]
	err := c.get(ctx, "/me/episodes", pageQuery(offset, pageLimit), &page)
	return mapPage(page, func(it savedEpisode) *Track { return it.Episode }), err
}

// ShowEpisodes returns a page of a podcast's episodes, newest first.
func (c *Client) ShowEpisodes(ctx context.Context, show Show, offset int) (Page[Track], error) {
	var page Page[*Track]
	if err := c.get(ctx, "/shows/"+show.ID+"/episodes", pageQuery(offset, pageLimit), &page); err != nil {
		return Page[Track]{}, err
	}
	out := derefPage(page)
	fillShow(out.Items, show)
	return out, nil
}

// fillShow sets the show on simplified episode objects, which omit it.
func fillShow(episodes []Track, show Show) {
	for i := range episodes {
		episodes[i].Show = &Show{ID: show.ID, Name: show.Name, URI: show.URI}
	}
}

// Search looks for tracks, albums, artists, playlists, podcasts and
// episodes matching query.
func (c *Client) Search(ctx context.Context, query string) (SearchResults, error) {
	var resp struct {
		Tracks    Page[*Track]    `json:"tracks"`
		Albums    Page[*Album]    `json:"albums"`
		Artists   Page[*Artist]   `json:"artists"`
		Playlists Page[*Playlist] `json:"playlists"`
		Shows     Page[*Show]     `json:"shows"`
		Episodes  Page[*Track]    `json:"episodes"`
	}
	q := url.Values{
		"q":     {query},
		"type":  {"track,album,artist,playlist,show,episode"},
		"limit": {strconv.Itoa(searchLimit)},
	}
	if err := c.get(ctx, "/search", q, &resp); err != nil {
		return SearchResults{}, err
	}
	return SearchResults{
		Tracks:    derefPage(resp.Tracks).Items,
		Albums:    derefPage(resp.Albums).Items,
		Artists:   derefPage(resp.Artists).Items,
		Playlists: derefPage(resp.Playlists).Items,
		Shows:     derefPage(resp.Shows).Items,
		Episodes:  derefPage(resp.Episodes).Items,
	}, nil
}

// InLibrary reports, for each URI, whether it is saved in the user's library.
func (c *Client) InLibrary(ctx context.Context, uris []string) ([]bool, error) {
	var out []bool
	for chunk := range slices.Chunk(uris, libraryURIsLimit) {
		var saved []bool
		if err := c.get(ctx, "/me/library/contains", url.Values{"uris": {strings.Join(chunk, ",")}}, &saved); err != nil {
			return nil, err
		}
		out = append(out, saved...)
	}
	return out, nil
}

// SaveToLibrary saves tracks, albums or episodes, or follows artists,
// playlists or podcasts.
func (c *Client) SaveToLibrary(ctx context.Context, uris []string) error {
	return c.libraryEdit(ctx, "PUT", uris)
}

// RemoveFromLibrary is the inverse of SaveToLibrary.
func (c *Client) RemoveFromLibrary(ctx context.Context, uris []string) error {
	return c.libraryEdit(ctx, "DELETE", uris)
}

func (c *Client) libraryEdit(ctx context.Context, method string, uris []string) error {
	for chunk := range slices.Chunk(uris, libraryURIsLimit) {
		if err := c.do(ctx, method, "/me/library", url.Values{"uris": {strings.Join(chunk, ",")}}, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// AddToPlaylist appends tracks to a playlist.
func (c *Client) AddToPlaylist(ctx context.Context, playlistID string, uris []string) error {
	return c.do(ctx, "POST", "/playlists/"+playlistID+"/items", nil, map[string]any{"uris": uris}, nil)
}

// derefPage drops nil entries, which Spotify sometimes returns for
// unavailable items.
func derefPage[T any](p Page[*T]) Page[T] {
	return mapPage(p, func(t *T) *T { return t })
}

func mapPage[In, Out any](p Page[In], fn func(In) *Out) Page[Out] {
	out := Page[Out]{Total: p.Total, Limit: p.Limit, Offset: p.Offset, Next: p.Next}
	out.Items = make([]Out, 0, len(p.Items))
	for _, it := range p.Items {
		if v := fn(it); v != nil {
			out.Items = append(out.Items, *v)
		}
	}
	return out
}
