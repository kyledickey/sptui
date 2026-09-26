package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/kyledickey/sptui/internal/spotify"
)

// advance moves simulated playback forward to now. Callers hold b.mu.
func (b *Backend) advance(now time.Time) {
	if !b.playing || b.index >= len(b.list) {
		b.at = now
		return
	}
	b.posMS += int(now.Sub(b.at).Milliseconds())
	b.at = now
	for b.playing && b.posMS >= b.list[b.index].DurationMS {
		b.posMS -= b.list[b.index].DurationMS
		b.skip(1, false)
	}
}

// skip moves to the next or previous track. Repeat-one only holds the
// track when it ends by itself, not when the user skips. Callers hold b.mu.
func (b *Backend) skip(dir int, manual bool) {
	if b.repeat == spotify.RepeatTrack && dir > 0 && !manual {
		return
	}
	if dir > 0 && len(b.queue) > 0 {
		// Queued tracks play next, inserted after the current one.
		b.list = slices.Insert(b.list, b.index+1, b.queue[0])
		b.queue = b.queue[1:]
		b.index++
		b.recent = append([]spotify.Track{b.list[b.index]}, b.recent[:min(len(b.recent), 49)]...)
		return
	}
	next := b.index + dir
	switch {
	case b.shuffle && dir > 0 && len(b.list) > 1:
		next = rand.IntN(len(b.list))
	case next >= len(b.list) && b.repeat == spotify.RepeatContext:
		next = 0
	case next >= len(b.list):
		b.playing, b.posMS = false, 0
		return
	case next < 0:
		next = 0
	}
	b.index = next
	b.recent = append([]spotify.Track{b.list[next]}, b.recent[:min(len(b.recent), 49)]...)
}

func (b *Backend) requireDevice() error {
	if b.active < 0 {
		return fmt.Errorf("%w: Player command failed: No active device found", spotify.ErrNoActiveDevice)
	}
	return nil
}

func (b *Backend) Playback(ctx context.Context) (*spotify.PlaybackState, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	if b.active < 0 {
		return nil, nil
	}
	st := &spotify.PlaybackState{
		Device:       b.devices[b.active],
		IsPlaying:    b.playing,
		ProgressMS:   b.posMS,
		ShuffleState: b.shuffle,
		RepeatState:  b.repeat,
	}
	st.Device.IsActive = true
	if b.index < len(b.list) {
		t := b.list[b.index]
		st.Item = &t
	}
	if b.context != "" {
		st.Context = &spotify.PlaybackContext{URI: b.context}
	}
	return st, nil
}

func (b *Backend) Devices(ctx context.Context) ([]spotify.Device, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	out := slices.Clone(b.devices)
	if b.active >= 0 {
		out[b.active].IsActive = true
	}
	return out, nil
}

func (b *Backend) Queue(ctx context.Context) (spotify.Queue, error) {
	if err := b.wait(ctx); err != nil {
		return spotify.Queue{}, err
	}
	defer b.mu.Unlock()
	var q spotify.Queue
	if b.index < len(b.list) {
		t := b.list[b.index]
		q.CurrentlyPlaying = &t
		q.Queue = append(slices.Clone(b.queue), b.list[b.index+1:min(len(b.list), b.index+21)]...)
	}
	return q, nil
}

func (b *Backend) Play(ctx context.Context, opts spotify.PlayOptions) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	defer b.mu.Unlock()
	if opts.DeviceID != "" {
		b.activate(opts.DeviceID)
	}
	if err := b.requireDevice(); err != nil {
		return err
	}
	switch {
	case opts.ContextURI != "":
		list := b.contextTracks(opts.ContextURI)
		if len(list) == 0 {
			return &spotify.Error{Status: 404, Message: "context not found"}
		}
		b.list, b.context = list, opts.ContextURI
		b.index = min(opts.OffsetIndex, len(list)-1)
		if opts.OffsetURI != "" {
			b.index = max(0, slices.IndexFunc(list, func(t spotify.Track) bool { return t.URI == opts.OffsetURI }))
		}
		b.posMS = 0
	case len(opts.URIs) > 0:
		b.list, b.context = nil, ""
		for _, u := range opts.URIs {
			if t, ok := b.findTrack(u); ok {
				b.list = append(b.list, t)
			}
		}
		b.index, b.posMS = min(opts.OffsetIndex, len(b.list)-1), 0
	case len(b.list) == 0:
		// Nothing to resume; start the liked songs like Spotify would.
		b.list, b.context, b.index = slices.Clone(b.liked), spotify.LikedSongsURI(b.me.ID), 0
	}
	b.playing, b.at = true, time.Now()
	return nil
}

func (b *Backend) contextTracks(uri string) []spotify.Track {
	id := spotify.ID(uri)
	switch {
	case strings.HasPrefix(uri, "spotify:album:"):
		return b.albumTracks(uri)
	case strings.HasPrefix(uri, "spotify:playlist:"):
		return slices.Clone(b.plTracks[id])
	case strings.HasPrefix(uri, "spotify:artist:"):
		var out []spotify.Track
		for _, al := range b.artistAlbums(id) {
			out = append(out, b.albumTracks(al.URI)...)
		}
		return out
	case strings.HasPrefix(uri, "spotify:show:"):
		return b.showEpisodes(uri)
	case strings.HasSuffix(uri, ":collection:your-episodes"):
		return b.savedEpisodes()
	case strings.HasSuffix(uri, ":collection"):
		return slices.Clone(b.liked)
	}
	return nil
}

func (b *Backend) activate(id string) {
	if i := slices.IndexFunc(b.devices, func(d spotify.Device) bool { return d.ID == id }); i >= 0 {
		b.active = i
	}
}

// control runs fn if a device is active.
func (b *Backend) control(ctx context.Context, fn func()) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	defer b.mu.Unlock()
	if err := b.requireDevice(); err != nil {
		return err
	}
	fn()
	return nil
}

func (b *Backend) Pause(ctx context.Context) error {
	return b.control(ctx, func() { b.playing = false })
}

func (b *Backend) Next(ctx context.Context) error {
	return b.control(ctx, func() { b.skip(1, true); b.posMS = 0 })
}

func (b *Backend) Previous(ctx context.Context) error {
	return b.control(ctx, func() {
		if b.posMS < 3000 {
			b.skip(-1, true)
		}
		b.posMS = 0
	})
}

func (b *Backend) Seek(ctx context.Context, ms int) error {
	return b.control(ctx, func() { b.posMS = ms })
}

func (b *Backend) SetVolume(ctx context.Context, percent int) error {
	return b.control(ctx, func() { b.devices[b.active].VolumePercent = ptr(percent) })
}

func (b *Backend) SetShuffle(ctx context.Context, on bool) error {
	return b.control(ctx, func() { b.shuffle = on })
}

func (b *Backend) SetRepeat(ctx context.Context, mode string) error {
	return b.control(ctx, func() { b.repeat = mode })
}

func (b *Backend) Transfer(ctx context.Context, deviceID string, play bool) error {
	if err := b.wait(ctx); err != nil {
		return err
	}
	b.activate(deviceID)
	resume := play && len(b.list) == 0
	if play && len(b.list) > 0 {
		b.playing, b.at = true, time.Now()
	}
	b.mu.Unlock()
	if resume {
		return b.Play(ctx, spotify.PlayOptions{})
	}
	return nil
}

func (b *Backend) AddToQueue(ctx context.Context, uri string) error {
	return b.control(ctx, func() {
		if t, ok := b.findTrack(uri); ok {
			b.queue = append(b.queue, t)
		}
	})
}
