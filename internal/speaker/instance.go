package speaker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Every sptui running at once needs its own speaker: two Connect devices
// with one ID confuse Spotify, and each sptui would take the other's
// playback for its own. The first sptui uses the state file it's given;
// others claim speaker-2.json, speaker-3.json… in turn, each with its own
// device ID, and borrow the first one's login.

// maxInstances bounds how many slots are tried.
const maxInstances = 16

// slotPath is the state file for slot n (1 is statePath itself).
func slotPath(statePath string, n int) string {
	if n == 1 {
		return statePath
	}
	ext := filepath.Ext(statePath)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(statePath, ext), n, ext)
}

// claim locks the first free slot, returning its number, its state file and
// a func that frees it again.
func claim(statePath string) (int, string, func(), error) {
	for n := 1; n <= maxInstances; n++ {
		path := slotPath(statePath, n)
		unlock, err := lock(path + ".lock")
		if err == errLocked {
			continue
		}
		if err != nil {
			return 0, "", nil, err
		}
		return n, path, unlock, nil
	}
	return 0, "", nil, fmt.Errorf("%d copies of sptui are already running", maxInstances)
}

// borrowLogin copies the first slot's saved login into another slot's state
// file, if it has none of its own.
func borrowLogin(statePath, path string) error {
	if path == statePath {
		return nil
	}
	own := &stateStore{path: path}
	state, err := own.Load()
	if err != nil || len(state.Credentials.Data) > 0 {
		return err
	}
	first, err := (&stateStore{path: statePath}).Load()
	if err != nil || len(first.Credentials.Data) == 0 {
		return err
	}
	state.Credentials = first.Credentials
	return own.Save(state)
}

// slots lists the state files other sptuis have made.
func slots(statePath string) []string {
	var paths []string
	for n := 2; n <= maxInstances; n++ {
		if _, err := os.Stat(slotPath(statePath, n)); err == nil {
			paths = append(paths, slotPath(statePath, n))
		}
	}
	return paths
}
