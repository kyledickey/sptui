package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// Page headers. An album, playlist, artist or podcast gets a hero: its
// cover with a kicker, the title, a line about it, some numbers and
// keycap buttons to play or shuffle it. Artists spell their name in big
// letters, playlists get a strip of their songs' cover colours, and search
// has tabs for each kind of result and a card for the top one.

// searchTab is one tab of search results.
type searchTab struct {
	label string
	shows func(row) bool
}

// artistTabs split an artist's releases.
var artistTabs = []searchTab{
	{"all", func(r row) bool { return r.kind == kindAlbum }},
	{"albums", func(r row) bool { return r.kind == kindAlbum && r.album.AlbumType != "single" }},
	{"singles", func(r row) bool { return r.kind == kindAlbum && r.album.AlbumType == "single" }},
}

var searchTabs = []searchTab{
	{"all", func(r row) bool { return r.kind != kindHeader }},
	{"songs", func(r row) bool { return r.kind == kindTrack && !r.track.IsEpisode() }},
	{"artists", func(r row) bool { return r.kind == kindArtist }},
	{"albums", func(r row) bool { return r.kind == kindAlbum }},
	{"playlists", func(r row) bool { return r.kind == kindPlaylist }},
	{"podcasts", func(r row) bool { return r.kind == kindShow || (r.kind == kindTrack && r.track.IsEpisode()) }},
}

// setTab shows one kind of search result.
func (p *page) setTab(tab int) {
	p.tab = tab
	p.cursor, p.scroll = 0, 0
	p.refilter()
}

const (
	heroButtonRows = bigRows
	cardRows       = 5 // the top-result card, borders included
	cardMinBody    = 28
)

// searchTabsW is the width of the search tabs drawn as keycaps.
func searchTabsW() int {
	w := 0
	for _, t := range searchTabs {
		w += len(t.label) + 4 + 1
	}
	return w - 1
}

// searchHeaderLines is the height of the search page's header.
func (m *Model) searchHeaderLines(p *page, cw int) int {
	n := 2 + 1 + 1 // title, input, tabs, spacer
	if cw >= searchTabsW() {
		n += bigRows - 1
	}
	if m.showCard(p) {
		n += cardRows + 1
	}
	return n
}

// topResult is the search result the card shows: the first artist, else
// the first thing found (after the user's own playlists).
func topResult(p *page) (row, bool) {
	var first *row
	for i := range p.rows {
		r := p.rows[i]
		if r.kind == kindArtist {
			return r, true
		}
		if first == nil && r.kind != kindHeader && r.kind != kindPlaylist {
			first = &p.rows[i]
		}
	}
	if first != nil {
		return *first, true
	}
	return row{}, false
}

func (m *Model) showCard(p *page) bool {
	_, ok := topResult(p)
	return p.isSearch && p.tab == 0 && ok && m.bodyHeight() >= cardMinBody
}

// rowImages is a row's artwork.
func rowImages(r row) []spotify.Image {
	switch r.kind {
	case kindTrack:
		return r.track.Cover()
	case kindAlbum:
		return r.album.Images
	case kindArtist:
		return r.artist.Images
	case kindPlaylist:
		return r.playlist.Images
	case kindShow:
		return r.show.Images
	}
	return nil
}

// viewSearchHeader is the search page's header: title, input, tabs and the
// top-result card.
func (m *Model) viewSearchHeader(p *page, cw int, right string) []string {
	title := spread(m.st.title.Render(clampWidth(p.title, cw-lipgloss.Width(right)-2)), right, cw)
	lines := []string{title, m.st.on.Render("⌕ ") + m.input.View()}
	if cw >= searchTabsW() {
		var caps [][bigRows]string
		for i, t := range searchTabs {
			caps = append(caps, m.keycapW(t.label, fmt.Sprint(i+1), i == p.tab, len(t.label)+2))
		}
		for row := range bigRows {
			var parts []string
			for _, c := range caps {
				parts = append(parts, c[row])
			}
			lines = append(lines, strings.Join(parts, " "))
		}
	} else {
		lines = append(lines, m.tabsLine(p, cw))
	}
	lines = append(lines, "")
	if m.showCard(p) {
		lines = append(lines, m.viewCard(p, cw)...)
		lines = append(lines, "")
	}
	return lines
}

