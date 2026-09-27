package tui

import (
	"context"
	"image"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/spotify"
)

const (
	coverSource = 300 // preferred source image width in pixels
	maxImages   = 48  // decoded images kept in memory
	thumbRows   = 2   // cover size in the player bar
)

// coverKey is a cover drawn at a particular size.
type coverKey struct {
	url        string
	cols, rows int
}

// covers fetches cover images and prepares them for drawing. All of it
// happens in Update (see syncCovers); View only looks up what's ready.
type covers struct {
	mode    art.Mode
	images  map[string]image.Image // decoded, by URL
	pending map[string]bool        // URL being fetched, or failed (not retried)
	ready   map[coverKey]string    // cells to draw
	ids     map[coverKey]int       // kitty image IDs sent or being sent
	lastID  int

	// The terminal's cell size in pixels, once it has told us. Images are
	// sized to their cells' exact pixels so every terminal shows them
	// filling their box (some scale images, some draw them as they are).
	cellW, cellH int
}

// cellPixels is the cell size to draw with, guessing until the terminal says.
func (c *covers) cellPixels() (w, h int) {
	if c.cellW > 0 && c.cellH > 0 {
		return c.cellW, c.cellH
	}
	return 10, 20
}

// coverCols is how many columns make a square cover rows tall.
func (m *Model) coverCols(rows int) int {
	w, h := m.covers.cellPixels()
	return max(1, (rows*h+w/2)/w)
}

// requestCellSize asks the terminal for its cell size in pixels (xterm's
// window operation 16); the answer arrives as a uv.CellSizeEvent.
func requestCellSize() tea.Cmd {
	const reportCellSize = 16
	return tea.Raw(ansi.WindowOp(reportCellSize))
}

// setCellSize records the terminal's cell size and redraws covers that were
// sized for another.
func (m *Model) setCellSize(w, h int) tea.Cmd {
	c := &m.covers
	if w <= 0 || h <= 0 || (w == c.cellW && h == c.cellH) {
		return nil
	}
	m.log.Debug("cell size", "width", w, "height", h)
	c.cellW, c.cellH = w, h
	free := m.freeCovers()
	clear(c.ids)
	clear(c.ready)
	return free
}

func newCovers(mode art.Mode) covers {
	return covers{
		mode:    mode,
		images:  map[string]image.Image{},
		pending: map[string]bool{},
		ready:   map[coverKey]string{},
		ids:     map[coverKey]int{},
		lastID:  art.MinID - 1,
	}
}

type (
	coverMsg struct {
		url string
		img image.Image
		err error
	}
	// coverSendMsg carries an encoded kitty image ready to write.
	coverSendMsg struct {
		key coverKey
		id  int
		seq string
	}
	// coverReadyMsg means the terminal has the image.
	coverReadyMsg struct {
		key coverKey
		id  int
	}
)

// wantedCovers lists the covers on screen right now, at their sizes.
func (m *Model) wantedCovers() []coverKey {
	if m.covers.mode == art.Off {
		return nil
	}
	var want []coverKey
	if m.showingNowPlaying() {
		// Just the big cover; the player bar is hidden.
		l := m.nowPlayingLayout()
		if url := m.thumbURL(); url != "" && l.coverRows > 0 && l.trackW > 0 {
			want = append(want, coverKey{url, l.coverCols, l.coverRows})
		}
		return want
	}
	if url := m.thumbURL(); url != "" {
		want = append(want, coverKey{url, m.coverCols(thumbRows), thumbRows})
	}
	p := m.current()
	if p != nil && p.cover != "" {
		if rows := m.heroRows(p); rows > 0 {
			want = append(want, coverKey{p.cover, m.coverCols(rows), rows})
		}
	}
	if p != nil && p.home {
		for _, vi := range m.homeShownTiles(p, m.contentWidth()) {
			if url := p.rows[p.visible[vi]].cover(); url != "" {
				rows := m.homeTileRows()
				want = append(want, coverKey{url, m.coverCols(rows), rows})
			}
		}
	}
	if p != nil && p.grid {
		for _, vi := range m.gridShown(p) {
			if url := p.rows[p.visible[vi]].cover(); url != "" {
				want = append(want, coverKey{url, m.coverCols(gridTileRows), gridTileRows})
			}
		}
	}
	if p != nil && m.showCard(p) {
		r, _ := topResult(p)
		if url := r.cover(); url != "" {
			want = append(want, coverKey{url, m.coverCols(cardRows - 2), cardRows - 2})
		}
	}
	return want
}

