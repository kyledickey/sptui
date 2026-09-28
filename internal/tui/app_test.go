package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/spotify"
)

func withArt(mode art.Mode) config.Config {
	c := config.Default()
	c.Theme.CoverArt = mode.String()
	return c
}

// The demo backend must stay in step with the real one.
var _ Backend = (*demo.Backend)(nil)

// driver runs a model like Bubble Tea would, executing commands and feeding
// their messages back in until every command has finished.
type driver struct {
	t   *testing.T
	m   *Model
	raw []string // sequences written straight to the terminal
}

// stuck is how long run waits for a message before giving up on the
// commands still running.
const stuck = 10 * time.Second

func init() {
	tickEvery, lyricsTick, animTick = time.Hour, time.Hour, time.Hour // ticks would never settle
	settleDelay, searchDebounce, volumeDebounce, localGrace = time.Millisecond, time.Millisecond, time.Millisecond, 0
	blinkCursor = false
	loadingSpinner.FPS = time.Millisecond
	// Drop the hour-long ticks rather than leave them pending forever.
	schedule = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		if d >= time.Hour {
			return nil
		}
		return tea.Tick(d, fn)
	}
}

func newDriver(t *testing.T, w, h int) *driver {
	t.Helper()
	return newDriverWith(t, w, h, demo.New(), Options{})
}

func newDriverWith(t *testing.T, w, h int, b Backend, opts Options) *driver {
	t.Helper()
	opts.Log = logging.Discard()
	if opts.Config == (config.Config{}) {
		opts.Config = withArt(art.Off) // don't depend on the test's terminal
	}
	d := &driver{t: t, m: New(b, opts)}
	d.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	d.run(d.m.Init())
	// Most tests start from a plain list, as sptui did before its home page.
	if p := d.m.current(); p != nil && p.home {
		d.open("Liked Songs")
	}
	return d
}

// navIndex is where the sidebar item labelled label is ("playlist" finds
// the first playlist).
func (d *driver) navIndex(label string) int {
	d.t.Helper()
	for i, it := range d.m.sidebar.items {
		if it.label == label || (label == "playlist" && it.playlist != nil) {
			return i
		}
	}
	d.t.Fatalf("no sidebar item %q", label)
	return 0
}

// open opens the sidebar item labelled label.
func (d *driver) open(label string) {
	d.t.Helper()
	i := d.navIndex(label)
	d.m.sidebar.cursor = i
	d.run(d.m.openNav(i))
}

// run executes cmd and everything it leads to until things go quiet.
func (d *driver) run(cmd tea.Cmd) {
	d.t.Helper()
	msgs := make(chan tea.Msg, 256)
	pending := 0
	start := func(c tea.Cmd) {
		if c != nil {
			pending++
			go func() { msgs <- c() }()
		}
	}
	// deliver hands a message to the model, the way tea.Program would.
	var deliver func(tea.Msg)
	deliver = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				start(c)
			}
		case tea.RawMsg:
			d.raw = append(d.raw, fmt.Sprint(msg.Msg))
		default:
			if seq, ok := sequence(msg); ok {
				for _, c := range seq {
					if c != nil {
						deliver(c())
					}
				}
				return
			}
			_, next := d.m.Update(msg)
			start(next)
		}
	}
	start(cmd)
	for pending > 0 {
		select {
		case msg := <-msgs:
			pending--
			deliver(msg)
		case <-time.After(stuck):
			d.t.Fatalf("%d commands still running after %s", pending, stuck)
		}
	}
}

// press sends keys, e.g. "j", "enter", "ctrl+r".
func (d *driver) press(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		d.run(func() tea.Msg { return keyMsg(k) })
	}
}

