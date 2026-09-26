package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/intro"
	"github.com/kyledickey/sptui/internal/tui"
)

var speeds = []float64{0.1, 0.25, 0.5, 1, 2}

const loopGap = 700 // ms of nothing between the end and playing again

type tickMsg struct{}

// lab plays one intro at a time on its own clock, so it can pause, step,
// scrub and slow down.
type lab struct {
	cfg           config.Config
	name          string
	width, height int

	termDark bool   // what the terminal says
	termBg   string // its background, "" until it says
	flipped  bool   // showing the other theme: light on dark or dark on light
	nameless bool   // greeting no one

	i      int // index into intro.All
	play   *intro.Play
	t      float64 // ms in
	last   time.Time
	paused bool
	speed  float64
	gap    float64 // ms waited since the end
}

func newLab(cfg config.Config, name string, i int) *lab {
	l := &lab{cfg: cfg, name: name, termDark: true, speed: 1, last: time.Now()}
	l.open(i)
	return l
}

func tick() tea.Cmd {
	return tea.Tick(intro.Frame, func(time.Time) tea.Msg { return tickMsg{} })
}

func (l *lab) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, tick()) }

// env is what the intro draws with, as the theme stands.
func (l *lab) env() intro.Env {
	dark, bg := l.termDark, l.termBg
	if l.flipped {
		dark, bg = !dark, ""
	}
	return tui.IntroEnv(l.cfg, dark, bg, pick(l.nameless, "", l.name))
}

// open starts intro i from the beginning.
func (l *lab) open(i int) {
	l.i = (i + len(intro.All)) % len(intro.All)
	l.play = intro.NewPlay(intro.All[l.i], l.env())
	l.restart()
}

func (l *lab) restart() {
	l.play.Reset()
	l.t, l.gap = 0, 0
}

// seek moves by dt ms, staying within the animation.
func (l *lab) seek(dt float64) {
	l.t = min(max(l.t+dt, 0), l.play.Anim.End)
	l.gap = 0
	if l.t == 0 {
		l.play.Reset()
	}
}

func (l *lab) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		r, g, b, _ := msg.Color.RGBA()
		l.termBg = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
		l.termDark = msg.IsDark()
	case tickMsg:
		now := time.Now()
		dt := float64(now.Sub(l.last).Milliseconds())
		l.last = now
		if !l.paused {
			if l.t < l.play.Anim.End {
				l.t = min(l.t+dt*l.speed, l.play.Anim.End)
			} else if l.gap += dt; l.gap > loopGap {
				l.restart()
			}
		}
		return l, tick()
	case tea.KeyPressMsg:
		return l, l.key(msg.String())
	}
	return l, nil
}

func (l *lab) key(k string) tea.Cmd {
	frame := float64(intro.Frame.Milliseconds())
	switch k {
	case "q", "esc", "ctrl+c":
		return tea.Quit
	case "right", "l", "tab":
		l.open(l.i + 1)
	case "left", "h", "shift+tab":
		l.open(l.i - 1)
	case "space":
		l.paused = !l.paused
		if !l.paused && l.t >= l.play.Anim.End {
			l.restart()
		}
	case "r", "home":
		l.restart()
	case "f", "end":
		l.seek(l.play.Anim.Fade - l.t)
	case ".":
		l.paused = true
		l.seek(frame)
	case ",":
		l.paused = true
		l.seek(-frame)
	case ">":
		l.seek(500)
	case "<":
		l.seek(-500)
	case "up", "k", "+", "=":
		l.speed = step(l.speed, 1)
	case "down", "j", "-":
		l.speed = step(l.speed, -1)
	case "t":
		l.flipped = !l.flipped
	case "g":
		l.nameless = !l.nameless
		l.play.Reset()
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			l.open(int(k[0] - '1'))
		}
	}
	return nil
}

// step moves to the next speed up (dir 1) or down (-1).
func step(cur float64, dir int) float64 {
	for i, s := range speeds {
		if s == cur {
			return speeds[min(max(i+dir, 0), len(speeds)-1)]
		}
	}
	return 1
}

func (l *lab) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = "sptui intros · " + l.play.Anim.Name
	if l.width == 0 {
		return v
	}
	env := l.env()
	l.play.Env = env
	if l.flipped {
		v.BackgroundColor = lipgloss.Color(env.BG)
	}
	scene := l.play.Frame(l.t)

	hud := l.hud(env)
	top := lipgloss.Place(l.width, max(l.height-lipgloss.Height(hud)-1, 0), lipgloss.Center, lipgloss.Center, scene)
	v.SetContent(lipgloss.JoinVertical(lipgloss.Left, top, "", lipgloss.PlaceHorizontal(l.width, lipgloss.Center, hud)))
	return v
}

// hud is the bar under the scene: the intros, a timeline, and the keys.
func (l *lab) hud(env intro.Env) string {
	a := l.play.Anim
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(env.Accent)).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(env.Muted))
	faint := lipgloss.NewStyle().Foreground(lipgloss.Color(env.Faint))

	names := make([]string, len(intro.All))
	for i, x := range intro.All {
		names[i] = pick(i == l.i, accent, faint).Render(fmt.Sprintf("%d %s", i+1, x.Name))
	}

	// The timeline: played lit, the fade dashed.
	const w = 48
	var bar strings.Builder
	for i := range w {
		at := a.End * float64(i) / w
		bar.WriteString(pick(at <= l.t, accent, faint).Render(pick(at >= a.Fade, "┅", "━")))
	}
	info := fmt.Sprintf("%s %5.0f / %.0f ms · fade at %.0f · ×%g",
		pick(l.paused, "❚❚", "▶ "), l.t, a.End, a.Fade, l.speed)

	var flags []string
	if l.flipped {
		flags = append(flags, pick(env.Dark, "dark", "light")+" theme")
	}
	if l.nameless {
		flags = append(flags, "no name")
	}
	if len(flags) > 0 {
		info += " · " + accent.Render(strings.Join(flags, ", "))
	}

	keys := "←→ 1-9 intro · space pause · ,. frame · <> ½s · f fade · ↑↓ speed · r restart · t theme · g name · q quit"
	return lipgloss.JoinVertical(lipgloss.Center,
		strings.Join(names, "  "),
		bar.String()+"  "+muted.Render(info),
		faint.Render(keys),
	)
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
