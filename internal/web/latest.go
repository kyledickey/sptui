package web

import (
	"bytes"
	"context"
	"html"
	"log/slog"
	"sync"
	"time"
)

const (
	// latestEvery is how long the latest release's tag is trusted before
	// asking GitHub again.
	latestEvery = 10 * time.Minute
	// latestMarker, in a page, is replaced with the latest release's tag.
	// Until it's known the marker stays, a comment, so the element holding
	// it is :empty and can hide itself.
	latestMarker = "<!--latest-->"
)

// latest is the newest sptui release, looked up on GitHub now and then,
// in the background, so no page waits on it.
type latest struct {
	fetch func(context.Context) (string, error)

	mu      sync.Mutex
	tag     string
	checked time.Time
	busy    bool
}

// get returns the latest release's tag, or "" if it isn't known yet, and
// looks it up again if it's been a while.
func (l *latest) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.busy && time.Since(l.checked) > latestEvery {
		l.busy = true
		go l.refresh()
	}
	return l.tag
}

func (l *latest) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, err := l.fetch(ctx)
	if err != nil {
		slog.Warn("couldn't look up the latest release", "err", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.busy = false
	l.checked = time.Now() // after a failure too, so GitHub isn't asked on every visit
	if err == nil {
		l.tag = tag
	}
}

// fill puts the latest release's tag in page, where it asks for it.
func (l *latest) fill(page []byte) []byte {
	if !bytes.Contains(page, []byte(latestMarker)) {
		return page
	}
	tag := l.get()
	if tag == "" {
		return page
	}
	return bytes.ReplaceAll(page, []byte(latestMarker), []byte(html.EscapeString(tag)))
}
