package speaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devgianlu/go-librespot/daemon"

	"github.com/kyledickey/sptui/internal/spotify"
)

// The Speaker is also a player: these methods control it directly, inside
// the process, instead of through Spotify's Web API. That makes playback
// instant and immune to Web API rate limits.

// handoff bounds how long a request waits for the player to pick it up. The
// player only listens once it has logged in. A variable for tests.
var handoff = 2 * time.Second

// errNotReady means the speaker hasn't finished logging in.
var errNotReady = errors.New("sptui's speaker is still connecting to Spotify")

// localServer is go-librespot's API server, minus the HTTP: requests are
// handed straight to the player.
type localServer struct {
	requests chan daemon.ApiRequest
}

func newLocalServer() *localServer {
	return &localServer{requests: make(chan daemon.ApiRequest)}
}

func (s *localServer) Emit(*daemon.ApiEvent)             {} // the UI polls status instead
func (s *localServer) Receive() <-chan daemon.ApiRequest { return s.requests }
func (s *localServer) SetAuthCode(*daemon.ApiDeviceAuth) {}
func (s *localServer) Close() error                      { return nil }

// request sends one request to the player and waits for its reply.
func (s *Speaker) request(ctx context.Context, typ daemon.ApiRequestType, data any) (any, error) {
	req, wait := daemon.NewApiRequest(typ, data)
	select {
	case s.server.requests <- req:
	case <-s.done:
		return nil, errors.New("sptui's speaker has stopped")
	case <-time.After(handoff):
		return nil, errNotReady
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp, err := wait(ctx)
	if errors.Is(err, daemon.ErrNoSession) {
		return nil, errNotReady
	}
	return resp, err
}

func (s *Speaker) status(ctx context.Context) (*daemon.ApiStatus, error) {
	resp, err := s.request(ctx, daemon.ApiRequestTypeStatus, nil)
	if err != nil {
		return nil, err
	}
	st, ok := resp.(*daemon.ApiStatus)
	if !ok {
		return nil, fmt.Errorf("unexpected status reply %T", resp)
	}
	return st, nil
}

// Playback reports what the speaker is playing, or nil if nothing.
func (s *Speaker) Playback(ctx context.Context) (*spotify.PlaybackState, error) {
	st, err := s.status(ctx)
	if errors.Is(err, errNotReady) {
		return nil, nil
	}
	if err != nil || st.Track == nil {
		return nil, err
	}
	t := st.Track
	track := &spotify.Track{
		Name:       t.Name,
		URI:        t.Uri,
		DurationMS: t.Duration,
		Album:      spotify.Album{Name: t.AlbumName, ReleaseDate: t.ReleaseDate},
	}
	if strings.HasPrefix(t.Uri, "spotify:episode:") {
		// The player reports an episode's show as both artist and album.
		track.Type = "episode"
		track.Show = &spotify.Show{Name: t.AlbumName}
		track.Album = spotify.Album{}
	} else {
		for _, name := range t.ArtistNames {
			track.Artists = append(track.Artists, spotify.Artist{Name: name})
		}
	}
	if t.AlbumCoverUrl != nil {
		track.Album.Images = []spotify.Image{{URL: *t.AlbumCoverUrl}}
	}
	repeat := spotify.RepeatOff
	switch {
	case st.RepeatTrack:
		repeat = spotify.RepeatTrack
	case st.RepeatContext:
		repeat = spotify.RepeatContext
	}
	return &spotify.PlaybackState{
		Device:       device(st),
		Item:         track,
		ProgressMS:   int(t.Position),
		IsPlaying:    !st.Paused && !st.Stopped,
		ShuffleState: st.ShuffleContext,
		RepeatState:  repeat,
	}, nil
}

func device(st *daemon.ApiStatus) spotify.Device {
	volume := int(st.Volume)
	if st.VolumeSteps > 0 {
		volume = int(st.Volume * 100 / st.VolumeSteps)
	}
	return spotify.Device{
		ID:             st.DeviceId,
		Name:           st.DeviceName,
		Type:           "Computer",
		IsActive:       true,
		SupportsVolume: true,
		VolumePercent:  &volume,
	}
}

// Devices lists the speaker itself, once it's connected.
func (s *Speaker) Devices(ctx context.Context) ([]spotify.Device, error) {
	st, err := s.status(ctx)
	if errors.Is(err, errNotReady) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []spotify.Device{device(st)}, nil
}

// Play starts a context (album, playlist, artist, liked songs) at an
// optional track, or a single track, or resumes when opts is empty.
func (s *Speaker) Play(ctx context.Context, opts spotify.PlayOptions) error {
	var play daemon.ApiPlay
	switch {
	case opts.ContextURI != "":
		play = daemon.ApiPlay{Uri: opts.ContextURI, SkipToUri: opts.OffsetURI}
	case len(opts.URIs) > 0:
		// A plain list of tracks isn't a Spotify context; play the chosen
		// one and let autoplay carry on from there.
		play = daemon.ApiPlay{Uri: opts.URIs[min(opts.OffsetIndex, len(opts.URIs)-1)]}
	default:
		return s.send(ctx, daemon.ApiRequestTypeResume, nil)
	}
	return s.send(ctx, daemon.ApiRequestTypePlay, play)
}

func (s *Speaker) send(ctx context.Context, typ daemon.ApiRequestType, data any) error {
	_, err := s.request(ctx, typ, data)
	return err
}

func (s *Speaker) Pause(ctx context.Context) error {
	return s.send(ctx, daemon.ApiRequestTypePause, nil)
}

func (s *Speaker) Next(ctx context.Context) error {
	return s.send(ctx, daemon.ApiRequestTypeNext, daemon.ApiNext{})
}

func (s *Speaker) Previous(ctx context.Context) error {
	return s.send(ctx, daemon.ApiRequestTypePrev, nil)
}

func (s *Speaker) Seek(ctx context.Context, positionMS int) error {
	return s.send(ctx, daemon.ApiRequestTypeSeek, daemon.ApiSeek{Position: int64(positionMS)})
}

// SetVolume takes a percentage; the speaker is configured with 100 steps.
func (s *Speaker) SetVolume(ctx context.Context, percent int) error {
	return s.send(ctx, daemon.ApiRequestTypeSetVolume, daemon.ApiSetVolume{Volume: int32(min(max(percent, 0), 100))})
}

func (s *Speaker) SetShuffle(ctx context.Context, on bool) error {
	return s.send(ctx, daemon.ApiRequestTypeSetShufflingContext, on)
}

func (s *Speaker) SetRepeat(ctx context.Context, mode string) error {
	if err := s.send(ctx, daemon.ApiRequestTypeSetRepeatingContext, mode == spotify.RepeatContext); err != nil {
		return err
	}
	return s.send(ctx, daemon.ApiRequestTypeSetRepeatingTrack, mode == spotify.RepeatTrack)
}

// Transfer resumes playback here; the speaker only knows about itself.
func (s *Speaker) Transfer(ctx context.Context, _ string, play bool) error {
	if !play {
		return nil
	}
	return s.send(ctx, daemon.ApiRequestTypeResume, nil)
}

func (s *Speaker) AddToQueue(ctx context.Context, uri string) error {
	return s.send(ctx, daemon.ApiRequestTypeAddToQueue, uri)
}