// tabsLine is a page's tabs on one line: squeezed, and search's
// abbreviated, if need be.
func (m *Model) tabsLine(p *page, cw int) string {
	short := []string{"all", "songs", "artists", "albums", "lists", "pods"}
	var line string
	for _, abbreviate := range []bool{false, true} {
		for _, gap := range []string{"   ", "  "} {
			var parts []string
			for i, t := range p.tabs {
				label := t.label
				if abbreviate && len(p.tabs) == len(short) {
					label = short[i]
				}
				text := m.st.key.Render(fmt.Sprint(i+1)) + " " + m.st.keyDesc.Render(label)
				if i == p.tab {
					text = m.st.on.Bold(true).Render(fmt.Sprintf("%d %s", i+1, label))
				}
				parts = append(parts, text)
			}
			if line = strings.Join(parts, gap); lipgloss.Width(line) <= cw {
				return line
			}
		}
	}
	return line
}

// viewCard is the top search result in a box, cardRows tall.
func (m *Model) viewCard(p *page, cw int) []string {
	r, _ := topResult(p)
	w := min(cw, 56)
	inner := w - 4
	var cover []string
	cols := 0
	if url := spotify.CoverURL(rowImages(r), coverSource); url != "" && m.pageCoverRows() > 0 {
		cols = m.coverCols(cardRows - 2)
		cover = strings.Split(m.coverView(url, cols, cardRows-2), "\n")
	}
	textW := inner - cols
	if cols > 0 {
		textW -= 2
	}
	kind, about := kindNoun[r.kind], ""
	switch r.kind {
	case kindArtist:
		about = strings.Join(r.artist.Genres, ", ")
		if r.artist.Followers != nil {
			about = joinNonEmpty(" · ", followers(r.artist.Followers.Total), about)
		}
	case kindTrack:
		kind = "song"
		if r.track.IsEpisode() {
			kind = "episode"
		}
		about = r.track.ArtistNames()
	case kindAlbum:
		about = joinNonEmpty(" · ", spotify.JoinArtists(r.album.Artists), r.album.Year())
	case kindShow:
		about = r.show.Publisher
	}
	text := []string{
		m.st.trackTitle.Render(clampWidth(r.name(), textW)),
		m.st.subtitle.Render(clampWidth(joinNonEmpty(" · ", kind, about), textW)),
		m.st.off.Render(clampWidth("↓ it's first in the list", textW)),
	}
	edge := m.st.off
	label := " TOP RESULT "
	lines := []string{edge.Render("╭─") + m.st.section.Render(label) + edge.Render(strings.Repeat("─", max(0, w-3-len(label)))+"╮")}
	for i := range cardRows - 2 {
		body := ""
		if cover != nil {
			body = cover[i] + "  "
		}
		body += text[i]
		lines = append(lines, edge.Render("│ ")+padRight(clampWidth(body, inner), inner)+edge.Render(" │"))
	}
	return append(lines, edge.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

// heroText is the text beside a page's cover: n lines, bottom-aligned.
// right goes at the end of the title line (a count, loading, cache age).
func (m *Model) heroText(p *page, w, n int, right string) []string {
	title := func() string {
		return spread(m.st.title.Render(clampWidth(p.title, w-lipgloss.Width(right)-2)), right, w)
	}
	about := m.st.rowMuted.Render(clampWidth(p.about, w))
	var lines []string
	switch {
	case p.self == nil:
		lines = []string{title(), m.st.subtitle.Render(clampWidth(p.subtitle, w)), about}
	case p.self.kind == kindArtist:
		return m.artistHero(p, w, n, right)
	default:
		lines = []string{
			m.st.colHead.Render(clampWidth(p.kicker, w)),
			title(),
			m.heroSubtitle(p, w),
			m.st.subtitle.Render(clampWidth(m.heroStats(p), w)),
		}
	}
	if p.self != nil && n-len(lines) >= heroButtonRows {
		for _, row := range m.heroButtons() {
			lines = append(lines, row)
		}
	}
	for len(lines) > n {
		lines = lines[1:] // the kicker goes first
	}
	for len(lines) < n {
		lines = append([]string{""}, lines...)
	}
	return lines
}

// artistHero is an artist's poster text, n lines: their name big, then
// genres, numbers and their latest release, spaced out, over the buttons.
// When it doesn't all fit, spacing goes first, then the latest release,
// then the genres, then the buttons.
func (m *Model) artistHero(p *page, w, n int, right string) []string {
	ar := p.self.artist
	type part struct {
		lines []string
		drop  int // order to drop in when short of room; 0 never
	}
	var name []string
	if big, ok := banner(ar.Name); ok && n >= 7 && lipgloss.Width(big[0]) <= w-lipgloss.Width(right)-2 {
		name = []string{spread(m.st.on.Render(big[0]), right, w), m.st.on.Render(big[1])}
	} else {
		name = []string{m.st.colHead.Render("ARTIST"),
			spread(m.st.title.Render(clampWidth(p.title, w-lipgloss.Width(right)-2)), right, w)}
	}
	var stats []string
	if ar.Followers != nil {
		stats = append(stats, followers(ar.Followers.Total))
	}
	if p.plays > 0 {
		stats = append(stats, fmt.Sprintf("%d of your last 50 plays", p.plays))
	}
	gap := part{[]string{""}, 1}
	parts := []part{{name, 0}, gap}
	if len(ar.Genres) > 0 {
		parts = append(parts, part{[]string{clampWidth(m.chips(ar.Genres), w)}, 3})
	}
	if len(stats) > 0 {
		parts = append(parts, part{[]string{m.st.subtitle.Render(clampWidth(strings.Join(stats, " · "), w))}, 0})
	}
	if latest, ok := m.latestRelease(p); ok {
		parts = append(parts, part{[]string{clampWidth(latest, w)}, 2})
	}
	buttons := m.heroButtons()
	parts = append(parts, gap, part{buttons[:], 4})

	count := func() int {
		total := 0
		for _, pt := range parts {
			total += len(pt.lines)
		}
		return total
	}
	for drop := 1; count() > n && drop <= 4; drop++ {
		parts = slices.DeleteFunc(parts, func(pt part) bool { return pt.drop == drop })
	}
	var lines []string
	for _, pt := range parts {
		lines = append(lines, pt.lines...)
	}
	lines = lines[max(0, len(lines)-n):]
	for len(lines) < n {
		lines = append([]string{""}, lines...)
	}
	return lines
}

func (m *Model) heroSubtitle(p *page, w int) string {
	switch p.self.kind {
	case kindAlbum:
		return m.st.trackArtist.Render(clampWidth(spotify.JoinArtists(p.self.album.Artists), w))
	case kindPlaylist, kindShow:
		if p.about != "" {
			return m.st.rowMuted.Render(clampWidth(p.about, w))
		}
	}
	return m.st.subtitle.Render(clampWidth(p.subtitle, w))
}

// heroStats is the numbers line: songs and running time, or new episodes.
func (m *Model) heroStats(p *page) string {
	var total time.Duration
	tracks, fresh := 0, 0
	for _, r := range p.rows {
		if r.kind == kindTrack {
			tracks++
			total += r.track.Duration()
			if r.track.IsEpisode() && time.Since(r.track.Released()) < newEpisodeAge {
				fresh++
			}
		}
	}
	n := max(p.total, tracks)
	switch p.self.kind {
	case kindShow:
		stats := plural(max(n, p.self.show.TotalEpisodes), "episode")
		if fresh > 0 {
			stats = fmt.Sprintf("● %d new · %s", fresh, stats)
		}
		return stats
	case kindAlbum, kindPlaylist:
		stats := plural(n, "song")
		if n > 0 && tracks == n {
			stats += " · " + runningTime(total)
		}
		return stats
	}
	return ""
}

// heroButtons are keycaps to play or shuffle the whole page.
func (m *Model) heroButtons() [heroButtonRows]string {
	k := m.keys
	caps := [][bigRows]string{
		m.keycapW("▶ play", k.PlayAll.Help().Key, false, 8),
		m.keycapW("⇄ shuffle", k.ShuffleAll.Help().Key, false, 11),
		m.keycapW("⋯ more", k.Menu.Help().Key, false, 8),
	}
	var rows [heroButtonRows]string
	for i := range rows {
		var parts []string
		for _, c := range caps {
			parts = append(parts, c[i])
		}
		rows[i] = strings.Join(parts, " ")
	}
	return rows
}

// chips draws genres as little tags.
func (m *Model) chips(tags []string) string {
	var parts []string
	for _, t := range tags {
		parts = append(parts, m.st.off.Render("[")+m.st.subtitle.Render(" "+t+" ")+m.st.off.Render("]"))
	}
	return strings.Join(parts, " ")
}

// viewStrip is a playlist's colour strip: every song as a slice of its
// cover's colour, with a marker under the one playing. Two lines.
func (m *Model) viewColourStrip(p *page, w int) [2]string {
	var tracks []spotify.Track
	for _, r := range p.rows {
		if r.kind == kindTrack {
			tracks = append(tracks, r.track)
		}
	}
	if len(tracks) == 0 || w <= 0 {
		return [2]string{}
	}
	playing := -1
	if t := m.player.track(); t != nil {
		for i, tr := range tracks {
			if tr.URI == t.URI {
				playing = i
			}
		}
	}
	var strip strings.Builder
	marker := -1
	for x := range w {
		i := x * len(tracks) / w
		if i == playing && marker < 0 {
			marker = x
		}
		style := m.st.off
		if sw, ok := m.swatches.known[swatchURL(tracks[i])]; ok && sw.average != "" {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(stripColor(sw.average)))
		}
		strip.WriteString(style.Render("▇"))
	}
	under := ""
	if marker >= 0 {
		label := "▲ now playing"
		if marker+len([]rune(label)) > w {
			label = "▲"
		}
		under = strings.Repeat(" ", marker) + m.st.on.Render(label)
	}
	return [2]string{strip.String(), under}
}

// followers formats a follower count briefly: 1.2M, 34K.
func followers(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM followers", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dK followers", n/1000)
	}
	return plural(n, "follower")
}

// runningTime formats a running time: "43 min", "3 h 41 min".
func runningTime(d time.Duration) string {
	mins := int(d.Round(time.Minute).Minutes())
	if mins < 60 {
		return fmt.Sprintf("%d min", mins)
	}
	return fmt.Sprintf("%d h %d min", mins/60, mins%60)
}

// latestRelease is a callout for an artist's newest release: "◉ LATEST
// Crystal Gardens · Sep 2025", marked NEW when it's recent.
func (m *Model) latestRelease(p *page) (string, bool) {
	for _, r := range p.rows { // newest first
		if r.kind != kindAlbum {
			continue
		}
		a := r.album
		when := a.Year()
		if d, err := time.Parse(time.DateOnly, a.ReleaseDate); err == nil {
			when = d.Format("Jan 2006")
		}
		line := m.st.on.Render("◉ LATEST  ") + m.st.row.Bold(true).Render(a.Name) +
			m.st.subtitle.Render(" · "+joinNonEmpty(" · ", a.AlbumType, when))
		if released(a.ReleaseDate) {
			line += m.st.on.Bold(true).Render("  NEW")
		}
		return line, true
	}
	return "", false
}
