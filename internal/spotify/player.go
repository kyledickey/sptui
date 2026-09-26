package spotify

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Playback returns the current playback state, or nil if nothing is playing
// on any device.
func (c *Client) Playback(ctx context.Context) (*PlaybackState, error) {
	var st PlaybackState
	q := url.Values{"additional_types": {"track,episode"}}
	if err := c.get(ctx, "/me/player", q, &st); err != nil {
		return nil, err
	}
	if st.Device.ID == "" && st.Item == nil {
		return nil, nil // 204: no active playback
	}
	return &st, nil
}

// Devices lists the user's available Spotify Connect devices.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var resp struct {
		Devices []Device `json:"devices"`
	}
	err := c.get(ctx, "/me/player/devices", nil, &resp)
	return resp.Devices, err
}

// Queue returns the currently playing track and what comes next.
func (c *Client) Queue(ctx context.Context) (Queue, error) {
	var q Queue
	err := c.get(ctx, "/me/player/queue", nil, &q)
	return q, err
}

// Play starts or resumes playback. A zero PlayOptions resumes.
func (c *Client) Play(ctx context.Context, opts PlayOptions) error {
	body := map[string]any{}
	if opts.ContextURI != "" {
		body["context_uri"] = opts.ContextURI
	}
	if len(opts.URIs) > 0 {
		body["uris"] = opts.URIs
	}
	switch {
	case opts.OffsetURI != "":
		body["offset"] = map[string]any{"uri": opts.OffsetURI}
	case opts.OffsetIndex > 0:
		body["offset"] = map[string]any{"position": opts.OffsetIndex}
	}
	var payload any
	if len(body) > 0 {
		payload = body
	}
	return c.do(ctx, http.MethodPut, "/me/player/play", deviceQuery(opts.DeviceID), payload, nil)
}

// Pause pauses playback.
func (c *Client) Pause(ctx context.Context) error {
	return c.do(ctx, http.MethodPut, "/me/player/pause", nil, nil, nil)
}

// Next skips to the next track.
func (c *Client) Next(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/me/player/next", nil, nil, nil)
}

// Previous skips to the previous track.
func (c *Client) Previous(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/me/player/previous", nil, nil, nil)
}

// Seek jumps to a position in the current track.
func (c *Client) Seek(ctx context.Context, positionMS int) error {
	q := url.Values{"position_ms": {strconv.Itoa(max(0, positionMS))}}
	return c.do(ctx, http.MethodPut, "/me/player/seek", q, nil, nil)
}

// SetVolume sets the volume of the active device (0-100).
func (c *Client) SetVolume(ctx context.Context, percent int) error {
	q := url.Values{"volume_percent": {strconv.Itoa(min(100, max(0, percent)))}}
	return c.do(ctx, http.MethodPut, "/me/player/volume", q, nil, nil)
}

// SetShuffle turns shuffle on or off.
func (c *Client) SetShuffle(ctx context.Context, on bool) error {
	q := url.Values{"state": {strconv.FormatBool(on)}}
	return c.do(ctx, http.MethodPut, "/me/player/shuffle", q, nil, nil)
}

// SetRepeat sets the repeat mode: RepeatOff, RepeatContext or RepeatTrack.
func (c *Client) SetRepeat(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPut, "/me/player/repeat", url.Values{"state": {mode}}, nil, nil)
}

// Transfer moves playback to another device.
func (c *Client) Transfer(ctx context.Context, deviceID string, play bool) error {
	body := map[string]any{"device_ids": []string{deviceID}, "play": play}
	return c.do(ctx, http.MethodPut, "/me/player", nil, body, nil)
}

// AddToQueue appends a track to the user's queue.
func (c *Client) AddToQueue(ctx context.Context, uri string) error {
	return c.do(ctx, http.MethodPost, "/me/player/queue", url.Values{"uri": {uri}}, nil, nil)
}

func deviceQuery(deviceID string) url.Values {
	if deviceID == "" {
		return nil
	}
	return url.Values{"device_id": {deviceID}}
}
