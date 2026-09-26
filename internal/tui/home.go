package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/spotify"
)

// The home page: a greeting with a week of listening, covers of what was
// played recently to jump back into, songs on repeat and new episodes of
// followed podcasts. It's built from Recently Played, the playlists and the
// followed podcasts, so it costs a handful of (cached) requests.
//
// Its rows are sections like search results: the tiles (albums and
// playlists), then songs on repeat, then episodes. Moving through them
// walks the tiles left to right, then down the lists.

const (
	homeTiles     = 10 // most tiles
	homeTileGap   = 3
	homeRepeatN   = 5 // songs on repeat shown
	homeEpisodeN  = 5 // new episodes shown
	homeShows     = 3 // podcasts checked for new episodes
	newEpisodeAge = 7 * 24 * time.Hour
)

// homeData is what the home page shows besides its rows.
type homeData struct {
	perDay [7]int         // plays per day, oldest first, today last
	plays  int            // plays in perDay
	counts map[string]int // plays per track URI among recent plays
}

// Section titles on the home page.
const (
	homeJump     = "Jump back in"
	homeRepeat   = "On repeat"
	homeEpisodes = "New episodes"
)

func homePage(b Backend) *page {
	p := newPage("Home", kindHeader, func(ctx context.Context, _ int) (chunk, error) {
		return loadHome(ctx, b, time.Now())
	})
	p.home = true
	return p
}

func loadHome(ctx context.Context, b Backend, now time.Time) (chunk, error) {
	recent, err := b.RecentlyPlayed(ctx)
	if err != nil {
		return chunk{}, err
	}
	// Both only add to the page, so a failure just leaves a section out.
	playlists, _ := b.Playlists(ctx, 0)
	shows, _ := b.SavedShows(ctx, 0)

	data := &homeData{counts: map[string]int{}}
	today := dayStart(now)
	for _, t := range recent {
		data.counts[t.URI]++
		if day := int(today.Sub(dayStart(t.PlayedAt)).Hours() / 24); !t.PlayedAt.IsZero() && day >= 0 && day < 7 {
			data.perDay[6-day]++
			data.plays++
		}
	}

	rows := []row{headerRow(homeJump)}
	seen := map[string]bool{}
	for _, t := range recent {
		if len(rows) > homeTiles {
			break
		}
		var r row
		if i := slices.IndexFunc(playlists.Items, func(pl spotify.Playlist) bool { return pl.URI == t.PlayedFrom }); i >= 0 {
			r = playlistRow(playlists.Items[i])
		} else if t.Album.URI != "" {
			r = albumRow(t.Album)
		} else {
			continue
		}
		if !seen[r.uri()] {
			seen[r.uri()] = true
			rows = append(rows, r)
		}
	}

	var repeats []spotify.Track
	for _, t := range recent {
		if data.counts[t.URI] > 1 && !slices.ContainsFunc(repeats, func(r spotify.Track) bool { return r.URI == t.URI }) {
			repeats = append(repeats, t)
		}
	}
	slices.SortStableFunc(repeats, func(a, b spotify.Track) int { return data.counts[b.URI] - data.counts[a.URI] })
	rows = append(rows, headerRow(homeRepeat))
	rows = append(rows, mapRows(repeats[:min(len(repeats), homeRepeatN)], trackRow)...)

	var episodes []spotify.Track
	for _, sh := range shows.Items[:min(len(shows.Items), homeShows)] {
		if pg, err := b.ShowEpisodes(ctx, sh, 0); err == nil && len(pg.Items) > 0 {
			episodes = append(episodes, pg.Items[0])
		}
	}
	slices.SortFunc(episodes, func(a, b spotify.Track) int { return b.Released().Compare(a.Released()) })
	rows = append(rows, headerRow(homeEpisodes))
	rows = append(rows, mapRows(episodes[:min(len(episodes), homeEpisodeN)], trackRow)...)

	return chunk{rows: rows, next: -1, homeData: data}, nil
}

func dayStart(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
}

// homeSections splits the home page's visible rows by section: indexes
// into p.visible.
func homeSections(p *page) map[string][]int {
	out := map[string][]int{}
	section := ""
	for vi, ri := range p.visible {
		if r := p.rows[ri]; r.kind == kindHeader {
			section = r.header
		} else {
			out[section] = append(out[section], vi)
		}
	}
	return out
}

// homeTileRows is how tall tile covers are: bigger on tall screens.
func (m *Model) homeTileRows() int {
	if m.bodyHeight() >= 34 {
		return 8
	}
	return 5
}

