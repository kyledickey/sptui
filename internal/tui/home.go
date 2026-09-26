package tui

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/spotify"
)

// The home page: a greeting with a week of listening, then shelves and
// lists of things the sidebar doesn't show: covers of what was played
// recently to jump back into, songs on repeat, new episodes of followed
// podcasts, new releases from followed artists, saved albums not played in
// a while, and the artists themselves. It's built from Recently Played and
// the library, so it costs a handful of (cached) requests.
//
// Its rows are sections like search results. Left and right walk along a
// shelf; up and down move between sections, or down a list. The page
// scrolls when it's taller than the screen.

const (
	homeTiles      = 10 // most tiles in a shelf
	homeRepeatN    = 5  // songs on repeat shown
	homeEpisodeN   = 5  // new episodes shown
	homeShows      = 3  // podcasts checked for new episodes
	homeNewArtists = 6  // followed artists checked for new releases
	newEpisodeAge  = 7 * 24 * time.Hour
)

// homeData is what the home page shows besides its rows.
type homeData struct {
	perDay [7]int         // plays per day, oldest first, today last
	plays  int            // plays in perDay
	counts map[string]int // plays per track URI among recent plays
}

// Section titles on the home page, in order.
const (
	homeJump       = "Jump back in"
	homeRepeat     = "On repeat"
	homeEpisodes   = "New episodes"
	homeNew        = "New from artists you follow"
	homeRediscover = "Rediscover"
	homeArtists    = "Artists you follow"
)

// homeShelves are the sections walked sideways: a row of covers, and a
// row of artists' names. The rest are lists, two side by side, so only the
// top of the page is pictures.
var homeShelves = []string{homeJump, homeArtists}

var homeEmpty = map[string]string{
	homeJump:       "Play something and it'll be here to jump back into.",
	homeRepeat:     "Songs you play again and again show up here.",
	homeEpisodes:   "Follow podcasts to see their new episodes.",
	homeNew:        "Follow artists to see what they release.",
	homeRediscover: "Albums you've saved and not played lately show up here.",
	homeArtists:    "Artists you follow show up here. Press l on one to follow.",
}

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
	// The rest only add to the page, so a failure just leaves a section out.
	playlists, _ := b.Playlists(ctx, 0)
	shows, _ := b.SavedShows(ctx, 0)
	followed, _ := b.FollowedArtists(ctx)
	saved, _ := b.SavedAlbums(ctx, 0)

	data := &homeData{counts: map[string]int{}}
	artistPlays := map[string]int{}
	playedAlbums := map[string]bool{}
	today := dayStart(now)
	for _, t := range recent {
		data.counts[t.URI]++
		playedAlbums[t.Album.URI] = true
		for _, a := range t.Artists {
			artistPlays[a.ID]++
		}
		if day := int(today.Sub(dayStart(t.PlayedAt)).Hours() / 24); !t.PlayedAt.IsZero() && day >= 0 && day < 7 {
			data.perDay[6-day]++
			data.plays++
		}
	}

	var rows []row
	section := func(title string, items []row) {
		rows = append(append(rows, headerRow(title)), items...)
	}

	var jump []row
	seen := map[string]bool{}
	for _, t := range recent {
		var r row
		switch i := slices.IndexFunc(playlists.Items, func(pl spotify.Playlist) bool { return pl.URI == t.PlayedFrom }); {
		case i >= 0:
			r = playlistRow(playlists.Items[i])
		case t.Album.URI != "":
			r = albumRow(t.Album)
		default:
			continue
		}
		if !seen[r.uri()] && len(jump) < homeTiles {
			seen[r.uri()] = true
			jump = append(jump, r)
		}
	}
	section(homeJump, jump)

	var repeats []spotify.Track
	for _, t := range recent {
		if data.counts[t.URI] > 1 && !slices.ContainsFunc(repeats, func(r spotify.Track) bool { return r.URI == t.URI }) {
			repeats = append(repeats, t)
		}
	}
	slices.SortStableFunc(repeats, func(a, b spotify.Track) int { return data.counts[b.URI] - data.counts[a.URI] })
	section(homeRepeat, mapRows(repeats[:min(len(repeats), homeRepeatN)], trackRow))

	var episodes []spotify.Track
	for _, sh := range shows.Items[:min(len(shows.Items), homeShows)] {
		if pg, err := b.ShowEpisodes(ctx, sh, 0); err == nil && len(pg.Items) > 0 {
			episodes = append(episodes, pg.Items[0])
		}
	}
	slices.SortFunc(episodes, func(a, b spotify.Track) int { return b.Released().Compare(a.Released()) })
	section(homeEpisodes, mapRows(episodes[:min(len(episodes), homeEpisodeN)], trackRow))

	// Artists played most first: their news matters most.
	slices.SortStableFunc(followed, func(a, b spotify.Artist) int { return artistPlays[b.ID] - artistPlays[a.ID] })
	var fresh []spotify.Album
	for _, ar := range followed[:min(len(followed), homeNewArtists)] {
		pg, err := b.ArtistAlbums(ctx, ar.ID, 0)
		if err != nil || len(pg.Items) == 0 {
			continue
		}
		newest := slices.MaxFunc(pg.Items, func(a, b spotify.Album) int { return strings.Compare(a.ReleaseDate, b.ReleaseDate) })
		fresh = append(fresh, newest)
	}
	slices.SortFunc(fresh, func(a, b spotify.Album) int { return strings.Compare(b.ReleaseDate, a.ReleaseDate) })
	section(homeNew, mapRows(fresh, albumRow))

	var forgotten []spotify.Album
	for _, a := range saved.Items {
		if !playedAlbums[a.URI] {
			forgotten = append(forgotten, a)
		}
	}
	// A different handful each day.
	rand.New(rand.NewPCG(uint64(today.Unix()), 7)).Shuffle(len(forgotten), func(i, j int) {
		forgotten[i], forgotten[j] = forgotten[j], forgotten[i]
	})
	section(homeRediscover, mapRows(forgotten[:min(len(forgotten), homeTiles)], albumRow))

	section(homeArtists, mapRows(followed[:min(len(followed), homeTiles)], artistRow))

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

