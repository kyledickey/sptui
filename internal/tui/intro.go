package tui

import (
	"cmp"
	"fmt"
	"image/color"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/intro"
)

// The intro is a short animation that plays while the library loads. The
// theme's intro setting picks one from package intro. Any key or click
// skips it.

type introTickMsg struct{}

// introPlay is the startup animation; nil once it's over.
type introPlay struct {
	*intro.Play
	start   time.Time
	preview bool // replayed from settings: keys skip it and still count
}

// playIntro starts the animation called name, if there is one.
func (m *Model) playIntro(name string, preview bool) tea.Cmd {
	anim, ok := intro.Find(name)
	if !ok {
		m.intro = nil
		return nil
	}
	ticking := m.intro != nil
	m.intro = &introPlay{Play: intro.NewPlay(anim, m.introEnv()), start: time.Now(), preview: preview}
	if ticking {
		return nil
	}
	return introTick()
}

func introTick() tea.Cmd {
	return tea.Tick(intro.Frame, func(time.Time) tea.Msg { return introTickMsg{} })
}

// introUpdate advances or ends the intro. It reports whether msg was
// the intro's to handle.
func (m *Model) introUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if m.intro == nil {
		return nil, false
	}
	switch msg.(type) {
	case introTickMsg:
		if float64(time.Since(m.intro.start).Milliseconds()) >= m.intro.Anim.End {
			m.intro = nil
			return nil, true
		}
		return introTick(), true
	case tea.KeyPressMsg, tea.MouseClickMsg:
		preview := m.intro.preview
		m.intro = nil // skipped
		return nil, !preview
	}
	return nil, false
}

// viewIntro draws the intro's current frame, centred on the screen.
func (m *Model) viewIntro() string {
	m.intro.Env = m.introEnv() // the theme may have changed
	scene := m.intro.Frame(float64(time.Since(m.intro.start).Milliseconds()))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, scene)
}

// bgHex is the terminal's background, or a guess at it.
func (m *Model) bgHex() string {
	return cmp.Or(m.bg, guessBg(m.dark))
}

func guessBg(dark bool) string { return pick(dark, "#101216", "#ffffff") }

func (m *Model) introEnv() intro.Env {
	return introEnv(m.st, m.dark, m.bgHex(), m.me.Name())
}

func introEnv(st styles, dark bool, bg, name string) intro.Env {
	return intro.Env{
		Accent: hexOfColor(st.accent), Purple: hexOfColor(st.artist), Text: hexOfColor(st.text),
		Muted: hexOfColor(st.muted), Faint: hexOfColor(st.faint),
		BG: bg, Dark: dark, Name: name,
	}
}

// IntroEnv is what the intros draw with under cfg's theme, for playing
// them outside the app. bg is the terminal's background, "" to guess; the
// accent follows cover art only in the app, so here it's the default.
func IntroEnv(cfg config.Config, dark bool, bg, name string) intro.Env {
	accent := cfg.Theme.Accent
	if accent == AccentFromCover || accent == "" {
		accent = DefaultAccent
	}
	return introEnv(newStyles(accent, dark), dark, cmp.Or(bg, guessBg(dark)), name)
}

func hexOfColor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
