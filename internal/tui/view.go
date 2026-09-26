package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kyledickey/sptui/internal/spotify"
)

// Fixed heights of the chrome around the two panes.
const (
	headerHeight = 1
	footerHeight = 1
	playerHeight = 5
	pageHeader   = 4 // title, subtitle, spacer/input, column names (without a cover)
	minWidth     = 60
	minHeight    = 16
)

// --- layout ---

func (m *Model) sidebarWidth() int { return min(max(m.width/4, 24), 34) }
func (m *Model) mainWidth() int    { return m.width - m.sidebarWidth() }
func (m *Model) bodyHeight() int {
	return max(m.height-headerHeight-footerHeight-playerHeight, 6)
}

// contentWidth is the usable width inside the main panel.
func (m *Model) contentWidth() int { return m.mainWidth() - 4 }

// headerHeight is how many lines a page's header takes: a cover (when it
// has one and there's room) plus a spacer and the column names, or the
// plain title block.
func (m *Model) headerHeight(p *page) int {
	if rows := m.pageCoverRows(); p != nil && p.cover != "" && rows > 0 {
		return rows + 2
	}
	return pageHeader
}

// listHeight is how many rows fit in the main panel.
func (m *Model) listHeight() int {
	if m.showingNowPlaying() {
		return max(m.nowPlayingLayout().queueH-2, 1)
	}
	h := m.bodyHeight() - 2 - m.headerHeight(m.current())
	if p := m.current(); p != nil && (m.inputMode == inputFilter || p.filter != "") {
		h--
	}
	return max(h, 1)
}

// View renders the whole screen.
func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sptui"
	if t := m.player.track(); t != nil {
		v.WindowTitle = t.Name + " · " + t.ArtistNames()
	}
	if m.width == 0 {
		return v
	}
	if m.width < minWidth || m.height < minHeight {
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.st.subtitle.Render("Make the terminal a little bigger")))
		return v
	}

	var screen string
	if m.showingNowPlaying() {
		// The view shows everything the player bar would, only bigger.
		screen = lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), m.viewNowPlaying(), m.viewFooter())
	} else {
		body := lipgloss.JoinHorizontal(lipgloss.Top, m.viewSidebar(), m.viewMain())
		screen = lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewPlayer(), m.viewFooter())
	}

	switch {
	case m.showHelp:
		screen = m.overlay(screen, m.viewHelp())
	case m.menu != nil:
		screen = m.overlay(screen, m.viewMenu())
	}
	v.SetContent(screen)
	return v
}

