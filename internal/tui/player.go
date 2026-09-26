package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

const (
	seekStep    = 10 * time.Second
	volumeStep  = 5
	callTimeout = 15 * time.Second
)

// Timers. Variables so tests can make them instant.
var (
	tickEvery       = time.Second            // redraw the progress bar
	settleDelay     = 400 * time.Millisecond // Spotify lags a little after commands
	searchDebounce  = 350 * time.Millisecond
	volumeDebounce  = 250 * time.Millisecond
	localDeviceWait = 500 * time.Millisecond // between checks for our speaker
	lyricsTick      = 200 * time.Millisecond // redraw rate while lyrics are shown
	localGrace      = time.Second            // ignore polls started this soon after a local change
)

var errNoDevice = errors.New("no Spotify devices found — open Spotify on a phone, computer or speaker, then press d")

// player mirrors Spotify's playback state between polls, moving the progress
// bar locally so the UI stays smooth without hammering the API.
type player struct {
	state     *spotify.PlaybackState
	at        time.Time // when state.ProgressMS was accurate
	lastFetch time.Time
	fetching  bool
	changedAt time.Time // last optimistic local edit; older fetches are stale

	since    time.Time // when the current track came on screen, for scrolling its title
	likedURI string    // track the liked flag belongs to
	liked    bool

	volumeSeq int           // debounces volume changes
	every     time.Duration // how often to fetch playback state
}

func (p *player) track() *spotify.Track {
	if p.state == nil {
		return nil
	}
	return p.state.Item
}

func (p *player) playing() bool { return p.state != nil && p.state.IsPlaying }

// progress is the estimated position in the current track at now.
func (p *player) progress(now time.Time) time.Duration {
	if p.state == nil {
		return 0
	}
	pos := time.Duration(p.state.ProgressMS) * time.Millisecond
	if p.state.IsPlaying {
		pos += now.Sub(p.at)
	}
	if t := p.track(); t != nil {
		pos = min(pos, t.Duration())
	}
	return pos
}

// due reports whether it's time to fetch playback state again.
func (p *player) due(now time.Time) bool {
	if p.fetching {
		return false
	}
	if now.Sub(p.lastFetch) >= p.every {
		return true
	}
	// Refresh right after a track ends instead of waiting for the next poll.
	t := p.track()
	return p.playing() && t != nil && p.progress(now) >= t.Duration() && now.Sub(p.lastFetch) > time.Second
}

// set stores a fresh state from Spotify.
func (p *player) set(st *spotify.PlaybackState, now time.Time) {
	p.state, p.at, p.lastFetch, p.fetching = st, now, now, false
}

// freeze pins progress to now, so local edits start from the right spot.
func (p *player) freeze(now time.Time) {
	if p.state != nil {
		p.state.ProgressMS = int(p.progress(now).Milliseconds())
		p.at = now
	}
}

func (p *player) volume() int {
	if p.state == nil || p.state.Device.VolumePercent == nil {
		return -1
	}
	return *p.state.Device.VolumePercent
}

// Messages produced by player commands.
type (
	tickMsg     time.Time
	refreshMsg  struct{}
	playbackMsg struct {
		state   *spotify.PlaybackState
		err     error
		started time.Time
	}
	likedMsg struct {
		uri   string
		liked bool
	}
	volumeMsg struct{ seq, percent int }
	// actionMsg reports the outcome of a command. ok is shown on success.
	actionMsg struct {
		ok      string
		err     error
		refresh bool // re-fetch playback afterwards
	}
)

func tick() tea.Cmd { return tickAfter(tickEvery) }

func tickAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshSoon() tea.Cmd {
	return tea.Tick(settleDelay, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m *Model) fetchPlayback() tea.Cmd {
	if m.player.fetching {
		return nil
	}
	m.player.fetching = true
	started := time.Now()
	return m.call(func(ctx context.Context) tea.Msg {
		st, err := m.backend.Playback(ctx)
		return playbackMsg{state: st, err: err, started: started}
	})
}

// checkLiked looks up whether uri is in the user's library.
func (m *Model) checkLiked(uri string) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		saved, err := m.backend.InLibrary(ctx, []string{uri})
		if err != nil || len(saved) == 0 {
			m.log.Debug("check liked failed", "uri", uri, "err", err)
			return nil
		}
		return likedMsg{uri: uri, liked: saved[0]}
	})
}

// call runs fn in the background with a timeout.
func (m *Model) call(fn func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return fn(ctx)
	}
}

// act runs a player command and reports the outcome, refreshing playback
// state afterwards.
func (m *Model) act(name, ok string, fn func(ctx context.Context) error) tea.Cmd {
	m.log.Debug("player action", "action", name)
	return m.call(func(ctx context.Context) tea.Msg {
		err := fn(ctx)
		if err != nil {
			m.log.Warn("player action failed", "action", name, "err", err)
		}
		return actionMsg{ok: ok, err: err, refresh: true}
	})
}

