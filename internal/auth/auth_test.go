package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyledickey/sptui/internal/logging"
)

// fakeSpotify stands in for the accounts service token endpoint.
func fakeSpotify(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "the-code" ||
			r.Form.Get("client_id") != testApp.ClientID || r.Form.Get("code_verifier") == "" {
			t.Errorf("bad token request: %v", r.Form)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access", "refresh_token": "refresh", "token_type": "Bearer", "expires_in": 3600,
		})
	}))
	t.Cleanup(srv.Close)
	old := endpoint.TokenURL
	endpoint.TokenURL = srv.URL
	t.Cleanup(func() { endpoint.TokenURL = old })
}

// fakeBrowser approves (or tampers with) the login like a user would.
func fakeBrowser(t *testing.T, tamper bool) {
	t.Helper()
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			t.Errorf("auth URL lacks PKCE: %s", authURL)
		}
		if !strings.Contains(q.Get("scope"), "streaming") || q.Get("client_id") != testApp.ClientID {
			t.Errorf("auth URL has wrong scopes or redirect: %s", authURL)
		}
		state := q.Get("state")
		if tamper {
			state = "evil"
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + state)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

var testApp = App{Purpose: "test", ClientID: "cid", Scopes: []string{"streaming", "user-read-private"}}

func newTestAuth(t *testing.T) (*Authenticator, string) {
	t.Helper()
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	return New(testApp, tokenPath, logging.Discard()), tokenPath
}

func TestLoginSavesToken(t *testing.T) {
	fakeSpotify(t)
	fakeBrowser(t, false)
	a, tokenPath := newTestAuth(t)

	tok, err := a.Login(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access" || tok.RefreshToken != "refresh" {
		t.Fatalf("token = %+v", tok)
	}
	info, err := os.Stat(tokenPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v, %v", info, err)
	}

	// A saved token means no second login.
	openBrowser = func(string) error { t.Error("logged in again"); return nil }
	if _, err := a.TokenSource(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}

	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatal("logout left the token behind")
	}
}

func TestLoginRejectsBadState(t *testing.T) {
	fakeSpotify(t)
	fakeBrowser(t, true)
	a, _ := newTestAuth(t)
	if _, err := a.Login(context.Background(), io.Discard); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("err = %v, want state mismatch", err)
	}
}

func TestOldLoginIsReplaced(t *testing.T) {
	fakeSpotify(t)
	a, tokenPath := newTestAuth(t)
	// A bare token, as written by earlier versions with the user's own app.
	os.WriteFile(tokenPath, []byte(`{"access_token":"old","refresh_token":"old","token_type":"Bearer"}`), 0o600)

	fakeBrowser(t, false)
	ts, err := a.TokenSource(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil || tok.AccessToken != "access" {
		t.Fatalf("got %v, %v; want a fresh login", tok, err)
	}
}

func TestLoginWithRegisteredRedirect(t *testing.T) {
	fakeSpotify(t)
	fakeBrowser(t, false)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	app := testApp
	app.RedirectURI = "http://" + addr + "/callback"
	a := New(app, "", logging.Discard())
	if _, err := a.Login(context.Background(), io.Discard); err != nil {
		t.Fatalf("login via %s: %v", app.RedirectURI, err)
	}
}

func TestTokenIsPerApp(t *testing.T) {
	fakeSpotify(t)
	fakeBrowser(t, false)
	a, tokenPath := newTestAuth(t)
	if _, err := a.Login(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	other := testApp
	other.ClientID = "someone-else"
	if _, err := New(other, tokenPath, logging.Discard()).loadToken(); err == nil {
		t.Fatal("a token saved for one app was accepted for another")
	}
}

// fakeAuthorize stands in for Spotify's consent page: it approves at once
// and redirects back, like Spotify does for apps the user already allowed.
func fakeAuthorize(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=the-code&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	old := endpoint.AuthURL
	endpoint.AuthURL = srv.URL
	t.Cleanup(func() { endpoint.AuthURL = old })
}

func TestChainedLoginsInOneTab(t *testing.T) {
	fakeSpotify(t)
	fakeAuthorize(t)
	opened := 0
	old := openBrowser
	t.Cleanup(func() { openBrowser = old })
	openBrowser = func(u string) error {
		opened++
		go func() {
			if resp, err := http.Get(u); err == nil { // follows every redirect
				resp.Body.Close()
			}
		}()
		return nil
	}

	second, _ := newTestAuth(t)
	first := New(testApp, "", logging.Discard())
	p2, err := second.Start("")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := first.Start(p2.URL)
	if err != nil {
		t.Fatal(err)
	}
	Prompt(io.Discard, "test", p1.URL)
	for _, p := range []*Pending{p1, p2} {
		if tok, err := p.Wait(context.Background()); err != nil || tok.AccessToken != "access" {
			t.Fatalf("login: %v, %v", tok, err)
		}
	}
	if opened != 1 || !second.LoggedIn() {
		t.Fatalf("opened the browser %d times; second saved: %v", opened, second.LoggedIn())
	}
}
