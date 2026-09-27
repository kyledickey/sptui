package tui

import (
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Headless runs sptui without a terminal, doing what tea.Program would, so
// its screens can be drawn somewhere else (the website draws the demo's).
// Redraw ticks are dropped, so after each step the UI settles and stays
// put.
type Headless struct{ m *Model }

// headlessWait is the most a step waits for the UI to settle.
const headlessWait = 5 * time.Second

// NewHeadless starts sptui on b in a w×h screen and waits for it to load.
func NewHeadless(b Backend, opts Options, w, h int) *Headless {
	opts.Intro = false
	hl := &Headless{m: New(b, opts)}
	hl.m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	hl.run(hl.m.Init())
	return hl
}

// View is the screen as text with ANSI styles.
func (hl *Headless) View() string { return hl.m.View().Content }

// Open opens the sidebar item labelled label, if there is one.
func (hl *Headless) Open(label string) {
	for i, it := range hl.m.sidebar.items {
		if it.label == label {
			hl.m.sidebar.cursor = i
			hl.run(hl.m.openNav(i))
			return
		}
	}
}

// Press presses keys: runes, or "enter", "esc", "tab", "space", "up",
// "down", "left" and "right".
func (hl *Headless) Press(keys ...string) {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	}
	for _, k := range keys {
		msg := tea.KeyPressMsg{Text: k}
		if code, ok := special[k]; ok {
			msg = tea.KeyPressMsg{Code: code}
		} else {
			msg.Code = []rune(k)[0]
		}
		_, cmd := hl.m.Update(msg)
		hl.run(cmd)
	}
}

// run executes cmd and everything it leads to, until nothing is left but
// ticks or headlessWait passes.
func (hl *Headless) run(cmd tea.Cmd) {
	msgs := make(chan tea.Msg, 256)
	pending := 0
	start := func(c tea.Cmd) {
		if c != nil {
			pending++
			go func() { msgs <- c() }()
		}
	}
	var deliver func(tea.Msg)
	deliver = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case nil, tickMsg, introTickMsg, tea.RawMsg:
		case tea.BatchMsg:
			for _, c := range msg {
				start(c)
			}
		default:
			if seq, ok := sequence(msg); ok {
				for _, c := range seq {
					if c != nil {
						deliver(c())
					}
				}
				return
			}
			_, next := hl.m.Update(msg)
			start(next)
		}
	}
	start(cmd)
	deadline := time.After(headlessWait)
	for pending > 0 {
		select {
		case msg := <-msgs:
			pending--
			deliver(msg)
		case <-deadline:
			return
		}
	}
}

// sequence unpacks tea.Sequence's unexported message, a []tea.Cmd, which
// must run in order.
func sequence(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	cmds := make([]tea.Cmd, v.Len())
	for i := range cmds {
		cmds[i] = v.Index(i).Interface().(tea.Cmd)
	}
	return cmds, true
}
