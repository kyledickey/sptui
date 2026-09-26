package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// menu is a small modal list of choices, used for actions, devices and
// playlist pickers.
type menu struct {
	title   string
	items   []menuItem
	cursor  int
	loading bool
	empty   string
}

type menuItem struct {
	label string
	note  string // dimmed text after the label
	run   func() tea.Cmd
}

func (mn *menu) move(delta int) {
	if len(mn.items) > 0 {
		mn.cursor = (mn.cursor + delta + len(mn.items)) % len(mn.items)
	}
}

// devicesMsg carries devices for the device picker.
type devicesMsg struct {
	devices []spotify.Device
	err     error
}

// actionsMenu lists what can be done with a row.
func (m *Model) actionsMenu(r row, from *page) *menu {
	mn := &menu{title: r.name()}
	add := func(label string, run func() tea.Cmd) {
		mn.items = append(mn.items, menuItem{label: label, run: run})
	}
	switch r.kind {
	case kindTrack:
		t := r.track
		add("Play", func() tea.Cmd { return m.playTrack(from, t) })
		add("Add to queue", func() tea.Cmd { return m.addToQueue(t) })
		if radioURI(r) != "" {
			add("Start song radio", func() tea.Cmd { return m.startRadio(r) })
		}
		if t.IsEpisode() {
			add("Save / unsave episode", func() tea.Cmd { return m.toggleSaved(r) })
		} else {
			add("Like / unlike", func() tea.Cmd { return m.toggleSaved(r) })
		}
		add("Add to playlist…", func() tea.Cmd { m.menu = m.playlistPicker(t); return nil })
		if t.Show != nil && t.Show.ID != "" {
			sh := *t.Show
			add("Go to podcast", func() tea.Cmd { return m.push(showPage(m.backend, sh)) })
		}
		if t.Album.ID != "" {
			add("Go to album", func() tea.Cmd { return m.push(albumPage(m.backend, t.Album)) })
		}
		for _, ar := range t.Artists {
			if ar.ID != "" { // the built-in speaker only knows artist names
				add("Go to "+ar.Name, func() tea.Cmd { return m.push(artistPage(m.backend, ar)) })
			}
		}
	case kindAlbum, kindArtist, kindPlaylist, kindShow:
		uri := r.uri()
		add("Play", func() tea.Cmd { return m.play(spotify.PlayOptions{ContextURI: uri}, nil) })
		add("Shuffle play", func() tea.Cmd { return m.shufflePlay(r) })
		if radioURI(r) != "" {
			add("Start radio", func() tea.Cmd { return m.startRadio(r) })
		}
		add("Open", func() tea.Cmd { return m.push(openRow(m.backend, r)) })
		label := "Save / remove from library"
		if r.kind == kindArtist || r.kind == kindShow {
			label = "Follow / unfollow"
		}
		if !m.ownPlaylist(r) {
			add(label, func() tea.Cmd { return m.toggleSaved(r) })
		}
		if r.kind == kindAlbum {
			for _, ar := range r.album.Artists {
				add("Go to "+ar.Name, func() tea.Cmd { return m.push(artistPage(m.backend, ar)) })
			}
		}
	}
	if uri := r.uri(); uri != "" {
		add("Copy link", func() tea.Cmd {
			m.setStatus("Link copied", false)
			return tea.SetClipboard(webURL(uri))
		})
	}
	return mn
}

// accountMenu offers logging out. The UI just exits with an Outcome; the
// caller owns the saved login and clears it.
func (m *Model) accountMenu() *menu {
	exit := func(o Outcome) func() tea.Cmd {
		return func() tea.Cmd {
			m.outcome = o
			m.log.Info("logging out", "sign in again", o == LogIn)
			return m.quit()
		}
	}
	return &menu{
		title: "Signed in as " + m.me.Name(),
		items: []menuItem{
			{label: "Log out and sign in again", note: "switch account", run: exit(LogIn)},
			{label: "Log out and quit", note: "forget this login", run: exit(LogOut)},
		},
	}
}

// devicesMenu opens the device picker and starts loading devices.
func (m *Model) devicesMenu() tea.Cmd {
	m.menu = &menu{title: "Play on…", loading: true, empty: "No devices found. Open Spotify on a phone, computer or speaker."}
	return m.call(func(ctx context.Context) tea.Msg {
		devices, err := m.backend.Devices(ctx)
		return devicesMsg{devices: devices, err: err}
	})
}

func (m *Model) setDevices(msg devicesMsg) {
	if m.menu == nil || !m.menu.loading {
		return
	}
	m.menu.loading = false
	if msg.err != nil {
		m.menu.empty = friendly(msg.err)
		return
	}
	for _, d := range msg.devices {
		local := m.opts.LocalDevice != "" && strings.EqualFold(d.Name, m.opts.LocalDevice)
		note := d.Type
		switch {
		case d.IsActive:
			note = "● playing"
		case local:
			note = "this computer"
		}
		m.menu.items = append(m.menu.items, menuItem{
			label: d.Name,
			note:  note,
			run: func() tea.Cmd {
				m.remoteChosen = !local
				return m.act("transfer", "Playing on "+d.Name, func(ctx context.Context) error {
					return m.backend.Transfer(ctx, d.ID, true)
				})
			},
		})
	}
}

// playlistPicker lists playlists the user can add t to.
func (m *Model) playlistPicker(t spotify.Track) *menu {
	mn := &menu{title: "Add to playlist", empty: "You don't have any playlists you can edit."}
	for _, pl := range m.sidebar.playlists() {
		if !pl.EditableBy(m.me.ID) {
			continue
		}
		mn.items = append(mn.items, menuItem{
			label: pl.Name,
			note:  fmt.Sprint(pl.TrackCount()),
			run: func() tea.Cmd {
				return m.act("add to playlist", "Added to "+pl.Name, func(ctx context.Context) error {
					return m.backend.AddToPlaylist(ctx, pl.ID, []string{t.URI})
				})
			},
		})
	}
	return mn
}