// homeShelf is the shelf the cursor is on, if it's on one.
func homeShelf(p *page) []int {
	sections := homeSections(p)
	for _, title := range homeShelves {
		if slices.Contains(sections[title], p.cursor) {
			return sections[title]
		}
	}
	return nil
}

// homeKey moves around the home page: along a shelf with left and right,
// and past a whole shelf with up and down. It reports whether it moved.
func homeKey(msg string, up, down bool, p *page) bool {
	shelf := homeShelf(p)
	if shelf == nil {
		switch {
		case up:
			p.move(-1)
			landOnShelf(p)
		case down:
			p.move(1)
		default:
			return false
		}
		return true
	}
	i := slices.Index(shelf, p.cursor)
	switch {
	case msg == "right" && i < len(shelf)-1:
		p.cursor = shelf[i+1]
	case msg == "left" && i > 0:
		p.cursor = shelf[i-1]
	case up:
		p.cursor = shelf[0]
		p.move(-1)
		landOnShelf(p)
	case down:
		p.cursor = shelf[len(shelf)-1]
		p.move(1)
	default:
		return false
	}
	return true
}

// landOnShelf puts the cursor on the first tile when moving up onto a
// shelf, instead of its last.
func landOnShelf(p *page) {
	if shelf := homeShelf(p); shelf != nil {
		p.cursor = shelf[0]
	}
}

// homeTileRows is how tall shelf covers are: bigger on tall screens.
func (m *Model) homeTileRows() int {
	if m.bodyHeight() >= 34 {
		return 7
	}
	return 5
}

// homeShownTiles are the covers on screen.
func (m *Model) homeShownTiles(p *page, cw int) []int {
	return m.shelfShown(p, homeSections(p)[homeJump], m.homeTileRows(), cw)
}

