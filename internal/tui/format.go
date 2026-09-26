package tui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/kyledickey/sptui/internal/spotify"
)

// fit truncates or pads plain text s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// clampWidth cuts rendered (possibly styled) text to at most w cells.
func clampWidth(s string, w int) string {
	return ansi.Truncate(s, max(w, 0), "…")
}

func joinNonEmpty(sep string, parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// clock formats a duration as m:ss or h:mm:ss.
func clock(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// ago says how long ago d was, briefly: "just now", "4m ago", "2h ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// cleanDescription turns a playlist description, which may hold HTML links
// and entities, into plain text.
func cleanDescription(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

// webURL converts spotify:track:abc to https://open.spotify.com/track/abc.
func webURL(uri string) string {
	parts := strings.Split(uri, ":")
	if len(parts) != 3 {
		return uri
	}
	return "https://open.spotify.com/" + parts[1] + "/" + parts[2]
}

// friendly turns an error into a short message for the status line.
func friendly(err error) string {
	// Drop the "Get https://api.spotify.com/...:" prefix from transport errors.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var apiErr *spotify.Error
	switch {
	case errors.Is(err, spotify.ErrNoActiveDevice):
		return "No active device — press d to pick one"
	case errors.Is(err, context.DeadlineExceeded):
		return "Spotify took too long to respond"
	case errors.As(err, &apiErr):
		switch apiErr.Status {
		case http.StatusTooManyRequests:
			return fmt.Sprintf("Spotify is rate limiting sptui — retrying in %s", apiErr.RetryAfter.Round(time.Second))
		case http.StatusUnauthorized:
			return "Spotify session expired — run: sptui login"
		case http.StatusForbidden:
			if strings.Contains(strings.ToLower(apiErr.Message), "premium") {
				return "Spotify Premium is required for playback control"
			}
			return "Spotify doesn't allow that: " + apiErr.Message
		case http.StatusNotFound:
			return "Not found on Spotify"
		}
		if apiErr.Message != "" {
			return apiErr.Message
		}
	}
	return err.Error()
}

// isForbidden reports whether err is a Spotify 403.
func isForbidden(err error) bool {
	var apiErr *spotify.Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden
}
