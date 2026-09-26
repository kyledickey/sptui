package tui

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	restart bool     // takes effect after restarting sptui
	get     func(config.Config) string
	set     func(*config.Config, string) error
	action  func() tea.Cmd // a button instead of a value
}

// accents are the colour presets, in the order they cycle.
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
			section: "Appearance", label: "Accent colour",
			help:    "From cover art takes the colour from whatever's playing, so sptui re-themes itself with every song.",
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
			section: "Appearance", label: "Scroll long titles",
			help:    "Titles too long to fit slide past, like a marquee. Off cuts them short with …",
			choices: []string{"on", "off"},
			get:     func(c config.Config) string { return onOff(c.Theme.ScrollTitles) },
			set:     func(c *config.Config, v string) error { c.Theme.ScrollTitles = v == "on"; return nil },
		},
		{
			section: "Appearance", label: "Album art",
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
			section: "Appearance", label: "Now playing cover",
			help:    "How wide the track panel (and its cover) is in the now-playing view (press o). There, 1 2 3 hide or show the track, lyrics and up next panels.",
			choices: []string{"small", "medium", "large"},
			get:     func(c config.Config) string { return c.Theme.NowPlayingCover },
			set:     func(c *config.Config, v string) error { c.Theme.NowPlayingCover = v; return nil },
		},
		{
			section: "Appearance", label: "Startup animation",
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
			section: "Player", label: "Play music in sptui", restart: true,
			help:    "Off turns sptui into a remote for your other Spotify devices (phone, speakers, the desktop app).",
			choices: []string{"on", "off"},
			get:     func(c config.Config) string { return onOff(c.Player.Enabled) },
			set:     func(c *config.Config, v string) error { c.Player.Enabled = v == "on"; return nil },
		},
		{
			section: "Player", label: "Keep screen awake",
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
			section: "Player", label: "Device name", restart: true, text: true,
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
			section: "Player", label: "Audio output", restart: true,
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
			section: "Player", label: "Quality", restart: true,
			choices: []string{"96 kbps", "160 kbps", "320 kbps"},
			get:     func(c config.Config) string { return fmt.Sprintf("%d kbps", c.Player.Bitrate) },
			set: func(c *config.Config, v string) error {
				_, err := fmt.Sscanf(v, "%d kbps", &c.Player.Bitrate)
				return err
			},
		},
		{
			section: "Spotify", label: "Your own Spotify app", restart: true, text: true,
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

func (m *Model) copy(text, done string) tea.Cmd {
	m.setStatus(done, false)
	return tea.SetClipboard(text)
}

func settingsPage() *page {
	p := newPage("Settings", kindHeader, nil)
	p.next = -1
	p.settings = true
	p.subtitle = "enter or ←/→ to change · saved as you go"
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
		switch {
		case s.action != nil:
			if msg.String() == "enter" {
				return s.action(), true
			}
		case s.text:
			if msg.String() == "enter" {
				m.editSetting(s)
			}
		default:
			step := 1
			if msg.String() == "left" {
				step = -1
			}
			i := slices.Index(s.choices, s.get(m.cfg))
			next := s.choices[(i+step+len(s.choices))%len(s.choices)]
			return m.applySetting(s, next), true
		}
	default:
		return nil, false
	}
	return nil, true
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

// viewSettings renders the settings list and the selected setting's help.
func (m *Model) viewSettings(p *page, cw, h int) []string {
	list := m.settings()
	p.cursor = min(max(p.cursor, 0), len(list)-1)
	labelW := 28
	var lines []string
	section := ""
	for i, s := range list {
		if s.section != section {
			section = s.section
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.st.section.Foreground(m.st.accent).Render(strings.ToUpper(section)))
		}
		selected := i == p.cursor
		bar := "  "
		if selected {
			bar = m.st.cursorBar.Render("▌ ")
		}
		label := m.st.row.Render(fit(s.label, labelW))
		var value string
		switch {
		case s.action != nil:
			label = m.st.row.Render(s.label)
			if selected {
				label = m.st.rowPlaying.Bold(true).Render(s.label + "  ↵")
			}
		case selected && m.inputMode == inputSetting:
			value = m.st.on.Render("› ") + m.input.View()
		case s.text:
			value = m.st.rowMuted.Render(s.get(m.cfg))
			if selected {
				value = m.st.rowPlaying.Render(s.get(m.cfg)) + m.st.off.Render("  enter to edit")
			}
		default:
			value = m.st.rowMuted.Render(s.get(m.cfg))
			if selected {
				value = m.st.off.Render("◂ ") + m.st.rowPlaying.Bold(true).Render(s.get(m.cfg)) + m.st.off.Render(" ▸")
			}
		}
		if s.restart && m.needsRestart {
			value += m.st.off.Render("  ↻")
		}
		lines = append(lines, clampWidth(bar+label+value, cw))
	}

	// Help for the selected setting, pinned to the bottom.
	if help := list[p.cursor].help; help != "" {
		wrapped := strings.Split(lipgloss.NewStyle().Width(cw).Render(help), "\n")
		for len(lines)+len(wrapped)+1 < h {
			lines = append(lines, "")
		}
		lines = append(lines, "")
		for _, w := range wrapped {
			lines = append(lines, m.st.subtitle.Render(w))
		}
	}
	return lines
}