// typeText sends each rune as a key press.
func (d *driver) typeText(s string) {
	d.t.Helper()
	for _, r := range s {
		d.run(func() tea.Msg { return tea.KeyPressMsg{Code: r, Text: string(r)} })
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	}
	if code, ok := special[k]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func (d *driver) page() *page {
	d.t.Helper()
	p := d.m.current()
	if p == nil {
		d.t.Fatal("no page on screen")
	}
	return p
}

func TestStartupLoadsLibrary(t *testing.T) {
	d := newDriver(t, 120, 40)
	if d.m.me.ID != "demo" {
		t.Fatalf("me = %+v", d.m.me)
	}
	p := d.page()
	if p.title != "Liked Songs" || len(p.rows) == 0 {
		t.Fatalf("page %q has %d rows", p.title, len(p.rows))
	}
	if len(d.m.sidebar.playlists()) != 9 {
		t.Fatalf("sidebar has %d playlists", len(d.m.sidebar.playlists()))
	}
}

func TestLazyLoadingOnScroll(t *testing.T) {
	d := newDriver(t, 120, 40)
	p := d.page()
	if len(p.rows) != 50 {
		t.Fatalf("first chunk = %d rows, want 50", len(p.rows))
	}
	d.press("G")
	if len(p.rows) <= 50 {
		t.Fatalf("scrolling to the bottom didn't load more (have %d)", len(p.rows))
	}
}

func TestPlayTrack(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("j", "enter")
	want := d.page().rows[1].track
	tr := d.m.player.track()
	if tr == nil || tr.URI != want.URI || !d.m.player.playing() {
		t.Fatalf("playing %+v, want %q", tr, want.Name)
	}
	d.press("space")
	if d.m.player.playing() {
		t.Fatal("space didn't pause")
	}
}

func TestSearch(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("/")
	d.typeText("neon")
	d.press("enter")
	p := d.page()
	if !p.isSearch || len(p.rows) == 0 {
		t.Fatalf("search page: %q with %d rows (err %v)", p.title, len(p.rows), p.err)
	}
	if r, ok := p.selected(); !ok || r.kind == kindHeader {
		t.Fatal("cursor should skip the section header")
	}
	if d.m.inputMode != inputNone {
		t.Fatal("enter should leave the search box")
	}
}

func TestOpenArtistThenBack(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("tab", "G", "k", "k", "k", "k", "k", "k", "k", "k", "k", "k") // walk up from the last playlist
	d.m.sidebar.cursor = d.navIndex("Artists")
	d.press("enter")
	if d.page().title != "Artists" {
		t.Fatalf("on %q", d.page().title)
	}
	d.press("enter")
	if d.page().kind != kindAlbum || len(d.m.stack) != 2 {
		t.Fatalf("artist page not opened: %q depth %d", d.page().title, len(d.m.stack))
	}
	d.press("esc")
	if len(d.m.stack) != 1 {
		t.Fatalf("esc didn't go back, depth %d", len(d.m.stack))
	}
}

func TestFilter(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("f")
	d.typeText("zzzz-no-match")
	p := d.page()
	if len(p.visible) != 0 {
		t.Fatalf("filter left %d rows", len(p.visible))
	}
	d.press("esc")
	if p.filter != "" || len(p.visible) != len(p.rows) {
		t.Fatal("esc should clear the filter")
	}
}

func TestForbiddenPlaylistCanStillPlay(t *testing.T) {
	d := newDriver(t, 120, 40)
	sb := &d.m.sidebar
	sb.cursor = len(sb.items) - 1 // "Chill Vibes", owned by Spotify
	d.press("tab", "enter")
	p := d.page()
	if !isForbidden(p.err) {
		t.Fatalf("err = %v, want 403", p.err)
	}
	d.press("enter")
	if d.m.player.track() == nil {
		t.Fatal("enter on a forbidden playlist should play it")
	}
}

func TestMenuAddToQueue(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("enter") // start playback so there is an active device
	d.press("j", "m")
	if d.m.menu == nil || d.m.menu.items[1].label != "Add to queue" {
		t.Fatalf("menu = %+v", d.m.menu)
	}
	d.press("j", "enter")
	if d.m.menu != nil || d.m.status.err || !strings.HasPrefix(d.m.status.text, "Queued") {
		t.Fatalf("status = %+v", d.m.status)
	}
}

// TestLayoutFits checks that every screen fills the terminal exactly, with no
// line overflowing, at a range of sizes and states.
func TestLayoutFits(t *testing.T) {
	sizes := [][2]int{{60, 16}, {80, 24}, {100, 30}, {140, 45}, {220, 60}}
	for _, sz := range sizes {
		d := newDriver(t, sz[0], sz[1])
		check := func(state string) {
			t.Helper()
			content := d.m.View().Content
			lines := strings.Split(content, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%dx%d %s: %d lines", sz[0], sz[1], state, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%dx%d %s: line %d is %d wide", sz[0], sz[1], state, i, w)
				}
			}
		}
		check("library")
		d.press("enter")
		check("playing")
		d.press("m")
		check("menu")
		d.press("esc", "?")
		check("help")
		d.press("esc", "/")
		d.typeText("a")
		d.press("enter")
		check("search")
		d.press("f")
		check("filter")
	}
}

func TestMouse(t *testing.T) {
	d := newDriver(t, 120, 40)
	top := headerHeight + 1
	x := d.m.sidebarWidth() + 5
	d.run(func() tea.Msg { return tea.MouseWheelMsg{X: x, Y: 10, Button: tea.MouseWheelDown} })
	if d.page().cursor != wheelStep {
		t.Fatalf("wheel moved cursor to %d", d.page().cursor)
	}
	row := top + pageHeader + 2 // third visible row
	click := func() tea.Msg { return tea.MouseClickMsg{X: x, Y: row, Button: tea.MouseLeft} }
	d.run(click)
	if d.page().cursor != 2 {
		t.Fatalf("click selected %d, want 2", d.page().cursor)
	}
	d.run(click)
	if tr := d.m.player.track(); tr == nil || tr.URI != d.page().rows[2].track.URI {
		t.Fatal("second click should play the row")
	}
	// Clicking a sidebar item opens it.
	d.run(func() tea.Msg {
		return tea.MouseClickMsg{X: 3, Y: top + d.navIndex("Recently Played"), Button: tea.MouseLeft}
	})
	if d.page().title != "Recently Played" {
		t.Fatalf("sidebar click opened %q", d.page().title)
	}
}

