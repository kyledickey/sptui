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
	// answer, if set, makes the menu a text box instead of a list; it's
	// called with what was typed. See ask.
	answer func(text string) tea.Cmd
	prompt string // leads the text box
	verb   string // what enter does
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

// playlistMadeMsg reports a new playlist, and adding songs to it.
type playlistMadeMsg struct {
	name, what string
	made       bool
	err        error
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
		add("Add to playlist", func() tea.Cmd {
			m.menu = m.playlistPicker("“"+t.Name+"”", func(context.Context) ([]string, error) { return []string{t.URI}, nil })
			return nil
		})
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
		if p := m.current(); p == nil || p.self == nil || p.self.uri() != uri {
			add("Open", func() tea.Cmd { return m.push(openRow(m.backend, r)) })
		}
		label := "Save / remove from library"
		if r.kind == kindArtist || r.kind == kindShow {
			label = "Follow / unfollow"
		}
		if !m.ownPlaylist(r) {
			add(label, func() tea.Cmd { return m.toggleSaved(r) })
		}
		if r.kind == kindAlbum {
			al := r.album
			add("Like all songs", func() tea.Cmd { return m.likeAll(al) })
			add("Add to playlist", func() tea.Cmd {
				m.menu = m.playlistPicker("“"+al.Name+"”", func(ctx context.Context) ([]string, error) {
					return m.albumTrackURIs(ctx, al)
				})
				return nil
			})
			for _, ar := range al.Artists {
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

// playlistPicker lists playlists the user can add songs to, after an
// option to make a new one. what names the songs in the status line; uris
// fetches them once a playlist is picked.
func (m *Model) playlistPicker(what string, uris func(context.Context) ([]string, error)) *menu {
	mn := &menu{title: "Add to playlist"}
	mn.items = append(mn.items, menuItem{
		label: "New playlist",
		note:  "+",
		run:   func() tea.Cmd { return m.newPlaylist(what, uris) },
	})
	for _, pl := range m.sidebar.playlists() {
		if !pl.EditableBy(m.me.ID) {
			continue
		}
		mn.items = append(mn.items, menuItem{
			label: pl.Name,
			note:  fmt.Sprint(pl.TrackCount()),
			run: func() tea.Cmd {
				return m.act("add to playlist", "Added "+what+" to "+pl.Name, func(ctx context.Context) error {
					u, err := uris(ctx)
					if err != nil {
						return err
					}
					return m.backend.AddToPlaylist(ctx, pl.ID, u)
				})
			},
		})
	}
	return mn
}

// ask turns mn into a text box starting at value, and shows it.
func (m *Model) ask(mn *menu, value, placeholder string) tea.Cmd {
	m.menu = mn
	m.inputMode = inputAnswer
	m.input.Placeholder = placeholder
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.fitInput()
	return m.input.Focus()
}

// newPlaylist asks for a name, then makes a playlist and adds the songs
// uris fetches to it, if any.
func (m *Model) newPlaylist(what string, uris func(context.Context) ([]string, error)) tea.Cmd {
	return m.ask(&menu{title: "New playlist", prompt: "name › ", verb: "create", answer: func(name string) tea.Cmd {
		return m.call(func(ctx context.Context) tea.Msg {
			pl, err := m.backend.CreatePlaylist(ctx, name)
			if err != nil {
				return playlistMadeMsg{err: err}
			}
			msg := playlistMadeMsg{name: pl.Name, made: true}
			if uris != nil {
				msg.what = what
				u, err := uris(ctx)
				if err == nil {
					err = m.backend.AddToPlaylist(ctx, pl.ID, u)
				}
				msg.err = err
			}
			return msg
		})
	}}, "", "Name it")
}