// play starts playback in the background, on sptui's own speaker unless the
// user chose another device. See startPlayback.
func (m *Model) play(opts spotify.PlayOptions, fallback []string) tea.Cmd {
	m.log.Debug("play", "context", opts.ContextURI, "uris", len(opts.URIs), "offset", opts.OffsetURI)
	local := m.wantLocal()
	return m.act("play", "", func(ctx context.Context) error {
		if err := m.playHere(ctx, local, &opts); err != nil {
			return err
		}
		return m.startPlayback(ctx, opts, fallback)
	})
}

// playHere points opts at sptui's own speaker when local is set (see
// wantLocal), waiting for the speaker if it's still connecting.
func (m *Model) playHere(ctx context.Context, local bool, opts *spotify.PlayOptions) error {
	if !local {
		return nil
	}
	dev, err := m.localDevice(ctx)
	if err != nil {
		return err
	}
	opts.DeviceID = dev.ID
	return nil
}

// wantLocal reports whether playback should move to sptui's own speaker: it
// exists, it isn't already the active device, and the user hasn't picked
// another one.
func (m *Model) wantLocal() bool {
	if m.opts.LocalDevice == "" || m.remoteChosen {
		return false
	}
	st := m.player.state
	return st == nil || !strings.EqualFold(st.Device.Name, m.opts.LocalDevice)
}

// localDevice finds sptui's speaker in the device list. Right after startup
// it can take a few seconds to register with Spotify, so it waits a little.
func (m *Model) localDevice(ctx context.Context) (spotify.Device, error) {
	for {
		devices, err := m.backend.Devices(ctx)
		if err != nil {
			return spotify.Device{}, err
		}
		for _, d := range devices {
			if strings.EqualFold(d.Name, m.opts.LocalDevice) {
				return d, nil
			}
		}
		m.log.Debug("waiting for the built-in speaker to appear", "name", m.opts.LocalDevice)
		select {
		case <-time.After(localDeviceWait):
		case <-ctx.Done():
			return spotify.Device{}, errors.New("sptui's speaker isn't connected to Spotify — see the log (sptui -debug)")
		}
	}
}

// startPlayback plays opts, picking a device first if none is active. If
// the context can't be played (e.g. Liked Songs on some accounts), it falls
// back to playing fallback as a plain list of tracks.
func (m *Model) startPlayback(ctx context.Context, opts spotify.PlayOptions, fallback []string) error {
	err := m.backend.Play(ctx, opts)
	if errors.Is(err, spotify.ErrNoActiveDevice) {
		var dev spotify.Device
		if dev, err = m.pickDevice(ctx); err != nil {
			return err
		}
		opts.DeviceID = dev.ID
		err = m.backend.Play(ctx, opts)
	}
	if apiErr, ok := errors.AsType[*spotify.Error](err); ok && apiErr.Status < 500 && opts.ContextURI != "" && len(fallback) > 0 {
		m.log.Info("context playback failed, playing tracks instead", "context", opts.ContextURI, "err", err)
		opts.OffsetIndex = max(0, slices.Index(fallback, opts.OffsetURI))
		opts.URIs, opts.ContextURI, opts.OffsetURI = fallback, "", ""
		err = m.backend.Play(ctx, opts)
	}
	return err
}

// resume continues playback: on sptui's own speaker if wanted, otherwise
// where it was, waking a device if nothing is active.
func (m *Model) resume() tea.Cmd {
	local := m.wantLocal()
	return m.act("resume", "", func(ctx context.Context) error {
		if local {
			dev, err := m.localDevice(ctx)
			if err != nil {
				return err
			}
			return m.backend.Transfer(ctx, dev.ID, true)
		}
		err := m.backend.Play(ctx, spotify.PlayOptions{})
		if errors.Is(err, spotify.ErrNoActiveDevice) {
			dev, derr := m.pickDevice(ctx)
			if derr != nil {
				return derr
			}
			return m.backend.Transfer(ctx, dev.ID, true)
		}
		return err
	})
}

// pickDevice chooses a device to play on: sptui's own speaker, else the
// first available.
func (m *Model) pickDevice(ctx context.Context) (spotify.Device, error) {
	devices, err := m.backend.Devices(ctx)
	if err != nil {
		return spotify.Device{}, err
	}
	var usable []spotify.Device
	for _, d := range devices {
		if !d.IsRestricted {
			usable = append(usable, d)
		}
	}
	if len(usable) == 0 {
		return spotify.Device{}, errNoDevice
	}
	for _, d := range usable {
		if m.opts.LocalDevice != "" && strings.EqualFold(d.Name, m.opts.LocalDevice) {
			return d, nil
		}
	}
	m.log.Info("no active device, using first available", "device", usable[0].Name)
	return usable[0], nil
}

// nextRepeat cycles off → context → track.
func nextRepeat(mode string) string {
	switch mode {
	case spotify.RepeatOff:
		return spotify.RepeatContext
	case spotify.RepeatContext:
		return spotify.RepeatTrack
	}
	return spotify.RepeatOff
}
