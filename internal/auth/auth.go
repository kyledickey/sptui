// Package auth logs in to Spotify (OAuth authorization code flow with PKCE)
// and keeps tokens on disk so users only log in once.
//
// Spotify rate-limits each app (client ID) as a whole, so sptui uses two:
// Streaming only to log the built-in speaker in, and WebAPI for everything
// else. Neither needs users to register an app, but they can bring their
// own for WebAPI to get a rate limit of their own.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// App is a Spotify app that sptui logs in through.
type App struct {
	Purpose     string // what the login is for, shown to the user
	ClientID    string
	RedirectURI string // registered redirect; "" means any free port on 127.0.0.1
	Scopes      []string
}

// Streaming is Spotify's own desktop client, which librespot uses to log in
// speakers. Its Web API rate limit is shared by every librespot-based app and
// is usually exhausted, so it's used for nothing but the speaker's login.
var Streaming = App{
	Purpose:  "play music in sptui",
	ClientID: "65b708073fc0480ea92a077233ca87bd",
	Scopes: []string{
		"app-remote-control", "playlist-modify", "playlist-modify-private", "playlist-modify-public",
		"playlist-read", "playlist-read-collaborative", "playlist-read-private", "streaming",
		"ugc-image-upload", "user-follow-modify", "user-follow-read", "user-library-modify",
		"user-library-read", "user-modify", "user-modify-playback-state", "user-modify-private",
		"user-personalized", "user-read-birthdate", "user-read-currently-playing", "user-read-email",
		"user-read-play-history", "user-read-playback-position", "user-read-playback-state",
		"user-read-private", "user-read-recently-played", "user-top-read",
	},
}

// WebAPI reads the library, searches and controls other devices. By
// default it's ncspot's registered app, which spotify-player uses the same
// way; see WebAPIWith to use your own.
var WebAPI = WebAPIWith("d420a117a32841c2b3474932e49fb54b", "http://127.0.0.1:8989/login")

// WebAPIWith returns the Web API app for a user's own client ID and the
// redirect URI registered for it.
func WebAPIWith(clientID, redirectURI string) App {
	return App{
		Purpose:     "browse your library",
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Scopes: []string{
			"user-read-private", "user-read-playback-state", "user-modify-playback-state",
			"user-read-currently-playing", "user-read-recently-played", "user-top-read",
			"user-library-read", "user-library-modify", "user-follow-read", "user-follow-modify",
			"playlist-read-private", "playlist-read-collaborative", "playlist-modify-public",
			"playlist-modify-private",
		},
	}
}

var endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.spotify.com/authorize",
	TokenURL:  "https://accounts.spotify.com/api/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// loginTimeout bounds how long we wait for the user to approve in the browser.
const loginTimeout = 5 * time.Minute

// Authenticator logs in through one App.
type Authenticator struct {
	app       App
	oauth     *oauth2.Config
	tokenPath string
	log       *slog.Logger
}

// New returns an Authenticator for app that keeps its token at tokenPath.
// With an empty tokenPath nothing is saved.
func New(app App, tokenPath string, log *slog.Logger) *Authenticator {
	return &Authenticator{
		app:       app,
		oauth:     &oauth2.Config{ClientID: app.ClientID, Scopes: app.Scopes, Endpoint: endpoint},
		tokenPath: tokenPath,
		log:       log,
	}
}

// TokenSource returns the user's token, refreshing and re-saving it as
// needed. If no token is saved it runs the browser login first, printing
// instructions to out. ctx must outlive the source; refreshes use it.
func (a *Authenticator) TokenSource(ctx context.Context, out io.Writer) (oauth2.TokenSource, error) {
	tok, err := a.loadToken()
	if err != nil {
		a.log.Info("no saved token, starting login", "reason", err)
		if tok, err = a.Login(ctx, out); err != nil {
			return nil, err
		}
	}
	// Token refreshes use this client; the default one never times out.
	refreshCtx := context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 15 * time.Second})
	return &savingSource{src: a.oauth.TokenSource(refreshCtx, tok), last: tok.AccessToken, save: a.saveToken, log: a.log}, nil
}

// NewHTTPClient returns a client that signs requests with tokens from ts.
func NewHTTPClient(ctx context.Context, ts oauth2.TokenSource) *http.Client {
	client := oauth2.NewClient(ctx, ts)
	client.Timeout = 20 * time.Second
	return client
}

// Login runs the interactive browser login and saves the token.
func (a *Authenticator) Login(ctx context.Context, out io.Writer) (*oauth2.Token, error) {
	p, err := a.Start("")
	if err != nil {
		return nil, err
	}
	Prompt(out, a.app.Purpose, p.URL)
	return p.Wait(ctx)
}

// LoggedIn reports whether a usable token is saved.
func (a *Authenticator) LoggedIn() bool {
	_, err := a.loadToken()
	return err == nil
}

// Prompt asks the user to approve access in their browser.
func Prompt(out io.Writer, purpose, url string) {
	fmt.Fprintf(out, "\nsptui needs your OK to %s.\nOpening your browser; if it doesn't open, visit:\n\n  %s\n\n", purpose, url)
	_ = openBrowser(url)
}

// Pending is a login waiting for the user to approve it in the browser.
type Pending struct {
	URL  string // the page to visit
	wait func(ctx context.Context) (*oauth2.Token, error)
}

