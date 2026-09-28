package tui

import (
	"image/color"
	"math"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The help screen is a keyboard. Keys that do something are solid caps
// with a word on them, colored by what they're for; the rest are dim.
// Shift and ctrl are layers you flip through with tab. Pressing a key
// picks it out and says what it does, instead of doing it. Small terminals
// get the plain list.

// A keycap is one key on the board: its name as bubbletea reports it, and
// its width in key units.
type keycap struct {
	id string
	u  float64
}

func caps(ids string) []keycap {
	var out []keycap
	for _, r := range ids {
		out = append(out, keycap{string(r), 1})
	}
	return out
}

// board is a real keyboard, trimmed to the keys sptui could use. Every row
// is boardUnits wide.
var board = [][]keycap{
	append(append([]keycap{{"esc", 1}}, caps("1234567890-=")...), keycap{"backspace", 1.5}),
	append(append([]keycap{{"tab", 1.5}}, caps("qwertyuiop[]")...), keycap{`\`, 1}),
	append(append([]keycap{{"caps", 1.75}}, caps("asdfghjkl;'")...), keycap{"enter", 1.75}),
	append(append([]keycap{{"shift", 2.25}}, caps("zxcvbnm,./")...), keycap{"shift", 2.25}),
	{{"ctrl", 1.5}, {"alt", 1.25}, {"space", 6.75}, {"", 1}, {"left", 1}, {"up", 1}, {"down", 1}, {"right", 1}},
}

const boardUnits = 14.5

// Layers of the board.
const (
	layerBase = iota
	layerShift
	layerCtrl
	layers
)

var layerNames = [layers]string{"plain", "⇧ shift", "ctrl"}

// shifted is what shift turns a key into.
var shifted = map[string]string{
	"1": "!", "2": "@", "3": "#", "4": "$", "5": "%", "6": "^", "7": "&", "8": "*", "9": "(", "0": ")",
	"-": "_", "=": "+", "[": "{", "]": "}", `\`: "|", ";": ":", "'": `"`, ",": "<", ".": ">", "/": "?",
}

// unshifted undoes shifted, for keys pressed while the help is open.
var unshifted = func() map[string]string {
	out := map[string]string{}
	for k, v := range shifted {
		out[v] = k
	}
	return out
}()

// capGlyph is what's printed on a cap.
var capGlyph = map[string]string{
	"backspace": "⌫", "enter": "enter ⏎", "space": "space", "tab": "tab ⇥", "caps": "caps",
	"shift": "⇧ shift", "ctrl": "ctrl", "alt": "alt", "esc": "esc", "left": "←", "up": "↑", "down": "↓", "right": "→",
}

// keyName is the key string a keycap sends on a layer.
func keyName(id string, layer int) string {
	switch layer {
	case layerShift:
		if s, ok := shifted[id]; ok {
			return s
		}
		if len(id) == 1 && id >= "a" && id <= "z" {
			return strings.ToUpper(id)
		}
		return "shift+" + id
	case layerCtrl:
		return "ctrl+" + id
	}
	return id
}

// capOf finds the keycap and layer that send a key string.
func capOf(s string) (id string, layer int) {
	switch {
	case strings.HasPrefix(s, "ctrl+"):
		return strings.TrimPrefix(s, "ctrl+"), layerCtrl
	case strings.HasPrefix(s, "shift+"):
		return strings.TrimPrefix(s, "shift+"), layerShift
	case unshifted[s] != "":
		return unshifted[s], layerShift
	case len(s) == 1 && s >= "A" && s <= "Z":
		return strings.ToLower(s), layerShift
	}
	return s, layerBase
}

// What a key is for, which picks its color.
const (
	forMoving = iota
	forPlaying
	forLibrary
)

// legend is a key on the board: its binding, words for the keycap (longest
// first; the first that fits is used), and what it's for.
type legend struct {
	b     key.Binding
	words []string
	kind  int
}

func (k keyMap) legends() []legend {
	num := func(n string) legend {
		return legend{key.NewBinding(key.WithKeys(n), key.WithHelp(n, "tab or panel "+n)), []string{"view " + n, "tab " + n}, forMoving}
	}
	return []legend{
		{k.Up, []string{"up"}, forMoving},
		{k.Down, []string{"down"}, forMoving},
		{k.Top, []string{"top"}, forMoving},
		{k.Bottom, []string{"bottom", "end"}, forMoving},
		{k.PageUp, []string{"page ↑", "pg ↑"}, forMoving},
		{k.PageDown, []string{"page ↓", "pg ↓"}, forMoving},
		{k.Enter, []string{"open"}, forMoving},
		{k.Back, []string{"back"}, forMoving},
		{k.Focus, []string{"pane"}, forMoving},
		{k.FocusLeft, []string{"side"}, forMoving},
		{k.FocusRight, []string{"list"}, forMoving},
		{k.Search, []string{"search", "find"}, forMoving},
		{k.Filter, []string{"filter", "sift"}, forMoving},
		{k.Reload, []string{"reload"}, forMoving},
		{k.NowPlaying, []string{"lyrics", "words"}, forMoving},
		num("1"), num("2"), num("3"),

		{k.PlayPause, []string{"play / pause"}, forPlaying},
		{k.Next, []string{"next"}, forPlaying},
		{k.Prev, []string{"prev"}, forPlaying},
		{k.SeekBack, []string{"−10s"}, forPlaying},
		{k.SeekFwd, []string{"+10s"}, forPlaying},
		{k.VolUp, []string{"vol +"}, forPlaying},
		{k.VolDown, []string{"vol −"}, forPlaying},
		{k.Shuffle, []string{"shuffle", "mix"}, forPlaying},
		{k.Repeat, []string{"repeat", "loop"}, forPlaying},
		{k.Devices, []string{"devices", "cast"}, forPlaying},
		{k.PlayAll, []string{"play all", "▶ all"}, forPlaying},
		{k.ShuffleAll, []string{"mix all", "⤮ all"}, forPlaying},
		{k.Radio, []string{"radio"}, forPlaying},

		{k.Menu, []string{"actions", "menu"}, forLibrary},
		{k.PageMenu, []string{"page ⋯", "⋯"}, forLibrary},
		{k.NowMenu, []string{"▶ menu", "▶ ⋯"}, forLibrary},
		{k.Like, []string{"like"}, forLibrary},
		{k.LikePlaying, []string{"like ▶", "♥ ▶"}, forLibrary},
		{k.Queue, []string{"queue"}, forLibrary},
		{k.Settings, []string{"config", "setup"}, forLibrary},
		{k.Account, []string{"account", "you"}, forLibrary},
		{k.Update, []string{"update", "new"}, forLibrary},
		{k.Help, []string{"help"}, forLibrary},
		{k.Quit, []string{"quit"}, forLibrary},
	}
}

// legendFor finds the legend of a key string.
func (k keyMap) legendFor(s string) (legend, bool) {
	for _, l := range k.legends() {
		for _, bk := range l.b.Keys() {
			if bk == s {
				return l, true
			}
		}
	}
	return legend{}, false
}

// helpKey handles a key while the help is open: esc, ? and q close it,
// tab flips the layer, and anything else is looked up. The plain list has
// nothing to look up on, so any key closes it.
func (m *Model) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); {
	case m.helpUnit() == 0, s == "esc", s == "?", s == "q":
		m.showHelp = false
	case s == "tab":
		m.helpLayer, m.helpPicked = (m.helpLayer+1)%layers, ""
	case s == "shift+tab":
		m.helpLayer, m.helpPicked = (m.helpLayer+layers-1)%layers, ""
	default:
		_, m.helpLayer = capOf(s)
		m.helpPicked = s
	}
	return nil
}

func (m *Model) openHelp() {
	m.showHelp, m.helpLayer, m.helpPicked = true, layerBase, ""
}

// helpUnit is the width of one key unit in columns, or 0 if the keyboard
// doesn't fit and the list should be shown instead.
func (m *Model) helpUnit() int {
	unit := min(9, int(float64(m.width-8)/boardUnits))
	if unit < 7 || m.height < 28 {
		return 0
	}
	return unit
}

func (m *Model) viewHelp() string {
	unit := m.helpUnit()
	if unit == 0 {
		return m.viewHelpList()
	}
	legends := map[string]legend{}
	for _, l := range m.keys.legends() {
		for _, k := range l.b.Keys() {
			if _, taken := legends[k]; !taken {
				legends[k] = l
			}
		}
	}
	ink := [...]color.Color{forMoving: m.st.text, forPlaying: m.st.accent, forLibrary: m.st.artist}
	face := pick(m.dark, lipgloss.Color("#30353d"), lipgloss.Color("#dadee4"))
	dead := pick(m.dark, lipgloss.Color("#1f2227"), lipgloss.Color("#eef0f3"))
	pickedID, _ := capOf(m.helpPicked)
	base := lipgloss.NewStyle()
	accentInk := inkOn(m.accentHex())

	var lines []string
	for _, row := range board {
		var top, bottom, lip strings.Builder
		cum, end := 0.0, 0
		for _, c := range row {
			start := int(math.Round(cum * float64(unit)))
			cum += c.u
			w := int(math.Round(cum*float64(unit))) - start - 1
			gap := strings.Repeat(" ", start-end)
			end = start + w
			top.WriteString(gap)
			bottom.WriteString(gap)
			lip.WriteString(gap)
			if c.id == "" {
				blank := strings.Repeat(" ", w)
				top.WriteString(blank)
				bottom.WriteString(blank)
				lip.WriteString(blank)
				continue
			}

			name := keyName(c.id, m.helpLayer)
			l, used := legends[name]
			glyph := capGlyph[c.id]
			if glyph == "" {
				glyph = c.id
				if m.helpLayer == layerShift {
					glyph = keyName(c.id, layerShift)
				}
			}
			modifier := (c.id == "shift" && m.helpLayer == layerShift) || (c.id == "ctrl" && m.helpLayer == layerCtrl)
			picked := m.helpPicked != "" && c.id == pickedID

			bg, glyphFg, wordFg := dead, m.st.faint, m.st.faint
			if used {
				bg, glyphFg, wordFg = face, m.st.text, ink[l.kind]
			}
			if picked || modifier {
				bg, glyphFg, wordFg = m.st.accent, accentInk, accentInk
			}
			var word string
			if used {
				for _, wd := range l.words {
					if word = wd; lipgloss.Width(wd) < w {
						break
					}
				}
			}
			cell := base.Background(bg)
			top.WriteString(cell.Foreground(glyphFg).Bold(used || modifier).Render(" " + fit(glyph, w-1)))
			bottom.WriteString(cell.Foreground(wordFg).Bold(picked).Render(" " + fit(word, w-1)))
			lip.WriteString(base.Foreground(bg).Render(strings.Repeat("▀", w)))
		}
		lines = append(lines, top.String(), bottom.String(), lip.String())
	}
	boardW := int(math.Round(boardUnits*float64(unit))) - 1

	// Title, with the layers as tabs and a color key.
	var tabs []string
	for i, n := range layerNames {
		if i == m.helpLayer {
			tabs = append(tabs, base.Foreground(accentInk).Background(m.st.accent).Bold(true).Render(" "+n+" "))
		} else {
			tabs = append(tabs, m.st.keyDesc.Render(" "+n+" "))
		}
	}
	left := m.st.modalTitle.Render("Keys") + "   " + strings.Join(tabs, " ")
	swatch := func(c color.Color, what string) string {
		return base.Foreground(c).Render("■") + " " + m.st.keyDesc.Render(what)
	}
	right := swatch(m.st.text, "move") + "   " + swatch(m.st.accent, "play") + "   " + swatch(m.st.artist, "library")
	title := spread(left, right, boardW)

	// What the picked key does.
	var detail string
	switch l, ok := m.keys.legendFor(m.helpPicked); {
	case m.helpPicked == "":
		detail = m.st.keyDesc.Render("Press any key to see what it does.")
	case !ok:
		detail = m.st.key.Render(m.helpPicked) + m.st.keyDesc.Render("  does nothing")
	default:
		chip := base.Foreground(accentInk).Background(m.st.accent).Bold(true).Render(" " + m.helpPicked + " ")
		detail = chip + "  " + base.Foreground(ink[l.kind]).Bold(true).Render(l.b.Help().Desc)
	}

	hint := func(k, what string) string { return m.st.key.Render(k) + " " + m.st.keyDesc.Render(what) }
	dot := m.st.off.Render("  ·  ")
	footer := hint("tab", "shift / ctrl") + dot + hint("m", "on anything shows all you can do with it") + dot + hint("esc", "close")

	body := title + "\n\n" + strings.Join(lines, "\n") + "\n\n" + detail + "\n\n" + footer
	return m.st.modal.Padding(1, 2).Render(body)
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