func TestUnlikeRemovesFromLikedSongs(t *testing.T) {
	d := newDriver(t, 120, 40)
	p := d.page()
	first := p.rows[0].track
	total := p.total
	d.press("l")
	if len(p.rows) == 0 || p.rows[0].track.URI == first.URI || p.total != total-1 {
		t.Fatalf("unliked song still listed (status %q)", d.m.status.text)
	}
	if !strings.HasPrefix(d.m.status.text, "Removed") {
		t.Fatalf("status = %q", d.m.status.text)
	}
}

func TestFilterOnSearchPageDoesNotSearch(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("/")
	d.typeText("neon")
	d.press("enter", "f")
	d.typeText("hollow")
	d.press("enter")
	p := d.page()
	if p.query != "neon" || p.filter != "hollow" {
		t.Fatalf("query %q filter %q; want neon / hollow", p.query, p.filter)
	}
	d.press("esc", "ctrl+r") // clear the filter, then reload the results
	if p.query != "neon" || len(p.rows) == 0 {
		t.Fatalf("reload searched for %q", p.query)
	}
}

func TestOwnPlaylistCannotBeUnfollowedByAccident(t *testing.T) {
	d := newDriver(t, 120, 40)
	before := len(d.m.sidebar.playlists())
	d.m.sidebar.cursor = d.navIndex("playlist") // owned by the demo user
	d.press("tab", "enter", "esc")
	if p := d.page(); p.self != nil {
		d.m.menu = d.m.actionsMenu(*p.self, nil)
		for _, it := range d.m.menu.items {
			if strings.Contains(it.label, "remove") {
				t.Fatalf("menu offers %q on the user's own playlist", it.label)
			}
		}
	}
	if len(d.m.sidebar.playlists()) != before {
		t.Fatal("playlists changed")
	}
}

func TestPlaysOnLocalSpeaker(t *testing.T) {
	b := demo.New()
	// Something is already playing on another device.
	ctx := t.Context()
	if err := b.Transfer(ctx, "dev-kitchen", true); err != nil {
		t.Fatal(err)
	}
	d := newDriverWith(t, 120, 40, b, Options{LocalDevice: "Demo Laptop"})
	if got := d.m.player.state.Device.Name; got != "Kitchen Speaker" {
		t.Fatalf("setup: playing on %q", got)
	}
	d.press("enter")
	if got := d.m.player.state.Device.Name; got != "Demo Laptop" {
		t.Fatalf("enter played on %q, want the local speaker", got)
	}

	// Choosing another device on purpose sticks.
	d.press("d")
	for i, it := range d.m.menu.items {
		if it.label == "Pocket Phone" {
			d.m.menu.cursor = i
		}
	}
	d.press("enter", "j", "enter")
	if got := d.m.player.state.Device.Name; got != "Pocket Phone" {
		t.Fatalf("after picking the phone, played on %q", got)
	}
}

// openFirstAlbum goes Albums → first album.
func (d *driver) openFirstAlbum() {
	d.t.Helper()
	d.m.sidebar.cursor = d.navIndex("Albums")
	d.press("tab", "enter", "enter")
	if p := d.page(); p.cover == "" || !p.noAlbum {
		d.t.Fatalf("not on an album page with a cover: %q", p.title)
	}
}

// TestAlbumPageMenu checks that "more" acts on the album, not the song under
// the cursor, and can add the whole album to a playlist.
func TestAlbumPageMenu(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.openFirstAlbum()
	p := d.page()
	d.press(".")
	if d.m.menu == nil || d.m.menu.title != p.self.name() {
		t.Fatalf("menu = %+v, want the album's", d.m.menu)
	}
	pick := func(label string) {
		t.Helper()
		for i, it := range d.m.menu.items {
			if it.label == label {
				d.m.menu.cursor = i
				d.press("enter")
				return
			}
		}
		t.Fatalf("menu has no %q", label)
	}
	pick("Add to playlist")
	pl := d.m.menu.items[1].label // after "New playlist"
	before := d.m.sidebar.playlists()
	d.press("j", "enter")
	if d.m.status.err || !strings.HasPrefix(d.m.status.text, "Added “"+p.self.name()+"” to "+pl) {
		t.Fatalf("status = %+v", d.m.status)
	}
	d.run(d.m.reloadPlaylists())
	for i, after := range d.m.sidebar.playlists() {
		if after.Name == pl && after.TrackCount() != before[i].TrackCount()+len(p.rows) {
			t.Fatalf("%s has %d songs, want %d", pl, after.TrackCount(), before[i].TrackCount()+len(p.rows))
		}
	}

	d.press(".")
	pick("Like all songs")
	if d.m.status.err || !strings.HasPrefix(d.m.status.text, "Liked every song") {
		t.Fatalf("status = %+v", d.m.status)
	}
}

