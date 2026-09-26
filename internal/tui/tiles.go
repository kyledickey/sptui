package tui

import (
	"cmp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/art"
)

// Tiles are covers with a name and a line about them underneath, laid out
// in shelves (a row that scrolls sideways, on the home page) or a grid (an
// artist's releases). The selected tile is underlined in the accent.

const tileGap = 3

// tileLines is how tall a tile is: its cover, name, about line and underline.
func tileLines(rows int) int { return rows + 3 }

// tileW is the width of a tile whose cover is rows tall.
func (m *Model) tileW(rows int) int { return max(m.coverCols(rows), 14) }

// tilesFit is how many tiles fit side by side in w cells.
func (m *Model) tilesFit(rows, w int) int {
	return max(1, (w+tileGap)/(m.tileW(rows)+tileGap))
}

// tileAbout is the line under a tile's name, and whether it's news.
func tileAbout(r row) (string, bool) {
	switch r.kind {
	case kindAlbum:
		a := r.album
		if released(a.ReleaseDate) {
			return joinNonEmpty(" · ", "NEW", cmp.Or(a.AlbumType, "album")), true
		}
		return joinNonEmpty(" · ", a.Year(), cmp.Or(a.AlbumType, "album")), false
	case kindArtist:
		if len(r.artist.Genres) > 0 {
			return r.artist.Genres[0], false
		}
		return "artist", false
	}
	return kindNoun[r.kind], false
}

// tile draws one tile, tileLines(rows) lines of tileW(rows) cells.
func (m *Model) tile(r row, rows int, selected bool) []string {
	tw, cols := m.tileW(rows), m.coverCols(rows)
	var cover []string
	if url := r.cover(); url != "" && m.covers.mode != art.Off {
		cover = strings.Split(m.coverView(url, cols, rows), "\n")
	} else {
		cover = m.tilePlaceholder(r, cols, rows)
	}
	lines := make([]string, 0, tileLines(rows))
	for _, c := range cover {
		lines = append(lines, lipgloss.PlaceHorizontal(tw, lipgloss.Left, c))
	}
	name := m.st.row
	if selected {
		name = m.st.rowPlaying.Bold(true)
	}
	about, news := tileAbout(r)
	aboutStyle := m.st.subtitle
	if news {
		aboutStyle = m.st.on
	}
	under := strings.Repeat(" ", tw)
	if selected {
		under = m.st.on.Render(strings.Repeat("▔", tw))
	}
	return append(lines, name.Render(fit(r.name(), tw)), aboutStyle.Render(fit(about, tw)), under)
}

// tilePlaceholder stands in for a cover when art is off or there's none:
// a shaded square with the kind's icon.
func (m *Model) tilePlaceholder(r row, cols, rows int) []string {
	icon := map[rowKind]string{kindAlbum: "◎", kindPlaylist: "≡", kindArtist: "♪"}[r.kind]
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

// joinTiles lays tiles side by side.
func joinTiles(tiles [][]string) []string {
	if len(tiles) == 0 {
		return nil
	}
	gap := strings.Repeat(" ", tileGap)
	lines := make([]string, len(tiles[0]))
	for n, t := range tiles {
		for i := range lines {
			if n > 0 {
				lines[i] += gap
			}
			lines[i] += t[i]
		}
	}
	return lines
}

// shelfShown is the part of a shelf that fits in w cells, scrolled to
// keep the cursor in view: indexes into p.visible.
func (m *Model) shelfShown(p *page, shelf []int, rows, w int) []int {
	fit := m.tilesFit(rows, w)
	first := 0
	if i := slices.Index(shelf, p.cursor); i >= fit {
		first = i - fit + 1
	}
	return shelf[first:min(len(shelf), first+fit)]
}

// viewShelf draws a shelf of tiles in w cells, or empty when it has none.
func (m *Model) viewShelf(p *page, shelf []int, rows, w int, focused bool, empty string) []string {
	shown := m.shelfShown(p, shelf, rows, w)
	if len(shown) == 0 {
		return []string{m.st.rowMuted.Render(clampWidth(empty, w))}
	}
	var tiles [][]string
	for _, vi := range shown {
		tiles = append(tiles, m.tile(p.rows[p.visible[vi]], rows, vi == p.cursor && focused))
	}
	return joinTiles(tiles)
}
