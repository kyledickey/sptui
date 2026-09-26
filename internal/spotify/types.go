package spotify

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// Image is cover art or an artist photo.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// User is a Spotify account.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	URI         string `json:"uri"`
}

// Name returns the display name, falling back to the user ID.
func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.ID
}

// Artist is a Spotify artist. Simplified artist objects only carry ID, Name and URI.
type Artist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	URI    string   `json:"uri"`
	Genres []string `json:"genres"`
	Images []Image  `json:"images"`
}

// Album is a Spotify album. Tracks is only set when fetching a single album.
type Album struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	URI         string       `json:"uri"`
	AlbumType   string       `json:"album_type"`
	Artists     []Artist     `json:"artists"`
	Images      []Image      `json:"images"`
	ReleaseDate string       `json:"release_date"`
	TotalTracks int          `json:"total_tracks"`
	Tracks      *Page[Track] `json:"tracks,omitempty"`
}

// Year returns the release year, or "" if unknown. It copes with the Web
// API's "2002-11-11" and the speaker's "year:2002 month:11 day:11".
func (a Album) Year() string {
	return yearIn.FindString(a.ReleaseDate)
}

var yearIn = regexp.MustCompile(`\b(1[89]|2[0-9])[0-9]{2}\b`)

// Show is a podcast. Episodes carry a simplified one with only ID, Name
// and URI.
type Show struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	URI           string  `json:"uri"`
	Publisher     string  `json:"publisher"`
	Description   string  `json:"description"`
	Images        []Image `json:"images"`
	TotalEpisodes int     `json:"total_episodes"`
}

// Track is a playable item. Episodes decode into Track too; they carry Show
// instead of Album and Artists.
type Track struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URI         string   `json:"uri"`
	Type        string   `json:"type"`
	Artists     []Artist `json:"artists"`
	Album       Album    `json:"album"`
	Show        *Show    `json:"show,omitempty"`
	DurationMS  int      `json:"duration_ms"`
	TrackNumber int      `json:"track_number"`
	Explicit    bool     `json:"explicit"`
	IsLocal     bool     `json:"is_local"`
	ReleaseDate string   `json:"release_date"` // episodes only
	Images      []Image  `json:"images"`       // episodes only
}

// IsEpisode reports whether t is a podcast episode rather than a song.
func (t Track) IsEpisode() bool {
	return t.Type == "episode" || strings.HasPrefix(t.URI, "spotify:episode:")
}

// Cover returns the track's artwork: the album cover, or for an episode
// its own image or its show's.
func (t Track) Cover() []Image {
	switch {
	case len(t.Album.Images) > 0:
		return t.Album.Images
	case len(t.Images) > 0:
		return t.Images
	case t.Show != nil:
		return t.Show.Images
	}
	return nil
}

// Duration returns the track length.
func (t Track) Duration() time.Duration {
	return time.Duration(t.DurationMS) * time.Millisecond
}

// ArtistNames joins artist names, or returns the show name for episodes.
func (t Track) ArtistNames() string {
	if len(t.Artists) == 0 && t.Show != nil {
		return t.Show.Name
	}
	return JoinArtists(t.Artists)
}

// JoinArtists returns a comma separated list of artist names.
func JoinArtists(artists []Artist) string {
	names := make([]string, len(artists))
	for i, a := range artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// Playlist is a Spotify playlist.
type Playlist struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	URI           string  `json:"uri"`
	Description   string  `json:"description"`
	Images        []Image `json:"images"`
	Owner         User    `json:"owner"`
	Collaborative bool    `json:"collaborative"`
	Items         *Count  `json:"items,omitempty"`
	Tracks        *Count  `json:"tracks,omitempty"` // deprecated by Spotify in favour of Items
}

// Count is a reference to a collection that only carries its size.
type Count struct {
	Total int `json:"total"`
}

// TrackCount returns the number of items in the playlist.
func (p Playlist) TrackCount() int {
	switch {
	case p.Items != nil:
		return p.Items.Total
	case p.Tracks != nil:
		return p.Tracks.Total
	}
	return 0
}

