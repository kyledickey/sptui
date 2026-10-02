package speaker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/daemon"

	"github.com/kyledickey/sptui/internal/spotify"
)

// shortDir is a temporary directory with a path short enough for a socket.
func shortDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "sptui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestClientControlsSharedSpeaker(t *testing.T) {
	primary, got := fakeSpeaker(t, &daemon.ApiStatus{
		DeviceId: "dev", DeviceName: "sptui", Volume: 30, VolumeSteps: 100,
		Track: &daemon.ApiTrack{Name: "Song", Uri: "spotify:track:1", ArtistNames: []string{"A"}},
	})
	primary.sock = filepath.Join(shortDir(t), "speaker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := primary.serve(ctx, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}

	client := &Speaker{server: newLocalServer(), done: make(chan struct{}), watcher: newObserver(), sock: primary.sock}
	client.client.Store(true)
	st, err := client.Playback(ctx)
	if err != nil || st == nil || st.Item.Name != "Song" || st.Device.Name != "sptui" || *st.Device.VolumePercent != 30 {
		t.Fatalf("Playback through the socket = %+v, %v", st, err)
	}
	if err := client.Play(ctx, spotify.PlayOptions{ContextURI: "spotify:album:1", OffsetURI: "spotify:track:2"}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetShuffle(ctx, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(*got) != 2 {
		t.Fatalf("speaker got %d requests, want 2", len(*got))
	}
	if p := (*got)[0].Data.(daemon.ApiPlay); p.Uri != "spotify:album:1" || p.SkipToUri != "spotify:track:2" {
		t.Errorf("play = %+v", p)
	}
	if (*got)[1].Type != daemon.ApiRequestTypeSetShufflingContext || (*got)[1].Data != true {
		t.Errorf("shuffle = %+v", (*got)[1])
	}
}

func TestClientWithoutSpeaker(t *testing.T) {
	// The sptui running the speaker has just quit.
	s := &Speaker{server: newLocalServer(), done: make(chan struct{}), watcher: newObserver(),
		sock: filepath.Join(shortDir(t), "speaker.sock")}
	s.client.Store(true)
	if st, err := s.Playback(context.Background()); st != nil || err != nil {
		t.Fatalf("Playback = %+v, %v; want nothing", st, err)
	}
	if err := s.Pause(context.Background()); err != errNotReady {
		t.Fatalf("Pause = %v, want not ready", err)
	}
}

func TestOneSpeakerPerComputer(t *testing.T) {
	path := filepath.Join(shortDir(t), "speaker.json.lock")
	unlock, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock(path); err != errLocked {
		t.Fatalf("second lock = %v, want errLocked", err)
	}
	waited := make(chan func())
	go func() {
		u, _ := lockWait(path)
		waited <- u
	}()
	select {
	case <-waited:
		t.Fatal("lockWait didn't wait")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case u := <-waited:
		u()
	case <-time.After(time.Second):
		t.Fatal("lockWait didn't get the freed lock")
	}
}
