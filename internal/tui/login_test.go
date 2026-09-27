package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/logging"
)

// fakeLogins stands in for main's logins.
type fakeLogins struct {
	started   []config.Config
	cancelled int
	opened    []string
	saved     []config.Config
}

func (f *fakeLogins) options(cfg config.Config, web bool) LoginOptions {
	return LoginOptions{
		Config:     cfg,
		SaveConfig: func(c config.Config) error { f.saved = append(f.saved, c); return nil },
		WebLogin:   web,
		Start: func(c config.Config) (LoginAttempt, error) {
			f.started = append(f.started, c)
			wait := func(context.Context) error { return nil }
			return LoginAttempt{
				URL:    "https://accounts.spotify.com/authorize?try=" + string(rune('0'+len(f.started))),
				Steps:  []LoginStep{{Purpose: "play music in sptui", Wait: wait}, {Purpose: "browse your library", Wait: wait}},
				Cancel: func() { f.cancelled++ },
			}, nil
		},
		OpenURL: func(u string) error { f.opened = append(f.opened, u); return nil },
		Log:     logging.Discard(),
	}
}

func newTestLogin(opts LoginOptions) *loginModel {
	m := newLogin(context.Background(), opts)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.Init()
	return m
}

func loginScreen(m *loginModel) string { return ansi.Strip(m.View().Content) }

func press(m *loginModel, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		}
		m.Update(msg)
	}
}

func TestLoginOffersOwnAppWithoutClientID(t *testing.T) {
	f := &fakeLogins{}
	m := newTestLogin(f.options(config.Default(), true))
	if m.stage != stageApp || len(f.started) != 0 {
		t.Fatalf("stage %v, %d logins started; want the app guide first", m.stage, len(f.started))
	}
	screen := loginScreen(m)
	for _, want := range []string{"Create an app", config.DefaultRedirectURI, "Client ID", "tab skip"} {
		if !strings.Contains(screen, want) {
			t.Errorf("app guide doesn't mention %q:\n%s", want, screen)
		}
	}

	press(m, "o")
	if len(f.opened) != 1 || f.opened[0] != dashboardURL {
		t.Errorf("o opened %v, want the dashboard", f.opened)
	}

	// Pasted IDs lose spaces and case; letters that can't be in one are keys.
	m.Update(tea.PasteMsg{Content: " 0123456789ABCDEF"})
	press(m, "0", "1", "2", "x", "3", "4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f")
	if got := m.input.Value(); got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("client ID field holds %q", got)
	}
	press(m, "enter")
	if len(f.saved) != 1 || f.saved[0].ClientID != "0123456789abcdef0123456789abcdef" || f.saved[0].RedirectURI != config.DefaultRedirectURI {
		t.Fatalf("saved %+v, want the client ID and default redirect URI", f.saved)
	}
	if m.stage != stageApprove || len(f.started) != 1 || f.started[0].ClientID == "" {
		t.Fatalf("stage %v, logins %+v; want a login with the new app", m.stage, f.started)
	}
	if !strings.Contains(loginScreen(m), "INVALID_CLIENT") {
		t.Errorf("own-app login doesn't explain a bad redirect URI:\n%s", loginScreen(m))
	}

	m.Update(stepDoneMsg{seq: m.seq, step: 0})
	if !strings.Contains(loginScreen(m), "✓ play music in sptui") {
		t.Errorf("first approval not ticked off:\n%s", loginScreen(m))
	}
	m.Update(stepDoneMsg{seq: m.seq, step: 1})
	if !m.done || m.stage != stageDone {
		t.Fatalf("after both approvals: done=%v stage=%v", m.done, m.stage)
	}
}

func TestLoginSkipsToSharedApp(t *testing.T) {
	f := &fakeLogins{}
	m := newTestLogin(f.options(config.Default(), true))
	press(m, "enter")
	if m.stage != stageApp || m.appErr == "" {
		t.Fatalf("enter with no Client ID should ask for one, stage %v", m.stage)
	}
	press(m, "tab")
	if m.stage != stageApprove || len(f.saved) != 0 || f.started[0].ClientID != "" {
		t.Fatalf("tab: stage %v saved %v started %v; want the shared app", m.stage, f.saved, f.started)
	}
	// Changing their mind gives up the login under way.
	press(m, "a")
	if m.stage != stageApp || f.cancelled != 1 {
		t.Fatalf("a: stage %v, %d cancelled", m.stage, f.cancelled)
	}
}

func TestLoginWithClientIDGoesStraightToBrowser(t *testing.T) {
	f := &fakeLogins{}
	cfg := config.Default()
	cfg.ClientID = "0123456789abcdef0123456789abcdef"
	m := newTestLogin(f.options(cfg, true))
	if m.stage != stageApprove || len(f.started) != 1 || len(f.opened) != 1 {
		t.Fatalf("stage %v, %d started, opened %v", m.stage, len(f.started), f.opened)
	}
	if strings.Contains(loginScreen(m), "your app  ›") {
		t.Errorf("trail shows the app stage that was skipped:\n%s", loginScreen(m))
	}
}

func TestLoginRetriesAfterFailure(t *testing.T) {
	f := &fakeLogins{}
	m := newTestLogin(f.options(config.Default(), false))
	m.Update(stepDoneMsg{seq: m.seq, step: 0, err: errors.New("login failed: access_denied")})
	if !strings.Contains(loginScreen(m), "cancelled in the browser") || f.cancelled != 1 {
		t.Fatalf("failure not shown or login not freed (%d cancelled):\n%s", f.cancelled, loginScreen(m))
	}
	old := m.seq
	press(m, "enter")
	if len(f.started) != 2 || m.err != nil {
		t.Fatalf("enter didn't try again: %d started, err %v", len(f.started), m.err)
	}
	// A late answer from the first try is ignored.
	m.Update(stepDoneMsg{seq: old, step: 0})
	if m.step != 0 {
		t.Errorf("stale approval moved the login on")
	}
}

func TestLoginWelcomesFirst(t *testing.T) {
	f := &fakeLogins{}
	opts := f.options(config.Default(), true)
	opts.Welcome = true
	m := newTestLogin(opts)
	if m.stage != stageWelcome || len(f.started) != 0 || !strings.Contains(loginScreen(m), "Welcome") {
		t.Fatalf("stage %v, %d started; want the welcome first:\n%s", m.stage, len(f.started), loginScreen(m))
	}
	press(m, "enter")
	if m.stage != stageApp {
		t.Fatalf("after the welcome: stage %v, want the app guide", m.stage)
	}
}
