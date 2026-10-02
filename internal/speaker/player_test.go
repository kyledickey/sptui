package speaker

import (
	"context"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/daemon"

	"github.com/kyledickey/sptui/internal/spotify"
)

// fakeSpeaker answers requests like go-librespot's player loop would, and
// records what it was asked, apart from status.
func fakeSpeaker(t *testing.T, status *daemon.ApiStatus) (*Speaker, *[]daemon.ApiRequest) {
	t.Helper()
	s := &Speaker{server: newLocalServer(), done: make(chan struct{}), watcher: newObserver()}
	var got []daemon.ApiRequest
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case req := <-s.server.requests:
				if req.Type == daemon.ApiRequestTypeStatus {
					req.Reply(status, nil)
					continue
				}
				got = append(got, req)
				req.Reply(nil, nil)
			case <-stop:
				return
			}
		}
	}()
	return s, &got
}

func TestPlaybackFromStatus(t *testing.T) {
	cover := "https://i.scdn.co/image/abc"
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{
		DeviceId: "dev", DeviceName: "sptui", Volume: 40, VolumeSteps: 100,
		ShuffleContext: true, RepeatTrack: true,
		Track: &daemon.ApiTrack{
			Name: "Song", Uri: "spotify:track:1", Duration: 200000, Position: 1500,
			AlbumName: "Album", AlbumCoverUrl: &cover, ArtistNames: []string{"A", "B"},
		},
	})
	st, err := s.Playback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsPlaying || st.ProgressMS != 1500 || !st.ShuffleState || st.RepeatState != spotify.RepeatTrack {
		t.Fatalf("state = %+v", st)
	}
	if st.Item.ArtistNames() != "A, B" || spotify.CoverURL(st.Item.Album.Images, 300) != cover {
		t.Fatalf("track = %+v", st.Item)
	}
	if st.Device.Name != "sptui" || *st.Device.VolumePercent != 40 {
		t.Fatalf("device = %+v", st.Device)
	}
}

func TestNothingPlaying(t *testing.T) {
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{DeviceName: "sptui", Stopped: true})
	if st, err := s.Playback(context.Background()); st != nil || err != nil {
		t.Fatalf("Playback = %+v, %v", st, err)
	}
}

func TestPlayRequests(t *testing.T) {
	s, got := fakeSpeaker(t, &daemon.ApiStatus{})
	ctx := context.Background()
	s.Play(ctx, spotify.PlayOptions{ContextURI: "spotify:album:1", OffsetURI: "spotify:track:2"})
	s.Play(ctx, spotify.PlayOptions{URIs: []string{"spotify:track:a", "spotify:track:b"}, OffsetIndex: 1})
	s.Play(ctx, spotify.PlayOptions{})
	s.SetRepeat(ctx, spotify.RepeatContext)

	want := []daemon.ApiRequestType{
		daemon.ApiRequestTypePlay, daemon.ApiRequestTypePlay, daemon.ApiRequestTypeResume,
		daemon.ApiRequestTypeSetRepeatingContext, daemon.ApiRequestTypeSetRepeatingTrack,
	}
	if len(*got) != len(want) {
		t.Fatalf("got %d requests, want %d", len(*got), len(want))
	}
	for i, req := range *got {
		if req.Type != want[i] {
			t.Errorf("request %d = %s, want %s", i, req.Type, want[i])
		}
	}
	if p := (*got)[0].Data.(daemon.ApiPlay); p.Uri != "spotify:album:1" || p.SkipToUri != "spotify:track:2" {
		t.Errorf("context play = %+v", p)
	}
	if p := (*got)[1].Data.(daemon.ApiPlay); p.Uri != "spotify:track:b" {
		t.Errorf("track play = %+v", p)
	}
	if (*got)[3].Data != true || (*got)[4].Data != false {
		t.Errorf("repeat context should set context on, track off")
	}
}

func TestNotReadyBeforeLogin(t *testing.T) {
	handoff = 50 * time.Millisecond
	// Nobody reads requests until the speaker has logged in.
	s := &Speaker{server: newLocalServer(), done: make(chan struct{}), watcher: newObserver()}
	if st, err := s.Playback(context.Background()); st != nil || err != nil {
		t.Fatalf("Playback before login = %+v, %v; want nothing", st, err)
	}
	if err := s.Pause(context.Background()); err != errNotReady {
		t.Fatalf("Pause before login = %v", err)
	}
}

func TestPlaybackEpisode(t *testing.T) {
	cover := "https://i.scdn.co/image/ep"
	s, _ := fakeSpeaker(t, &daemon.ApiStatus{
		DeviceName: "sptui",
		Track: &daemon.ApiTrack{
			Name: "Ep 1", Uri: "spotify:episode:e1", Duration: 3600000,
			AlbumName: "Pod", AlbumCoverUrl: &cover, ArtistNames: []string{"Pod"},
		},
	})
	st, err := s.Playback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	it := st.Item
	if !it.IsEpisode() || it.ArtistNames() != "Pod" || it.Album.Name != "" || spotify.CoverURL(it.Cover(), 300) != cover {
		t.Fatalf("episode = %+v", it)
	}
}