// Wait waits for the user to approve, then saves and returns the token.
func (p *Pending) Wait(ctx context.Context) (*oauth2.Token, error) { return p.wait(ctx) }

// Start begins a login: it listens for Spotify's redirect and returns the
// page the user must visit. After approving, the browser is sent to next if
// it's set, which chains several logins in one browser tab.
func (a *Authenticator) Start(next string) (*Pending, error) {
	redirect := a.app.RedirectURI
	if redirect == "" {
		// Spotify accepts any loopback port for apps registered with one.
		redirect = "http://127.0.0.1:0/login"
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid redirect URI %q", redirect)
	}
	ln, err := net.Listen("tcp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("listen for login callback on %s: %w", u.Host, err)
	}
	u.Host = ln.Addr().String() // fills in the port when it was 0
	conf := *a.oauth
	conf.RedirectURL = u.String()
	path := u.Path
	if path == "" {
		path = "/"
	}

	state := randomString()
	verifier := oauth2.GenerateVerifier()

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	send := func(r result) {
		select {
		case results <- r:
		default: // already have a result; ignore repeat visits
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			send(result{err: errors.New("login failed: state mismatch")})
		case q.Get("error") != "":
			fmt.Fprint(w, page("Login cancelled", "You can close this tab."))
			send(result{err: fmt.Errorf("login failed: %s", q.Get("error"))})
		case next != "":
			http.Redirect(w, r, next, http.StatusFound)
			send(result{code: q.Get("code")})
		default:
			fmt.Fprint(w, page("You're logged in", "Head back to your terminal — sptui is starting."))
			send(result{code: q.Get("code")})
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln) //nolint:errcheck // always returns ErrServerClosed after Shutdown

	wait := func(ctx context.Context) (*oauth2.Token, error) {
		defer srv.Shutdown(context.Background())
		ctx, cancel := context.WithTimeout(ctx, loginTimeout)
		defer cancel()
		var res result
		select {
		case res = <-results:
		case <-ctx.Done():
			return nil, fmt.Errorf("login timed out: %w", ctx.Err())
		}
		if res.err != nil {
			return nil, res.err
		}
		tok, err := conf.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("exchange code for token: %w", err)
		}
		if err := a.saveToken(tok); err != nil {
			return nil, err
		}
		a.log.Info("logged in", "purpose", a.app.Purpose, "expires", tok.Expiry)
		return tok, nil
	}
	return &Pending{URL: conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), wait: wait}, nil
}

// Logout deletes the saved token.
func (a *Authenticator) Logout() error {
	if a.tokenPath == "" {
		return nil
	}
	err := os.Remove(a.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// savedLogin is the token file. It records what the token was issued for,
// so a login from an older sptui (another client or fewer scopes) is
// replaced instead of failing later in confusing ways.
type savedLogin struct {
	ClientID string        `json:"client_id"`
	Scopes   string        `json:"scopes"`
	Token    *oauth2.Token `json:"token"`
}

func (a *Authenticator) loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(a.tokenPath)
	if err != nil {
		return nil, err
	}
	var saved savedLogin
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	switch {
	case saved.ClientID != a.app.ClientID || saved.Scopes != strings.Join(a.app.Scopes, " "):
		return nil, errors.New("saved login is from an older version of sptui")
	case saved.Token == nil || saved.Token.RefreshToken == "":
		return nil, errors.New("saved login has no refresh token")
	}
	return saved.Token, nil
}

func (a *Authenticator) saveToken(tok *oauth2.Token) error {
	if a.tokenPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.tokenPath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(savedLogin{ClientID: a.app.ClientID, Scopes: strings.Join(a.app.Scopes, " "), Token: tok})
	if err != nil {
		return err
	}
	// Write then rename so a crash never leaves a half-written token.
	tmp := a.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.tokenPath)
}

// savingSource persists the token whenever it is refreshed.
type savingSource struct {
	src  oauth2.TokenSource
	save func(*oauth2.Token) error
	log  *slog.Logger

	mu   sync.Mutex
	last string
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err != nil {
		s.log.Error("token refresh failed", "err", err)
		if Revoked(err) {
			return nil, fmt.Errorf("spotify login expired, run: sptui login (%w)", err)
		}
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok.AccessToken != s.last {
		s.last = tok.AccessToken
		s.log.Debug("token refreshed", "expires", tok.Expiry)
		if err := s.save(tok); err != nil {
			s.log.Error("save refreshed token", "err", err)
		}
	}
	return tok, nil
}

// Revoked reports whether err means the saved login is no longer valid, as
// opposed to a temporary failure talking to Spotify.
func Revoked(err error) bool {
	var re *oauth2.RetrieveError
	return errors.As(err, &re) && re.ErrorCode == "invalid_grant"
}

func randomString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// openBrowser opens u in the user's browser. A variable so tests can stand in
// for the user.
var openBrowser = func(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

func page(title, body string) string {
	return `<!doctype html><meta charset="utf-8"><title>sptui</title>
<body style="font-family:system-ui;background:#121212;color:#eee;display:grid;place-items:center;height:100vh;margin:0">
<div style="text-align:center"><h1 style="color:#1ed760">` + title + `</h1><p>` + body + `</p></div>`
}
