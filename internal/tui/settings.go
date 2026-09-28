package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/intro"
)

// The settings screen edits the config from inside sptui. Appearance
// changes apply at once; player and Spotify changes need a restart, which
// the screen offers.

const (
	dashboardURL = "https://developer.spotify.com/dashboard"
)

// setting is one line on the settings screen.
type setting struct {
	section string
	label   string
	help    string   // shown below the list while selected
	choices []string // cycled with enter and ←/→
	text    bool     // edited as text instead
	key     string   // where it lives in the config file, like theme.accent
	restart bool     // takes effect after restarting sptui
	get     func(config.Config) string
	set     func(*config.Config, string) error
	action  func() tea.Cmd // a button instead of a value; get, if set, is shown beside it
}

// accents are the color presets, in the order they cycle.
var accents = []struct{ name, hex string }{
	{"green", "#1ed760"}, {"blue", "#4da3ff"}, {"purple", "#b18cff"}, {"pink", "#ff7ab6"},
	{"orange", "#ff9f43"}, {"teal", "#2dd4bf"}, {"yellow", "#f5d547"}, {"red", "#ff6b6b"},
	{"from cover art", AccentFromCover},
}

// artChoices maps what the screen shows to config values.
var artChoices = map[string]string{"auto": "auto", "images": "kitty", "pixels": "blocks", "off": "off"}

var clientIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (m *Model) settings() []setting {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	backends := []string{"auto", "pulseaudio", "alsa"}
	switch runtime.GOOS {
	case "darwin":
		backends = []string{"auto", "audio-toolbox"}
	case "windows":
		backends = []string{"auto", "wasapi"}
	}

	list := []setting{
		{
			section: "Appearance", label: "Accent color", key: "theme.accent",
			help:    "From cover art takes the color from whatever's playing, so sptui re-themes itself with every song.",
			choices: names(accents),
			get: func(c config.Config) string {
				for _, a := range accents {
					if strings.EqualFold(a.hex, c.Theme.Accent) || (c.Theme.Accent == "" && a.hex == DefaultAccent) {
						return a.name
					}
				}
				return "custom"
			},
			set: func(c *config.Config, v string) error {
				for _, a := range accents {
					if a.name == v {
						c.Theme.Accent = a.hex
					}
				}
				return nil
			},
		},
		{
			section: "Appearance", label: "Scroll long titles", key: "theme.scroll_titles",
			help:    "Titles too long to fit slide past, like a marquee. Off cuts them short with …",
			choices: []string{"on", "off"},
			get:     func(c config.Config) string { return onOff(c.Theme.ScrollTitles) },
			set:     func(c *config.Config, v string) error { c.Theme.ScrollTitles = v == "on"; return nil },
		},
		{
			section: "Appearance", label: "Album art", key: "theme.cover_art",
			help:    "Images need a terminal with kitty graphics (kitty, Ghostty). Pixels work in any terminal. Auto picks for you.",
			choices: []string{"auto", "images", "pixels", "off"},
			get: func(c config.Config) string {
				for shown, value := range artChoices {
					if value == c.Theme.CoverArt {
						return shown
					}
				}
				return "auto"
			},
			set: func(c *config.Config, v string) error { c.Theme.CoverArt = artChoices[v]; return nil },
		},
		{
			section: "Appearance", label: "Now playing cover", key: "theme.now_playing_cover",
			help:    "How wide the track panel (and its cover) is in the now-playing view (press o). There, 1 2 3 hide or show the track, lyrics and up next panels.",
			choices: []string{"small", "medium", "large"},
			get:     func(c config.Config) string { return c.Theme.NowPlayingCover },
			set:     func(c *config.Config, v string) error { c.Theme.NowPlayingCover = v; return nil },
		},
		{
			section: "Appearance", label: "Startup animation", key: "theme.intro",
			help:    "Plays while your library loads; any key skips it. Changing it here plays it for you.",
			choices: append(intro.Names(), "off"),
			get: func(c config.Config) string {
				if _, ok := intro.Find(c.Theme.Intro); ok {
					return c.Theme.Intro
				}
				return "off"
			},
			set: func(c *config.Config, v string) error { c.Theme.Intro = v; return nil },
		},
		{
			section: "Player", label: "Play music in sptui", key: "player.enabled", restart: true,
			help:    "Off turns sptui into a remote for your other Spotify devices (phone, speakers, the desktop app).",
			choices: []string{"on", "off"},
			get:     func(c config.Config) string { return onOff(c.Player.Enabled) },
			set:     func(c *config.Config, v string) error { c.Player.Enabled = v == "on"; return nil },
		},
		{
			section: "Player", label: "Keep screen awake", key: "player.keep_awake",
			help:    "Stops the computer sleeping and the screen turning off. Closing the lid or choosing Sleep still works.",
			choices: []string{"while playing", "always", "off"},
			get: func(c config.Config) string {
				return map[string]string{"playing": "while playing", "always": "always", "off": "off"}[c.Player.KeepAwake]
			},
			set: func(c *config.Config, v string) error {
				c.Player.KeepAwake = map[string]string{"while playing": "playing", "always": "always", "off": "off"}[v]
				return nil
			},
		},
		{
			section: "Player", label: "Device name", key: "player.name", restart: true, text: true,
			help: "How sptui shows up in Spotify's device list.",
			get:  func(c config.Config) string { return c.Player.Name },
			set: func(c *config.Config, v string) error {
				if v = strings.TrimSpace(v); v == "" {
					return errors.New("the device needs a name")
				}
				c.Player.Name = v
				return nil
			},
		},
		{
			section: "Player", label: "Audio output", key: "player.backend", restart: true,
			help:    "PulseAudio also covers PipeWire. Auto picks what's running.",
			choices: backends,
			get: func(c config.Config) string {
				if c.Player.Backend == "" {
					return "auto"
				}
				return c.Player.Backend
			},
			set: func(c *config.Config, v string) error {
				c.Player.Backend = strings.TrimPrefix(v, "auto")
				return nil
			},
		},
		{
			section: "Player", label: "Quality", key: "player.bitrate", restart: true,
			help:    "Streaming bitrate. 320 kbps sounds best; lower ones use less data.",
			choices: []string{"96 kbps", "160 kbps", "320 kbps"},
			get:     func(c config.Config) string { return fmt.Sprintf("%d kbps", c.Player.Bitrate) },
			set: func(c *config.Config, v string) error {
				_, err := fmt.Sscanf(v, "%d kbps", &c.Player.Bitrate)
				return err
			},
		},
		{
			section: "Spotify", label: "Your own Spotify app", key: "client_id", restart: true, text: true,
			help: "Spotify limits requests per app, and the shared one sptui uses is often busy. " +
				"Your own app gets a limit of its own:\n" +
				"1. Open " + dashboardURL + " and click Create app.\n" +
				"2. Add the redirect URI " + config.DefaultRedirectURI + ", tick Web API, and save.\n" +
				"3. Copy the Client ID from the app's Settings and paste it here.",
			get: func(c config.Config) string {
				if c.ClientID == "" {
					return "not set (shared)"
				}
				return c.ClientID
			},
			set: func(c *config.Config, v string) error {
				v = strings.ToLower(strings.TrimSpace(v))
				if v != "" && !clientIDPattern.MatchString(v) {
					return errors.New("a Client ID is 32 letters and numbers, like 1a2b3c…")
				}
				c.ClientID = v
				if v != "" && c.RedirectURI == "" {
					c.RedirectURI = config.DefaultRedirectURI
				}
				return nil
			},
		},
		{
			section: "Spotify", label: "Copy dashboard link",
			action: func() tea.Cmd { return m.copy(dashboardURL, "Dashboard link copied") },
		},
		{
			section: "Spotify", label: "Copy redirect URI",
			help:   "Paste this into your Spotify app's Redirect URIs.",
			action: func() tea.Cmd { return m.copy(config.DefaultRedirectURI, "Redirect URI copied") },
		},
		{
			section: "Account", label: "Log out and sign in again",
			help:   "Signed in as " + m.me.Name() + ". Use this to switch accounts.",
			action: func() tea.Cmd { m.outcome = LogIn; return m.quit() },
		},
		{
			section: "Account", label: "Log out and quit",
			action: func() tea.Cmd { m.outcome = LogOut; return m.quit() },
		},
	}
	account := slices.IndexFunc(list, func(s setting) bool { return s.section == "Account" })
	list = slices.Insert(list, account, append(m.updateSettings(), m.aboutSettings()...)...)
	if m.needsRestart {
		list = append(list, setting{
			section: "Account", label: "Restart sptui to apply changes",
			action: func() tea.Cmd { m.outcome = Restart; return m.quit() },
		})
	}
	return list
}

