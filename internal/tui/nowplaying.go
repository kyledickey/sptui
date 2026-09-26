package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/spotify"
)

// The now-playing view has three panels, numbered like btop's: 1 the
// track (cover, info, controls), 2 lyrics, 3 up next. Pressing a number
// hides or shows its panel and the rest reflow:
//
//	╭─1 track──────╮╭─2 lyrics───────────────╮
//	│   ████████   ││   a line just sung     │
//	│   ████████   ││   THE LINE BEING SUNG  │
//	│    Title     │╰────────────────────────╯
//	│    Artist    │╭─3 up next──────────────╮
//	│  ━━━━──────  ││ …                      │
//	╰──────────────╯╰────────────────────────╯
//
// With the track panel hidden, a slim strip at the bottom keeps the song
// and progress in view.

// Panels of the now-playing view, by their key.
const (
	panelTrack  = '1'
	panelLyrics = '2'
	panelQueue  = '3'
)

// npInfoLines is the text under the cover: spacer, title, artist, album,
// spacer, progress, spacer, controls.
const npInfoLines = 8

// stripHeight is the bottom strip shown when the track panel is hidden.
const stripHeight = 4

// npLayout is the geometry of the now-playing view. Zero sizes are hidden.
type npLayout struct {
	height               int // of the whole view
	trackW               int // the track panel, full height
	sideW                int // lyrics and queue, stacked beside it
	lyricsH, queueH      int
	stripH               int // bottom strip when the track panel is hidden
	coverRows, coverCols int
}

// shown reports whether a now-playing panel is visible.
func (m *Model) shown(panel rune) bool {
	return strings.ContainsRune(m.cfg.Theme.NowPlayingPanels, panel)
}

func (m *Model) nowPlayingLayout() npLayout {
	h := max(m.height-headerHeight-footerHeight, 8)
	l := npLayout{height: h}
	track, lyrics, queue := m.shown(panelTrack), m.shown(panelLyrics), m.shown(panelQueue)
	side := h
	switch {
	case track && (lyrics || queue):
		// Beside the lyrics or queue: sized by the cover setting.
		want := map[string]int{"small": 34, "medium": 46, "large": 62}[m.cfg.Theme.NowPlayingCover]
		l.trackW = max(30, min(want, m.width*45/100))
		l.sideW = m.width - l.trackW
	case track:
		l.trackW = m.width // alone: as big as fits
	default:
		l.sideW = m.width
		l.stripH = stripHeight
		side = h - stripHeight
	}
	switch {
	case lyrics && queue:
		l.lyricsH = max(side*3/5, 5)
		l.queueH = side - l.lyricsH
	case lyrics:
		l.lyricsH = side
	case queue:
		l.queueH = side
	}
	if track {
		// The biggest square cover that fits above the track info.
		inner := l.trackW - 6
		l.coverRows = min(h-2-npInfoLines, inner)
		for l.coverRows > 0 && m.coverCols(l.coverRows) > inner {
			l.coverRows--
		}
		if l.coverRows < 4 {
			l.coverRows = 0
		}
		l.coverCols = m.coverCols(l.coverRows)
	}
	return l
}

// togglePanel hides or shows a now-playing panel, keeping at least one.
func (m *Model) togglePanel(panel rune) tea.Cmd {
	panels := m.cfg.Theme.NowPlayingPanels
	if strings.ContainsRune(panels, panel) {
		panels = strings.ReplaceAll(panels, string(panel), "")
	} else {
		panels += string(panel)
	}
	if panels == "" {
		m.setStatus("One panel has to stay", true)
		return nil
	}
	// Keep them in order so the config reads nicely.
	sorted := []rune(panels)
	slices.Sort(sorted)
	m.cfg.Theme.NowPlayingPanels = string(sorted)
	if m.opts.SaveConfig != nil {
		if err := m.opts.SaveConfig(m.cfg); err != nil {
			m.log.Warn("save panels", "err", err)
		}
	}
	return nil
}

// showingNowPlaying reports whether the now-playing view is on screen.
func (m *Model) showingNowPlaying() bool {
	p := m.current()
	return p != nil && p.nowPlaying
}

// lyricsState holds the lyrics of the playing track.
type lyricsState struct {
	uri      string // track the lyrics are for
	loading  bool
	lyrics   lyrics.Lyrics
	err      error
	attempts int       // failed tries while the lyrics service is down
	retryAt  time.Time // when the next try is due
}

