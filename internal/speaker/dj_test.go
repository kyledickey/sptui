package speaker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDJResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dj.json")
	d := &djResume{path: path}
	if got := d.track(); got != "" {
		t.Fatalf("nothing saved yet, got %q", got)
	}
	d.playing("spotify:track:a")
	d.playing("spotify:track:b")
	if got := d.track(); got != "spotify:track:b" {
		t.Fatalf("track() = %q, want the last one heard", got)
	}

	// Yesterday's session is gone; start at the top.
	data, _ := json.Marshal(djSaved{Track: "spotify:track:b", Until: time.Now().Add(-time.Minute)})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := d.track(); got != "" {
		t.Fatalf("expired session resumed at %q", got)
	}
}