// homeTileW is the width of one tile.
func (m *Model) homeTileW() int { return max(m.coverCols(m.homeTileRows()), 14) }

// homeShownTiles are the tiles that fit in cw, scrolled to keep the
// selected one in view: indexes into p.visible.
func (m *Model) homeShownTiles(p *page, cw int) []int {
	tiles := homeSections(p)[homeJump]
	fit := max(1, (cw+homeTileGap)/(m.homeTileW()+homeTileGap))
	first := 0
	if i := slices.Index(tiles, p.cursor); i >= fit {
		first = i - fit + 1
	}
	return tiles[first:min(len(tiles), first+fit)]
}

// homeTile reports whether the selected row is a tile.
func homeTile(p *page) bool {
	return slices.Contains(homeSections(p)[homeJump], p.cursor)
}

// viewHome draws the home page in cw×h cells.
func (m *Model) viewHome(p *page, cw, h int, focused bool) []string {
	greeting := m.st.title.Render(greetingFor(time.Now()) + ", " + firstName(m.me.Name()) + ".")
	right := ""
	if p.homeData != nil {
		right = m.st.on.Render(sparkline(p.homeData.perDay[:])) + m.st.subtitle.Render(
			fmt.Sprintf("  %s this week", plural(p.homeData.plays, "play")))
	}
	if p.loading && len(p.rows) == 0 {
		right = m.spinner.View() + m.st.subtitle.Render(" loading")
	}
	lines := []string{spread(greeting, right, cw), ""}
	if p.err != nil && len(p.rows) == 0 {
		return append(lines, m.pageError(p, cw)...)
	}
	if len(p.rows) == 0 {
		return lines
	}

	sections := homeSections(p)
	lines = append(lines, m.homeRule(homeJump, cw))
	lines = append(lines, m.viewTiles(p, cw, focused)...)
	lines = append(lines, "")

	repeat := m.homeList(p, homeRepeat, sections[homeRepeat], focused, "Songs you play again and again show up here.")
	episodes := m.homeList(p, homeEpisodes, sections[homeEpisodes], focused, "Follow podcasts to see their new episodes.")
	if cw >= 90 {
		half := (cw - 3) / 2
		left, rightCol := m.renderHomeList(repeat, half), m.renderHomeList(episodes, cw-3-half)
		for i := range max(len(left), len(rightCol)) {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(rightCol) {
				r = rightCol[i]
			}
			lines = append(lines, padRight(l, half)+"   "+r)
		}
	} else {
		lines = append(lines, m.renderHomeList(repeat, cw)...)
		lines = append(lines, "")
		lines = append(lines, m.renderHomeList(episodes, cw)...)
	}
	return lines[:min(len(lines), h)]
}

func (m *Model) homeRule(title string, w int) string {
	label := strings.ToUpper(title) + " "
	return m.st.section.Render(label) + m.st.progressBg.Render(strings.Repeat("─", max(0, w-lipgloss.Width(label))))
}

// viewTiles draws the "jump back in" covers with names under them.
func (m *Model) viewTiles(p *page, cw int, focused bool) []string {
	shown := m.homeShownTiles(p, cw)
	if len(shown) == 0 {
		return []string{m.st.rowMuted.Render("Play something and it'll be here to jump back into.")}
	}
	tw := m.homeTileW()
	rows := m.homeTileRows()
	cols := m.coverCols(rows)
	gap := strings.Repeat(" ", homeTileGap)
	lines := make([]string, rows+3)
	for n, vi := range shown {
		r := p.rows[p.visible[vi]]
		selected := vi == p.cursor && focused
		var cover []string
		if url := tileCover(r); url != "" && m.covers.mode != art.Off {
			cover = strings.Split(m.coverView(url, cols, rows), "\n")
		} else {
			cover = m.tilePlaceholder(r, cols, rows)
		}
		name, kind := m.st.row, m.st.subtitle
		if selected {
			name = m.st.rowPlaying.Bold(true)
		}
		cells := make([]string, 0, len(lines))
		for _, c := range cover {
			cells = append(cells, lipgloss.PlaceHorizontal(tw, lipgloss.Left, c))
		}
		under := strings.Repeat(" ", tw)
		if selected {
			under = m.st.on.Render(strings.Repeat("▔", tw))
		}
		label := kindNoun[r.kind]
		if r.kind == kindAlbum {
			label = cmp.Or(r.album.AlbumType, "album")
		}
		cells = append(cells, name.Render(fit(r.name(), tw)), kind.Render(fit(label, tw)), under)
		for i := range lines {
			if n > 0 {
				lines[i] += gap
			}
			lines[i] += cells[i]
		}
	}
	return lines
}

