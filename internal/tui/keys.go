package tui

import "charm.land/bubbles/v2/key"

// keyMap holds every binding. Help text is generated from it, so the help
// screen can never drift from the real bindings.
type keyMap struct {
	// Navigation
	Up, Down, Top, Bottom, PageUp, PageDown   key.Binding
	Enter, Back, Focus, FocusLeft, FocusRight key.Binding
	Search, Filter, Menu, NowMenu, Reload     key.Binding
	NowPlaying, Settings                      key.Binding

	// Playback
	PlayPause, Next, Prev, SeekBack, SeekFwd key.Binding
	VolUp, VolDown, Shuffle, Repeat, Devices key.Binding
	PlayAll, ShuffleAll, Radio               key.Binding

	// Library
	Like, LikePlaying, Queue, Account key.Binding

	Help, Quit key.Binding
}

func newKeyMap() keyMap {
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up:         b("↑/k", "up", "up", "k"),
		Down:       b("↓/j", "down", "down", "j"),
		Top:        b("g", "top", "g", "home"),
		Bottom:     b("G", "bottom", "G", "end"),
		PageUp:     b("pgup", "page up", "pgup", "ctrl+u"),
		PageDown:   b("pgdn", "page down", "pgdown", "ctrl+d"),
		Enter:      b("enter", "play / open", "enter"),
		Back:       b("esc", "back", "esc", "backspace"),
		Focus:      b("tab", "switch pane", "tab", "shift+tab"),
		FocusLeft:  b("←", "sidebar", "left"),
		FocusRight: b("→", "list", "right"),
		Search:     b("/", "search", "/"),
		Filter:     b("f", "filter", "f"),
		Menu:       b("m", "actions", "m", "."),
		NowMenu:    b("M", "now playing actions", "M"),
		Reload:     b("ctrl+r", "reload", "ctrl+r"),
		NowPlaying: b("o", "now playing + lyrics", "o"),
		Settings:   b(",", "settings", ","),

		PlayPause:  b("space", "play/pause", "space"),
		Next:       b("n", "next", "n"),
		Prev:       b("p", "previous", "p"),
		SeekBack:   b("[", "back 10s", "["),
		SeekFwd:    b("]", "fwd 10s", "]"),
		VolUp:      b("+", "volume up", "+", "="),
		VolDown:    b("-", "volume down", "-"),
		Shuffle:    b("s", "shuffle", "s"),
		Repeat:     b("r", "repeat", "r"),
		Devices:    b("d", "devices", "d"),
		PlayAll:    b("P", "play this page", "P"),
		ShuffleAll: b("S", "shuffle this page", "S"),
		Radio:      b("R", "radio from this", "R"),

		Like:        b("l", "like", "l"),
		LikePlaying: b("L", "like playing", "L"),
		Queue:       b("a", "queue", "a"),
		Account:     b("u", "account / log out", "u"),

		Help: b("?", "help", "?"),
		Quit: b("q", "quit", "q", "ctrl+c"),
	}
}

// helpGroups is the layout of the full help screen.
func (k keyMap) helpGroups() []helpGroup {
	return []helpGroup{
		{"Navigate", []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.PageDown, k.Enter, k.Back, k.Focus, k.Search, k.Filter, k.NowPlaying, k.Reload}},
		{"Playback", []key.Binding{k.PlayPause, k.Next, k.Prev, k.SeekBack, k.SeekFwd, k.VolUp, k.VolDown, k.Shuffle, k.Repeat, k.PlayAll, k.ShuffleAll, k.Radio, k.Devices}},
		{"Library", []key.Binding{k.Menu, k.NowMenu, k.Like, k.LikePlaying, k.Queue, k.Settings, k.Account, k.Help, k.Quit}},
	}
}

type helpGroup struct {
	title    string
	bindings []key.Binding
}
