package tui

import (
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strconv"
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
	playerHeight = 4
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
	if p == nil {
		return pageHeader
	}
	if p.isSearch {
		return m.searchHeaderLines(p, m.contentWidth())
	}
	n := pageHeader
	if hero, _ := m.hasHero(p); hero {
		n = m.heroRows(p) + 2
	}
	if p.grid {
		n++ // a gap between the tabs and the covers
	}
	if p.strip {
		n++
	}
	return n
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
	if m.intro != nil {
		v.SetContent(m.viewIntro())
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

// logo is the wordmark: the name on an accent slab, and beside it a small
// spectrum that dances while music plays and lies flat when it doesn't.
func (m *Model) logo() string {
	slab := m.st.logoEdge.Render("▐") + m.st.logo.Render("sptui") + m.st.logoEdge.Render("▌")
	return " " + slab + " " + m.spectrum(time.Now())
}

const spectrumBars = 8

var levels = []rune("▁▂▃▄▅▆▇█")

// spectrum is a fake analyser for the playing song. Each song gets its own
// shape from its URI, with the low bands riding higher, as in most music.
func (m *Model) spectrum(now time.Time) string {
	t := m.player.track()
	if t == nil || !m.player.playing() {
		return m.st.off.Render(strings.Repeat("▁", spectrumBars))
	}
	h := fnv.New32a()
	h.Write([]byte(t.URI))
	seed := h.Sum32()
	step := float64(now.UnixMilli() / animTick.Milliseconds())
	bars := make([]rune, spectrumBars)
	for i := range bars {
		phase := float64(seed>>(i*4)&15) / 2.5
		v := 0.7 - 0.35*float64(i)/spectrumBars +
			0.3*math.Sin(step*0.9+phase) + 0.2*math.Sin(step*2.3+phase*1.7)
		bars[i] = levels[min(max(int(v*float64(len(levels))), 0), len(levels)-1)]
	}
	return m.st.on.Render(string(bars))
}

func (m *Model) viewHeader() string {
	left := m.logo()
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
	if m.showingNowPlaying() { // no sidebar to show the update in
		right = m.updateMarker() + right
	}
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
	case m.menu != nil && m.menu.answer != nil:
		hints = []key.Binding{enter(m.menu.verb), esc("cancel")}
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

// viewSidebar lists the library and playlists. Headings sit on slabs like
// the logo's, the selection is a solid accent slab, each playlist shows a
// block of its cover's color, and whatever is playing gets an equalizer.
func (m *Model) viewSidebar() string {
	w, h := m.sidebarWidth(), m.bodyHeight()
	inner, rows := w-2, h-2
	box := m.sidebarUpdateBox()
	rows -= len(box)
	sb := &m.sidebar
	sb.scrollTo(rows)
	focused := m.focus == focusSidebar && m.menu == nil

	playing := -1
	if m.player.playing() {
		if st := m.player.state; st.Context != nil {
			playing = sb.playingFrom(st.Context.URI)
		}
		if playing < 0 {
			playing = slices.IndexFunc(sb.items, func(it navItem) bool { return it.label == "Now Playing" })
		}
	}

	lines := box // the update, if there is one, comes first
	rows += len(box)
	for i := sb.scroll; i < len(sb.items) && len(lines) < rows; i++ {
		it := sb.items[i]
		if it.header {
			lines = append(lines, m.sidebarHeading(it, inner))
			continue
		}
		lines = append(lines, m.sidebarRow(it, inner, i == playing, i == sb.cursor && focused, i == sb.active))
	}
	if !sb.loaded && rows > len(lines) {
		lines = append(lines, m.st.rowMuted.Render("   "+m.spinner.View()+" loading playlists…"))
	}
	style := m.st.panel
	if focused {
		style = m.st.panelFocused
	}
	return style.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// sidebarHeading is a section's name on a slab, with how many playlists
// there are (and whether they came from the cache) at the right.
func (m *Model) sidebarHeading(it navItem, w int) string {
	if it.label == "" {
		return ""
	}
	tag := " " + m.st.tagEdge.Render("▐") + m.st.tag.Render(strings.ToUpper(it.label)) + m.st.tagEdge.Render("▌")
	var right string
	if it.label == "Playlists" {
		if mark := m.cacheMarker(m.sidebar.origin); mark != "" {
			right = mark + " "
		} else if n := len(m.sidebar.playlists()); n > 0 {
			right = m.st.off.Render(strconv.Itoa(n)) + "  " // in line with the song counts
		}
	}
	return spread(tag, right, w)
}

// sidebarRow is one entry: an icon (or a playlist's color), the name, an
// equalizer when it's playing, and a playlist's song count.
func (m *Model) sidebarRow(it navItem, w int, playing, selected, active bool) string {
	text, faded := m.st.rowMuted, m.st.off
	if active {
		text = m.st.rowPlaying.Bold(true)
	}
	if selected {
		text = m.st.logo
		faded = m.st.logo.Bold(false)
	}

	icon := m.st.rowMuted.Render(fit(it.icon, 2))
	if it.playlist != nil {
		icon = faded.Render("░░")
		if hex, ok := m.swatchColor(playlistSwatchURL(*it.playlist)); ok {
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("██")
		}
	} else if active || selected {
		icon = text.Render(fit(it.icon, 2))
	}

	var tail string
	if it.playlist != nil {
		if n := it.playlist.TrackCount(); n > 0 {
			tail = strconv.Itoa(n)
		}
	}
	eq := ""
	if playing {
		eq = equalizer(time.Now())
	}
	// edge, space, icon, space, name, eq, count, space, edge
	nameW := w - 8 - len(tail)
	if eq != "" {
		nameW -= 4
	}
	body := text.Render(" ") + icon + text.Render(" "+fit(it.label, max(nameW, 1)))
	if eq != "" {
		eqStyle := m.st.on
		if selected {
			eqStyle = text
		}
		body += text.Render(" ") + eqStyle.Render(eq)
	}
	body += text.Render(" ") + faded.Render(tail) + text.Render(" ")
	if selected {
		return m.st.logoEdge.Render("▐") + body + m.st.logoEdge.Render("▌")
	}
	if active {
		return m.st.cursorBar.Render("▐") + body + " "
	}
	return " " + body + " "
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

	// box frames lines in the panel, cut to fit inside it.
	box := func(lines []string) string {
		return style.Width(w).Height(h).Render(crop(strings.Join(lines, "\n"), cw, h-2))
	}

	p := m.current()
	switch {
	case p == nil:
		msg := m.spinner.View() + " Loading your library…"
		if m.status.err {
			msg = m.st.errText.Render(m.status.text)
		}
		return style.Width(w).Height(h).Render(lipgloss.Place(cw, h-2, lipgloss.Center, lipgloss.Center, msg))
	case p.home:
		return box(m.viewHome(p, cw, h-2, focused))
	case p.grid:
		return box(append(m.pageHeader(p, cw), m.viewGrid(p, cw, focused)...))
	case p.settings:
		return box(m.viewSettings(p, cw, h-2))
	}
	lines := m.pageHeader(p, cw)

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
	return box(lines)
}

// pageHeader renders the top of a page: a hero (cover with the title,
// numbers and buttons beside it) when there is one, else a title block;
// then a playlist's color strip, and the column names. It is
// headerHeight(p) lines.
func (m *Model) pageHeader(p *page, cw int) []string {
	// Right of the title: a loading indicator or a count.
	right := ""
	switch {
	case p.loading:
		right = m.spinner.View() + m.st.subtitle.Render(" loading")
	case p.filter != "":
		right = m.st.subtitle.Render(fmt.Sprintf("%d of %d", countVisible(p), len(p.rows)))
	case p.total > 0 && !p.isSearch:
		noun := kindNoun[p.kind]
		if p.episodes {
			noun = "episode"
		}
		right = m.st.subtitle.Render(plural(p.total, noun))
	}
	if mark := m.cacheMarker(p.origin); mark != "" && !p.loading {
		right = joinNonEmpty("  ", mark, right)
	}
	colHeader := ""
	if len(p.visible) > 0 {
		colHeader = m.columnHeader(p, cw)
	}

	if p.isSearch {
		return m.viewSearchHeader(p, cw, right)
	}
	hero, cover := m.hasHero(p)
	if p.self != nil && hero && !p.loading && p.filter == "" {
		// The hero counts songs itself; keep only the cache age.
		right = m.cacheMarker(p.origin)
	}

	if p.grid {
		colHeader = m.st.colHead.Render("RELEASES   ") + m.tabsLine(p, cw-11)
	}
	var lines []string
	switch rows := m.heroRows(p); {
	case !hero:
		lines = []string{
			spread(m.st.title.Render(clampWidth(p.title, cw-lipgloss.Width(right)-2)), right, cw),
			m.st.subtitle.Render(clampWidth(p.subtitle, cw)),
		}
	case !cover:
		lines = m.heroText(p, cw, rows, right)
	default:
		// Cover on the left, text bottom-aligned beside it like a record sleeve.
		cols := m.coverCols(rows)
		cover := strings.Split(m.coverView(p.cover, cols, rows), "\n")
		text := m.heroText(p, cw-cols-3, rows, right)
		for i := range rows {
			lines = append(lines, cover[i]+"   "+text[i])
		}
	}
	if p.strip {
		strip := m.viewColorStrip(p, cw)
		lines = append(lines, strip[0], strip[1])
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, colHeader)
	if p.grid {
		lines = append(lines, "")
	}
	return lines
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

func countVisible(p *page) int {
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

var kindNoun = map[rowKind]string{kindTrack: "song", kindAlbum: "release", kindArtist: "artist", kindPlaylist: "playlist", kindShow: "podcast"}

// columns returns the widths of the flexible columns for a row kind.
// Every row is: bar(2) lead(4) [flex columns separated by gaps] tail.
func columns(kind rowKind, w int, withAlbum bool) (flex []int, tail int) {
	const gap = 2
	switch kind {
	case kindTrack:
		tail = 7 // fits an hour-long episode, "1:24:00"
		avail := w - 2 - 4 - tail
		if avail >= 70 && withAlbum {
			avail -= 3 * gap
			return []int{avail * 4 / 10, avail * 3 / 10, avail - avail*4/10 - avail*3/10}, tail
		}
		avail -= 2 * gap
		return []int{avail * 6 / 10, avail - avail*6/10}, tail
	case kindAlbum, kindPlaylist, kindShow:
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
		switch {
		case p.episodes && p.noAlbum:
			names = []string{"TITLE", "RELEASED      PROGRESS"}
		case p.episodes:
			names = []string{"TITLE", "PODCAST", "RELEASED"}
		case p.lengths:
			names = []string{"TITLE", "LENGTH"}
		}
	case kindAlbum:
		names, tailName = []string{"ALBUM", "ARTIST"}, "YEAR"
	case kindPlaylist:
		names, tailName = []string{"PLAYLIST", "OWNER"}, "SONGS"
	case kindShow:
		names, tailName = []string{"PODCAST", "PUBLISHER"}, "EPISODES"
	case kindArtist:
		names = []string{"ARTIST", "GENRES"}
	}
	var b strings.Builder
	b.WriteString("    # ")
	for i, width := range flex {
		b.WriteString("  " + fit(names[i], width))
	}
	if tail > 0 {
		fmt.Fprintf(&b, "%*s", tail, tailName)
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
	current := m.player.track()
	playing := current != nil && r.kind == kindTrack && current.URI == r.track.URI

	bar := "  "
	if selected {
		bar = "▌ "
	}
	flex, tail := columns(r.kind, w, !p.noAlbum)
	var lead, tailText string
	var cells []string
	styled := map[int]string{} // cells drawn with their own colors
	primary, secondary := bg(m.st.row), bg(m.st.rowMuted)
	if playing {
		primary = bg(m.st.rowPlaying.Bold(true))
	}
	leadStyle := secondary

	switch r.kind {
	case kindTrack:
		t := r.track
		lead = fmt.Sprintf("%3d ", trackNumber(p, i))
		if t.IsEpisode() && time.Since(t.Released()) < newEpisodeAge {
			lead, leadStyle = "  ● ", bg(m.st.on)
		}
		if playing {
			lead, leadStyle = " "+equalizer(time.Now()), primary
			if !m.player.playing() {
				lead = "  ‖ "
			}
		}
		cells = []string{t.Name, t.ArtistNames(), t.Album.Name}
		switch {
		case t.IsEpisode() && p.noAlbum: // a podcast's own page
			cells = []string{t.Name, ""}
			styled[1] = m.episodeProgress(t, flex[1], bg)
		case t.IsEpisode():
			cells[2] = shortDate(t.Released())
		case p.lengths:
			cells = []string{t.Name, ""}
			styled[1] = m.lengthBar(p, t, flex[1], playing, bg)
		}
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
	case kindShow:
		sh := r.show
		lead = "  ◉ "
		cells = []string{sh.Name, sh.Publisher}
		if sh.TotalEpisodes > 0 {
			tailText = fmt.Sprint(sh.TotalEpisodes)
		}
	}

	var b strings.Builder
	b.WriteString(bg(m.st.cursorBar).Render(bar))
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
		if cell, ok := styled[ci]; ok {
			b.WriteString(cell)
			continue
		}
		cell := cells[ci]
		if ci == 0 && selected && focused {
			cell = m.scroll(cell, width, m.selectedAt)
		}
		b.WriteString(st.Render(fit(cell, width)))
	}
	if tail > 0 {
		b.WriteString(secondary.Render(fmt.Sprintf("%*s", tail, clampWidth(tailText, tail))))
	}
	return b.String()
}

// episodeProgress is an episode's release date and how far the user got:
// "Sep 17   ▰▰▰▰▱▱▱▱ 41 min left", in w cells.
func (m *Model) episodeProgress(t spotify.Track, w int, bg func(lipgloss.Style) lipgloss.Style) string {
	date := bg(m.st.rowMuted).Render(fit(shortDate(t.Released()), 13))
	const cells = 8
	var bar, note string
	switch rp := t.ResumePoint; {
	case rp != nil && rp.FullyPlayed:
		bar, note = bg(m.st.on).Render(strings.Repeat("▰", cells)), "played ✓"
	case rp != nil && rp.ResumePositionMS > 0 && t.DurationMS > 0:
		n := max(1, min(cells-1, rp.ResumePositionMS*cells/t.DurationMS))
		bar = bg(m.st.on).Render(strings.Repeat("▰", n)) + bg(m.st.off).Render(strings.Repeat("▱", cells-n))
		note = fmt.Sprintf("%d min left", (t.DurationMS-rp.ResumePositionMS)/60000)
	default:
		bar = bg(m.st.off).Render(strings.Repeat("▱", cells))
	}
	// Drop the note, then the bar, when they don't fit.
	s := date + bar + bg(m.st.rowMuted).Render(" "+note)
	if lipgloss.Width(s) > w {
		s = date + bar
	}
	if lipgloss.Width(s) > w {
		s = date
	}
	return bg(lipgloss.NewStyle()).Render(padRight(clampWidth(s, w), w))
}

// lengthBar draws a song's length against the longest on the page.
func (m *Model) lengthBar(p *page, t spotify.Track, w int, playing bool, bg func(lipgloss.Style) lipgloss.Style) string {
	longest := 0
	for _, r := range p.rows {
		longest = max(longest, r.track.DurationMS)
	}
	n := 0
	if longest > 0 {
		n = max(1, t.DurationMS*min(w, 24)/longest)
	}
	st := m.st.off
	if playing {
		st = m.st.on
	}
	return bg(st).Render(strings.Repeat("▬", n)) + bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(0, w-n)))
}

// released reports whether a release date is within the last few months.
func released(date string) bool {
	d, err := time.Parse(time.DateOnly, date)
	return err == nil && time.Since(d) < 120*24*time.Hour
}

// shortDate is "Sep 17", with the year when it isn't this one.
func shortDate(d time.Time) string {
	if d.IsZero() {
		return ""
	}
	if d.Year() == time.Now().Year() {
		return d.Format("Jan 2")
	}
	return d.Format("Jan 2 2006")
}

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
	w := m.menuWidth()
	inner := w - 4
	lines := []string{m.st.modalTitle.Render(clampWidth(mn.title, inner)), ""}

	if mn.answer != nil {
		lines = append(lines, m.st.on.Render(mn.prompt)+m.input.View(), "", m.st.keyDesc.Render("enter "+mn.verb+" · esc cancel"))
		return m.st.modal.Width(w).Render(strings.Join(lines, "\n"))
	}

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

func (m *Model) menuWidth() int { return min(52, m.width-6) }