func names(presets []struct{ name, hex string }) []string {
	out := make([]string, len(presets))
	for i, p := range presets {
		out[i] = p.name
	}
	return out
}

// aboutSettings are the files sptui keeps, for copying their paths.
func (m *Model) aboutSettings() []setting {
	var list []setting
	if path := m.opts.ConfigPath; path != "" {
		list = append(list, setting{
			section: "About", label: "Config file",
			help: "Everything on this screen is saved here as you change it. It's plain TOML with comments, " +
				"if you'd rather edit it by hand. Enter copies the path.",
			get:    func(config.Config) string { return tildePath(path) },
			action: func() tea.Cmd { return m.copy(path, "Config path copied") },
		})
	}
	if path := m.opts.LogPath; path != "" {
		list = append(list, setting{
			section: "About", label: "Log file",
			help:   "What sptui did and what went wrong, worth attaching to a bug report. Enter copies the path.",
			get:    func(config.Config) string { return tildePath(path) },
			action: func() tea.Cmd { return m.copy(path, "Log path copied") },
		})
	}
	return list
}

// tildePath shortens a path in the home directory to ~/….
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home); ok && (rest == "" || rest[0] == filepath.Separator) {
		return "~" + rest
	}
	return path
}

func (m *Model) copy(text, done string) tea.Cmd {
	m.setStatus(done, false)
	return tea.SetClipboard(text)
}

func settingsPage() *page {
	p := newPage("Settings", kindHeader, nil)
	p.next = -1
	p.settings = true
	return p
}

// settingsKey handles keys on the settings screen. It reports whether it
// used the key.
func (m *Model) settingsKey(msg tea.KeyPressMsg, p *page) (tea.Cmd, bool) {
	list := m.settings()
	p.cursor = min(max(p.cursor, 0), len(list)-1)
	s := list[p.cursor]
	k := m.keys
	switch {
	case key.Matches(msg, k.Up):
		p.cursor = max(p.cursor-1, 0)
	case key.Matches(msg, k.Down):
		p.cursor = min(p.cursor+1, len(list)-1)
	case key.Matches(msg, k.Top):
		p.cursor = 0
	case key.Matches(msg, k.Bottom):
		p.cursor = len(list) - 1
	case key.Matches(msg, k.Enter), msg.String() == "right", msg.String() == "left":
		return m.changeSetting(s, msg.String()), true
	default:
		return nil, false
	}
	return nil, true
}

// changeSetting does what key (enter, left or right) does to s: press a
// button, start editing text, or step to the next choice.
func (m *Model) changeSetting(s setting, key string) tea.Cmd {
	switch {
	case s.action != nil:
		if key == "enter" {
			return s.action()
		}
	case s.text:
		if key == "enter" {
			m.editSetting(s)
		}
	default:
		step := 1
		if key == "left" {
			step = -1
		}
		i := slices.Index(s.choices, s.get(m.cfg))
		next := s.choices[(i+step+len(s.choices))%len(s.choices)]
		return m.applySetting(s, next)
	}
	return nil
}

// editSetting starts editing a text setting in the input box.
func (m *Model) editSetting(s setting) {
	m.inputMode = inputSetting
	m.input.Placeholder = s.label
	value := s.get(m.cfg)
	if s.label == "Your own Spotify app" && m.cfg.ClientID == "" {
		value = ""
	}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Focus()
}

