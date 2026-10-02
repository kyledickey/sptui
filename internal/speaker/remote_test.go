package speaker

import (
	"context"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"

	"github.com/kyledickey/sptui/internal/spotify"
)

// phoneCluster is the account as Spotify pushes it while a phone plays.
func phoneCluster(active string) *connectpb.Cluster {
	now := time.Now().UnixMilli()
	return &connectpb.Cluster{
		ActiveDeviceId:    active,
		ServerTimestampMs: now,
		PlayerState: &connectpb.PlayerState{
			Timestamp:             now - 2000,
			PositionAsOfTimestamp: 10000,
			Duration:              200000,
			IsPlaying:             true,
			Options:               &connectpb.ContextPlayerOptions{ShufflingContext: true},
			Track: &connectpb.ProvidedTrack{
				Uri: "spotify:track:1",
				Metadata: map[string]string{
					"title":       "Song",
					"album_title": "Album",
					"image_url":   "spotify:image:abc",
				},
			},
		},
		Device: map[string]*connectpb.DeviceInfo{
			"phone": {DeviceId: "phone", Name: "Phone", DeviceType: devicespb.DeviceType_SMARTPHONE, Volume: 65535 / 2,
				Capabilities: &connectpb.Capabilities{IsControllable: true}},
			"dev":      {DeviceId: "dev", Name: "sptui"},
			"observer": {DeviceId: "observer", Name: "sptui remote", Capabilities: &connectpb.Capabilities{Hidden: true}},
		},
	}
}

func TestPlaybackFromCluster(t *testing.T) {
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{DeviceId: "dev", DeviceName: "sptui", Stopped: true})
	s.watcher.set(phoneCluster("phone"))
	st, err := s.Playback(context.Background())
	if err != nil || st == nil {
		t.Fatalf("Playback = %+v, %v", st, err)
	}
	if st.Device.Name != "Phone" || st.Device.Type != "Smartphone" || *st.Device.VolumePercent != 49 {
		t.Errorf("device = %+v", st.Device)
	}
	if !st.IsPlaying || !st.ShuffleState || st.ProgressMS < 12000 || st.ProgressMS > 13000 {
		t.Errorf("state = %+v", st)
	}
	it := st.Item
	if it.Name != "Song" || it.Album.Name != "Album" || it.DurationMS != 200000 ||
		spotify.CoverURL(it.Album.Images, 300) != "https://i.scdn.co/image/abc" {
		t.Errorf("track = %+v", it)
	}
}

func TestPlayingHereWinsOverCluster(t *testing.T) {
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{DeviceId: "dev", DeviceName: "sptui",
		Track: &daemon.ApiTrack{Name: "Local", Uri: "spotify:track:2"}})
	s.watcher.set(phoneCluster("phone"))
	st, _ := s.Playback(context.Background())
	if st == nil || st.Item.Name != "Local" {
		t.Fatalf("Playback = %+v, want the local track", st)
	}
	if to := s.target(context.Background()); to != "" {
		t.Fatalf("target = %q, want this speaker", to)
	}
}

func TestNoActiveDevice(t *testing.T) {
	// With nobody active, the cluster's player state is just a leftover.
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{DeviceId: "dev", Stopped: true})
	s.watcher.set(phoneCluster(""))
	if st, err := s.Playback(context.Background()); st != nil || err != nil {
		t.Fatalf("Playback = %+v, %v; want nothing", st, err)
	}
	if to := s.target(context.Background()); to != "" {
		t.Fatalf("target = %q, want this speaker", to)
	}
}

func TestDevicesFromCluster(t *testing.T) {
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{DeviceId: "dev", DeviceName: "sptui", Stopped: true})
	s.watcher.id = "observer"
	s.watcher.set(phoneCluster("phone"))
	devices, err := s.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "sptui" || devices[0].IsActive ||
		devices[1].Name != "Phone" || !devices[1].IsActive {
		t.Fatalf("devices = %+v", devices)
	}
}
