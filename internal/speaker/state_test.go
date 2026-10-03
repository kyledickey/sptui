package speaker

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	librespot "github.com/devgianlu/go-librespot"
	exprand "golang.org/x/exp/rand"
)

func TestForgetKeepsDeviceID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speaker.json")
	store := &stateStore{path: path}
	state := &librespot.AppState{DeviceId: "dev123"}
	state.Credentials.Username, state.Credentials.Data = "kyle", []byte("secret")
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	if err := Forget(path); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceId != "dev123" || got.Credentials.Username != "" || len(got.Credentials.Data) != 0 {
		t.Fatalf("state after Forget = %+v", got)
	}
	if err := Forget(filepath.Join(t.TempDir(), "missing.json")); err != nil {
		t.Fatalf("forgetting a missing file: %v", err)
	}
}

// TestSharedDeviceIDReplaced: a state with the ID every sptui used to get
// loses it, and keeps its login.
func TestSharedDeviceIDReplaced(t *testing.T) {
	store := &stateStore{path: filepath.Join(t.TempDir(), "speaker.json")}
	state := &librespot.AppState{DeviceId: sharedDeviceID}
	state.Credentials.Username, state.Credentials.Data = "kyle", []byte("secret")
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceId != "" || got.Credentials.Username != "kyle" {
		t.Fatalf("loaded %+v, want no device ID and the login kept", got)
	}
}

// TestDeviceIDsRandom draws an ID the way go-librespot does, which must not
// be the one every unseeded process makes.
func TestDeviceIDsRandom(t *testing.T) {
	b := make([]byte, 20)
	_, _ = exprand.Read(b)
	if hex.EncodeToString(b) == sharedDeviceID {
		t.Fatal("golang.org/x/exp/rand isn't seeded: every sptui gets the same device ID")
	}
}