// finishEdit applies the text being edited to the selected setting.
func (m *Model) finishEdit() tea.Cmd {
	p := m.current()
	list := m.settings()
	if p == nil || !p.settings || p.cursor >= len(list) {
		return nil
	}
	return m.applySetting(list[p.cursor], m.input.Value())
}

// applySetting changes a setting, saves the config and applies what it can
// right away.
func (m *Model) applySetting(s setting, value string) tea.Cmd {
	cfg := m.cfg
	if err := s.set(&cfg, value); err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	if cfg == m.cfg {
		return nil
	}
	if m.opts.SaveConfig != nil {
		if err := m.opts.SaveConfig(cfg); err != nil {
			m.log.Error("save settings", "err", err)
			m.setStatus("Couldn't save settings: "+err.Error(), true)
			return nil
		}
	}
	m.log.Info("setting changed", "setting", s.label, "value", s.get(cfg))
	old := m.cfg
	m.cfg = cfg
	if s.restart {
		m.needsRestart = true
		m.setStatus(s.label+" saved — restart sptui to apply (bottom of this list)", false)
	} else {
		m.setStatus(s.label+": "+s.get(cfg), false)
	}

	// Live changes.
	if old.Theme.Accent != cfg.Theme.Accent {
		m.setTheme(m.dark)
	}
	if old.CheckUpdates != cfg.CheckUpdates && m.upd.stage == updateIdle {
		m.upd.latest = "" // off hides the notice; on looks again
		if cfg.CheckUpdates && m.opts.Updates != nil {
			return m.checkUpdates(false)
		}
	}
	if old.Theme.Intro != cfg.Theme.Intro {
		return m.playIntro(cfg.Theme.Intro, true)
	}
	if artMode(old) != artMode(cfg) {
		free := m.freeCovers()
		m.covers = newCovers(artMode(cfg))
		return free
	}
	return nil
}

// --- the screen ---

// settingsWide is the content width from which the selected setting's
// details sit beside the list, instead of a line of help below it.
const settingsWide = 92

// settingsHits maps the lines of the settings list on screen back to
// settings, for the mouse.
type settingsHits struct {
	top, width int   // where the list starts, and how wide it is
	rows       []int // the setting on each line, or -1
}

// viewSettings renders the settings screen in cw×h: a header with sptui's
// version, the list, and the selected setting's details.
func (m *Model) viewSettings(p *page, cw, h int) []string {
	list := m.settings()
	p.cursor = min(max(p.cursor, 0), len(list)-1)
	s := list[p.cursor]
	lines := m.settingsHeader(cw)

	listW := cw
	var detail, below []string
	if cw >= settingsWide {
		listW = cw * 11 / 20
		detail = m.settingDetail(s, cw-listW-2, h-len(lines))
	} else if s.help != "" {
		below = append([]string{""}, m.wrap(s.help, cw, m.st.subtitle)...)
	}

	body, rows := m.settingsList(list, p.cursor, listW)
	avail := max(h-len(lines)-len(below), 1)
	cur := slices.Index(rows, p.cursor)
	top := cur
	for top > 0 && rows[top-1] < 0 && cur-top < 2 { // its heading, and the gap above
		top--
	}
	if top < p.scroll {
		p.scroll = top
	}
	if cur >= p.scroll+avail {
		p.scroll = cur - avail + 1
	}
	p.scroll = min(max(p.scroll, 0), max(len(body)-avail, 0))
	end := min(p.scroll+avail, len(body))
	body, rows = body[p.scroll:end], rows[p.scroll:end]
	p.hits = settingsHits{top: len(lines), width: listW, rows: rows}

	for i := range max(len(body), len(detail)) {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		if i < len(detail) {
			line = padRight(line, listW) + "  " + detail[i]
		}
		lines = append(lines, line)
	}
	if len(below) > 0 {
		for len(lines) < h-len(below) {
			lines = append(lines, "")
		}
		lines = append(lines, below...)
	}
	return lines
}

