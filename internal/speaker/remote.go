package speaker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	extmetadatapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"

	"google.golang.org/protobuf/proto"

	"github.com/kyledickey/sptui/internal/spotify"
)

// When another device is playing (see cluster.go), the speaker reports its
// state and forwards controls to it as Connect commands, as Spotify's own
// apps do.

// errNoObserver means sptui can't see or reach other devices yet.
var errNoObserver = errors.New("sptui hasn't connected to Spotify's other devices yet")

// remote returns the account's cluster if a device other than this
// speaker (whose ID is localID) is the active one.
func (s *Speaker) remote(localID string) (*connectpb.Cluster, time.Time, bool) {
	c, at := s.watcher.snapshot()
	if c == nil {
		return nil, at, false
	}
	switch id := c.GetActiveDeviceId(); id {
	case "", localID, s.watcher.id:
		return nil, at, false
	}
	return c, at, true
}

// target picks where a control goes: the active device when it's another
// one, else the device picked while nothing played, or "" for this
// speaker. The speaker playing right now wins over a cluster that may not
// have caught up yet.
func (s *Speaker) target(ctx context.Context) string {
	st, err := s.status(ctx)
	if err != nil || playingHere(st) {
		return ""
	}
	if c, _, ok := s.remote(st.DeviceId); ok {
		return c.ActiveDeviceId
	}
	if st.Track == nil {
		return s.chosenDevice()
	}
	return ""
}

// choose remembers the device picked while nothing was playing, so the next
// play goes there; "" means this speaker.
func (s *Speaker) choose(id string) {
	s.chosen.Lock()
	s.chosen.id = id
	s.chosen.Unlock()
}

func (s *Speaker) chosenDevice() string {
	s.chosen.Lock()
	defer s.chosen.Unlock()
	return s.chosen.id
}

// remotePlayback turns the cluster's player state into what the UI shows.
func (s *Speaker) remotePlayback(ctx context.Context, c *connectpb.Cluster, at time.Time) *spotify.PlaybackState {
	ps := c.GetPlayerState()
	t := ps.GetTrack()
	if t == nil || t.Uri == "" {
		return nil
	}
	playing := ps.IsPlaying && !ps.IsPaused
	position := ps.PositionAsOfTimestamp
	if playing {
		now := time.Now().UnixMilli()
		if c.ServerTimestampMs > 0 {
			now = c.ServerTimestampMs + time.Since(at).Milliseconds()
		}
		speed := ps.PlaybackSpeed
		if speed <= 0 {
			speed = 1
		}
		position += int64(float64(now-ps.Timestamp) * speed)
	}
	position = min(max(position, 0), ps.Duration)

	track := s.meta.lookup(ctx, s, t)
	track.DurationMS = int(ps.Duration)
	repeat := spotify.RepeatOff
	switch {
	case ps.GetOptions().GetRepeatingTrack():
		repeat = spotify.RepeatTrack
	case ps.GetOptions().GetRepeatingContext():
		repeat = spotify.RepeatContext
	}
	return &spotify.PlaybackState{
		Device:       remoteDevice(c.Device[c.ActiveDeviceId], true),
		Item:         &track,
		ProgressMS:   int(position),
		IsPlaying:    playing,
		ShuffleState: ps.GetOptions().GetShufflingContext(),
		RepeatState:  repeat,
	}
}

// remoteDevice describes another Connect device the way the Web API would.
func remoteDevice(d *connectpb.DeviceInfo, active bool) spotify.Device {
	if d == nil {
		return spotify.Device{IsActive: active}
	}
	volume := int(d.Volume) * 100 / 65535
	typ := strings.ToLower(d.DeviceType.String())
	if typ != "" {
		typ = strings.ToUpper(typ[:1]) + typ[1:]
	}
	return spotify.Device{
		ID:             d.DeviceId,
		Name:           d.Name,
		Type:           typ,
		IsActive:       active,
		IsRestricted:   !d.GetCapabilities().GetIsControllable(),
		SupportsVolume: !d.GetCapabilities().GetDisableVolume(),
		VolumePercent:  &volume,
	}
}

// remoteDevices lists the account's other devices, skipping hidden ones
// (like every sptui's observer) and this speaker.
func (s *Speaker) remoteDevices(localID string) []spotify.Device {
	c, _ := s.watcher.snapshot()
	var devices []spotify.Device
	for id, d := range c.GetDevice() {
		if id == localID || id == s.watcher.id || d.GetCapabilities().GetHidden() {
			continue
		}
		devices = append(devices, remoteDevice(d, id == c.ActiveDeviceId))
	}
	return devices
}

// command sends a player command to another device.
func (s *Speaker) command(ctx context.Context, to string, cmd map[string]any) error {
	return s.connectRequest(ctx, http.MethodPost, "/connect-state/v1/player/command/from/"+s.watcher.id+"/to/"+to, map[string]any{"command": cmd})
}

// transfer moves playback from one device to another.
func (s *Speaker) transfer(ctx context.Context, from, to string, play bool) error {
	restore := "pause"
	if play {
		restore = "resume"
	}
	return s.connectRequest(ctx, http.MethodPost, "/connect-state/v1/connect/transfer/from/"+from+"/to/"+to,
		map[string]any{"transfer_options": map[string]any{"restore_paused": restore}})
}