// thumbURL is the now-playing cover, if any.
func (m *Model) thumbURL() string {
	t := m.player.track()
	if t == nil || m.width < minWidth {
		return ""
	}
	return spotify.CoverURL(t.Cover(), coverSource)
}

// syncCovers fetches and prepares covers that are wanted but missing, and
// frees kitty images that are no longer on screen.
func (m *Model) syncCovers() tea.Cmd {
	c := &m.covers
	if c.mode == art.Off {
		return nil
	}
	want := m.wantedCovers()
	var cmds []tea.Cmd

	for _, k := range want {
		if _, ok := c.ready[k]; ok {
			continue
		}
		img, ok := c.images[k.url]
		switch {
		case !ok && !c.pending[k.url]:
			c.pending[k.url] = true
			cmds = append(cmds, m.fetchCover(k.url))
		case !ok:
			// Being fetched, or failed.
		case c.mode == art.Blocks:
			c.ready[k] = art.BlockArt(img, k.cols, k.rows)
		default:
			if _, sending := c.ids[k]; !sending {
				cmds = append(cmds, c.send(k, img))
			}
		}
	}

	// Forget what's off screen: kitty images use terminal memory, and block
	// art is cheap to redo.
	for k := range c.ready {
		if !slices.Contains(want, k) {
			delete(c.ready, k)
		}
	}
	for k, id := range c.ids {
		if !slices.Contains(want, k) {
			delete(c.ids, k)
			cmds = append(cmds, tea.Raw(art.KittyDelete(id)))
		}
	}
	if len(c.images) > maxImages {
		for url := range c.images {
			if !slices.ContainsFunc(want, func(k coverKey) bool { return k.url == url }) {
				delete(c.images, url)
				delete(c.pending, url)
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) fetchCover(url string) tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		img, err := m.backend.CoverArt(ctx, url)
		return coverMsg{url: url, img: img, err: err}
	})
}

// send encodes img for kitty in the background under a fresh image ID.
func (c *covers) send(k coverKey, img image.Image) tea.Cmd {
	id := c.nextID()
	c.ids[k] = id
	cw, ch := c.cellPixels()
	return func() tea.Msg {
		framed := art.Frame(img, k.cols*cw, k.rows*ch)
		seq, err := art.KittyTransmit(id, framed, k.cols, k.rows)
		if err != nil {
			return coverMsg{url: k.url, err: err}
		}
		return coverSendMsg{key: k, id: id, seq: seq}
	}
}

// nextID picks an unused kitty image ID, cycling through the range.
func (c *covers) nextID() int {
	for {
		c.lastID++
		if c.lastID > art.MaxID {
			c.lastID = art.MinID
		}
		if !slices.Contains(slices.Collect(maps.Values(c.ids)), c.lastID) {
			return c.lastID
		}
	}
}

// handleCover processes cover messages.
func (m *Model) handleCover(msg tea.Msg) tea.Cmd {
	c := &m.covers
	switch msg := msg.(type) {
	case coverMsg:
		if msg.err != nil {
			m.log.Debug("cover art failed", "url", msg.url, "err", msg.err)
			c.pending[msg.url] = true // don't retry this session
			return nil
		}
		c.images[msg.url] = msg.img
		delete(c.pending, msg.url)
	case coverSendMsg:
		if c.ids[msg.key] != msg.id {
			return nil // no longer wanted
		}
		ready := func() tea.Msg { return coverReadyMsg{key: msg.key, id: msg.id} }
		return tea.Sequence(tea.Raw(msg.seq), ready)
	case coverReadyMsg:
		if c.ids[msg.key] == msg.id {
			c.ready[msg.key] = art.KittyPlaceholder(msg.id, msg.key.cols, msg.key.rows)
		}
	}
	return nil
}

// coverView draws the cover at url in cols×rows cells, or a blank square
// while it loads.
func (m *Model) coverView(url string, cols, rows int) string {
	if cells, ok := m.covers.ready[coverKey{url, cols, rows}]; ok {
		return cells
	}
	blank := m.st.row.Background(m.st.subtle).Render(strings.Repeat(" ", cols))
	return strings.TrimSuffix(strings.Repeat(blank+"\n", rows), "\n")
}

// freeCovers releases every kitty image, for when sptui quits.
func (m *Model) freeCovers() tea.Cmd {
	var seq strings.Builder
	for _, id := range m.covers.ids {
		seq.WriteString(art.KittyDelete(id))
	}
	if seq.Len() == 0 {
		return nil
	}
	return tea.Raw(seq.String())
}
