package speaker

import (
	"encoding/json"
	"errors"
	"os"

	librespot "github.com/devgianlu/go-librespot"
)

// stateStore keeps the speaker's device ID, saved login and last volume in a
// JSON file.
type stateStore struct {
	path string
}

var _ librespot.StateStore = (*stateStore)(nil)

func (s *stateStore) Load() (*librespot.AppState, error) {
	state := &librespot.AppState{}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	return state, json.Unmarshal(data, state)
}

func (s *stateStore) Save(state *librespot.AppState) error {
	state.Lock()
	data, err := json.Marshal(state)
	state.Unlock()
	if err != nil {
		return err
	}
	// Write then rename so a crash never leaves a half-written file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// forgetLogin drops the saved login, keeping the device ID. It reports
// whether there was one to drop.
func (s *stateStore) forgetLogin() (bool, error) {
	state, err := s.Load()
	if err != nil || len(state.Credentials.Data) == 0 {
		return false, err
	}
	state.Credentials.Username, state.Credentials.Data = "", nil
	return true, s.Save(state)
}