// settingsHeader is the title, with sptui's version on a slab at the right
// and a line about this install under it.
func (m *Model) settingsHeader(cw int) []string {
	ver := strings.TrimSpace("sptui " + m.opts.Version)
	right := m.st.logoEdge.Render("▐") + m.st.logo.Render(" "+ver+" ") + m.st.logoEdge.Render("▌")
	if state := m.versionState(); state != "" {
		right = state + "  " + right
	}
	device := "remote control only"
	if m.opts.LocalDevice != "" {
		device = "plays here as “" + m.opts.LocalDevice + "”"
	}
	var who string
	if name := m.me.Name(); name != "" {
		who = "signed in as " + name
	}
	facts := joinNonEmpty(" · ", who, device, osName()+" "+runtime.GOARCH, "changes save as you go")
	return []string{
		spread(m.st.title.Render("Settings"), right, cw),
		m.st.subtitle.Render(clampWidth(facts, cw)),
		"",
	}
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	}
	return runtime.GOOS
}

// settingsList renders every setting under its section's heading, w wide.
// rows says which setting each line shows, -1 for headings and gaps.
func (m *Model) settingsList(list []setting, cursor, w int) (lines []string, rows []int) {
	section := ""
	for i, s := range list {
		if s.section != section {
			section = s.section
			if len(lines) > 0 {
				lines, rows = append(lines, ""), append(rows, -1)
			}
			lines, rows = append(lines, m.settingsHeading(section, w)), append(rows, -1)
		}
		lines, rows = append(lines, clampWidth(m.settingRow(s, i == cursor, w), w)), append(rows, i)
	}
	return lines, rows
}

// settingsHeading is a section's name on a slab, like the sidebar's, with a
// rule running to the edge.
func (m *Model) settingsHeading(name string, w int) string {
	tag := m.st.tagEdge.Render("▐") + m.st.tag.Render(strings.ToUpper(name)) + m.st.tagEdge.Render("▌")
	rule := max(w-lipgloss.Width(tag)-1, 0)
	return tag + " " + m.st.progressBg.Render(strings.Repeat("─", rule))
}

// settingRow is one setting: its name, then its value.
func (m *Model) settingRow(s setting, selected bool, w int) string {
	bar := "  "
	if selected {
		bar = m.st.cursorBar.Render("▌ ")
	}
	labelW := min(22, (w-2)/2)
	label := m.st.row.Render(fit(s.label, labelW))
	if selected {
		label = m.st.row.Bold(true).Render(fit(s.label, labelW))
	}
	var value string
	switch {
	case s.action != nil && s.get == nil:
		label = m.st.row.Render(s.label)
		if selected {
			label = m.st.rowPlaying.Bold(true).Render(s.label) + m.st.off.Render("  ↵")
		}
	case s.action != nil:
		value = m.st.rowMuted.Render(s.get(m.cfg))
		if selected {
			value = m.st.row.Render(s.get(m.cfg)) + m.st.off.Render("  ↵ copy")
		}
	case selected && m.inputMode == inputSetting:
		value = m.st.on.Render("› ") + m.input.View()
	case s.text:
		value = m.st.rowMuted.Render(s.get(m.cfg))
		if selected {
			value = m.st.rowPlaying.Render(s.get(m.cfg)) + m.st.off.Render("  ↵ edit")
		}
	default:
		value = m.choiceValue(s, selected, w-2-labelW)
	}
	if !strings.HasPrefix(ansi.Strip(value), " ") && !strings.HasPrefix(ansi.Strip(value), "▐") && value != "" {
		value = " " + value // in line with choices, which start with a slab's edge
	}
	if s.restart && m.needsRestart {
		value += m.st.stale.Render("  ↻")
	}
	return bar + label + value
}

