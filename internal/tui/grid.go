package tui

import "github.com/kyledickey/sptui/internal/art"

// A grid of tiles, for an artist's releases: a wall of covers walked with
// the arrow keys, scrolling a row of tiles at a time.

const (
	gridTileRows = 5 // cover height of grid tiles
	posterRows   = 10
	posterBody   = 32 // body height from which an artist gets a poster
)

// heroRows is how tall a page's hero is (its cover, or just the text
// when art is off), or 0 when there's no room: artists get a taller
// poster when there's plenty.
func (m *Model) heroRows(p *page) int {
	switch h := m.bodyHeight(); {
	case h < 18:
		return 0
	case p != nil && p.grid && h >= posterBody:
		return posterRows
	case h < 26:
		return 5
	}
	return 7
}

// hasHero reports whether p's header is a hero, and whether it has a cover.
func (m *Model) hasHero(p *page) (hero, cover bool) {
	if m.heroRows(p) == 0 {
		return false, false
	}
	cover = p.cover != "" && m.covers.mode != art.Off
	return p.self != nil || cover, cover
}

// gridCols is how many tiles fit across the main pane.
func (m *Model) gridCols() int { return m.tilesFit(gridTileRows, m.contentWidth()) }

// gridRowsShown is how many rows of tiles fit under the page header.
func (m *Model) gridRowsShown() int {
	return max(1, (m.listHeight()+1)/(tileLines(gridTileRows)+1))
}

// gridKey moves the cursor through the grid. It reports whether it did.
func (m *Model) gridKey(msg string, up, down bool, p *page) bool {
	cols := m.gridCols()
	switch {
	case msg == "right" && p.cursor < len(p.visible)-1:
		p.cursor++
	case msg == "left" && p.cursor%cols > 0:
		p.cursor--
	case up && p.cursor >= cols:
		p.cursor -= cols
	case down && p.cursor+cols < len(p.visible):
		p.cursor += cols
	case down && p.cursor/cols < (len(p.visible)-1)/cols:
		p.cursor = len(p.visible) - 1 // onto a shorter last row
	default:
		return up || down // at an edge: stay put
	}
	return true
}

// gridShown is the tiles on screen, scrolled a row at a time to keep the
// cursor in view: indexes into p.visible.
func (m *Model) gridShown(p *page) []int {
	cols, shown := m.gridCols(), m.gridRowsShown()
	row := p.cursor / cols
	p.scroll = min(max(p.scroll, row-shown+1), row)
	var out []int
	for i := p.scroll * cols; i < len(p.visible) && i < (p.scroll+shown)*cols; i++ {
		out = append(out, i)
	}
	return out
}

// viewGrid draws the grid in cw cells.
func (m *Model) viewGrid(p *page, cw int, focused bool) []string {
	if len(p.visible) == 0 {
		if p.loading {
			return []string{m.spinner.View() + m.st.subtitle.Render(" loading")}
		}
		if p.err != nil {
			return m.pageError(p, cw)
		}
		return []string{m.st.rowMuted.Render(p.empty)}
	}
	cols := m.gridCols()
	shown := m.gridShown(p)
	var lines []string
	for start := 0; start < len(shown); start += cols {
		var tiles [][]string
		for _, vi := range shown[start:min(len(shown), start+cols)] {
			tiles = append(tiles, m.tile(p.rows[p.visible[vi]], gridTileRows, vi == p.cursor && focused))
		}
		if start > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, joinTiles(tiles)...)
	}
	return lines
}