// EditableBy reports whether the user can add items to the playlist.
func (p Playlist) EditableBy(userID string) bool {
	return p.Collaborative || p.Owner.ID == userID
}

// Page is one page of an offset-paginated collection.
type Page[T any] struct {
	Items  []T    `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Next   string `json:"next"`
}

// HasMore reports whether there are more items after this page.
func (p Page[T]) HasMore() bool { return p.Next != "" }

// Device is a Spotify Connect device.
type Device struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	IsActive       bool   `json:"is_active"`
	IsRestricted   bool   `json:"is_restricted"`
	VolumePercent  *int   `json:"volume_percent"`
	SupportsVolume bool   `json:"supports_volume"`
}

// Repeat modes accepted by the player.
const (
	RepeatOff     = "off"
	RepeatContext = "context"
	RepeatTrack   = "track"
)

// PlaybackContext is what the current track is playing from.
type PlaybackContext struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

// PlaybackState is the state of the user's player.
type PlaybackState struct {
	Device       Device           `json:"device"`
	Context      *PlaybackContext `json:"context"`
	Item         *Track           `json:"item"`
	ProgressMS   int              `json:"progress_ms"`
	IsPlaying    bool             `json:"is_playing"`
	ShuffleState bool             `json:"shuffle_state"`
	RepeatState  string           `json:"repeat_state"`
}

// Queue is the user's upcoming tracks.
type Queue struct {
	CurrentlyPlaying *Track  `json:"currently_playing"`
	Queue            []Track `json:"queue"`
}

// SearchResults holds one page of results per type.
type SearchResults struct {
	Tracks    []Track
	Albums    []Album
	Artists   []Artist
	Playlists []Playlist
	Shows     []Show
	Episodes  []Track
}

// PlayOptions describes what to start playing. Set either ContextURI (an
// album, playlist or artist) or URIs (a list of tracks). OffsetURI or
// OffsetIndex selects where in the context to start.
type PlayOptions struct {
	DeviceID    string
	ContextURI  string
	URIs        []string
	OffsetURI   string
	OffsetIndex int
}

// CoverURL picks the smallest image at least minSize pixels wide, or the
// largest one if none is big enough. Images with unknown sizes (0) count as
// big enough. It returns "" when there are no images.
func CoverURL(images []Image, minSize int) string {
	best := -1
	for i, img := range images {
		fits := img.Width == 0 || img.Width >= minSize
		switch {
		case best < 0:
			best = i
		case fits && (images[best].Width < minSize || img.Width < images[best].Width):
			best = i
		case !fits && images[best].Width < minSize && img.Width > images[best].Width:
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return images[best].URL
}

// ID returns the ID part of a Spotify URI such as spotify:track:abc.
func ID(uri string) string {
	if i := strings.LastIndexByte(uri, ':'); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// LikedSongsURI is the playback context for a user's saved tracks.
func LikedSongsURI(userID string) string {
	return "spotify:user:" + userID + ":collection"
}

// YourEpisodesURI is the playback context for a user's saved episodes.
func YourEpisodesURI(userID string) string {
	return LikedSongsURI(userID) + ":your-episodes"
}

type freshKey struct{}

// WithFresh marks ctx as wanting up-to-date data, skipping any cache in
// front of the client (see package cache). The UI's reload uses it.
func WithFresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshKey{}, true)
}

// WantsFresh reports whether ctx was marked by WithFresh.
func WantsFresh(ctx context.Context) bool {
	fresh, _ := ctx.Value(freshKey{}).(bool)
	return fresh
}

// Origin says where an answer came from. The UI passes one along with a
// request (WithOrigin); a cache in front of the client fills it in.
type Origin struct {
	CachedAt time.Time // when the answer was saved; zero if it's live
	Stale    bool      // a saved copy, served because Spotify refused
}

type originKey struct{}

// WithOrigin returns a context that records in o where answers come from.
func WithOrigin(ctx context.Context, o *Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// NoteOrigin records where an answer came from, if the caller asked.
func NoteOrigin(ctx context.Context, cachedAt time.Time, stale bool) {
	if o, ok := ctx.Value(originKey{}).(*Origin); ok {
		o.CachedAt, o.Stale = cachedAt, stale
	}
}