type (
	lyricsMsg struct {
		uri    string
		lyrics lyrics.Lyrics
		err    error
	}
	lyricsRetryMsg struct{ uri string }
)

// lyricsRetries is how long to wait before each retry while the lyrics
// service is down; the last one repeats.
var lyricsRetries = []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}

// syncLyrics fetches lyrics for the playing track while the now-playing
// view is open. Lyrics already found this session are reused.
func (m *Model) syncLyrics() tea.Cmd {
	t := m.player.track()
	if !m.showingNowPlaying() || t == nil || t.URI == m.lyrics.uri || t.IsEpisode() {
		return nil
	}
	if found, ok := m.lyricsFound[t.URI]; ok {
		m.lyrics = found
		return nil
	}
	m.lyrics = lyricsState{uri: t.URI}
	return m.fetchLyrics(*t)
}

func (m *Model) fetchLyrics(track spotify.Track) tea.Cmd {
	m.lyrics.loading = true
	m.log.Debug("fetch lyrics", "track", track.Name, "attempt", m.lyrics.attempts+1)
	return tea.Batch(m.startSpinner(), m.call(func(ctx context.Context) tea.Msg {
		l, err := m.backend.Lyrics(ctx, track)
		return lyricsMsg{uri: track.URI, lyrics: l, err: err}
	}))
}

func (m *Model) setLyrics(msg lyricsMsg) tea.Cmd {
	if msg.uri != m.lyrics.uri {
		return nil // the song changed while fetching
	}
	st := lyricsState{uri: msg.uri, lyrics: msg.lyrics, err: msg.err, attempts: m.lyrics.attempts}
	switch {
	case msg.err == nil || errors.Is(msg.err, lyrics.ErrNotFound):
		m.lyricsFound[msg.uri] = st // a real answer; no need to ask again
	case errors.Is(msg.err, lyrics.ErrUnavailable):
		wait := lyricsRetries[min(st.attempts, len(lyricsRetries)-1)]
		st.attempts++
		st.retryAt = time.Now().Add(wait)
		m.log.Warn("lyrics service unavailable, will retry", "err", msg.err, "in", wait)
		m.lyrics = st
		uri := msg.uri
		return tea.Tick(wait, func(time.Time) tea.Msg { return lyricsRetryMsg{uri} })
	default:
		m.log.Warn("lyrics", "err", msg.err)
	}
	m.lyrics = st
	return nil
}

// retryLyrics tries again if the same song is still playing and shown.
func (m *Model) retryLyrics(msg lyricsRetryMsg) tea.Cmd {
	t := m.player.track()
	if t == nil || t.URI != msg.uri || m.lyrics.uri != msg.uri || !m.showingNowPlaying() {
		return nil
	}
	return m.fetchLyrics(*t)
}

