package tui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
)

// newHomeDriver starts sptui like a user would, on the home page.
func newHomeDriver(t *testing.T, w, h int, cfg config.Config) *driver {
	t.Helper()
	d := &driver{t: t, m: New(demo.New(), Options{Config: cfg, Log: logging.Discard()})}
	d.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	d.run(d.m.Init())
	return d
}

func TestHome(t *testing.T) {
	d := newHomeDriver(t, 120, 40, withArt(art.Off))
	p := d.page()
	if !p.home || p.homeData == nil || p.homeData.plays == 0 {
		t.Fatalf("didn't start on a loaded home page: %q", p.title)
	}
	sections := homeSections(p)
	for _, s := range []string{homeJump, homeRepeat, homeEpisodes} {
		if len(sections[s]) == 0 {
			t.Errorf("home has no %q", s)
		}
	}
	view := ansi.Strip(d.m.View().Content)
	for _, want := range []string{"JUMP BACK IN", "ON REPEAT", "NEW EPISODES", "plays this week"} {
		if !strings.Contains(view, want) {
			t.Errorf("home lacks %q", want)
		}
	}

	// The tiles are walked with left and right, and open what they show.
	first := p.cursor
	d.press("right")
	if p.cursor == first || !homeTile(p) {
		t.Fatalf("right didn't move along the tiles (cursor %d)", p.cursor)
	}
	d.press("left")
	if p.cursor != first {
		t.Fatalf("left didn't come back (cursor %d)", p.cursor)
	}
	tile, _ := p.selected()
	d.press("enter")
	if d.page().title != tile.name() {
		t.Fatalf("enter on a tile opened %q, want %q", d.page().title, tile.name())
	}

	// Songs on repeat play.
	d.press("esc")
	p = d.page()
	p.cursor = sections[homeRepeat][0]
	want, _ := p.selected()
	d.press("enter")
	if tr := d.m.player.track(); tr == nil || tr.URI != want.track.URI {
		t.Fatalf("playing %+v, want %q", tr, want.track.Name)
	}
}

func TestPlayAndShuffleWholePage(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.openFirstAlbum()
	album := d.page()
	d.press("P")
	if tr := d.m.player.track(); tr == nil || tr.Album.URI != album.context {
		t.Fatalf("P played %+v, want the album", tr)
	}
	d.press("S")
	if !d.m.player.state.ShuffleState {
		t.Fatal("S didn't shuffle")
	}
}

func TestSearchTabs(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("/")
	d.typeText("neon")
	d.press("enter", "3")
	p := d.page()
	if p.tab != 2 || len(p.visible) == 0 {
		t.Fatalf("tab %d with %d rows", p.tab, len(p.visible))
	}
	for _, i := range p.visible {
		if p.rows[i].kind != kindArtist {
			t.Fatalf("artists tab shows a %s", kindNoun[p.rows[i].kind])
		}
	}
	d.press("1")
	if !strings.Contains(ansi.Strip(d.m.View().Content), "TOP RESULT") {
		t.Error("no top result card on the all tab")
	}
}

func TestArtistTimeline(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.open("Artists")
	d.press("enter")
	p := d.page()
	if !p.timeline || len(p.rows) < 2 {
		t.Fatalf("artist page: timeline %v, %d rows", p.timeline, len(p.rows))
	}
	for i := 1; i < len(p.rows); i++ {
		if p.rows[i].album.ReleaseDate > p.rows[i-1].album.ReleaseDate {
			t.Fatal("releases aren't newest first")
		}
	}
	if !strings.Contains(ansi.Strip(d.m.View().Content), "┬─") {
		t.Error("no timeline drawn")
	}
}

func TestAccentFromCover(t *testing.T) {
	cfg := withArt(art.Off)
	cfg.Theme.Accent = AccentFromCover
	d := newDriverWith(t, 120, 40, demo.New(), Options{Config: cfg})
	if d.m.accentShown != DefaultAccent {
		t.Fatalf("accent before playing = %s", d.m.accentShown)
	}
	d.press("enter")
	if d.m.accentShown == DefaultAccent || d.m.accentShown == "" {
		t.Fatalf("accent didn't follow the cover: %q", d.m.accentShown)
	}
}

func TestSwatch(t *testing.T) {
	solid := func(c color.Color) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, 20, 20))
		for y := range 20 {
			for x := range 20 {
				img.Set(x, y, c)
			}
		}
		return img
	}
	red := swatchOf(solid(color.RGBA{200, 30, 30, 255}))
	if red.average != "#c81e1e" || !strings.HasPrefix(red.vivid, "#") {
		t.Fatalf("red cover: %+v", red)
	}
	var r, g, b int
	if _, err := fmtSscanf(red.vivid, &r, &g, &b); err != nil || r <= g || r <= b {
		t.Fatalf("vivid colour of a red cover isn't red: %s", red.vivid)
	}
	if grey := swatchOf(solid(color.RGBA{120, 120, 120, 255})); grey.vivid != "" {
		t.Fatalf("grey cover has a vivid colour %s", grey.vivid)
	}
}

func TestMarquee(t *testing.T) {
	s := "A song title that's far too long"
	if got := marquee(s, 10, 0); got != "A song tit" {
		t.Fatalf("at rest: %q", got)
	}
	if got := marquee(s, 10, marqueePause+2*marqueeStep); got != "song title" {
		t.Fatalf("after two steps: %q", got)
	}
	pass := marqueePause + time.Duration(len([]rune(s+marqueeGap)))*marqueeStep
	if got := marquee(s, 10, pass); got != "A song tit" {
		t.Fatalf("after a whole pass: %q", got)
	}
}

func TestBanner(t *testing.T) {
	big, ok := banner("Hi 5")
	if !ok || big[0] != "█▄█ ▀█▀    ██▀" || big[1] != "█ █ ▄█▄    ▄▄▀" {
		t.Fatalf("banner = %q, %v", big, ok)
	}
	if _, ok := banner("Björk"); ok {
		t.Fatal("letters outside the font should fall back")
	}
}

func TestHelpKeyboard(t *testing.T) {
	d := newDriver(t, 120, 40)
	d.press("?")
	view := ansi.Strip(d.m.View().Content)
	for _, want := range []string{"│  q  │", "quit", "shufl", "S shuffle this page"} {
		if !strings.Contains(view, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

// Every page fits the screen at every size, with and without art.
func TestPagesFit(t *testing.T) {
	for _, mode := range []art.Mode{art.Off, art.Blocks} {
		for _, sz := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {220, 60}} {
			d := newHomeDriver(t, sz[0], sz[1], withArt(mode))
			check := func(state string) {
				t.Helper()
				lines := strings.Split(d.m.View().Content, "\n")
				if len(lines) != sz[1] {
					t.Errorf("%v %dx%d %s: %d lines", mode, sz[0], sz[1], state, len(lines))
				}
				for i, l := range lines {
					if w := lipgloss.Width(l); w > sz[0] {
						t.Errorf("%v %dx%d %s: line %d is %d wide", mode, sz[0], sz[1], state, i, w)
					}
				}
			}
			check("home")
			d.press("enter")
			check("album")
			d.press("enter")
			check("playing")
			d.open("Artists")
			d.press("enter")
			check("artist")
			d.open("playlist")
			check("playlist")
			d.open("Podcasts")
			d.press("enter")
			check("podcast")
			d.press("/")
			d.typeText("neon")
			d.press("enter")
			check("search")
			d.press("?")
			check("help")
		}
	}
}

func fmtSscanf(hex string, r, g, b *int) (int, error) {
	return fmt.Sscanf(hex, "#%02x%02x%02x", r, g, b)
}