func tileCover(r row) string {
	switch r.kind {
	case kindAlbum:
		return spotify.CoverURL(r.album.Images, coverSource)
	case kindPlaylist:
		return spotify.CoverURL(r.playlist.Images, coverSource)
	}
	return ""
}

// tilePlaceholder stands in for a cover when art is off: a shaded square
// with the kind's icon.
func (m *Model) tilePlaceholder(r row, cols, rows int) []string {
	icon := map[rowKind]string{kindAlbum: "◎", kindPlaylist: "≡"}[r.kind]
	fill := m.st.off.Background(m.st.subtle)
	lines := make([]string, rows)
	for i := range lines {
		text := strings.Repeat(" ", cols)
		if i == rows/2 {
			text = lipgloss.PlaceHorizontal(cols, lipgloss.Center, icon)
		}
		lines[i] = fill.Render(text)
	}
	return lines
}

// homeListLine is one entry of a home list, ready to lay out.
type homeListLine struct {
	selected   bool
	lead, main string // lead: a number or a dot
	sub, right string
	bar        string
}

type homeList struct {
	title string
	lines []homeListLine
	empty string
}

func (m *Model) homeList(p *page, title string, vis []int, focused bool, empty string) homeList {
	l := homeList{title: title, empty: empty}
	top := 0
	if p.homeData != nil && len(vis) > 0 {
		top = p.homeData.counts[p.rows[p.visible[vis[0]]].track.URI]
	}
	for n, vi := range vis {
		t := p.rows[p.visible[vi]].track
		line := homeListLine{selected: vi == p.cursor && focused, main: t.Name}
		if t.IsEpisode() {
			line.lead = " "
			if time.Since(t.Released()) < newEpisodeAge {
				line.lead = "●"
			}
			line.sub = t.ArtistNames()
			line.right = ago(time.Since(t.Released()))
		} else {
			count := p.homeData.counts[t.URI]
			line.lead = fmt.Sprint(n + 1)
			line.sub = t.ArtistNames()
			line.bar = eighths(float64(count)/float64(max(top, 1)), 10)
			line.right = fmt.Sprint(count)
		}
		l.lines = append(l.lines, line)
	}
	return l
}

// renderHomeList draws a home list at width w.
func (m *Model) renderHomeList(l homeList, w int) []string {
	lines := []string{m.homeRule(l.title, w)}
	if len(l.lines) == 0 {
		return append(lines, m.st.rowMuted.Render(clampWidth(l.empty, w)))
	}
	for _, e := range l.lines {
		bg := func(s lipgloss.Style) lipgloss.Style {
			if e.selected {
				return s.Background(m.st.subtle)
			}
			return s
		}
		bar := "  "
		if e.selected {
			bar = "▌ "
		}
		lead := bg(m.st.subtitle).Render(fmt.Sprintf("%-2s ", e.lead))
		if e.lead == "●" {
			lead = bg(m.st.on).Render("●  ")
		}
		right := bg(m.st.subtitle).Render(" " + e.right)
		if e.bar != "" {
			right = bg(m.st.on).Render(e.bar) + right
		}
		textW := w - 2 - 3 - lipgloss.Width(right) - 1
		mainW := min(ansi.StringWidth(e.main), textW*6/10)
		text := bg(m.st.row).Render(fit(e.main, mainW)) + bg(m.st.rowMuted).Render(fit("  "+e.sub, textW-mainW))
		line := bg(m.st.cursorBar).Render(bar) + lead + text + bg(lipgloss.NewStyle()).Render(" ") + right
		lines = append(lines, clampWidth(line, w))
	}
	return lines
}

// eighths draws frac (0..1) of w cells with eighth blocks, for fine bars.
func eighths(frac float64, w int) string {
	parts := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	n := int(min(max(frac, 0), 1) * float64(w) * 8)
	s := strings.Repeat("█", n/8) + parts[n%8]
	return padRight(s, w)
}

// sparkline draws counts as a row of bars.
func sparkline(counts []int) string {
	levels := []rune("▁▂▃▄▅▆▇█")
	top := slices.Max(counts)
	var b strings.Builder
	for _, c := range counts {
		i := 0
		if top > 0 {
			i = c * (len(levels) - 1) / top
		}
		b.WriteRune(levels[i])
	}
	return b.String()
}

func greetingFor(t time.Time) string {
	switch h := t.Hour(); {
	case h < 5:
		return "Up late"
	case h < 12:
		return "Good morning"
	case h < 18:
		return "Good afternoon"
	}
	return "Good evening"
}

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return cmp.Or(name, "there")
}