// TestNewPlaylist makes a playlist from the sidebar, then another with an
// album in it from the album's menu.
func TestNewPlaylist(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.m.sidebar.cursor = d.navIndex("New playlist")
	d.press("tab", "enter")
	if d.m.menu == nil || d.m.menu.answer == nil {
		t.Fatalf("menu = %+v, want a name prompt", d.m.menu)
	}
	d.press("enter") // no name yet: nothing happens
	if d.m.menu == nil {
		t.Fatal("an empty name closed the prompt")
	}
	d.typeText("quiet mornings")
	d.press("enter")
	if d.m.menu != nil || d.m.inputMode != inputNone {
		t.Fatal("the prompt stayed open")
	}
	if d.m.status.text != "Created “quiet mornings”" {
		t.Fatalf("status = %+v", d.m.status)
	}
	if pls := d.m.sidebar.playlists(); len(pls) == 0 || pls[0].Name != "quiet mornings" {
		t.Fatalf("sidebar playlists = %+v", pls)
	}

	d.m.focus = focusMain
	d.openFirstAlbum()
	album := d.page()
	d.press(".")
	for i, it := range d.m.menu.items {
		if it.label == "Add to playlist" {
			d.m.menu.cursor = i
		}
	}
	d.press("enter", "enter") // "New playlist" comes first
	d.typeText("an album")
	d.press("enter")
	if d.m.status.err || d.m.status.text != "Added “"+album.self.name()+"” to an album" {
		t.Fatalf("status = %+v", d.m.status)
	}
	pl := d.m.sidebar.playlists()[0]
	if pl.Name != "an album" || pl.TrackCount() != len(album.rows) {
		t.Fatalf("new playlist = %s with %d songs, want %d", pl.Name, pl.TrackCount(), len(album.rows))
	}
}

// TestEditPlaylist renames, describes and deletes the user's own playlist
// from its page, keeping the page and sidebar in step.
func TestEditPlaylist(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.open("Morning Coffee") // the demo user's
	p := d.page()
	pick := func(label string) {
		t.Helper()
		d.press(".")
		for i, it := range d.m.menu.items {
			if it.label == label {
				d.m.menu.cursor = i
				d.press("enter")
				return
			}
		}
		t.Fatalf("menu has no %q", label)
	}
	clear := func() {
		for range 40 {
			d.run(func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyBackspace} })
		}
	}

	pick("Rename")
	if d.m.input.Value() != "Morning Coffee" {
		t.Fatalf("rename starts at %q", d.m.input.Value())
	}
	clear()
	d.typeText("Evening Tea")
	d.press("enter")
	if p.title != "Evening Tea" || d.m.sidebar.playlists()[0].Name != "Evening Tea" {
		t.Fatalf("title %q, sidebar %q", p.title, d.m.sidebar.playlists()[0].Name)
	}
	if it := d.m.sidebar.items[d.m.sidebar.active]; it.label != "Evening Tea" {
		t.Fatalf("sidebar highlights %q after the reload", it.label)
	}

	pick("Edit description")
	clear()
	d.typeText("for winding down")
	d.press("enter")
	if p.about != "for winding down" || d.m.sidebar.playlists()[0].Description != "for winding down" {
		t.Fatalf("about = %q", p.about)
	}
	pick("Edit description")
	clear()
	d.press("enter") // a blank description is allowed
	if p.about != "" {
		t.Fatalf("about = %q, want it cleared", p.about)
	}

	before := len(d.m.sidebar.playlists())
	pick("Delete playlist")
	d.press("enter") // "Keep it" comes first
	if len(d.m.sidebar.playlists()) != before {
		t.Fatal("keeping it deleted it")
	}
	pick("Delete playlist")
	d.press("j", "enter")
	if len(d.m.sidebar.playlists()) != before-1 || d.m.status.text != "Deleted “Evening Tea”" {
		t.Fatalf("status %q, %d playlists", d.m.status.text, len(d.m.sidebar.playlists()))
	}
	if cur := d.m.current(); cur == nil || !cur.home {
		t.Fatal("still on the deleted playlist")
	}
}

func TestCoverArtBlocks(t *testing.T) {
	d := newDriverWith(t, 120, 40, demo.New(), Options{Config: withArt(art.Blocks)})
	d.openFirstAlbum()
	view := d.m.View().Content
	if !strings.Contains(view, "▀") {
		t.Fatal("album page has no block art")
	}
	d.press("enter") // play: the player bar gets a thumbnail too
	key := coverKey{d.m.thumbURL(), thumbRows * 2, thumbRows}
	if _, ok := d.m.covers.ready[key]; !ok {
		t.Fatal("no thumbnail for the playing track")
	}
}

func TestCoverArtKitty(t *testing.T) {
	d := newDriverWith(t, 120, 40, demo.New(), Options{Config: withArt(art.Kitty)})
	d.openFirstAlbum()
	if len(d.raw) == 0 || !strings.Contains(d.raw[len(d.raw)-1], "U=1") {
		t.Fatalf("no kitty image was sent: %q", d.raw)
	}
	if !strings.ContainsRune(d.m.View().Content, '\U0010EEEE') {
		t.Fatal("album page has no kitty placeholders")
	}
	sent := len(d.raw)
	d.press("esc") // leave the page: its image is freed
	if len(d.raw) == sent || !strings.Contains(d.raw[len(d.raw)-1], "a=d") {
		t.Fatalf("image not freed after leaving the page: %q", d.raw[sent:])
	}
	if len(d.m.covers.ids) != 0 {
		t.Fatalf("still tracking %d images", len(d.m.covers.ids))
	}
}

