package speaker

import (
	"path/filepath"
	"testing"

	librespot "github.com/devgianlu/go-librespot"
)

func TestClaimSlots(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "speaker.json")
	first := &stateStore{path: statePath}
	state := &librespot.AppState{DeviceId: "first"}
	state.Credentials.Username, state.Credentials.Data = "me", []byte("login")
	if err := first.Save(state); err != nil {
		t.Fatal(err)
	}

	n1, p1, unlock1, err := claim(statePath)
	if err != nil || n1 != 1 || p1 != statePath {
		t.Fatalf("first claim = %d %s %v", n1, p1, err)
	}
	n2, p2, unlock2, err := claim(statePath)
	if err != nil || n2 != 2 || filepath.Base(p2) != "speaker-2.json" {
		t.Fatalf("second claim = %d %s %v", n2, p2, err)
	}
	if err := borrowLogin(statePath, p2); err != nil {
		t.Fatal(err)
	}
	got, _ := (&stateStore{path: p2}).Load()
	if string(got.Credentials.Data) != "login" || got.DeviceId == "first" {
		t.Fatalf("second slot state = %+v", got)
	}

	unlock1()
	if n, _, unlock, _ := claim(statePath); n != 1 {
		t.Fatalf("freed slot 1 not reused: got %d", n)
	} else {
		unlock()
	}
	unlock2()

	if err := Forget(statePath); err != nil {
		t.Fatal(err)
	}
	if HasLogin(statePath) || HasLogin(p2) {
		t.Fatal("Forget left a login behind")
	}
}
