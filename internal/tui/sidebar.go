package tui

import (
	"github.com/kyledickey/sptui/internal/spotify"
)

// navItem is an entry in the sidebar.
type navItem struct {
	icon     string
	label    string
	header   bool
	playlist *spotify.Playlist
	open     func(m *Model) *page
}

// sidebar lists library sections followed by the user's playlists.
type sidebar struct {
	items  []navItem
	cursor int
	scroll int
	active int            // index of the item whose page is on screen
	loaded bool           // all playlists have arrived
	origin spotify.Origin // where the playlists came from
}

func newSidebar() sidebar {
	return sidebar{
		active: 1,
		cursor: 1,
		items: []navItem{
			{label: "Library", header: true},
			{icon: "♥", label: "Liked Songs", open: func(m *Model) *page { return likedPage(m.backend, m.me) }},
			{icon: "◷", label: "Recently Played", open: func(m *Model) *page { return recentPage(m.backend) }},
			{icon: "★", label: "Top Tracks", open: func(m *Model) *page { return topPage(m.backend) }},
			{icon: "◎", label: "Albums", open: func(m *Model) *page { return albumsPage(m.backend) }},
			{icon: "♪", label: "Artists", open: func(m *Model) *page { return artistsPage(m.backend) }},
			{icon: "▶", label: "Now Playing", open: func(m *Model) *page { return nowPlayingPage(m.backend) }},
			{label: "Playlists", header: true},
		},
	}
}

// addPlaylists appends playlists to the sidebar.
func (s *sidebar) addPlaylists(pls []spotify.Playlist) {
	for _, pl := range pls {
		s.items = append(s.items, navItem{
			label:    pl.Name,
			playlist: &pl,
			open:     func(m *Model) *page { return playlistPage(m.backend, pl) },
		})
	}
}

// playlists returns every playlist in the sidebar.
func (s *sidebar) playlists() []spotify.Playlist {
	var out []spotify.Playlist
	for _, it := range s.items {
		if it.playlist != nil {
			out = append(out, *it.playlist)
		}
	}
	return out
}

func (s *sidebar) selected() navItem { return s.items[s.cursor] }

// move shifts the cursor, skipping headers.
func (s *sidebar) move(delta int) {
	dir := 1
	if delta < 0 {
		dir = -1
	}
	start := min(max(s.cursor+delta, 0), len(s.items)-1)
	// Look for a selectable item in the direction of travel, then backwards
	// (e.g. when jumping to the top lands on a header).
	for _, step := range []int{dir, -dir} {
		for i := start; i >= 0 && i < len(s.items); i += step {
			if !s.items[i].header {
				s.cursor = i
				return
			}
		}
	}
}

func (s *sidebar) scrollTo(height int) {
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	if s.cursor >= s.scroll+height {
		s.scroll = s.cursor - height + 1
	}
	s.scroll = max(0, min(s.scroll, len(s.items)-height))
}