func TestCoverLayoutFits(t *testing.T) {
	for _, mode := range []art.Mode{art.Blocks, art.Kitty} {
		for _, sz := range [][2]int{{60, 16}, {80, 24}, {100, 30}, {160, 50}} {
			d := newDriverWith(t, sz[0], sz[1], demo.New(), Options{Config: withArt(mode)})
			d.openFirstAlbum()
			d.press("enter")
			lines := strings.Split(d.m.View().Content, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%v %dx%d: %d lines", mode, sz[0], sz[1], len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%v %dx%d: line %d is %d wide", mode, sz[0], sz[1], i, w)
				}
			}
			// Clicking a row still lands on the right one below the cover.
			top := headerHeight + 1 + d.m.headerHeight(d.page())
			d.run(func() tea.Msg {
				return tea.MouseClickMsg{X: d.m.sidebarWidth() + 5, Y: top + 1, Button: tea.MouseLeft}
			})
			if d.page().cursor != 1 && d.m.listHeight() > 1 {
				t.Errorf("%v %dx%d: click selected row %d", mode, sz[0], sz[1], d.page().cursor)
			}
		}
	}
}

func TestAccountMenuLogsOut(t *testing.T) {
	for _, c := range []struct {
		pick int
		want Outcome
	}{{0, LogIn}, {1, LogOut}} {
		d := newDriver(t, 120, 40)
		d.press("u")
		if d.m.menu == nil || !strings.Contains(d.m.menu.title, "Demo Listener") {
			t.Fatalf("account menu = %+v", d.m.menu)
		}
		d.m.menu.cursor = c.pick
		d.press("enter")
		if d.m.Outcome() != c.want {
			t.Fatalf("picked %d: outcome %v, want %v", c.pick, d.m.Outcome(), c.want)
		}
	}
	if d := newDriver(t, 120, 40); d.m.Outcome() != Quit {
		t.Fatal("default outcome should be Quit")
	}
}

// rateLimited fails the first Me call the way Spotify does when busy.
type rateLimited struct {
	*demo.Backend
	calls int
}

func (r *rateLimited) Me(ctx context.Context) (spotify.User, error) {
	r.calls++
	if r.calls == 1 {
		return spotify.User{}, &spotify.Error{Status: 429, Message: "rate limited", RetryAfter: 20 * time.Millisecond}
	}
	return r.Backend.Me(ctx)
}

func TestRateLimitedStartupRetries(t *testing.T) {
	b := &rateLimited{Backend: demo.New()}
	d := newDriverWith(t, 120, 40, b, Options{})
	if b.calls < 2 || d.m.me.ID != "demo" || d.m.current() == nil {
		t.Fatalf("after a 429, profile loaded %d times, me=%q", b.calls, d.m.me.ID)
	}
}

// findLyrics plays songs until one has synced lyrics (the demo makes some
// instrumental or lyric-less).
func (d *driver) playSongWithLyrics() {
	d.t.Helper()
	for range 10 {
		if d.m.lyrics.lyrics.Synced && len(d.m.lyrics.lyrics.Lines) > 0 {
			return
		}
		d.press("n")
		d.run(func() tea.Msg { return refreshMsg{} })
	}
	d.t.Fatal("no song with lyrics found")
}

func TestNowPlayingView(t *testing.T) {
	d := newDriverWith(t, 140, 45, demo.New(), Options{Config: withArt(art.Blocks)})
	d.press("enter", "o")
	if !d.m.showingNowPlaying() {
		t.Fatal("o didn't open the now-playing view")
	}
	d.playSongWithLyrics()
	view := ansi.Strip(d.m.View().Content)
	tr := d.m.player.track()
	for _, want := range []string{"2 lyrics", "3 up next", tr.Name, "▀", "▮"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if len(d.page().rows) == 0 {
		t.Error("queue is empty")
	}
	// The current lyric line is highlighted once the song reaches it.
	first := d.m.lyrics.lyrics.Lines[0]
	d.m.player.state.ProgressMS = int(first.At.Milliseconds()) + 100
	d.m.player.at = time.Now()
	d.m.player.state.IsPlaying = false
	if cur := d.m.lyrics.lyrics.Current(d.m.player.progress(time.Now())); cur != 0 {
		t.Fatalf("current line = %d", cur)
	}
	if first.Text != "" && !strings.Contains(d.m.View().Content, first.Text) {
		t.Errorf("current line %q not shown", first.Text)
	}
	d.press("o")
	if d.m.showingNowPlaying() {
		t.Fatal("o again should close the view")
	}
}

func TestNowPlayingLayoutFits(t *testing.T) {
	for _, mode := range []art.Mode{art.Off, art.Blocks, art.Kitty} {
		for _, sz := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {220, 60}} {
			for _, panels := range []string{"123", "1"} {
				cfg := withArt(mode)
				cfg.Theme.NowPlayingPanels = panels
				d := newDriverWith(t, sz[0], sz[1], demo.New(), Options{Config: cfg})
				d.press("o") // nothing playing yet
				check := func(state string) {
					t.Helper()
					lines := strings.Split(d.m.View().Content, "\n")
					if len(lines) != sz[1] {
						t.Errorf("%v %s %dx%d %s: %d lines", mode, panels, sz[0], sz[1], state, len(lines))
					}
					for i, l := range lines {
						if w := lipgloss.Width(l); w > sz[0] {
							t.Errorf("%v %s %dx%d %s: line %d is %d wide", mode, panels, sz[0], sz[1], state, i, w)
						}
					}
				}
				check("idle")
				d.press("esc", "enter", "o")
				if panels == "1" {
					d.press("n")
					d.run(func() tea.Msg { return refreshMsg{} })
				} else {
					d.playSongWithLyrics()
				}
				check("playing")
			}
		}
	}
}