// overlay draws box centred on top of base, cropped to the screen.
func (m *Model) overlay(base, box string) string {
	lines := strings.Split(box, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = clampWidth(lines[i], m.width)
	}
	box = strings.Join(lines, "\n")
	x := max(0, (m.width-lipgloss.Width(box))/2)
	y := max(0, (m.height-lipgloss.Height(box))/2)
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

// spread puts left and right on one line of width w.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return clampWidth(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- header and footer ---

func (m *Model) viewHeader() string {
	left := m.st.logo.Render(" ◆ sptui")
	var crumbs []string
	for i, p := range m.stack {
		title := p.title
		if i == len(m.stack)-1 {
			crumbs = append(crumbs, m.st.crumbActive.Render(title))
		} else {
			crumbs = append(crumbs, m.st.crumb.Render(title))
		}
	}
	if len(crumbs) > 0 {
		left += m.st.crumb.Render("   ") + strings.Join(crumbs, m.st.crumb.Render(" › "))
	}
	right := m.st.crumb.Render(m.me.Name()+"  ") + m.st.key.Render(",") + m.st.keyDesc.Render(" settings  ") +
		m.st.key.Render("?") + m.st.keyDesc.Render(" help ")
	return spread(left, right, m.width)
}

func (m *Model) viewFooter() string {
	var hints []key.Binding
	k := m.keys
	enter := func(desc string) key.Binding { return key.NewBinding(key.WithHelp("enter", desc)) }
	esc := func(desc string) key.Binding { return key.NewBinding(key.WithHelp("esc", desc)) }
	switch {
	case m.showHelp:
		hints = []key.Binding{esc("close")}
	case m.menu != nil:
		hints = []key.Binding{k.Up, k.Down, enter("select"), esc("close")}
	case m.inputMode == inputSearch:
		hints = []key.Binding{enter("search"), key.NewBinding(key.WithHelp("↓", "results")), esc("done")}
	case m.inputMode == inputFilter:
		hints = []key.Binding{enter("keep filter"), esc("clear")}
	case m.inputMode == inputSetting:
		hints = []key.Binding{enter("save"), esc("cancel")}
	case m.current() != nil && m.current().settings:
		hints = []key.Binding{k.Up, k.Down, enter("change"), key.NewBinding(key.WithHelp("←/→", "cycle")), esc("back")}
	case m.showingNowPlaying():
		hints = []key.Binding{key.NewBinding(key.WithHelp("1 2 3", "panels")), k.PlayPause, k.Next, k.Prev, k.SeekFwd, enter("play"), esc("back"), k.Help}
	case m.focus == focusSidebar:
		hints = []key.Binding{enter("open"), k.Focus, k.Search, k.PlayPause, k.Devices, k.Help}
	default:
		hints = []key.Binding{enter("play/open"), k.Menu, k.Like, k.Queue, k.Filter, k.Search, esc("back"), k.Help}
	}
	var parts []string
	for _, b := range hints {
		h := b.Help()
		parts = append(parts, m.st.key.Render(h.Key)+" "+m.st.keyDesc.Render(h.Desc))
	}
	left := " " + strings.Join(parts, m.st.keyDesc.Render("  ·  "))

	right := ""
	if m.status.text != "" {
		st := m.st.status
		if m.status.err {
			st = m.st.errText
		}
		right = st.Render(m.status.text) + " "
		// The status matters more than the hints; make room for it.
		left = clampWidth(left, m.width-lipgloss.Width(right)-2)
	}
	return spread(left, right, m.width)
}

// --- sidebar ---

func (m *Model) viewSidebar() string {
	w, h := m.sidebarWidth(), m.bodyHeight()
	inner, rows := w-2, h-2
	sb := &m.sidebar
	sb.scrollTo(rows)
	focused := m.focus == focusSidebar && m.menu == nil

	var lines []string
	for i := sb.scroll; i < len(sb.items) && len(lines) < rows; i++ {
		it := sb.items[i]
		if it.header {
			label := " " + strings.ToUpper(it.label) + " "
			mark := ""
			if it.label == "Playlists" {
				if mark = m.cacheMarker(sb.origin); mark != "" {
					mark = " " + mark
				}
			}
			rule := strings.Repeat("─", max(0, inner-lipgloss.Width(label)-lipgloss.Width(mark)-1))
			lines = append(lines, clampWidth(m.st.section.Render(label)+m.st.progressBg.Render(rule)+mark, inner))
			continue
		}
		icon := it.icon
		if icon == "" {
			icon = " "
		}
		text := fit(" "+icon+" "+it.label, inner-1)
		switch {
		case i == sb.cursor && focused:
			lines = append(lines, m.st.cursorBar.Background(m.st.subtle).Render("▌")+
				m.st.row.Background(m.st.subtle).Bold(true).Render(text))
		case i == sb.active:
			lines = append(lines, " "+m.st.rowPlaying.Bold(true).Render(text))
		case i == sb.cursor:
			lines = append(lines, " "+m.st.row.Render(text))
		default:
			lines = append(lines, " "+m.st.rowMuted.Render(text))
		}
	}
	if !sb.loaded && rows > len(lines) {
		lines = append(lines, m.st.rowMuted.Render("   loading playlists…"))
	}

	style := m.st.panel
	if focused {
		style = m.st.panelFocused
	}
	return style.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// --- main pane ---

func (m *Model) viewMain() string {
	w, h := m.mainWidth(), m.bodyHeight()
	cw := m.contentWidth()
	focused := m.focus == focusMain && m.menu == nil
	style := m.st.panel.Padding(0, 1)
	if focused {
		style = m.st.panelFocused.Padding(0, 1)
	}

	p := m.current()
	if p == nil {
		msg := m.spinner.View() + " Loading your library…"
		if m.status.err {
			msg = m.st.errText.Render(m.status.text)
		}
		return style.Width(w).Height(h).Render(lipgloss.Place(cw, h-2, lipgloss.Center, lipgloss.Center, msg))
	}

	lines := m.pageHeader(p, cw)
	if p.settings {
		lines = append(append(lines[:2], ""), m.viewSettings(p, cw, h-2-3)...)
		for i := range lines {
			lines[i] = clampWidth(lines[i], cw)
		}
		return style.Width(w).Height(h).Render(strings.Join(lines[:min(len(lines), h-2)], "\n"))
	}

	listH := m.listHeight()
	p.scrollTo(listH)
	switch {
	case p.err != nil && len(p.rows) == 0:
		lines = append(lines, m.pageError(p, cw)...)
	case len(p.visible) == 0 && !p.loading:
		empty := p.empty
		if p.filter != "" {
			empty = "Nothing matches “" + p.filter + "”"
		}
		lines = append(lines, m.st.rowMuted.Render(clampWidth(empty, cw)))
	default:
		end := min(p.scroll+listH, len(p.visible))
		for i := p.scroll; i < end; i++ {
			lines = append(lines, m.renderRow(p, i, cw, focused))
		}
	}

	// Pad so the filter line sits at the bottom.
	for len(lines) < h-2-1 {
		lines = append(lines, "")
	}
	switch {
	case m.inputMode == inputFilter:
		lines = append(lines, m.st.on.Render("filter › ")+m.input.View())
	case p.filter != "":
		lines = append(lines, m.st.on.Render("filter › ")+m.st.row.Render(p.filter)+m.st.rowMuted.Render("   esc to clear"))
	}
	for i := range lines {
		lines[i] = clampWidth(lines[i], cw)
	}
	if len(lines) > h-2 {
		lines = lines[:h-2]
	}
	return style.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// pageHeader renders the top of a page: a cover with the title beside it
// when there is one, else a title block. It is headerHeight(p) lines.
func (m *Model) pageHeader(p *page, cw int) []string {
	// Right of the title: a loading indicator or a count.
	right := ""
	switch {
	case p.loading:
		right = m.spinner.View() + m.st.subtitle.Render(" loading")
	case p.filter != "":
		right = m.st.subtitle.Render(fmt.Sprintf("%d of %d", m.countVisible(p), len(p.rows)))
	case p.total > 0 && !p.isSearch:
		right = m.st.subtitle.Render(plural(p.total, kindNoun[p.kind]))
	}
	if mark := m.cacheMarker(p.origin); mark != "" && !p.loading {
		right = joinNonEmpty("  ", mark, right)
	}
	colHeader := ""
	if len(p.visible) > 0 {
		colHeader = m.columnHeader(p, cw)
	}

	rows := m.pageCoverRows()
	if p.cover == "" || rows == 0 {
		title := spread(m.st.title.Render(clampWidth(p.title, cw-lipgloss.Width(right)-2)), right, cw)
		subtitle := m.st.subtitle.Render(clampWidth(p.subtitle, cw))
		if p.isSearch {
			return []string{title, m.st.on.Render("⌕ ") + m.input.View(), subtitle, ""}
		}
		return []string{title, subtitle, "", colHeader}
	}

	// Cover on the left, text bottom-aligned beside it like a record sleeve.
	cols := m.coverCols(rows)
	cover := strings.Split(m.coverView(p.cover, cols, rows), "\n")
	textW := cw - cols - 3
	text := make([]string, rows)
	text[rows-3] = spread(m.st.title.Render(clampWidth(p.title, textW-lipgloss.Width(right)-2)), right, textW)
	text[rows-2] = m.st.subtitle.Render(clampWidth(p.subtitle, textW))
	text[rows-1] = m.st.rowMuted.Render(clampWidth(p.about, textW))
	lines := make([]string, 0, rows+2)
	for i := range rows {
		lines = append(lines, cover[i]+"   "+text[i])
	}
	return append(lines, "", colHeader)
}

// cacheMarker is a small note that data came from sptui's cache: how old it
// is, and whether Spotify refused a fresher copy. Empty for live data.
func (m *Model) cacheMarker(o spotify.Origin) string {
	if o.CachedAt.IsZero() {
		return ""
	}
	age := time.Since(o.CachedAt)
	if o.Stale {
		return m.st.stale.Render("◷ " + ago(age) + " · Spotify busy")
	}
	if age < time.Minute {
		return ""
	}
	return m.st.off.Render("◷ " + ago(age))
}

func (m *Model) countVisible(p *page) int {
	n := 0
	for _, i := range p.visible {
		if p.rows[i].kind != kindHeader {
			n++
		}
	}
	return n
}

func (m *Model) pageError(p *page, cw int) []string {
	if isForbidden(p.err) && p.context != "" {
		return []string{
			m.st.row.Render(clampWidth("Spotify only lists songs for playlists you own or collaborate on.", cw)),
			m.st.rowMuted.Render(clampWidth("You can still play it: press enter, or m for more.", cw)),
		}
	}
	return []string{
		m.st.errText.Render(clampWidth("Couldn't load this: "+friendly(p.err), cw)),
		m.st.rowMuted.Render("Press ctrl+r to try again."),
	}
}

var kindNoun = map[rowKind]string{kindTrack: "song", kindAlbum: "release", kindArtist: "artist", kindPlaylist: "playlist"}

// columns returns the widths of the flexible columns for a row kind.
// Every row is: bar(2) lead(4) [flex columns separated by gaps] tail.
func columns(kind rowKind, w int, withAlbum bool) (flex []int, tail int) {
	const gap = 2
	switch kind {
	case kindTrack:
		tail = 6
		avail := w - 2 - 4 - tail
		if avail >= 70 && withAlbum {
			avail -= 3 * gap
			return []int{avail * 4 / 10, avail * 3 / 10, avail - avail*4/10 - avail*3/10}, tail
		}
		avail -= 2 * gap
		return []int{avail * 6 / 10, avail - avail*6/10}, tail
	case kindAlbum, kindPlaylist:
		tail = 10
		avail := w - 2 - 4 - tail - 2*gap
		return []int{avail * 6 / 10, avail - avail*6/10}, tail
	default:
		avail := w - 2 - 4 - 2*gap
		return []int{avail * 5 / 10, avail - avail*5/10}, 0
	}
}

func (m *Model) columnHeader(p *page, w int) string {
	if p.kind == kindHeader {
		return ""
	}
	var names []string
	tailName := ""
	flex, tail := columns(p.kind, w, !p.noAlbum)
	switch p.kind {
	case kindTrack:
		names, tailName = []string{"TITLE", "ARTIST", "ALBUM"}, "TIME"
	case kindAlbum:
		names, tailName = []string{"ALBUM", "ARTIST"}, "YEAR"
	case kindPlaylist:
		names, tailName = []string{"PLAYLIST", "OWNER"}, "SONGS"
	case kindArtist:
		names = []string{"ARTIST", "GENRES"}
	}
	var b strings.Builder
	b.WriteString("    # ")
	for i, width := range flex {
		b.WriteString("  " + fit(names[i], width))
	}
	if tail > 0 {
		b.WriteString(fmt.Sprintf("%*s", tail, tailName))
	}
	return m.st.colHead.Render(b.String())
}

// renderRow draws visible row i of p at width w.
func (m *Model) renderRow(p *page, i, w int, focused bool) string {
	r := p.rows[p.visible[i]]
	if r.kind == kindHeader {
		label := r.header + " "
		return m.st.section.Foreground(m.st.accent).Render(label) +
			m.st.progressBg.Render(strings.Repeat("─", max(0, w-lipgloss.Width(label))))
	}

	selected := i == p.cursor
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected && focused {
			return s.Background(m.st.subtle)
		}
		return s
	}
	playing := false
	if t := m.player.track(); t != nil && r.kind == kindTrack && t.URI == r.track.URI {
		playing = true
	}

	bar := "  "
	if selected {
		bar = "▌ "
	}
	flex, tail := columns(r.kind, w, !p.noAlbum)
	var lead, tailText string
	var cells []string
	primary, secondary := bg(m.st.row), bg(m.st.rowMuted)
	if playing {
		primary = bg(m.st.rowPlaying.Bold(true))
	}

	switch r.kind {
	case kindTrack:
		t := r.track
		lead = fmt.Sprintf("%3d ", trackNumber(p, i))
		if playing {
			lead = "  ♪ "
			if !m.player.playing() {
				lead = "  ‖ "
			}
		}
		cells = []string{t.Name, t.ArtistNames(), t.Album.Name}
		tailText = clock(t.Duration())
	case kindAlbum:
		a := r.album
		lead = "  ◎ "
		cells = []string{a.Name, spotify.JoinArtists(a.Artists)}
		tailText = a.Year()
	case kindArtist:
		lead = "  ♪ "
		cells = []string{r.artist.Name, strings.Join(r.artist.Genres, ", ")}
	case kindPlaylist:
		pl := r.playlist
		lead = "  ≡ "
		cells = []string{pl.Name, pl.Owner.Name()}
		tailText = fmt.Sprint(pl.TrackCount())
	}

	var b strings.Builder
	b.WriteString(bg(m.st.cursorBar).Render(bar))
	leadStyle := secondary
	if playing {
		leadStyle = primary
	}
	b.WriteString(leadStyle.Render(lead))
	for ci, width := range flex {
		st := secondary
		if ci == 0 {
			st = primary
		}
		if ci == 1 && r.kind == kindTrack && !playing {
			st = bg(m.st.row)
		}
		b.WriteString(bg(lipgloss.NewStyle()).Render("  "))
		b.WriteString(st.Render(fit(cells[ci], width)))
	}
	if tail > 0 {
		b.WriteString(secondary.Render(fmt.Sprintf("%*s", tail, clampWidth(tailText, tail))))
	}
	return b.String()
}

// trackNumber is the 1-based position of visible row i among the page's
// tracks. Filtering keeps the original numbers so rows are easy to find again.
func trackNumber(p *page, i int) int {
	if !p.isSearch {
		return p.visible[i] + 1
	}
	n := 0
	for _, idx := range p.visible[:i+1] {
		if p.rows[idx].kind == kindTrack {
			n++
		}
	}
	return n
}

// --- player bar ---

func (m *Model) viewPlayer() string {
	w := m.width
	cw := w - 4
	style := m.st.panel.Padding(0, 1).Width(w).Height(playerHeight)

	t := m.player.track()
	if t == nil {
		lines := []string{
			m.st.title.Render("Nothing playing"),
			m.st.subtitle.Render(m.idleHint()),
			"",
		}
		for i := range lines {
			lines[i] = clampWidth(lines[i], cw)
		}
		return style.Render(strings.Join(lines, "\n"))
	}

	now := time.Now()
	st := m.player.state
	rightW := 0
	if cw >= 90 {
		rightW = 32
	}
	leftW := cw - rightW

	// Cover thumbnail on the far left.
	var thumb []string
	if url := m.thumbURL(); url != "" {
		thumb = strings.Split(m.coverView(url, m.coverCols(thumbRows), thumbRows), "\n")
		leftW -= m.coverCols(thumbRows) + 2
	}

	icon := "▶"
	if !st.IsPlaying {
		icon = "‖"
	}
	heart := ""
	if m.player.liked {
		heart = m.st.on.Render("  ♥")
	}
	line1 := m.st.on.Render(icon+"  ") + m.st.trackTitle.Render(clampWidth(t.Name, leftW-8)) + heart
	sub := t.ArtistNames()
	line2 := "   " + m.st.trackArtist.Render(clampWidth(sub, leftW/2))
	if t.Album.Name != "" {
		line2 += m.st.subtitle.Render(clampWidth(" · "+t.Album.Name, leftW-lipgloss.Width(line2)))
	}

	pos, dur := m.player.progress(now), t.Duration()
	elapsed, total := clock(pos), clock(dur)
	barW := max(leftW-3-len(elapsed)-len(total)-4, 5)
	filled := 0
	if dur > 0 {
		filled = int(float64(barW) * float64(pos) / float64(dur))
	}
	filled = min(max(filled, 0), barW)
	bar := m.st.progress.Render(strings.Repeat("━", filled)) + m.st.progressBg.Render(strings.Repeat("─", barW-filled))
	line3 := "   " + m.st.subtitle.Render(elapsed) + "  " + bar + "  " + m.st.subtitle.Render(total)

	left := []string{line1, line2, line3}
	for i := range left {
		left[i] = clampWidth(left[i], leftW)
		if thumb != nil {
			left[i] = thumb[i] + "  " + left[i]
		}
	}
	if rightW == 0 {
		return style.Render(strings.Join(left, "\n"))
	}

	onOff := func(on bool, label string) string {
		if on {
			return m.st.on.Render(label)
		}
		return m.st.off.Render(label)
	}
	repeat := repeatLabel(st.RepeatState)
	right := []string{
		onOff(st.ShuffleState, "⇄ shuffle") + "   " + onOff(st.RepeatState != spotify.RepeatOff, "↻ repeat "+repeat),
		m.st.subtitle.Render("◉ " + clampWidth(st.Device.Name, rightW-2)),
		m.volumeBar(),
	}
	var lines []string
	for i := range 3 {
		lines = append(lines, spread(left[i], right[i], cw))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) idleHint() string {
	if m.opts.LocalDevice != "" {
		return "Pick a song and press enter to play it here."
	}
	return "Pick a song and press enter, or press d to choose a device."
}

func (m *Model) volumeBar() string {
	v := m.player.volume()
	if v < 0 {
		return m.st.off.Render("volume n/a")
	}
	const cells = 10
	n := (v + 5) / 10
	return m.st.subtitle.Render("vol ") + m.st.progress.Render(strings.Repeat("▮", n)) +
		m.st.progressBg.Render(strings.Repeat("▮", cells-n)) + m.st.subtitle.Render(fmt.Sprintf(" %3d%%", v))
}

// --- overlays ---

func (m *Model) viewMenu() string {
	mn := m.menu
	w := min(52, m.width-6)
	inner := w - 4
	lines := []string{m.st.modalTitle.Render(clampWidth(mn.title, inner)), ""}

	switch {
	case mn.loading:
		lines = append(lines, m.spinner.View()+m.st.subtitle.Render(" Loading…"))
	case len(mn.items) == 0:
		lines = append(lines, m.st.rowMuted.Width(inner).Render(mn.empty))
	default:
		maxRows := max(3, m.height-12)
		start := max(0, min(mn.cursor-maxRows/2, len(mn.items)-maxRows))
		end := min(len(mn.items), start+maxRows)
		for i := start; i < end; i++ {
			it := mn.items[i]
			note := m.st.rowMuted.Render(it.note)
			label := clampWidth(it.label, inner-lipgloss.Width(it.note)-4)
			if i == mn.cursor {
				hl := func(s lipgloss.Style) lipgloss.Style { return s.Background(m.st.subtle) }
				text := hl(m.st.cursorBar).Render("▌ ") + hl(m.st.row.Bold(true)).Render(label)
				pad := inner - lipgloss.Width(text) - lipgloss.Width(it.note)
				lines = append(lines, text+hl(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(pad, 1)))+hl(m.st.rowMuted).Render(it.note))
				continue
			}
			lines = append(lines, spread("  "+m.st.row.Render(label), note, inner))
		}
	}
	lines = append(lines, "", m.st.keyDesc.Render("enter select · esc close"))
	return m.st.modal.Width(w).Render(strings.Join(lines, "\n"))
}

func (m *Model) viewHelp() string {
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
