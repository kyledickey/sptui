package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// navItem is an entry in the sidebar.
type navItem struct {
	icon     string
	label    string
	header   bool
	uri      string // the context it plays, to show when it's playing
	playlist *spotify.Playlist
	open     func(m *Model) *page
	play     func(m *Model) tea.Cmd // starts playback instead of opening a page
}

// sidebar lists library sections followed by the user's playlists.
type sidebar struct {
	items  []navItem
	cursor int
	scroll int
	active int            // index of the item whose page is on screen
	loaded bool           // all playlists have arrived
	origin spotify.Origin // where the playlists came from

	// The playlists that were active and under the cursor before a reload,
	// to find them again when they come back.
	keepActive, keepCursor string
}

func newSidebar() sidebar {
	return sidebar{
		active: 1,
		cursor: 1,
		items: []navItem{
			{label: "Library", header: true},
			{icon: "⌂", label: "Home", open: func(m *Model) *page { return homePage(m.backend) }},
			{icon: "♥", label: "Liked Songs", uri: ":collection", open: func(m *Model) *page { return likedPage(m.backend, m.me) }},
			{icon: "◷", label: "Recently Played", open: func(m *Model) *page { return recentPage(m.backend) }},
			{icon: "★", label: "Top Tracks", open: func(m *Model) *page { return topPage(m.backend) }},
			{icon: "◎", label: "Albums", open: func(m *Model) *page { return albumsPage(m.backend) }},
			{icon: "♪", label: "Artists", open: func(m *Model) *page { return artistsPage(m.backend) }},
			{icon: "◉", label: "Podcasts", open: func(m *Model) *page { return podcastsPage(m.backend) }},
			{icon: "◈", label: "Your Episodes", open: func(m *Model) *page { return episodesPage(m.backend, m.me) }},
			{icon: "✦", label: "DJ X", uri: spotify.DJURI, play: func(m *Model) tea.Cmd { return m.startDJ() }},
			{icon: "▶", label: "Now Playing", play: func(m *Model) tea.Cmd { return m.push(nowPlayingPage(m.backend)) }},
			{header: true}, // a gap
			{label: "Playlists", header: true},
			{icon: "+", label: "New playlist", play: func(m *Model) tea.Cmd { return m.newPlaylist("", nil) }},
		},
	}
}

// addPlaylists appends playlists to the sidebar.
func (s *sidebar) addPlaylists(pls []spotify.Playlist) {
	for _, pl := range pls {
		if pl.URI == s.keepActive {
			s.active = len(s.items)
		}
		if pl.URI == s.keepCursor {
			s.cursor = len(s.items)
		}
		s.items = append(s.items, navItem{
			label:    pl.Name,
			uri:      pl.URI,
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

// playingFrom is the index of the item whose context is uri, or -1. Liked
// Songs plays as the user's collection, whose URI ends ":collection".
func (s *sidebar) playingFrom(uri string) int {
	if uri == "" {
		return -1
	}
	for i, it := range s.items {
		if it.uri != "" && (it.uri == uri || strings.HasPrefix(it.uri, ":") && strings.HasSuffix(uri, it.uri)) {
			return i
		}
	}
	return -1
}

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

// scrollTo keeps the cursor inside a viewport of height lines.
func (s *sidebar) scrollTo(height int) {
	s.scroll = keepInView(s.scroll, s.cursor, height, len(s.items))
}