// cachedLibrary answers like sptui's cache does: from a saved copy, noting
// how old it is.
type cachedLibrary struct {
	*demo.Backend
	age   time.Duration
	stale bool
}

func (c *cachedLibrary) SavedTracks(ctx context.Context, offset int) (spotify.Page[spotify.Track], error) {
	spotify.NoteOrigin(ctx, time.Now().Add(-c.age), c.stale)
	return c.Backend.SavedTracks(ctx, offset)
}

func (c *cachedLibrary) Playlists(ctx context.Context, offset int) (spotify.Page[spotify.Playlist], error) {
	spotify.NoteOrigin(ctx, time.Now().Add(-c.age), c.stale)
	return c.Backend.Playlists(ctx, offset)
}

func TestCacheMarker(t *testing.T) {
	d := newDriverWith(t, 140, 40, &cachedLibrary{Backend: demo.New(), age: 5 * time.Minute}, Options{})
	view := d.m.View().Content
	if strings.Count(view, "◷ 5m ago") != 2 { // Liked Songs header and the playlists heading
		t.Fatalf("expected the cache marker on the page and sidebar:\n%s", view)
	}
	if strings.Contains(view, "Spotify busy") {
		t.Fatal("a fresh-enough cached copy shouldn't say Spotify is busy")
	}

	d = newDriverWith(t, 140, 40, &cachedLibrary{Backend: demo.New(), age: 3 * time.Hour, stale: true}, Options{})
	if !strings.Contains(d.m.View().Content, "◷ 3h ago · Spotify busy") {
		t.Fatal("a saved copy served on error should say so")
	}

	d = newDriverWith(t, 140, 40, &cachedLibrary{Backend: demo.New(), age: 20 * time.Second}, Options{})
	if strings.Contains(d.m.View().Content, " ago") {
		t.Fatal("very recent data needs no marker")
	}
}

// settingIndex finds a setting on the settings screen by label.
func (d *driver) settingIndex(label string) int {
	d.t.Helper()
	for i, s := range d.m.settings() {
		if s.label == label {
			return i
		}
	}
	d.t.Fatalf("no setting %q", label)
	return -1
}

func TestSettings(t *testing.T) {
	var saved []config.Config
	opts := Options{
		Config:     withArt(art.Blocks),
		SaveConfig: func(c config.Config) error { saved = append(saved, c); return nil },
	}
	d := newDriverWith(t, 120, 40, demo.New(), opts)
	d.press(",")
	p := d.page()
	if !p.settings {
		t.Fatal(", didn't open settings")
	}

	// Choices cycle and apply at once.
	p.cursor = d.settingIndex("Album art")
	d.press("right") // pixels → off
	if d.m.cfg.Theme.CoverArt != "off" || d.m.covers.mode != art.Off {
		t.Fatalf("album art = %q, covers %v", d.m.cfg.Theme.CoverArt, d.m.covers.mode)
	}
	p.cursor = d.settingIndex("Accent color")
	before := d.m.st.accent
	d.press("enter")
	if d.m.cfg.Theme.Accent != "#4da3ff" || d.m.st.accent == before {
		t.Fatalf("accent = %q, not applied", d.m.cfg.Theme.Accent)
	}
	if len(saved) != 2 || saved[1].Theme.Accent != "#4da3ff" {
		t.Fatalf("saved %d times: %+v", len(saved), saved)
	}
	if d.m.needsRestart {
		t.Fatal("appearance changes shouldn't need a restart")
	}

	// A bad Client ID is refused; a good one is saved and asks for a restart.
	p.cursor = d.settingIndex("Your own Spotify app")
	d.press("enter")
	d.typeText("nope")
	d.press("enter")
	if d.m.cfg.ClientID != "" || !d.m.status.err {
		t.Fatalf("bad client ID accepted: %q", d.m.cfg.ClientID)
	}
	id := "0123456789abcdef0123456789abcdef"
	d.press("enter")
	d.typeText(id)
	d.press("enter")
	if d.m.cfg.ClientID != id || d.m.cfg.RedirectURI != config.DefaultRedirectURI || !d.m.needsRestart {
		t.Fatalf("client ID = %q, redirect %q, restart %v", d.m.cfg.ClientID, d.m.cfg.RedirectURI, d.m.needsRestart)
	}

	// The restart button appears and ends the UI with Restart.
	p.cursor = d.settingIndex("Restart sptui to apply changes")
	d.press("enter")
	if d.m.Outcome() != Restart {
		t.Fatalf("outcome = %v", d.m.Outcome())
	}
}

