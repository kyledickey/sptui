package tui

import tea "charm.land/bubbletea/v2"

// wheelStep is how many rows one wheel notch moves.
const wheelStep = 3

// wheelDelta is how many rows a wheel notch moves: up is negative.
func wheelDelta(ms tea.Mouse) int {
	if ms.Button == tea.MouseWheelUp {
		return -wheelStep
	}
	return wheelStep
}

// handleMouse scrolls the pane under the pointer, and selects what's clicked.
// Clicking a sidebar item opens it; clicking the selected row plays/opens it.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.menu != nil || m.showHelp || m.inputMode != inputNone {
		return nil
	}
	ms := msg.Mouse()
	// The name at the top right opens the account menu.
	if _, click := msg.(tea.MouseClickMsg); click && ms.Y == 0 && ms.X > m.width*2/3 {
		m.menu = m.accountMenu()
		return nil
	}
	if m.showingNowPlaying() {
		// Only the queue scrolls here.
		if _, ok := msg.(tea.MouseWheelMsg); ok {
			m.current().move(wheelDelta(ms))
		}
		return nil
	}
	// Clicking the player bar opens the now-playing view.
	if _, click := msg.(tea.MouseClickMsg); click && ms.Y >= m.height-footerHeight-playerHeight && ms.Y < m.height-footerHeight {
		return m.push(nowPlayingPage(m.backend))
	}
	inSidebar := ms.X < m.sidebarWidth()
	top := headerHeight + 1 // first line inside the panels
	if ms.Y < top || ms.Y >= headerHeight+m.bodyHeight()-1 {
		return nil
	}
	p := m.current()

	switch msg.(type) {
	case tea.MouseWheelMsg:
		delta := wheelDelta(ms)
		if inSidebar {
			m.sidebar.move(delta)
			return nil
		}
		if p != nil {
			p.move(delta)
			return m.loadMore(p)
		}

	case tea.MouseClickMsg:
		if ms.Button != tea.MouseLeft {
			return nil
		}
		if inSidebar {
			i := m.sidebar.scroll + ms.Y - top
			if i < len(m.sidebar.items) && !m.sidebar.items[i].header {
				m.sidebar.cursor = i
				return m.openNav(i)
			}
			return nil
		}
		if p == nil || p.home || p.grid {
			return nil // the home page is laid out in tiles and columns
		}
		i := p.scroll + ms.Y - top - m.headerHeight(p)
		if i < 0 || i >= len(p.visible) || p.rows[p.visible[i]].kind == kindHeader {
			return nil
		}
		m.focus = focusMain
		if i == p.cursor {
			return m.activate(p)
		}
		p.cursor = i
	}
	return nil
}
