package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/update"
)

// fakeUpdater has a release out, and installs it (or fails to).
type fakeUpdater struct {
	latest    string
	installed string
	fail      error
	checks    int
	fresh     int
}

func (f *fakeUpdater) Available(ctx context.Context, fresh bool) (string, error) {
	f.checks++
	if fresh {
		f.fresh++
	}
	return f.latest, nil
}

func (f *fakeUpdater) Install(ctx context.Context, version string) error {
	if f.fail != nil {
		return f.fail
	}
	f.installed = version
	return nil
}

func newUpdateDriver(t *testing.T, u *fakeUpdater) *driver {
	t.Helper()
	return newDriverWith(t, 120, 40, demo.New(), Options{Version: "v0.1.0", Updates: u})
}

func (d *driver) screen() string {
	return ansi.Strip(d.m.View().Content)
}

func TestUpdateInstallAndRestart(t *testing.T) {
	u := &fakeUpdater{latest: "v0.2.0"}
	d := newUpdateDriver(t, u)
	if u.checks != 1 {
		t.Fatalf("checked %d times at startup, want once", u.checks)
	}
	if s := d.screen(); !strings.Contains(s, "v0.2.0 IS OUT") || !strings.Contains(s, "U update now") {
		t.Fatalf("no update box in the sidebar:\n%s", s)
	}
	if !strings.Contains(d.m.status.text, "v0.2.0 is out") {
		t.Errorf("status = %q, want a word about the update", d.m.status.text)
	}

	d.press("U")
	if u.installed != "v0.2.0" {
		t.Fatalf("installed %q, want v0.2.0", u.installed)
	}
	if s := d.screen(); !strings.Contains(s, "v0.2.0 INSTALLED") || !strings.Contains(s, "U restart into it") {
		t.Fatalf("box doesn't say it's installed:\n%s", s)
	}
	if v, installed := d.m.NewRelease(); v != "v0.2.0" || !installed {
		t.Errorf("NewRelease = %q, %v", v, installed)
	}
	if d.m.Outcome() != Quit {
		t.Fatal("quit before being asked to restart")
	}

	d.press("U")
	if d.m.Outcome() != Relaunch {
		t.Fatalf("outcome %v, want Relaunch", d.m.Outcome())
	}
}

func TestUpdateFailure(t *testing.T) {
	u := &fakeUpdater{latest: "v0.2.0", fail: fmt.Errorf("can't write to /usr/local/bin: %w", update.ErrNotWritable)}
	d := newUpdateDriver(t, u)
	d.press("U")
	s := d.screen()
	if !strings.Contains(s, "UPDATE FAILED") || !strings.Contains(s, "sudo sptui update") {
		t.Fatalf("failure not shown:\n%s", s)
	}
	u.fail = errors.New("offline")
	d.press("U")
	if !d.m.status.err || !strings.Contains(d.m.status.text, "offline") {
		t.Errorf("status = %+v, want the new failure", d.m.status)
	}
	u.fail = nil
	d.press("U")
	if u.installed != "v0.2.0" {
		t.Fatal("U didn't try again")
	}
}

func TestUpToDate(t *testing.T) {
	u := &fakeUpdater{}
	d := newUpdateDriver(t, u)
	if strings.Contains(d.screen(), "UPDATE") {
		t.Fatal("update box with nothing to update")
	}
	d.press("U")
	if u.fresh != 1 || !strings.Contains(d.m.status.text, "v0.1.0 is the latest") {
		t.Fatalf("U when up to date: %d fresh checks, status %q", u.fresh, d.m.status.text)
	}

	// A release comes out while sptui is open; U finds and installs it.
	u.latest = "v0.2.0"
	d.press("U")
	if u.installed != "v0.2.0" {
		t.Fatal("U didn't install the release it found")
	}
}

func TestUpdateChecksCanBeTurnedOff(t *testing.T) {
	u := &fakeUpdater{latest: "v0.2.0"}
	cfg := withArt(art.Off)
	cfg.CheckUpdates = false
	d := newDriverWith(t, 120, 40, demo.New(), Options{Config: cfg, Version: "v0.1.0", Updates: u})
	if u.checks != 0 || strings.Contains(d.screen(), "IS OUT") {
		t.Fatal("checked for updates with checks off")
	}

	// Turning them on in the settings looks right away.
	d.press(",")
	d.page().cursor = d.settingIndex("Check for updates")
	d.press("enter")
	if !d.m.cfg.CheckUpdates || u.checks != 1 {
		t.Fatalf("turned on: CheckUpdates %v, %d checks", d.m.cfg.CheckUpdates, u.checks)
	}
	if d.settingIndex("Update to v0.2.0") < 0 {
		t.Fatal("no update button in the settings")
	}
	// And off hides what it found.
	d.press("enter")
	if v, _ := d.m.NewRelease(); v != "" {
		t.Fatalf("still showing %s with checks off", v)
	}
}

func TestNoUpdatesForDevelopmentBuilds(t *testing.T) {
	d := newDriverWith(t, 120, 40, demo.New(), Options{Version: "dev"})
	d.press("U")
	if !d.m.status.err || !strings.Contains(d.m.status.text, "doesn't update itself") {
		t.Fatalf("status = %+v", d.m.status)
	}
}

func TestUpdateBoxClick(t *testing.T) {
	u := &fakeUpdater{latest: "v0.2.0"}
	d := newUpdateDriver(t, u)
	lines := strings.Split(d.screen(), "\n")
	y := -1
	for i, l := range lines {
		if strings.Contains(l, "U update now") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("no update box")
	}
	d.run(func() tea.Msg { return tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft} })
	if u.installed != "v0.2.0" {
		t.Fatal("clicking the box didn't update")
	}

	// The library moves down to make room; clicks still land on it.
	top := headerHeight + 1 + len(d.m.sidebarUpdateBox())
	d.run(func() tea.Msg {
		return tea.MouseClickMsg{X: 3, Y: top + d.navIndex("Recently Played"), Button: tea.MouseLeft}
	})
	if d.page().title != "Recently Played" {
		t.Fatalf("sidebar click opened %q", d.page().title)
	}
}

func TestSettingsShowVersion(t *testing.T) {
	d := newUpdateDriver(t, &fakeUpdater{})
	d.press(",")
	if s := d.screen(); !strings.Contains(s, "sptui v0.1.0") || !strings.Contains(s, "up to date") {
		t.Fatalf("no version in the settings header:\n%s", s)
	}

	d = newUpdateDriver(t, &fakeUpdater{latest: "v0.2.0"})
	d.press(",")
	if s := d.screen(); !strings.Contains(s, "sptui v0.1.0") || !strings.Contains(s, "v0.2.0 is out") {
		t.Fatalf("the header doesn't mention the new release:\n%s", s)
	}
}
