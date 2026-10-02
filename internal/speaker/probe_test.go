package speaker

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyledickey/sptui/internal/spotify"
)

// TestProbeRemote runs two real, muted speakers with the saved login: A
// plays, and B, through its observer, must see and control it. Manual
// only, as it plays on the account: SPTUI_PROBE=1.
func TestProbeRemote(t *testing.T) {
	if os.Getenv("SPTUI_PROBE") == "" {
		t.Skip("set SPTUI_PROBE=1")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Both share one state file, as two copies of sptui do.
	path := probeState(t)
	a := probeSpeaker(t, ctx, path, log)
	b := probeSpeaker(t, ctx, path, log)
	if a.Name() != "sptui-probe" || b.Name() != "sptui-probe 2" {
		t.Fatalf("names = %q, %q", a.Name(), b.Name())
	}
	defer a.Close()
	defer b.Close()
	for _, s := range []*Speaker{a, b} {
		waitFor(t, "speaker ready", func() bool { return s.SetVolume(ctx, 0) == nil })
		<-s.watcher.ready
	}
	defer func() { _ = a.Pause(context.Background()); _ = b.Pause(context.Background()) }()

	stA, err := a.status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stA.Volume != 0 {
		t.Fatalf("A's volume is %d, not muted; stopping", stA.Volume)
	}
	if err := a.Play(ctx, spotify.PlayOptions{DeviceID: stA.DeviceId, URIs: []string{"spotify:track:4uLU6hMCjMI75M1A2tKUQC"}}); err != nil {
		t.Fatal("A play:", err)
	}

	var st *spotify.PlaybackState
	waitFor(t, "B sees A playing", func() bool {
		st, _ = b.Playback(ctx)
		return st != nil && st.Device.Name == a.Name() && st.IsPlaying
	})
	t.Logf("B sees: %q by %q on %q, %dms of %dms, cover %q", st.Item.Name, st.Item.ArtistNames(),
		st.Device.Name, st.ProgressMS, st.Item.DurationMS, spotify.CoverURL(st.Item.Album.Images, 300))
	if st.Item.ArtistNames() == "" {
		t.Error("no artists")
	}

	devices, _ := b.Devices(ctx)
	out, _ := json.Marshal(devices)
	t.Logf("B's devices: %s", out)

	if err := b.Seek(ctx, 60000); err != nil {
		t.Fatal("B seek:", err)
	}
	waitFor(t, "A seeked", func() bool {
		st, _ := a.status(ctx)
		return st.Track != nil && st.Track.Position >= 60000
	})
	if err := b.Pause(ctx); err != nil {
		t.Fatal("B pause:", err)
	}
	waitFor(t, "A paused", func() bool { st, _ := a.status(ctx); return st.Paused })
	if err := b.Play(ctx, spotify.PlayOptions{}); err != nil {
		t.Fatal("B resume:", err)
	}
	waitFor(t, "A resumed", func() bool { st, _ := a.status(ctx); return !st.Paused })
	time.Sleep(3 * time.Second) // as a person would, before moving it

	st, _ = b.Playback(ctx)
	t.Logf("B sees: %q by %q on %q, %dms of %dms, cover %q", st.Item.Name, st.Item.ArtistNames(),
		st.Device.Name, st.ProgressMS, st.Item.DurationMS, spotify.CoverURL(st.Item.Album.Images, 300))

	stB, _ := b.status(ctx)
	if err := b.Transfer(ctx, stB.DeviceId, true); err != nil {
		t.Fatal("B transfer here:", err)
	}
	waitFor(t, "B playing after transfer", func() bool { st, _ := b.status(ctx); return playingHere(st) })
	waitFor(t, "A sees B", func() bool {
		st, _ := a.Playback(ctx)
		return st != nil && st.Device.Name == b.Name()
	})
	t.Log("transfer ok")
	if hold, _ := time.ParseDuration(os.Getenv("SPTUI_PROBE_HOLD")); hold > 0 {
		time.Sleep(hold) // keep playing, to look at from a real sptui
	}
}

func probeState(t *testing.T) string {
	home, _ := os.UserCacheDir()
	src, err := os.ReadFile(filepath.Join(home, "sptui", "speaker.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(src, &state); err != nil {
		t.Fatal(err)
	}
	delete(state, "device_id") // a fresh device, so a running sptui isn't disturbed
	state["last_volume"] = 0
	data, _ := json.Marshal(state)
	path := filepath.Join(t.TempDir(), "speaker.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func probeSpeaker(t *testing.T, ctx context.Context, path string, log *slog.Logger) *Speaker {
	s, err := Start(ctx, Config{Name: "sptui-probe", Bitrate: 96, StatePath: path}, Credentials{}, log)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("%s", what)
}