// choiceValue shows a setting's choices side by side, the current one on a
// slab when the row is selected, if they fit in w. Otherwise it shows the
// current one, with arrows to step through the rest.
func (m *Model) choiceValue(s setting, selected bool, w int) string {
	cur := s.get(m.cfg)
	if len(s.choices) <= 4 {
		var b strings.Builder
		for i, c := range s.choices {
			if i > 0 {
				b.WriteString(" ")
			}
			switch {
			case c == cur && selected:
				b.WriteString(m.st.logoEdge.Render("▐") + m.st.logo.Render(c) + m.st.logoEdge.Render("▌"))
			case c == cur:
				b.WriteString(m.st.row.Bold(true).Render(" " + c + " "))
			default:
				b.WriteString(pick(selected, m.st.rowMuted, m.st.off).Render(" " + c + " "))
			}
		}
		if lipgloss.Width(b.String()) <= w {
			return b.String()
		}
	}
	if !selected {
		return m.swatch(s, cur) + m.st.rowMuted.Render(cur)
	}
	count := ""
	if i := slices.Index(s.choices, cur); i >= 0 {
		count = m.st.off.Render(fmt.Sprintf("  %d/%d", i+1, len(s.choices)))
	}
	return m.st.off.Render("◂ ") + m.swatch(s, cur) + m.st.rowPlaying.Bold(true).Render(cur) + m.st.off.Render(" ▸") + count
}

// swatch is a block of an accent choice's color, and nothing for other
// settings.
func (m *Model) swatch(s setting, choice string) string {
	if s.key != "theme.accent" {
		return ""
	}
	for _, a := range accents {
		if a.name == choice {
			hex := a.hex
			if hex == AccentFromCover {
				hex = m.accentHex()
			}
			return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("██") + " "
		}
	}
	return ""
}

// settingDetail is a panel, w wide and at most h tall, about the selected
// setting: where it's kept, every choice, and what it does.
func (m *Model) settingDetail(s setting, w, h int) []string {
	inner := w - 4
	lines := []string{m.st.modalTitle.Render(clampWidth(strings.ToUpper(s.label), inner))}
	if s.key != "" {
		lines = append(lines, m.st.off.Render(clampWidth(s.key+" in the config file", inner)))
	}
	lines = append(lines, "")
	switch {
	case len(s.choices) > 0:
		cur := s.get(m.cfg)
		for _, c := range s.choices {
			line := m.st.off.Render("○ ") + m.swatch(s, c) + m.st.rowMuted.Render(c)
			if c == cur {
				line = m.st.on.Render("● ") + m.swatch(s, c) + m.st.row.Bold(true).Render(c)
			}
			lines = append(lines, clampWidth(line, inner))
		}
		lines = append(lines, "")
	case s.text || s.get != nil:
		lines = append(lines, m.wrap(s.get(m.cfg), inner, m.st.row)...)
		lines = append(lines, "")
	}
	if s.help != "" {
		lines = append(lines, m.wrap(s.help, inner, m.st.subtitle)...)
	}
	if s.restart {
		note := "↻ applies the next time sptui starts"
		if m.needsRestart {
			note = "↻ restart from the bottom of the list to apply"
		}
		lines = append(lines, "", m.st.stale.Render(clampWidth(note, inner)))
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = lines[:min(len(lines), max(h-2, 1))]
	return strings.Split(m.st.panel.Padding(0, 1).Width(w).Render(strings.Join(lines, "\n")), "\n")
}

// wrap breaks text into lines w wide, each drawn with st.
func (m *Model) wrap(text string, w int, st lipgloss.Style) []string {
	wrapped := strings.Split(lipgloss.NewStyle().Width(w).Render(text), "\n")
	for i, l := range wrapped {
		wrapped[i] = st.Render(l)
	}
	return wrapped
}

// settingsClick selects the setting clicked at x, y in the main pane's
// content, or changes it when it's already selected.
func (m *Model) settingsClick(p *page, x, y int) tea.Cmd {
	y -= p.hits.top
	if y < 0 || y >= len(p.hits.rows) || x < 0 || x >= p.hits.width || p.hits.rows[y] < 0 {
		return nil
	}
	m.focus = focusMain
	i := p.hits.rows[y]
	if i != p.cursor {
		p.cursor = i
		return nil
	}
	list := m.settings()
	if i >= len(list) {
		return nil
	}
	return m.changeSetting(list[i], "enter")
}