// setRemoteVolume sets another device's volume. Unlike the other commands,
// this one is a protobuf.
func (s *Speaker) setRemoteVolume(ctx context.Context, to string, percent int) error {
	data, err := proto.Marshal(&connectpb.SetVolumeCommand{
		Volume:         int32(min(max(percent, 0), 100) * 65535 / 100),
		SentByDeviceId: s.watcher.id,
	})
	if err != nil {
		return err
	}
	return s.connectSend(ctx, http.MethodPut, "/connect-state/v1/connect/volume/from/"+s.watcher.id+"/to/"+to,
		"application/x-protobuf", data)
}

func (s *Speaker) connectRequest(ctx context.Context, method, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return s.connectSend(ctx, method, path, "application/json", data)
}

func (s *Speaker) connectSend(ctx context.Context, method, path, contentType string, data []byte) error {
	sp := s.watcher.client()
	if sp == nil {
		return errNoObserver
	}
	resp, err := sp.Request(ctx, method, path, nil, http.Header{"Content-Type": {contentType}}, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("spotify refused the command: %d %s", resp.StatusCode, msg)
	}
	return nil
}

// remotePlay starts opts on another device.
func (s *Speaker) remotePlay(ctx context.Context, to string, opts spotify.PlayOptions) error {
	uri, skipTo := opts.ContextURI, opts.OffsetURI
	switch {
	case uri == spotify.DJURI:
		return s.sessionCommand(ctx, s.watcher.client(), s.watcher.id, to, spotify.DJURI, djSession+spotify.DJURI, s.dj.track())
	case uri == "" && len(opts.URIs) > 0:
		uri = opts.URIs[min(opts.OffsetIndex, len(opts.URIs)-1)]
		skipTo = ""
	case uri == "":
		return s.command(ctx, to, map[string]any{"endpoint": "resume"})
	}
	return s.sessionCommand(ctx, s.watcher.client(), s.watcher.id, to, uri, "context://"+uri, skipTo)
}

// trackMeta fills in what the cluster leaves out of a track (the artists,
// an episode's show), looked up once per track.
type trackMeta struct {
	mu   sync.Mutex
	uri  string
	meta extraMeta
}

// extraMeta is what the metadata lookup adds to the cluster's track.
type extraMeta struct {
	name, album string
	artists     []spotify.Artist
	show        *spotify.Show
}

func (m *trackMeta) lookup(ctx context.Context, s *Speaker, t *connectpb.ProvidedTrack) spotify.Track {
	md := t.Metadata
	track := spotify.Track{
		Name:  md["title"],
		URI:   t.Uri,
		Album: spotify.Album{Name: md["album_title"], URI: t.AlbumUri, Images: images(md)},
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uri != t.Uri {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		extra, err := s.fetchMeta(ctx, t.Uri)
		if err != nil {
			// Show what the cluster had; try again next time.
			return track
		}
		m.uri, m.meta = t.Uri, extra
	}
	if track.Name == "" {
		track.Name = m.meta.name
	}
	if track.Album.Name == "" {
		track.Album.Name = m.meta.album
	}
	track.Artists = m.meta.artists
	if m.meta.show != nil {
		track.Type = "episode"
		track.Show = m.meta.show
		track.Images, track.Album = track.Album.Images, spotify.Album{}
	}
	return track
}

func (s *Speaker) fetchMeta(ctx context.Context, uri string) (extraMeta, error) {
	var extra extraMeta
	sp := s.watcher.client()
	if sp == nil {
		return extra, errNoObserver
	}
	id, err := librespot.SpotifyIdFromUri(uri)
	if err != nil {
		return extra, err
	}
	if id.Type() == librespot.SpotifyIdTypeEpisode {
		var ep metadatapb.Episode
		if err := sp.ExtendedMetadataSimple(ctx, *id, extmetadatapb.ExtensionKind_EPISODE_V4, &ep); err != nil {
			return extra, err
		}
		extra.name = ep.GetName()
		extra.show = &spotify.Show{Name: ep.GetShow().GetName()}
		return extra, nil
	}
	var meta metadatapb.Track
	if err := sp.ExtendedMetadataSimple(ctx, *id, extmetadatapb.ExtensionKind_TRACK_V4, &meta); err != nil {
		return extra, err
	}
	extra.name, extra.album = meta.GetName(), meta.GetAlbum().GetName()
	for _, a := range meta.GetArtist() {
		extra.artists = append(extra.artists, spotify.Artist{Name: a.GetName()})
	}
	return extra, nil
}

// images turns the cluster's cover art (spotify:image:<id>) into URLs.
func images(md map[string]string) []spotify.Image {
	var out []spotify.Image
	for _, size := range []struct {
		key   string
		width int
	}{{"image_large_url", 640}, {"image_url", 300}, {"image_small_url", 64}} {
		if id, ok := strings.CutPrefix(md[size.key], "spotify:image:"); ok {
			out = append(out, spotify.Image{URL: "https://i.scdn.co/image/" + id, Width: size.width, Height: size.width})
		}
	}
	return out
}
