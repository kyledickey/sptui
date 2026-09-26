package speaker

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/kyledickey/sptui/internal/atomicfile"
)

// Spotify builds one DJ session a day and serves it from the top every time
// it's loaded. Its own apps pick up where you left off, so the speaker
// remembers the DJ's song and skips back to it next time.

// djResume is the DJ song last heard, saved to path.
type djResume struct {
	path string
	mu   sync.Mutex
	last string // track URI, to skip saving the same one every poll
}

type djSaved struct {
	Track string    `json:"track"`
	Until time.Time `json:"until"` // the session is rebuilt after this
}

func (d *djResume) playing(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.path == "" || uri == d.last {
		return
	}
	d.last = uri
	now := time.Now()
	// The session expires around local midnight.
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	data, err := json.Marshal(djSaved{Track: uri, Until: midnight})
	if err == nil {
		_ = atomicfile.Write(d.path, data, 0o600) // losing the spot only costs a repeat
	}
}

// track is where to resume today's session, or "" to start at the top.
func (d *djResume) track() string {
	if d.path == "" {
		return ""
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return ""
	}
	var saved djSaved
	if json.Unmarshal(data, &saved) != nil || time.Now().After(saved.Until) {
		return ""
	}
	return saved.Track
}
