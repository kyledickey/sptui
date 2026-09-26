package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// DefaultAccent is used when the config does not set one.
const DefaultAccent = "#1ed760"

// styles is every lipgloss style the UI uses, built from a small palette.
type styles struct {
	accent color.Color
	text   color.Color
	muted  color.Color
	faint  color.Color
	subtle color.Color // selection background
	artist color.Color
	danger color.Color
	warn   color.Color

	logo        lipgloss.Style // the name, on an accent slab
	logoEdge    lipgloss.Style // the slab's half-block ends
	tag         lipgloss.Style // a sidebar heading, on a quiet slab
	tagEdge     lipgloss.Style
	crumb       lipgloss.Style
	crumbActive lipgloss.Style

	panel        lipgloss.Style
	panelFocused lipgloss.Style

	title    lipgloss.Style
	subtitle lipgloss.Style
	section  lipgloss.Style
	colHead  lipgloss.Style

	row        lipgloss.Style
	rowMuted   lipgloss.Style
	rowPlaying lipgloss.Style
	cursorBar  lipgloss.Style

	trackTitle  lipgloss.Style
	trackArtist lipgloss.Style
	progress    lipgloss.Style
	progressBg  lipgloss.Style
	on          lipgloss.Style
	off         lipgloss.Style

	key     lipgloss.Style
	keyDesc lipgloss.Style
	status  lipgloss.Style
	errText lipgloss.Style
	stale   lipgloss.Style

	modal      lipgloss.Style
	modalTitle lipgloss.Style
}

func newStyles(accentHex string, dark bool) styles {
	if accentHex == "" {
		accentHex = DefaultAccent
	}
	pick := lipgloss.LightDark(dark)
	s := styles{
		accent: lipgloss.Color(accentHex),
		text:   pick(lipgloss.Color("#1f2328"), lipgloss.Color("#e4e6eb")),
		muted:  pick(lipgloss.Color("#5f6670"), lipgloss.Color("#9aa0a9")),
		faint:  pick(lipgloss.Color("#b4b9c0"), lipgloss.Color("#474c55")),
		subtle: pick(lipgloss.Color("#e8ebef"), lipgloss.Color("#23272e")),
		artist: pick(lipgloss.Color("#7c4dcc"), lipgloss.Color("#b9a4f5")),
		danger: pick(lipgloss.Color("#c62828"), lipgloss.Color("#ff6b6b")),
		warn:   pick(lipgloss.Color("#9a6700"), lipgloss.Color("#b8914a")),
	}
	base := lipgloss.NewStyle()

	s.logo = base.Foreground(inkOn(accentHex)).Background(s.accent).Bold(true)
	s.logoEdge = base.Foreground(s.accent)
	s.tag = base.Foreground(s.text).Background(s.faint).Bold(true)
	s.tagEdge = base.Foreground(s.faint)
	s.crumb = base.Foreground(s.muted)
	s.crumbActive = base.Foreground(s.text).Bold(true)

	s.panel = base.Border(lipgloss.RoundedBorder()).BorderForeground(s.faint)
	s.panelFocused = s.panel.BorderForeground(s.accent)

	s.title = base.Foreground(s.text).Bold(true)
	s.subtitle = base.Foreground(s.muted)
	s.section = base.Foreground(s.muted).Bold(true)
	s.colHead = base.Foreground(s.muted)

	s.row = base.Foreground(s.text)
	s.rowMuted = base.Foreground(s.muted)
	s.rowPlaying = base.Foreground(s.accent)
	s.cursorBar = base.Foreground(s.accent)

	s.trackTitle = base.Foreground(s.text).Bold(true)
	s.trackArtist = base.Foreground(s.artist)
	s.progress = base.Foreground(s.accent)
	s.progressBg = base.Foreground(s.faint)
	s.on = base.Foreground(s.accent)
	s.off = base.Foreground(s.faint)

	s.key = base.Foreground(s.text).Bold(true)
	s.keyDesc = base.Foreground(s.muted)
	s.status = base.Foreground(s.accent)
	s.errText = base.Foreground(s.danger)
	s.stale = base.Foreground(s.warn)

	s.modal = base.Border(lipgloss.RoundedBorder()).BorderForeground(s.accent).Padding(0, 1)
	s.modalTitle = base.Foreground(s.accent).Bold(true)
	return s
}

// inkOn is a text colour that reads on a background of hex: near-black on
// light colours, white on dark ones.
func inkOn(hex string) color.Color {
	r, g, b, ok := parseHex(hex)
	if ok && 0.2126*r+0.7152*g+0.0722*b < 0.5 {
		return lipgloss.Color("#ffffff")
	}
	return lipgloss.Color("#0d1014")
}
