// Package lyrics finds song lyrics on LRCLIB (https://lrclib.net), a free,
// open lyrics database. Most songs there have synced lyrics: each line
// carries the time it's sung, so a player can follow along.
package lyrics

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kyledickey/sptui/internal/spotify"
)

// ErrNotFound means LRCLIB has no lyrics for the song.
var ErrNotFound = errors.New("no lyrics found")

// ErrUnavailable means LRCLIB couldn't answer right now (down, overloaded or
// unreachable). Trying again later may work.
var ErrUnavailable = errors.New("lyrics service unavailable")

// Line is one line of lyrics. At is when it starts; zero for plain lyrics.
type Line struct {
	At   time.Duration
	Text string
}

// Lyrics are a song's lyrics.
type Lyrics struct {
	Lines        []Line
	Synced       bool // lines have times
	Instrumental bool // the song has no words
}

// Current returns the index of the line being sung at pos, or -1 before the
// first line. Plain lyrics have no current line.
func (l Lyrics) Current(pos time.Duration) int {
	if !l.Synced {
		return -1
	}
	cur := -1
	for i, line := range l.Lines {
		if line.At > pos {
			break
		}
		cur = i
	}
	return cur
}

// DefaultBaseURL is LRCLIB's API.
const DefaultBaseURL = "https://lrclib.net/api"

// Client talks to LRCLIB.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
	log       *slog.Logger
}

// New returns a Client. LRCLIB asks apps to identify themselves with
// userAgent, e.g. "sptui (https://github.com/…)".
func New(userAgent string, log *slog.Logger) *Client {
	return &Client{
		http:      &http.Client{Timeout: 15 * time.Second},
		baseURL:   DefaultBaseURL,
		userAgent: userAgent,
		log:       log,
	}
}

// WithBaseURL returns a copy of c that talks to baseURL. Used in tests.
func (c *Client) WithBaseURL(baseURL string) *Client {
	cp := *c
	cp.baseURL = baseURL
	return &cp
}

// record is LRCLIB's lyrics object.
type record struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Lyrics finds lyrics for t: an exact match first, then the closest search
// result by length, then a search without extras like "- Remastered 2011".
func (c *Client) Lyrics(ctx context.Context, t spotify.Track) (Lyrics, error) {
	artist := ""
	if len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}
	q := url.Values{
		"track_name":  {t.Name},
		"artist_name": {artist},
		"album_name":  {t.Album.Name},
		"duration":    {strconv.Itoa(int(t.Duration().Seconds()))},
	}
	var rec record
	switch err := c.get(ctx, "/get", q, &rec); {
	case err == nil:
		return parse(rec), nil
	case !errors.Is(err, ErrNotFound):
		return Lyrics{}, err
	}
	names := []string{t.Name}
	if short := baseName(t.Name); short != t.Name {
		names = append(names, short)
	}
	for _, name := range names {
		var results []record
		if err := c.get(ctx, "/search", url.Values{"track_name": {name}, "artist_name": {artist}}, &results); err != nil {
			return Lyrics{}, err
		}
		if rec, ok := closest(results, t.Duration()); ok {
			return parse(rec), nil
		}
	}
	return Lyrics{}, ErrNotFound
}

// baseName drops what Spotify adds to titles: "Song - Remastered 2011",
// "Song (feat. Someone)".
func baseName(name string) string {
	if i := strings.Index(name, " - "); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, " ("); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// closest picks the result nearest in length to d, preferring synced
// lyrics. Results more than 10s off are probably other versions.
func closest(results []record, d time.Duration) (record, bool) {
	best, bestScore := record{}, math.Inf(1)
	for _, r := range results {
		off := math.Abs(r.Duration - d.Seconds())
		if off > 10 || (r.PlainLyrics == "" && r.SyncedLyrics == "" && !r.Instrumental) {
			continue
		}
		score := off
		if r.SyncedLyrics == "" {
			score += 5
		}
		if score < bestScore {
			best, bestScore = r, score
		}
	}
	return best, !math.IsInf(bestScore, 1)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	c.log.Debug("lyrics request", "path", path, "status", resp.StatusCode, "took", time.Since(start).Round(time.Millisecond))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return fmt.Errorf("%w: lrclib %s", ErrUnavailable, resp.Status)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("lrclib: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func parse(r record) Lyrics {
	if r.Instrumental {
		return Lyrics{Instrumental: true}
	}
	if lines := parseLRC(r.SyncedLyrics); len(lines) > 0 {
		return Lyrics{Lines: lines, Synced: true}
	}
	var lines []Line
	for text := range strings.Lines(r.PlainLyrics) {
		lines = append(lines, Line{Text: strings.TrimSpace(text)})
	}
	return Lyrics{Lines: lines}
}

// lrcTime matches an LRC timestamp like [01:23.45].
var lrcTime = regexp.MustCompile(`^\[(\d+):(\d+(?:\.\d+)?)\]`)

// parseLRC reads LRC lines like "[00:31.48] Like the legend of the phoenix".
// A line may carry several timestamps when it repeats.
func parseLRC(s string) []Line {
	var lines []Line
	for raw := range strings.Lines(s) {
		raw = strings.TrimSpace(raw)
		var times []time.Duration
		for {
			m := lrcTime.FindStringSubmatch(raw)
			if m == nil {
				break
			}
			mins, _ := strconv.Atoi(m[1])
			secs, _ := strconv.ParseFloat(m[2], 64)
			times = append(times, time.Duration(mins)*time.Minute+time.Duration(secs*float64(time.Second)))
			raw = raw[len(m[0]):]
		}
		for _, at := range times {
			lines = append(lines, Line{At: at, Text: strings.TrimSpace(raw)})
		}
	}
	// Repeated lines can make the order jump; keep it chronological.
	slices.SortStableFunc(lines, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	return lines
}