// viewNowPlaying renders the whole body of the now-playing view.
func (m *Model) viewNowPlaying() string {
	l := m.nowPlayingLayout()
	focused := m.focus == focusMain && m.menu == nil
	var side []string
	if l.lyricsH > 0 {
		side = append(side, m.panel(panelLyrics, "lyrics", "", l.sideW, l.lyricsH, false,
			m.viewLyrics(l.sideW-4, l.lyricsH-2)))
	}
	if l.queueH > 0 {
		side = append(side, m.panel(panelQueue, "up next", m.cacheMarker(m.current().origin), l.sideW, l.queueH, focused,
			m.viewQueue(l.sideW-4, l.queueH-2)))
	}
	if l.stripH > 0 {
		side = append(side, m.st.panel.Padding(0, 1).Width(m.width).Height(l.stripH).Render(m.viewStrip(m.width-4)))
	}
	right := lipgloss.JoinVertical(lipgloss.Left, side...)
	if l.trackW == 0 {
		return right
	}
	left := m.panel(panelTrack, "track", "", l.trackW, l.height, false, m.viewTrackCard(l))
	if l.sideW == 0 {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// panel draws a box with btop-style title in its top border — the key that
// toggles it, then its name — and note at the right of the border.
func (m *Model) panel(key rune, title, note string, w, h int, focused bool, body string) string {
	border := m.st.faint
	if focused {
		border = m.st.accent
	}
	line := lipgloss.NewStyle().Foreground(border)
	label := m.st.on.Bold(true).Render(string(key)) + " " + m.st.section.Render(title)
	if note != "" {
		note = " " + note + " "
	}
	fill := max(0, w-4-lipgloss.Width(label)-lipgloss.Width(note))
	top := line.Render("╭─") + label + line.Render(" "+strings.Repeat("─", fill)) + note + line.Render("╮")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderTop(false).BorderForeground(border).
		Padding(0, 1).Width(w).Height(h - 1)
	return top + "\n" + box.Render(crop(body, w-4, h-2))
}

// crop cuts s to at most h lines of at most w cells, so content can never
// stretch a fixed-size box.
func crop(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	lines = lines[:min(len(lines), max(h, 0))]
	for i := range lines {
		lines[i] = clampWidth(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

// viewTrackCard is the cover, track info, progress and controls, centred
// as one block.
func (m *Model) viewTrackCard(l npLayout) string {
	w, h := l.trackW-4, l.height-2
	center := func(s string) string { return lipgloss.PlaceHorizontal(w, lipgloss.Center, clampWidth(s, w)) }
	t := m.player.track()
	if t == nil {
		return m.notice(w, h, "♪", "Nothing playing", "Pick a song and press enter.", m.st.subtitle)
	}

	var lines []string
	if l.coverRows > 0 {
		for _, row := range strings.Split(m.coverView(m.thumbURL(), l.coverCols, l.coverRows), "\n") {
			lines = append(lines, center(row))
		}
		lines = append(lines, "")
	}
	title := t.Name
	if m.player.liked {
		title += m.st.on.Render("  ♥")
	}
	lines = append(lines,
		center(m.st.trackTitle.Render(title)),
		center(m.st.trackArtist.Render(t.ArtistNames())),
		center(m.st.subtitle.Render(joinNonEmpty(" · ", t.Album.Name, t.Album.Year()))),
		"",
		center(m.progressLine(t, min(w, max(l.coverCols, 40)))),
		"",
		center(m.controlsLine(m.player.state, w)),
	)
	return lipgloss.PlaceVertical(h, lipgloss.Center, strings.Join(lines, "\n"))
}

// viewStrip is the slim now-playing strip shown when the track panel is
// hidden: the song on one line, progress and controls on the next.
func (m *Model) viewStrip(w int) string {
	t := m.player.track()
	if t == nil {
		return m.st.subtitle.Render("Nothing playing") + "\n" + m.st.off.Render("press 1 to show the track panel")
	}
	icon := "▶  "
	if !m.player.playing() {
		icon = "‖  "
	}
	song := m.st.on.Render(icon) + m.st.trackTitle.Render(t.Name) +
		m.st.subtitle.Render("  ·  ") + m.st.trackArtist.Render(t.ArtistNames())
	controls := m.controlsLine(m.player.state, w/2)
	return clampWidth(song, w) + "\n" + spread(m.progressLine(t, w-lipgloss.Width(controls)-3), controls, w)
}

// progressLine is "1:23 ━━━━━───── 3:45" at width w.
func (m *Model) progressLine(t *spotify.Track, w int) string {
	pos, dur := m.player.progress(time.Now()), t.Duration()
	elapsed, total := clock(pos), clock(dur)
	barW := max(w-len(elapsed)-len(total)-4, 5)
	filled := 0
	if dur > 0 {
		filled = min(max(int(float64(barW)*float64(pos)/float64(dur)), 0), barW)
	}
	bar := m.st.progress.Render(strings.Repeat("━", filled)) + m.st.progressBg.Render(strings.Repeat("─", barW-filled))
	return m.st.subtitle.Render(elapsed) + "  " + bar + "  " + m.st.subtitle.Render(total)
}

// controlsLine shows play state, shuffle, repeat and volume in w cells,
// dropping the words when they don't fit.
func (m *Model) controlsLine(st *spotify.PlaybackState, w int) string {
	onOff := func(on bool, label string) string {
		if on {
			return m.st.on.Render(label)
		}
		return m.st.off.Render(label)
	}
	shuffle, repeat := onOff(st.ShuffleState, "⇄ shuffle"), onOff(st.RepeatState != spotify.RepeatOff, "↻ "+repeatLabel(st.RepeatState))
	icon := m.st.on.Render("▶ playing")
	if !st.IsPlaying {
		icon = m.st.subtitle.Render("‖ paused")
	}
	full := icon + "   " + shuffle + "   " + repeat + "   " + m.volumeBar()
	if lipgloss.Width(full) <= w {
		return full
	}
	icon = m.st.on.Render("▶")
	if !st.IsPlaying {
		icon = m.st.subtitle.Render("‖")
	}
	vol := ""
	if v := m.player.volume(); v >= 0 {
		vol = m.st.subtitle.Render(fmt.Sprintf("vol %d%%", v))
	}
	return icon + "  " + onOff(st.ShuffleState, "⇄") + "  " + repeat + "  " + vol
}

// viewLyrics shows lyrics in a w×h box. Synced lyrics follow the song with
// the current line a third of the way down; plain lyrics show from the top.
func (m *Model) viewLyrics(w, h int) string {
	ly := m.lyrics
	switch {
	case m.player.track() == nil:
		return m.notice(w, h, "♪", "", "Lyrics show up here while a song plays.", m.st.subtitle)
	case m.player.track().IsEpisode():
		return m.notice(w, h, "◉", "", "Podcasts don't have lyrics.", m.st.subtitle)
	case ly.loading && ly.attempts == 0:
		return m.notice(w, h, m.spinner.View(), "", "Finding lyrics…", m.st.subtitle)
	case ly.loading:
		return m.notice(w, h, m.spinner.View(), "Trying LRCLIB again…", "", m.st.subtitle)
	case errors.Is(ly.err, lyrics.ErrNotFound):
		return m.notice(w, h, "♪", "No lyrics for this one",
			"LRCLIB, where sptui finds lyrics, doesn't have them for this song yet.", m.st.subtitle)
	case errors.Is(ly.err, lyrics.ErrUnavailable):
		wait := max(time.Until(ly.retryAt), 0).Round(time.Second)
		when := fmt.Sprintf("%ds", int(wait.Seconds()))
		if wait >= time.Minute {
			when = clock(wait)
		}
		return m.notice(w, h, "☁", "Lyrics are unavailable right now",
			"LRCLIB, where sptui finds lyrics, isn't answering.\nTrying again in "+when+".", m.st.stale)
	case ly.err != nil:
		return m.notice(w, h, "!", "Couldn't load lyrics", friendly(ly.err), m.st.stale)
	case ly.lyrics.Instrumental:
		return m.notice(w, h, "♪ ♪ ♪", "Instrumental", "Just the music on this one.", m.st.subtitle)
	}

	cur := ly.lyrics.Current(m.player.progress(time.Now()))
	var rows []string
	curRow := 0
	for i, line := range ly.lyrics.Lines {
		text := line.Text
		if text == "" {
			text = "♪"
		}
		st := m.st.subtitle
		switch {
		case !ly.lyrics.Synced:
			st = m.st.row
		case i == cur:
			st = m.st.rowPlaying.Bold(true)
			curRow = len(rows)
		case i < cur:
			st = m.st.off
		}
		wrapped := lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(text)
		for _, r := range strings.Split(wrapped, "\n") {
			rows = append(rows, st.Render(r))
		}
	}
	start := 0
	if ly.lyrics.Synced && cur >= 0 {
		start = max(0, min(curRow-h/3, len(rows)-h))
	}
	end := min(start+h, len(rows))
	return strings.Join(rows[start:end], "\n")
}

// notice fills a w×h box with a centred message: a glyph, a title and a
// quieter explanation. tone colours the glyph and title.
func (m *Model) notice(w, h int, glyph, title, detail string, tone lipgloss.Style) string {
	center := lipgloss.NewStyle().Width(w).Align(lipgloss.Center)
	var parts []string
	if glyph != "" {
		parts = append(parts, center.Render(tone.Bold(true).Render(glyph)), "")
	}
	if title != "" {
		parts = append(parts, center.Render(tone.Bold(true).Render(title)))
	}
	if detail != "" {
		parts = append(parts, center.Render(m.st.subtitle.Render(detail)))
	}
	return lipgloss.PlaceVertical(h, lipgloss.Center, strings.Join(parts, "\n"))
}

// viewQueue lists upcoming songs in a w×h box.
func (m *Model) viewQueue(w, h int) string {
	p := m.current()
	switch {
	case p.err != nil && len(p.rows) == 0:
		return m.st.subtitle.Render(clampWidth(friendly(p.err), w))
	case len(p.visible) == 0 && !p.loading:
		return m.st.rowMuted.Render(clampWidth(p.empty, w))
	}
	p.scrollTo(h)
	var lines []string
	for i := p.scroll; i < min(p.scroll+h, len(p.visible)); i++ {
		lines = append(lines, m.renderRow(p, i, w, m.focus == focusMain && m.menu == nil))
	}
	return strings.Join(lines, "\n")
}