func TestSettingsLayoutFits(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {80, 24}, {140, 45}} {
		d := newDriverWith(t, sz[0], sz[1], demo.New(), Options{})
		d.press(",")
		for _, label := range []string{"Accent color", "Your own Spotify app"} {
			d.page().cursor = d.settingIndex(label)
			lines := strings.Split(d.m.View().Content, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%dx%d on %q: %d lines", sz[0], sz[1], label, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%dx%d on %q: line %d is %d wide", sz[0], sz[1], label, i, w)
				}
			}
		}
	}
}

// flakyLyrics is down for the first request, like LRCLIB having a moment.
type flakyLyrics struct {
	*demo.Backend
	calls int
}

func (f *flakyLyrics) Lyrics(ctx context.Context, t spotify.Track) (lyrics.Lyrics, error) {
	f.calls++
	if f.calls == 1 {
		return lyrics.Lyrics{}, fmt.Errorf("%w: lrclib 503 Service Unavailable", lyrics.ErrUnavailable)
	}
	return lyrics.Lyrics{Synced: true, Lines: []lyrics.Line{{At: time.Second, Text: "we're back"}}}, nil
}

func TestLyricsRetryWhenServiceIsDown(t *testing.T) {
	old := lyricsRetries
	lyricsRetries = []time.Duration{time.Hour} // retry by hand below
	t.Cleanup(func() { lyricsRetries = old })

	b := &flakyLyrics{Backend: demo.New()}
	d := newDriverWith(t, 120, 40, b, Options{})
	d.press("enter", "o")
	view := d.m.View().Content
	if !strings.Contains(view, "Lyrics are unavailable right now") || strings.Contains(view, "503") {
		t.Fatalf("expected a friendly notice without the raw error:\n%s", view)
	}
	d.run(func() tea.Msg { return lyricsRetryMsg{uri: d.m.player.track().URI} })
	if b.calls != 2 || !strings.Contains(d.m.View().Content, "we're back") {
		t.Fatalf("retry didn't load lyrics (calls %d)", b.calls)
	}
	// Found lyrics are remembered: coming back doesn't ask again.
	d.press("esc", "o")
	if b.calls != 2 {
		t.Fatalf("lyrics fetched again (calls %d)", b.calls)
	}
}

func TestKeepAwake(t *testing.T) {
	var got []bool
	opts := Options{Config: withArt(art.Off), KeepAwake: func(on bool) { got = append(got, on) }}
	d := newDriverWith(t, 120, 40, demo.New(), opts)
	if len(got) != 0 {
		t.Fatalf("asked to stay awake before anything played: %v", got)
	}
	d.press("enter") // play
	if len(got) != 1 || !got[0] {
		t.Fatalf("playing: %v", got)
	}
	d.press("space") // pause
	if len(got) != 2 || got[1] {
		t.Fatalf("paused: %v", got)
	}

	// "always" keeps it awake while paused; "off" never does.
	d.press(",")
	d.page().cursor = d.settingIndex("Keep screen awake")
	d.press("right") // while playing → always
	if d.m.cfg.Player.KeepAwake != "always" || !got[len(got)-1] {
		t.Fatalf("always: setting %q, calls %v", d.m.cfg.Player.KeepAwake, got)
	}
	d.press("right") // → off
	if d.m.cfg.Player.KeepAwake != "off" || got[len(got)-1] {
		t.Fatalf("off: setting %q, calls %v", d.m.cfg.Player.KeepAwake, got)
	}
}

func TestSearchFindsMyPlaylists(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("/")
	d.typeText("coffee")
	d.press("enter")
	p := d.page()
	if len(p.rows) < 2 || p.rows[0].header != "Your playlists" || p.rows[1].playlist.Name != "Morning Coffee" {
		t.Fatalf("first results: %+v", p.rows[:min(2, len(p.rows))])
	}
	if r, _ := p.selected(); r.playlist.Name != "Morning Coffee" {
		t.Fatalf("cursor on %q", r.name())
	}
	d.press("enter") // opens it
	if d.page().title != "Morning Coffee" {
		t.Fatalf("opened %q", d.page().title)
	}
}

