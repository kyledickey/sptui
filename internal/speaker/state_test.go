package speaker

import (
	"path/filepath"
	"testing"

	librespot "github.com/devgianlu/go-librespot"
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
