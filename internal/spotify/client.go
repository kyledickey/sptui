// Package spotify is a small client for the parts of the Spotify Web API
// that sptui uses. It knows nothing about authentication: hand it an
// *http.Client that adds bearer tokens (see package auth).
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // cover art formats
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// DefaultBaseURL is the Spotify Web API root.
const DefaultBaseURL = "https://api.spotify.com/v1"

// maxWait is the longest a request sits out a rate limit before giving up.
const maxWait = 5 * time.Second

// ErrNoActiveDevice is returned by player calls when nothing is playing anywhere.
var ErrNoActiveDevice = errors.New("no active Spotify device")

// Error is an error response from the Web API.
type Error struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
	// RetryAfter is how long Spotify asked us to wait, for 429 responses.
	RetryAfter time.Duration `json:"-"`
}

func rateLimited(wait time.Duration) *Error {
	return &Error{
		Status:     http.StatusTooManyRequests,
		Message:    fmt.Sprintf("rate limited, try again in %s", wait.Round(time.Second)),
		RetryAfter: wait,
	}
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("spotify: %d %s", e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("spotify: %d %s", e.Status, e.Message)
}

// Client talks to the Spotify Web API.
type Client struct {
	http    *http.Client
	images  *http.Client // cover art is public; no token needed
	baseURL string
	log     *slog.Logger
	limit   *limiter
}

// limiter remembers Spotify's Retry-After so every request honours it, not
// just the one that got the 429. Hammering a rate-limited API extends the
// penalty.
type limiter struct {
	mu    sync.Mutex
	until time.Time
}

func (l *limiter) set(wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := time.Now().Add(wait); until.After(l.until) {
		l.until = until
	}
}

// wait sits out a short rate limit and fails fast on a long one, so the UI
// stays responsive.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	left := time.Until(l.until)
	l.mu.Unlock()
	switch {
	case left <= 0:
		return nil
	case left > maxWait:
		return rateLimited(left)
	}
	select {
	case <-time.After(left):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// New returns a Client. httpClient must attach an OAuth token to requests.
func New(httpClient *http.Client, log *slog.Logger) *Client {
	return &Client{
		http:    httpClient,
		images:  &http.Client{Timeout: 20 * time.Second},
		baseURL: DefaultBaseURL,
		log:     log,
		limit:   &limiter{},
	}
}

// CoverArt downloads and decodes a cover image.
func (c *Client) CoverArt(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := c.images.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.log.Debug("cover art", "url", url, "status", resp.StatusCode, "took", time.Since(start).Round(time.Millisecond))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover art: %s", resp.Status)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("decode cover art: %w", err)
	}
	return img, nil
}

// WithBaseURL returns a copy of c that sends requests to baseURL. Used in tests.
func (c *Client) WithBaseURL(baseURL string) *Client {
	cp := *c
	cp.baseURL = baseURL
	return &cp
}

// get fetches path and decodes the JSON response into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// do sends a request. body, if not nil, is JSON encoded. out, if not nil,
// receives the decoded response. A 204 response leaves out untouched.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		if err := c.limit.wait(ctx); err != nil {
			return err
		}
		resp, err := c.send(ctx, method, u, payload)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp)
			resp.Body.Close()
			c.limit.set(wait)
			c.log.Warn("rate limited", "method", method, "path", path, "retry after", wait)
			// Reads are retried once after a short wait; writes never are, so
			// nothing happens twice.
			if method == http.MethodGet && attempt == 0 && wait <= maxWait {
				continue
			}
			return rateLimited(wait)
		}
		defer resp.Body.Close()
		return decode(resp, out)
	}
}

func (c *Client) send(ctx context.Context, method, u string, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Debug("request failed", "method", method, "url", req.URL.Path, "err", err)
		return nil, err
	}
	c.log.Debug("request", "method", method, "path", req.URL.Path, "query", req.URL.RawQuery,
		"status", resp.StatusCode, "took", time.Since(start).Round(time.Millisecond))
	return resp, nil
}

func decode(resp *http.Response, out any) error {
	if resp.StatusCode >= 300 {
		return parseError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var wrapper struct {
		Error json.RawMessage `json:"error"`
	}
	apiErr := &Error{Status: resp.StatusCode}
	if json.Unmarshal(data, &wrapper) == nil && len(wrapper.Error) > 0 {
		// The API sends either {"error": {...}} or, from the accounts
		// service, {"error": "code", "error_description": "..."}.
		if json.Unmarshal(wrapper.Error, apiErr) != nil {
			var code string
			_ = json.Unmarshal(wrapper.Error, &code)
			apiErr.Message = code
		}
		apiErr.Status = resp.StatusCode
	}
	if apiErr.Reason == "NO_ACTIVE_DEVICE" {
		return fmt.Errorf("%w: %s", ErrNoActiveDevice, apiErr.Message)
	}
	return apiErr
}

func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs < 0 {
		return time.Second
	}
	return time.Duration(secs) * time.Second
}

// pageQuery builds limit/offset query parameters.
func pageQuery(offset, limit int) url.Values {
	return url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
}