func TestNowPlayingPanels(t *testing.T) {
	var saved config.Config
	opts := Options{Config: withArt(art.Blocks), SaveConfig: func(c config.Config) error { saved = c; return nil }}
	d := newDriverWith(t, 120, 40, demo.New(), opts)
	d.press("enter", "o")
	d.press("2") // hide lyrics
	view := ansi.Strip(d.m.View().Content)
	if strings.Contains(view, "2 lyrics") || !strings.Contains(view, "3 up next") {
		t.Fatal("2 didn't hide the lyrics")
	}
	if saved.Theme.NowPlayingPanels != "13" {
		t.Fatalf("saved panels %q", saved.Theme.NowPlayingPanels)
	}
	d.press("1", "3") // lyrics and track hidden, queue too: refused, one stays
	if d.m.cfg.Theme.NowPlayingPanels != "3" || !d.m.status.err {
		t.Fatalf("panels %q, status %+v", d.m.cfg.Theme.NowPlayingPanels, d.m.status)
	}
	if !strings.Contains(d.m.View().Content, d.m.player.track().Name) {
		t.Fatal("with the track panel hidden, the strip should show the song")
	}
	d.press("2", "1")
	if d.m.cfg.Theme.NowPlayingPanels != "123" {
		t.Fatalf("panels %q", d.m.cfg.Theme.NowPlayingPanels)
	}
}

func TestNowPlayingPanelLayoutsFit(t *testing.T) {
	for _, panels := range []string{"1", "2", "3", "12", "13", "23", "123"} {
		for _, size := range []string{"small", "medium", "large"} {
			for _, sz := range [][2]int{{60, 16}, {100, 30}, {200, 55}} {
				cfg := withArt(art.Kitty)
				cfg.Theme.NowPlayingPanels, cfg.Theme.NowPlayingCover = panels, size
				d := newDriverWith(t, sz[0], sz[1], demo.New(), Options{Config: cfg})
				d.press("enter", "o")
				lines := strings.Split(d.m.View().Content, "\n")
				if len(lines) != sz[1] {
					t.Errorf("%s %s %dx%d: %d lines", panels, size, sz[0], sz[1], len(lines))
				}
				for i, l := range lines {
					if w := lipgloss.Width(l); w != sz[0] {
						t.Errorf("%s %s %dx%d: line %d is %d wide", panels, size, sz[0], sz[1], i, w)
						break
					}
				}
			}
		}
	}
}

// sentImages decodes the size of every kitty image sptui wrote to the
// terminal.
func sentImages(t *testing.T, raw []string) []image.Config {
	t.Helper()
	var out []image.Config
	for _, seq := range raw {
		if !strings.Contains(seq, "a=T") {
			continue
		}
		var payload strings.Builder
		for _, chunk := range strings.Split(seq, "\x1b_G")[1:] {
			payload.WriteString(strings.TrimSuffix(chunk[strings.IndexByte(chunk, ';')+1:], "\x1b\\"))
		}
		data, err := base64.StdEncoding.DecodeString(payload.String())
		if err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode png: %v", err)
		}
		out = append(out, cfg)
	}
	return out
}

func TestKittyImagesFillTheirCells(t *testing.T) {
	d := newDriverWith(t, 140, 45, demo.New(), Options{Config: withArt(art.Kitty)})
	d.run(func() tea.Msg { return uv.CellSizeEvent{Width: 9, Height: 18} })
	d.press("enter", "o")
	l := d.m.nowPlayingLayout()
	var img image.Config
	for _, sent := range sentImages(t, d.raw) {
		if sent.Width > img.Width {
			img = sent // the big cover, not the player bar's thumbnail
		}
	}
	if img.Width != l.coverCols*9 || img.Height != l.coverRows*18 {
		t.Fatalf("sent %dx%d px for %dx%d cells of 9x18", img.Width, img.Height, l.coverCols, l.coverRows)
	}
	// Square in pixels, give or take half a cell.
	if diff := img.Width - img.Height; diff < -5 || diff > 5 {
		t.Fatalf("cover box %dx%d px isn't square", img.Width, img.Height)
	}
}

func TestPodcasts(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("tab")
	d.m.sidebar.cursor = d.navIndex("Podcasts")
	d.press("enter")
	if p := d.page(); p.title != "Podcasts" || p.kind != kindShow || len(p.rows) == 0 {
		t.Fatalf("on %q with %d rows", p.title, len(p.rows))
	}
	d.press("enter")
	p := d.page()
	if !p.episodes || len(d.m.stack) != 2 || len(p.rows) == 0 {
		t.Fatalf("podcast page not opened: %q depth %d", p.title, len(d.m.stack))
	}
	if content := d.m.View().Content; !strings.Contains(content, "RELEASED") {
		t.Fatalf("episode columns missing:\n%s", content)
	}
	d.press("enter")
	tr := d.m.player.track()
	if tr == nil || !tr.IsEpisode() || tr.URI != p.rows[0].track.URI {
		t.Fatalf("playing %+v, want the first episode", tr)
	}
	d.press("o")
	if d.m.lyrics.uri == tr.URI || !strings.Contains(d.m.View().Content, "Podcasts don't have lyrics") {
		t.Fatal("lyrics looked up for an episode")
	}
	d.press("esc", "l")
	if !strings.HasSuffix(d.m.status.text, "Your Episodes") {
		t.Fatalf("status = %q", d.m.status.text)
	}
}

func TestSearchFindsPodcasts(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("/")
	d.typeText("signal")
	d.press("enter")
	var sections []string
	for _, r := range d.page().rows {
		if r.kind == kindHeader {
			sections = append(sections, r.header)
		}
	}
	if !slices.Contains(sections, "Podcasts") {
		t.Fatalf("sections = %v", sections)
	}
}
