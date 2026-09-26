package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// The help screen is a map of the keyboard: every key that does something
// is lit with a word or two, the rest stay dim. Special keys and shifted
// letters are listed underneath. Narrow terminals get the plain list.

var keyboardRows = []string{"qwertyuiop", "asdfghjkl", "zxcvbnm,./"}

const keyW = 5 // inner width of a key

// shortHelp names each binding in a word, for its key on the map.
func (k keyMap) shortHelp() map[string]string {
	short := map[*key.Binding]string{
		&k.Up: "up", &k.Down: "down", &k.Top: "top", &k.Bottom: "end",
		&k.Search: "find", &k.Filter: "filtr", &k.Menu: "menu", &k.NowMenu: "npmnu",
		&k.NowPlaying: "now", &k.Settings: "setup", &k.PlayPause: "play",
		&k.Next: "next", &k.Prev: "prev", &k.SeekBack: "-10s", &k.SeekFwd: "+10s",
		&k.VolUp: "vol+", &k.VolDown: "vol-", &k.Shuffle: "shufl", &k.Repeat: "rpt",
		&k.Devices: "devs", &k.Like: "like", &k.LikePlaying: "like▶", &k.Queue: "queue",
		&k.Account: "acct", &k.Help: "help", &k.Quit: "quit",
		&k.PlayAll: "play⋯", &k.ShuffleAll: "shuf⋯",
	}
	out := map[string]string{}
	for b, word := range short {
		for _, k := range b.Keys() {
			if _, taken := out[k]; !taken {
				out[k] = word
			}
		}
	}
	return out
}

func (m *Model) viewHelp() string {
	width := len(keyboardRows[0])*(keyW+1) + 1 + 4 // keys, stagger
	if m.width < width+8 || m.height < 24 {
		return m.viewHelpList()
	}
	words := m.keys.shortHelp()
	var lines []string
	for i, row := range keyboardRows {
		indent := strings.Repeat(" ", i*2)
		var top, face, word, bottom strings.Builder
		for j, r := range row {
			k := string(r)
			w, used := words[k]
			edge, cap, label := m.st.off, m.st.off, m.st.off
			if used {
				edge, cap, label = m.st.subtitle, m.st.key, m.st.on
			}
			left := pick(j == 0, "│", "") // keys share the border between them
			top.WriteString(edge.Render(pick(j == 0, "╭", "┬") + strings.Repeat("─", keyW)))
			face.WriteString(edge.Render(left) + cap.Render(lipgloss.PlaceHorizontal(keyW, lipgloss.Center, k)) + edge.Render("│"))
			word.WriteString(edge.Render(left) + label.Render(lipgloss.PlaceHorizontal(keyW, lipgloss.Center, w)) + edge.Render("│"))
			bottom.WriteString(edge.Render(pick(j == 0, "╰", "┴") + strings.Repeat("─", keyW)))
		}
		top.WriteString(m.st.off.Render("╮"))
		bottom.WriteString(m.st.off.Render("╯"))
		lines = append(lines, indent+top.String(), indent+face.String(), indent+word.String(), indent+bottom.String())
	}

	// Keys that aren't letters, and shifted letters.
	k := m.keys
	extra := func(bs ...key.Binding) string {
		var parts []string
		for _, b := range bs {
			h := b.Help()
			parts = append(parts, m.st.key.Render(h.Key)+" "+m.st.keyDesc.Render(h.Desc))
		}
		return strings.Join(parts, m.st.off.Render("  ·  "))
	}
	lines = append(lines, "",
		extra(k.PlayPause, k.Enter, k.Back, k.Focus),
		extra(k.SeekBack, k.SeekFwd, k.VolUp, k.VolDown, k.Reload),
		extra(k.Bottom, k.LikePlaying, k.NowMenu, k.PlayAll, k.ShuffleAll),
		"",
		m.st.keyDesc.Render("Press m on anything to see everything you can do with it. Any key closes."),
	)
	title := m.st.modalTitle.Render("Keys")
	return m.st.modal.Padding(1, 2).Render(title + "\n\n" + strings.Join(lines, "\n"))
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// viewHelpList is the help screen as columns of keys, for small terminals.
func (m *Model) viewHelpList() string {
	var cols []string
	colW := min(28, (m.width-8)/3)
	for _, g := range m.keys.helpGroups() {
		lines := []string{m.st.modalTitle.Render(g.title), ""}
		for _, b := range g.bindings {
			h := b.Help()
			lines = append(lines, m.st.key.Render(fit(h.Key, 8))+m.st.keyDesc.Render(h.Desc))
		}
		cols = append(cols, lipgloss.NewStyle().Width(colW).Render(strings.Join(lines, "\n")))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	footer := m.st.keyDesc.Width(lipgloss.Width(body)).
		Render("Tip: press m on anything to see everything you can do with it. Any key closes.")
	return m.st.modal.Padding(1, 2).Render(body + "\n\n" + footer)
}
