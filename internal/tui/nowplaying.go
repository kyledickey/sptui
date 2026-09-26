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

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/spotify"
)

// The now-playing view has three panels, numbered like btop's: 1 the
// track (cover, info, controls), 2 lyrics, 3 up next. Pressing a number
// hides or shows its panel and the rest reflow. The track panel has no box
// of its own:
//
//	                 ╭─2 lyrics───────────────╮
//	   ████████      │   a line just sung     │
//	   ████████      │   THE LINE BEING SUNG  │
//	    Title        ╰────────────────────────╯
//	    Artist       ╭─3 up next──────────────╮
//	  ━━━●──────     │ …                      │
//	╭───╮╭───╮╭───╮  │                        │
//	│ ⇄ ││ ▶ ││ ↻ │  │                        │
//	╰ s ╯╰spc╯╰ r ╯  │                        │
//	♥ liked vol ▮▮▮  ╰────────────────────────╯
//
// Alone, the track panel puts the cover and the info side by side.
//
// With the track panel hidden, a slim strip at the bottom keeps the song
// and progress in view.

// Panels of the now-playing view, by their key.
const (
	panelTrack  = '1'
	panelLyrics = '2'
	panelQueue  = '3'
)

// Room for the track info. Stacked under the cover: spacer, title,
// artist, album, spacer, progress, spacer, buttons, spacer, like and
// volume. Solo (beside the cover) adds a heading and what's up next.
const (
	soloInfoW = 54 // the info column beside the cover
	soloGap   = 6  // between cover and info
	bigRows   = 3  // height of the keycap buttons
	bigNeeds  = 16 // view height from which the buttons are big
)

