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

// playingHere reports whether the speaker itself is playing right now.
func playingHere(st *daemon.ApiStatus) bool {
	return st.Track != nil && !st.Paused && !st.Stopped
}

// Playback reports what the speaker is playing, or else what another of the
// account's devices is, or nil if nothing.
func (s *Speaker) Playback(ctx context.Context) (*spotify.PlaybackState, error) {
	st, err := s.status(ctx)
	if errors.Is(err, errNotReady) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !playingHere(st) {
		if c, at, ok := s.remote(st.DeviceId); ok {
			return s.remotePlayback(ctx, c, at), nil
		}
	}
	if st.Track == nil {
		return nil, nil
	}
	t := st.Track
	if st.ContextUri != nil && *st.ContextUri == spotify.DJURI {
		s.dj.playing(t.Uri)
	}
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

// Devices lists the speaker itself, once it's connected, and the account's
// other devices.
func (s *Speaker) Devices(ctx context.Context) ([]spotify.Device, error) {
	st, err := s.status(ctx)
	if errors.Is(err, errNotReady) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	here := device(st)
	_, _, elsewhere := s.remote(st.DeviceId)
	here.IsActive = !elsewhere
	return append([]spotify.Device{here}, s.remoteDevices(st.DeviceId)...), nil
}

// Play starts a context (album, playlist, artist, liked songs) at an
// optional track, or a single track, or resumes when opts is empty. It plays
// on opts.DeviceID, or else wherever is playing now.
func (s *Speaker) Play(ctx context.Context, opts spotify.PlayOptions) error {
	to := opts.DeviceID
	if to == "" {
		to = s.target(ctx)
	}
	if to != "" && !s.isHere(ctx, to) {
		return s.remotePlay(ctx, to, opts)
	}
	var play daemon.ApiPlay
	switch {
	case opts.ContextURI == spotify.DJURI:
		return s.playSession(ctx, spotify.DJURI, djSession+spotify.DJURI, s.dj.track())
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

// isHere reports whether id is this speaker.
func (s *Speaker) isHere(ctx context.Context, id string) bool {
	st, err := s.status(ctx)
	return err != nil || st.DeviceId == id
}

// control runs a control on whichever device is playing: here via the
// player, elsewhere as the Connect command cmd.
func (s *Speaker) control(ctx context.Context, typ daemon.ApiRequestType, data any, cmd map[string]any) error {
	if to := s.target(ctx); to != "" {
		return s.command(ctx, to, cmd)
	}
	return s.send(ctx, typ, data)
}

func (s *Speaker) Pause(ctx context.Context) error {
	return s.control(ctx, daemon.ApiRequestTypePause, nil, map[string]any{"endpoint": "pause"})
}

func (s *Speaker) Next(ctx context.Context) error {
	return s.control(ctx, daemon.ApiRequestTypeNext, daemon.ApiNext{}, map[string]any{"endpoint": "skip_next"})
}

func (s *Speaker) Previous(ctx context.Context) error {
	return s.control(ctx, daemon.ApiRequestTypePrev, nil, map[string]any{"endpoint": "skip_prev"})
}

func (s *Speaker) Seek(ctx context.Context, positionMS int) error {
	return s.control(ctx, daemon.ApiRequestTypeSeek, daemon.ApiSeek{Position: int64(positionMS)},
		map[string]any{"endpoint": "seek_to", "value": positionMS})
}

// SetVolume takes a percentage; the speaker is configured with 100 steps.
func (s *Speaker) SetVolume(ctx context.Context, percent int) error {
	if to := s.target(ctx); to != "" {
		return s.setRemoteVolume(ctx, to, percent)
	}
	return s.send(ctx, daemon.ApiRequestTypeSetVolume, daemon.ApiSetVolume{Volume: int32(min(max(percent, 0), 100))})
}

func (s *Speaker) SetShuffle(ctx context.Context, on bool) error {
	return s.control(ctx, daemon.ApiRequestTypeSetShufflingContext, on,
		map[string]any{"endpoint": "set_shuffling_context", "value": on})
}

func (s *Speaker) SetRepeat(ctx context.Context, mode string) error {
	if to := s.target(ctx); to != "" {
		return s.command(ctx, to, map[string]any{"endpoint": "set_options",
			"repeating_context": mode == spotify.RepeatContext, "repeating_track": mode == spotify.RepeatTrack})
	}
	if err := s.send(ctx, daemon.ApiRequestTypeSetRepeatingContext, mode == spotify.RepeatContext); err != nil {
		return err
	}
	return s.send(ctx, daemon.ApiRequestTypeSetRepeatingTrack, mode == spotify.RepeatTrack)
}

// Transfer moves playback to deviceID: this speaker or another device.
func (s *Speaker) Transfer(ctx context.Context, deviceID string, play bool) error {
	st, err := s.status(ctx)
	if err != nil {
		return err
	}
	from := st.DeviceId
	if c, _, ok := s.remote(st.DeviceId); ok {
		from = c.ActiveDeviceId
	}
	switch {
	case from == deviceID || (deviceID == st.DeviceId && st.Track != nil):
		// Already there.
		if !play {
			return nil
		}
		if deviceID == st.DeviceId {
			return s.send(ctx, daemon.ApiRequestTypeResume, nil)
		}
		return s.command(ctx, deviceID, map[string]any{"endpoint": "resume"})
	case from == st.DeviceId && st.Track == nil:
		// Nothing is playing anywhere to move; the next play goes there.
		if deviceID == st.DeviceId && play {
			return s.send(ctx, daemon.ApiRequestTypeResume, nil)
		}
		return nil
	}
	return s.transfer(ctx, from, deviceID, play)
}

func (s *Speaker) AddToQueue(ctx context.Context, uri string) error {
	return s.control(ctx, daemon.ApiRequestTypeAddToQueue, uri, map[string]any{"endpoint": "add_to_queue",
		"track": map[string]any{"uri": uri, "metadata": map[string]string{"is_queued": "true"}, "provider": "queue"}})
}
