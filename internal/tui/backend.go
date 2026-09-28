package tui

import (
	"context"
	"image"

	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/spotify"
)

// Backend is everything the UI needs from Spotify. *spotify.Client
// implements it against the real Web API; demo.Backend fakes it offline.
// The two halves can come from different places: sptui's built-in speaker
// is a Player that never touches the Web API.
type Backend interface {
	Library
	Player
	LyricsSource
}

// LyricsSource finds lyrics for a track (see package lyrics).
type LyricsSource interface {
	Lyrics(ctx context.Context, t spotify.Track) (lyrics.Lyrics, error)
}

// Library reads and edits the user's music.
type Library interface {
	Me(ctx context.Context) (spotify.User, error)
	Playlists(ctx context.Context, offset int) (spotify.Page[spotify.Playlist], error)
	SavedTracks(ctx context.Context, offset int) (spotify.Page[spotify.Track], error)
	SavedAlbums(ctx context.Context, offset int) (spotify.Page[spotify.Album], error)
	FollowedArtists(ctx context.Context) ([]spotify.Artist, error)
	RecentlyPlayed(ctx context.Context) ([]spotify.Track, error)
	TopTracks(ctx context.Context) ([]spotify.Track, error)
	PlaylistTracks(ctx context.Context, playlistID string, offset int) (spotify.Page[spotify.Track], error)
	AlbumTracks(ctx context.Context, album spotify.Album, offset int) (spotify.Page[spotify.Track], error)
	Artist(ctx context.Context, id string) (spotify.Artist, error)
	ArtistAlbums(ctx context.Context, artistID string, offset int) (spotify.Page[spotify.Album], error)
	SavedShows(ctx context.Context, offset int) (spotify.Page[spotify.Show], error)
	SavedEpisodes(ctx context.Context, offset int) (spotify.Page[spotify.Track], error)
	ShowEpisodes(ctx context.Context, show spotify.Show, offset int) (spotify.Page[spotify.Track], error)
	Search(ctx context.Context, query string) (spotify.SearchResults, error)
	InLibrary(ctx context.Context, uris []string) ([]bool, error)
	SaveToLibrary(ctx context.Context, uris []string) error
	RemoveFromLibrary(ctx context.Context, uris []string) error
	CreatePlaylist(ctx context.Context, name string) (spotify.Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, uris []string) error
	EditPlaylist(ctx context.Context, playlistID string, changes spotify.PlaylistChanges) error
	DeletePlaylist(ctx context.Context, playlistID string) error
	CoverArt(ctx context.Context, url string) (image.Image, error)
	Queue(ctx context.Context) (spotify.Queue, error)
}

// Player controls playback on Spotify Connect devices.
type Player interface {
	Playback(ctx context.Context) (*spotify.PlaybackState, error)
	Devices(ctx context.Context) ([]spotify.Device, error)
	Play(ctx context.Context, opts spotify.PlayOptions) error
	Pause(ctx context.Context) error
	Next(ctx context.Context) error
	Previous(ctx context.Context) error
	Seek(ctx context.Context, positionMS int) error
	SetVolume(ctx context.Context, percent int) error
	SetShuffle(ctx context.Context, on bool) error
	SetRepeat(ctx context.Context, mode string) error
	Transfer(ctx context.Context, deviceID string, play bool) error
	AddToQueue(ctx context.Context, uri string) error
}

// The real client provides both halves.
var (
	_ Library = (*spotify.Client)(nil)
	_ Player  = (*spotify.Client)(nil)
)