// viewHome draws the home page in cw×h cells, scrolled to keep the
// selection in view.
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

	// Lay everything out, noting which lines each selectable row is on.
	span := map[int][2]int{}
	sections := homeSections(p)
	note := func(title string, start, each int) {
		for n, vi := range sections[title] {
			if each == 0 {
				span[vi] = [2]int{start, len(lines)}
			} else {
				span[vi] = [2]int{start + n*each, start + (n+1)*each + 1}
			}
		}
	}
	// Two lists side by side when there's room, else one after the other.
	pair := func(a, b string) {
		if cw < 90 {
			for _, title := range []string{a, b} {
				start := len(lines)
				lines = append(lines, m.renderHomeList(m.homeList(p, title, sections[title], focused), cw)...)
				note(title, start+2, 1)
				lines = append(lines, "")
			}
			return
		}
		half := (cw - 4) / 2
		left := m.renderHomeList(m.homeList(p, a, sections[a], focused), half)
		right := m.renderHomeList(m.homeList(p, b, sections[b], focused), cw-4-half)
		start := len(lines)
		for i := range max(len(left), len(right)) {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, padRight(l, half)+"    "+r)
		}
		note(a, start+2, 1)
		note(b, start+2, 1)
		lines = append(lines, "")
	}

	start := len(lines)
	lines = append(lines, m.homeRule(homeJump, cw), "")
	lines = append(lines, m.viewShelf(p, sections[homeJump], m.homeTileRows(), cw, focused, homeEmpty[homeJump])...)
	lines = append(lines, "")
	note(homeJump, start, 0)

	pair(homeRepeat, homeEpisodes)
	pair(homeNew, homeRediscover)

	start = len(lines)
	lines = append(lines, m.homeRule(homeArtists, cw), "", m.viewArtistChips(p, sections[homeArtists], cw, focused), "")
	note(homeArtists, start, 0)

	// Scroll so the selection shows; the top stays put while it fits.
	if s, ok := span[p.cursor]; ok {
		switch {
		case s[1] <= h:
			p.scroll = 0
		case s[0] < p.scroll:
			p.scroll = s[0]
		case s[1] > p.scroll+h:
			p.scroll = s[1] - h
		}
	}
	p.scroll = max(0, min(p.scroll, len(lines)-h))
	return lines[p.scroll:min(len(lines), p.scroll+h)]
}

func (m *Model) homeRule(title string, w int) string {
	label := strings.ToUpper(title) + " "
	return m.st.section.Render(label) + m.st.progressBg.Render(strings.Repeat("─", max(0, w-lipgloss.Width(label))))
}

// homeListLine is one entry of a home list, ready to lay out.
type homeListLine struct {
	selected   bool
	lit        bool   // the lead is news, drawn in the accent
	lead, main string // lead: a number or a dot
	sub, right string
	bar        string
}

type homeList struct {
	title string
	lines []homeListLine
	empty string
}

func (m *Model) homeList(p *page, title string, vis []int, focused bool) homeList {
	l := homeList{title: title, empty: homeEmpty[title]}
	top := 0
	if p.homeData != nil && len(vis) > 0 {
		top = p.homeData.counts[p.rows[p.visible[vis[0]]].track.URI]
	}
	for n, vi := range vis {
		r := p.rows[p.visible[vi]]
		if r.kind == kindAlbum {
			a := r.album
			line := homeListLine{selected: vi == p.cursor && focused, lead: "◎", main: a.Name,
				sub: spotify.JoinArtists(a.Artists), right: a.Year()}
			if released(a.ReleaseDate) {
				line.lead, line.lit, line.right = "◉", true, "NEW"
			}
			l.lines = append(l.lines, line)
			continue
		}
		t := r.track
		line := homeListLine{selected: vi == p.cursor && focused, main: t.Name}
		if t.IsEpisode() {
			line.lead = " "
			if time.Since(t.Released()) < newEpisodeAge {
				line.lead, line.lit = "●", true
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
	lines := []string{m.homeRule(l.title, w), ""}
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
		if e.lit {
			lead = bg(m.st.on).Render(fmt.Sprintf("%-2s ", e.lead))
		}
		right := bg(m.st.subtitle).Render(" " + e.right)
		if e.right == "NEW" {
			right = bg(m.st.on.Bold(true)).Render(" NEW")
		}
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

// viewArtistChips is the followed artists as a row of name tags, scrolled
// to keep the selected one in view.
func (m *Model) viewArtistChips(p *page, vis []int, w int, focused bool) string {
	if len(vis) == 0 {
		return m.st.rowMuted.Render(clampWidth(homeEmpty[homeArtists], w))
	}
	chip := func(vi int) string {
		name := " ♪ " + p.rows[p.visible[vi]].artist.Name + " "
		if vi == p.cursor && focused {
			return m.st.on.Bold(true).Reverse(true).Render(name)
		}
		return m.st.off.Render("[") + m.st.row.Render(name) + m.st.off.Render("]")
	}
	// Start far enough along that the selection fits.
	first := 0
	if i := slices.Index(vis, p.cursor); i > 0 {
		for first < i {
			width := 0
			for _, vi := range vis[first : i+1] {
				width += lipgloss.Width(chip(vi)) + 2
			}
			if width <= w {
				break
			}
			first++
		}
	}
	var parts []string
	used := 0
	for _, vi := range vis[first:] {
		c := chip(vi)
		if used+lipgloss.Width(c) > w {
			break
		}
		parts = append(parts, c)
		used += lipgloss.Width(c) + 2
	}
	return strings.Join(parts, "  ")
}