func stackInfoLines(big bool) int {
	if big {
		return 10 + bigRows - 1
	}
	return 10
}

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
	solo                 bool // the track panel alone: cover and info side by side
	big                  bool // room for the big transport buttons
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
		l.trackW = m.width
		l.solo = true
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
		l.big = h-2 >= bigNeeds
		// Stacked: the biggest square cover that fits above the info.
		// Solo: beside the info, and no taller than 3/5 of the view so
		// it doesn't swallow the screen.
		maxCols, minRows := l.trackW-6, 4
		l.coverRows = min(h-3-stackInfoLines(l.big), maxCols)
		if l.solo {
			maxCols, minRows = m.width-4-soloInfoW-soloGap, 8
			l.coverRows = min(h-2, h*3/5)
		}
		for l.coverRows > 0 && m.coverCols(l.coverRows) > maxCols {
			l.coverRows--
		}
		if l.coverRows < minRows || m.covers.mode == art.Off {
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
	// The track panel is the view's centrepiece, so it goes without a box.
	left := lipgloss.NewStyle().Padding(1, 2).Width(l.trackW).Height(l.height).
		Render(crop(m.viewTrackCard(l), l.trackW-4, l.height-2))
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
// as one block: stacked beside the other panels, side by side when alone.
func (m *Model) viewTrackCard(l npLayout) string {
	w, h := l.trackW-4, l.height-2
	t := m.player.track()
	if t == nil {
		return m.notice(w, h, "♪", "Nothing playing", "Pick a song and press enter.", m.st.subtitle)
	}
	var cover string
	if l.coverRows > 0 {
		cover = m.coverView(m.thumbURL(), l.coverCols, l.coverRows)
	}
	if l.solo {
		info := m.soloInfo(t, min(soloInfoW, w), l.big)
		block := info
		if cover != "" {
			block = lipgloss.JoinHorizontal(lipgloss.Center, cover, strings.Repeat(" ", soloGap), info)
		}
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
	}

	center := func(s string) string { return lipgloss.PlaceHorizontal(w, lipgloss.Center, clampWidth(s, w)) }
	var lines []string
	if cover != "" {
		for row := range strings.SplitSeq(cover, "\n") {
			lines = append(lines, center(row))
		}
		lines = append(lines, "")
	}
	barW := min(w, max(l.coverCols, 40))
	lines = append(lines,
		center(m.st.trackTitle.Render(m.scroll(t.Name, w, m.player.since))),
		center(m.st.trackArtist.Render(t.ArtistNames())),
		center(m.st.subtitle.Render(trackMeta(t, m.player.progress(time.Now())))),
		"",
		center(m.progressLine(t, barW)),
		"",
	)
	for _, row := range m.transport(m.player.state, l.big, w) {
		lines = append(lines, center(row))
	}
	lines = append(lines, "", center(m.likeAndVolume(min(barW, capsW))))
	return lipgloss.PlaceVertical(h, lipgloss.Center, strings.Join(lines, "\n"))
}

// soloInfo is the column beside the cover when the track panel is alone:
// a heading, the track, progress, buttons, like and volume, and what's up
// next, all left-aligned in w cells.
func (m *Model) soloInfo(t *spotify.Track, w int, big bool) string {
	st := m.player.state
	heading := m.st.on.Render("●") + m.st.section.Render(" NOW PLAYING")
	if !st.IsPlaying {
		heading = m.st.off.Render("‖") + m.st.section.Render(" PAUSED")
	}
	if st.Device.Name != "" {
		heading += m.st.off.Render(" on ") + m.st.subtitle.Render(st.Device.Name)
	}
	lines := []string{
		heading,
		"",
		m.st.trackTitle.Render(m.scroll(t.Name, w, m.player.since)),
		m.st.trackArtist.Render(t.ArtistNames()),
		m.st.subtitle.Render(trackMeta(t, m.player.progress(time.Now()))),
		"",
		m.progressLine(t, w),
		"",
	}
	lines = append(lines, m.transport(st, big, w)...)
	lines = append(lines, "", m.likeAndVolume(min(w, capsW)))
	if next := m.upNext(); next != nil {
		lines = append(lines, "", m.st.colHead.Render("UP NEXT  ")+
			m.st.row.Render(next.Name)+m.st.subtitle.Render(" · "+next.ArtistNames()))
	}
	for i := range lines {
		lines[i] = clampWidth(lines[i], w)
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

// upNext is the first track in the queue, if it's loaded.
func (m *Model) upNext() *spotify.Track {
	if p := m.current(); p != nil && p.nowPlaying {
		for _, r := range p.rows {
			if r.kind == kindTrack {
				return &r.track
			}
		}
	}
	return nil
}

// trackMeta is the line under the artist: album and year for a song; for
// an episode, that it's a podcast, when it came out and how much is left.
func trackMeta(t *spotify.Track, pos time.Duration) string {
	if t.IsEpisode() {
		left := ""
		if rest := t.Duration() - pos; rest > time.Minute {
			left = fmt.Sprintf("%d min left", int(rest.Minutes()))
		}
		return joinNonEmpty(" · ", "Podcast", t.ReleaseDate, left)
	}
	return joinNonEmpty(" · ", t.Album.Name, t.Album.Year())
}

// capsW is the width of the row of keycap buttons: five caps of
// capInner+2 cells with a space between.
const (
	capInner = 5
	capsW    = 5*(capInner+2) + 4
)

// keycap draws a bigRows-tall button whose face is inner cells wide: glyph
// in the middle, the key that presses it set into the bottom border. Lit
// caps glow in the accent.
func (m *Model) keycap(glyph, key string, lit bool, inner int) [bigRows]string {
	edge, face, label := m.st.off, m.st.trackTitle, m.st.subtitle
	if lit {
		edge, face, label = m.st.on, m.st.on, m.st.on.Bold(true)
	}
	pad := inner - lipgloss.Width(glyph)
	mid := strings.Repeat(" ", pad/2) + face.Render(glyph) + strings.Repeat(" ", pad-pad/2)
	name := key
	if lipgloss.Width(name) < inner {
		name = " " + name + " "
	}
	dashes := max(inner-lipgloss.Width(name), 0)
	bottom := edge.Render("╰"+strings.Repeat("─", dashes/2)) + label.Render(name) +
		edge.Render(strings.Repeat("─", dashes-dashes/2)+"╯")
	return [bigRows]string{
		edge.Render("╭" + strings.Repeat("─", inner) + "╮"),
		edge.Render("│") + mid + edge.Render("│"),
		bottom,
	}
}

// joinCaps lays keycaps side by side, a space apart.
func joinCaps(caps ...[bigRows]string) [bigRows]string {
	var rows [bigRows]string
	for i := range rows {
		parts := make([]string, len(caps))
		for j, c := range caps {
			parts[j] = c[i]
		}
		rows[i] = strings.Join(parts, " ")
	}
	return rows
}

// transport draws shuffle, previous, play/pause, next and repeat as
// keycaps when there's room (capsW wide, bigRows tall), or on one line.
// Play/pause shows what's happening now.
func (m *Model) transport(st *spotify.PlaybackState, big bool, w int) []string {
	shuffle, repeat := st.ShuffleState, st.RepeatState != spotify.RepeatOff
	repeatGlyph := "↻"
	if st.RepeatState == spotify.RepeatTrack {
		repeatGlyph = "↻¹"
	}
	state := "▶"
	if !st.IsPlaying {
		state = "‖"
	}
	if !big || w < capsW {
		lit := func(on bool, glyph string) string {
			if on {
				return m.st.on.Render(glyph)
			}
			return m.st.off.Render(glyph)
		}
		return []string{strings.Join([]string{
			lit(shuffle, "⇄"), m.st.subtitle.Render("◀◀"), lit(st.IsPlaying, state),
			m.st.subtitle.Render("▶▶"), lit(repeat, repeatGlyph),
		}, "    ")}
	}
	rows := joinCaps(
		m.keycap("⇄", "s", shuffle, capInner),
		m.keycap("◀◀", "p", false, capInner),
		m.keycap(state, "space", st.IsPlaying, capInner),
		m.keycap(" ▶▶", "n", false, capInner), // mirrors ◀◀, which sits left of centre
		m.keycap(repeatGlyph, "r", repeat, capInner),
	)
	return rows[:]
}

// likeAndVolume is the line under the buttons: the heart at the left and
// the volume bar at the right of w cells.
func (m *Model) likeAndVolume(w int) string {
	heart := m.st.off.Render("♡ like")
	if m.player.liked {
		heart = m.st.on.Render("♥ liked")
	}
	vol := m.volumeBar()
	if lipgloss.Width(heart)+lipgloss.Width(vol)+2 > w {
		return vol
	}
	return spread(heart, vol, w)
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

// progressLine is "1:23 ━━━━━●──── 3:45" at width w.
func (m *Model) progressLine(t *spotify.Track, w int) string {
	pos, dur := m.player.progress(time.Now()), t.Duration()
	elapsed, total := clock(pos), clock(dur)
	barW := max(w-len(elapsed)-len(total)-4, 5)
	filled := 0
	if dur > 0 {
		filled = min(max(int(float64(barW)*float64(pos)/float64(dur)), 0), barW)
	}
	// A knob marks the playhead.
	filled = min(filled, barW-1)
	bar := m.st.progress.Render(strings.Repeat("━", filled)+"●") + m.st.progressBg.Render(strings.Repeat("─", barW-filled-1))
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
		for r := range strings.SplitSeq(wrapped, "\n") {
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
