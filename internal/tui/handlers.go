package tui

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// handleKey routes a key press to whatever currently has focus: help, a
// menu, a text input, then global keys, then the focused pane.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	switch {
	case m.showHelp:
		return m.helpKey(msg)
	case m.menu != nil:
		return m.menuKey(msg)
	case m.inputMode != inputNone:
		return m.inputKey(msg)
	}

	if cmd, ok := m.globalKey(msg); ok {
		return cmd
	}
	if m.showingNowPlaying() {
		switch s := msg.String(); s {
		case "1", "2", "3":
			return m.togglePanel(rune(s[0]))
		}
	}
	if p := m.current(); p != nil && len(p.tabs) > 0 && m.focus == focusMain {
		if s := msg.String(); len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.tabs) {
			p.setTab(int(s[0] - '1'))
			return nil
		}
	}

	if m.focus == focusSidebar {
		return m.sidebarKey(msg)
	}
	p := m.current()
	if p == nil {
		if key.Matches(msg, k.FocusLeft, k.Focus, k.Back) {
			m.focus = focusSidebar
		}
		return nil
	}
	if p.settings {
		if cmd, used := m.settingsKey(msg, p); used {
			return cmd
		}
	}
	return m.listKey(msg, p)
}

// globalKey handles keys that work everywhere.
func (m *Model) globalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Quit):
		return m.quit(), true
	case key.Matches(msg, k.Help):
		m.openHelp()
	case key.Matches(msg, k.Search):
		return m.openSearch(), true
	case key.Matches(msg, k.PlayPause):
		return m.togglePlay(), true
	case key.Matches(msg, k.Next):
		return m.act("next", "", m.backend.Next), true
	case key.Matches(msg, k.Prev):
		return m.act("previous", "", m.backend.Previous), true
	case key.Matches(msg, k.SeekBack):
		return m.seek(-seekStep), true
	case key.Matches(msg, k.SeekFwd):
		return m.seek(seekStep), true
	case key.Matches(msg, k.VolUp):
		return m.changeVolume(volumeStep), true
	case key.Matches(msg, k.VolDown):
		return m.changeVolume(-volumeStep), true
	case key.Matches(msg, k.Shuffle):
		return m.toggleShuffle(), true
	case key.Matches(msg, k.Repeat):
		return m.cycleRepeat(), true
	case key.Matches(msg, k.Devices):
		return tea.Batch(m.devicesMenu(), m.startSpinner()), true
	case key.Matches(msg, k.Account):
		m.menu = m.accountMenu()
	case key.Matches(msg, k.Settings):
		if p := m.current(); p != nil && p.settings {
			m.back()
			return nil, true
		}
		return m.push(settingsPage()), true
	case key.Matches(msg, k.NowPlaying):
		if m.showingNowPlaying() {
			m.back()
			return nil, true
		}
		return m.push(nowPlayingPage(m.backend)), true
	case key.Matches(msg, k.NowMenu):
		if t := m.player.track(); t != nil {
			m.menu = m.actionsMenu(trackRow(*t), nil)
		}
	case key.Matches(msg, k.LikePlaying):
		if t := m.player.track(); t != nil {
			return m.toggleSaved(trackRow(*t)), true
		}
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) sidebarKey(msg tea.KeyPressMsg) tea.Cmd {
	k, sb := m.keys, &m.sidebar
	switch {
	case key.Matches(msg, k.Up):
		sb.move(-1)
	case key.Matches(msg, k.Down):
		sb.move(1)
	case key.Matches(msg, k.Top):
		sb.move(-len(sb.items))
	case key.Matches(msg, k.Bottom):
		sb.move(len(sb.items))
	case key.Matches(msg, k.PageUp):
		sb.move(-10)
	case key.Matches(msg, k.PageDown):
		sb.move(10)
	case key.Matches(msg, k.Enter):
		return m.openNav(sb.cursor)
	case key.Matches(msg, k.FocusRight, k.Focus):
		if m.current() != nil {
			m.focus = focusMain
		}
	case key.Matches(msg, k.Reload):
		return m.reloadPlaylists()
	case key.Matches(msg, k.Filter):
		if m.current() != nil {
			m.focus = focusMain
			return m.openFilter()
		}
	case key.Matches(msg, k.Menu):
		if it := sb.selected(); it.playlist != nil {
			m.menu = m.actionsMenu(playlistRow(*it.playlist), nil)
		}
	}
	return nil
}

func (m *Model) listKey(msg tea.KeyPressMsg, p *page) tea.Cmd {
	k := m.keys
	up, down := key.Matches(msg, k.Up), key.Matches(msg, k.Down)
	if p.home && homeKey(msg.String(), up, down, p) {
		return nil
	}
	if p.grid && m.gridKey(msg.String(), up, down, p) {
		return m.loadMore(p)
	}
	switch {
	case key.Matches(msg, k.Up):
		p.move(-1)
	case key.Matches(msg, k.Down):
		p.move(1)
	case key.Matches(msg, k.Top):
		p.move(-len(p.visible))
	case key.Matches(msg, k.Bottom):
		p.move(len(p.visible))
	case key.Matches(msg, k.PageUp):
		p.move(-m.listHeight())
	case key.Matches(msg, k.PageDown):
		p.move(m.listHeight())
	case key.Matches(msg, k.Enter):
		return m.activate(p)
	case key.Matches(msg, k.Back):
		if p.filter != "" {
			p.setFilter("")
		} else if !m.back() {
			m.focus = focusSidebar
		}
	case key.Matches(msg, k.FocusLeft, k.Focus):
		m.focus = focusSidebar
	case key.Matches(msg, k.Filter):
		return m.openFilter()
	case key.Matches(msg, k.Reload):
		return m.reload(p)
	case key.Matches(msg, k.PlayAll):
		if p.context != "" {
			return m.play(spotify.PlayOptions{ContextURI: p.context}, nil)
		}
	case key.Matches(msg, k.ShuffleAll):
		switch {
		case p.self != nil:
			return m.shufflePlay(*p.self)
		case p.context != "":
			return m.shufflePlay(playlistRow(spotify.Playlist{Name: p.title, URI: p.context, Items: &spotify.Count{Total: p.total}}))
		}
	case key.Matches(msg, k.Radio):
		switch r, ok := p.selected(); {
		case ok:
			return m.startRadio(r)
		case p.self != nil:
			return m.startRadio(*p.self)
		}
	case key.Matches(msg, k.Menu):
		if r, ok := p.selected(); ok {
			m.menu = m.actionsMenu(r, p)
		} else if p.self != nil {
			m.menu = m.actionsMenu(*p.self, nil)
		}
	case key.Matches(msg, k.Like):
		if r, ok := p.selected(); ok {
			return m.toggleSaved(r)
		}
	case key.Matches(msg, k.Queue):
		if r, ok := p.selected(); ok {
			if r.kind != kindTrack {
				m.setStatus("Only songs can be added to the queue", true)
				return nil
			}
			return m.addToQueue(r.track)
		}
	}
	return m.loadMore(p)
}

func (m *Model) menuKey(msg tea.KeyPressMsg) tea.Cmd {
	k, mn := m.keys, m.menu
	switch {
	case key.Matches(msg, k.Up):
		mn.move(-1)
	case key.Matches(msg, k.Down):
		mn.move(1)
	case key.Matches(msg, k.Back, k.Quit, k.Menu, k.Devices):
		m.menu = nil
	case key.Matches(msg, k.Enter):
		m.menu = nil
		if len(mn.items) == 0 {
			return nil
		}
		// run may open another menu (e.g. the playlist picker).
		return mn.items[mn.cursor].run()
	}
	return nil
}

func (m *Model) inputKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.current()
	switch {
	case msg.String() == "esc":
		if m.inputMode == inputFilter {
			p.setFilter("")
		}
		m.closeInput()
		if p.isSearch && len(p.rows) == 0 && p.query == "" {
			m.back()
		}
		return nil
	case msg.String() == "enter":
		mode := m.inputMode
		m.closeInput()
		switch mode {
		case inputSetting:
			return m.finishEdit()
		case inputSearch:
			m.searchSeq++ // cancel the pending debounce
			return m.runSearch(m.input.Value())
		}
		return nil
	case msg.String() == "up" || msg.String() == "down":
		if m.inputMode == inputSearch {
			m.closeInput()
			m.searchSeq++
			return m.runSearch(m.input.Value())
		}
		return m.listKey(msg, p)
	}
	return m.updateInput(msg)
}

// --- actions ---

// activate plays a track or opens an album, artist or playlist.
func (m *Model) activate(p *page) tea.Cmd {
	r, ok := p.selected()
	if !ok {
		// Spotify won't list tracks of playlists the user doesn't own, but
		// they can still be played as a whole.
		if len(p.rows) == 0 && p.context != "" && p.err != nil {
			return m.play(spotify.PlayOptions{ContextURI: p.context}, nil)
		}
		return nil
	}
	if r.kind == kindTrack {
		return m.playTrack(p, r.track)
	}
	return m.push(openRow(m.backend, r))
}

// playTrack plays t in the context of page p (nil plays just t).
func (m *Model) playTrack(p *page, t spotify.Track) tea.Cmd {
	if p == nil {
		return m.play(spotify.PlayOptions{URIs: []string{t.URI}}, nil)
	}
	uris, i := p.tracks(t.URI)
	// Keep the request small but allow skipping back and forth a little.
	lo, hi := max(0, i-50), min(len(uris), i+100)
	window := uris[lo:hi]
	if p.context != "" {
		return m.play(spotify.PlayOptions{ContextURI: p.context, OffsetURI: t.URI}, window)
	}
	return m.play(spotify.PlayOptions{URIs: window, OffsetIndex: i - lo}, nil)
}

func (m *Model) shufflePlay(r row) tea.Cmd {
	opts := spotify.PlayOptions{ContextURI: r.uri()}
	var n int
	switch r.kind {
	case kindAlbum:
		n = r.album.TotalTracks
	case kindPlaylist:
		n = r.playlist.TrackCount()
	case kindShow:
		n = r.show.TotalEpisodes
	}
	if n > 1 {
		opts.OffsetIndex = rand.IntN(n)
	}
	local := m.wantLocal()
	return m.act("shuffle play", "Shuffling “"+r.name()+"”", func(ctx context.Context) error {
		if err := m.playHere(ctx, local, &opts); err != nil {
			return err
		}
		// The random start makes the first song a surprise too.
		if err := m.startPlayback(ctx, opts, nil); err != nil {
			return err
		}
		return m.backend.SetShuffle(ctx, true)
	})
}

// radioURI is Spotify's endless station seeded by r, or "" if r can't seed
// one (podcasts, episodes, local files).
func radioURI(r row) string {
	switch r.kind {
	case kindTrack:
		if r.track.IsEpisode() || r.track.IsLocal || r.track.ID == "" {
			return ""
		}
	case kindAlbum, kindArtist, kindPlaylist:
	default:
		return ""
	}
	if uri := r.uri(); strings.HasPrefix(uri, "spotify:") {
		return "spotify:station:" + strings.TrimPrefix(uri, "spotify:")
	}
	return ""
}

// startRadio plays an endless mix of songs like r.
func (m *Model) startRadio(r row) tea.Cmd {
	uri := radioURI(r)
	if uri == "" {
		m.setStatus("Radio only works for songs, albums, artists and playlists", true)
		return nil
	}
	return m.playSpeakerOnly("radio", uri, "Radio from “"+r.name()+"”")
}

func (m *Model) startDJ() tea.Cmd {
	return m.playSpeakerOnly("DJ", spotify.DJURI, "DJ X is on")
}

// playSpeakerOnly plays a context that only sptui's own speaker can handle
// (stations, the DJ); the Web API refuses them for other devices.
func (m *Model) playSpeakerOnly(name, uri, ok string) tea.Cmd {
	local := m.wantLocal()
	onSpeaker := local || (m.opts.LocalDevice != "" && !m.remoteChosen)
	return m.act(name, ok, func(ctx context.Context) error {
		opts := spotify.PlayOptions{ContextURI: uri}
		if err := m.playHere(ctx, local, &opts); err != nil {
			return err
		}
		err := m.startPlayback(ctx, opts, nil)
		if err != nil && !onSpeaker {
			return errors.New(name + " only plays on sptui's own speaker — press d to switch")
		}
		return err
	})
}

func (m *Model) addToQueue(t spotify.Track) tea.Cmd {
	return m.act("queue", "Queued “"+t.Name+"”", func(ctx context.Context) error {
		return m.backend.AddToQueue(ctx, t.URI)
	})
}

// toggleSaved likes/unlikes a track, or saves/removes anything else.
func (m *Model) toggleSaved(r row) tea.Cmd {
	if m.ownPlaylist(r) {
		// Unfollowing your own playlist deletes it; too easy to do by accident.
		m.setStatus("That's your playlist — delete it in Spotify if you want it gone", true)
		return nil
	}
	uri := r.uri()
	return m.call(func(ctx context.Context) tea.Msg {
		saved, err := m.backend.InLibrary(ctx, []string{uri})
		if err != nil {
			return savedMsg{r: r, err: err}
		}
		if len(saved) == 1 && saved[0] {
			return savedMsg{r: r, saved: false, err: m.backend.RemoveFromLibrary(ctx, []string{uri})}
		}
		return savedMsg{r: r, saved: true, err: m.backend.SaveToLibrary(ctx, []string{uri})}
	})
}

func (m *Model) ownPlaylist(r row) bool {
	return r.kind == kindPlaylist && r.playlist.Owner.ID == m.me.ID
}

// --- player controls, updated optimistically so the UI feels instant ---

func (m *Model) togglePlay() tea.Cmd {
	now := time.Now()
	st := m.player.state
	if st == nil {
		return m.resume()
	}
	m.player.freeze(now)
	m.player.changedAt = now
	st.IsPlaying = !st.IsPlaying
	if st.IsPlaying {
		return m.resume()
	}
	return m.act("pause", "", m.backend.Pause)
}

func (m *Model) seek(delta time.Duration) tea.Cmd {
	t := m.player.track()
	if t == nil {
		return nil
	}
	now := time.Now()
	pos := min(max(m.player.progress(now)+delta, 0), t.Duration())
	m.player.freeze(now)
	m.player.changedAt = now
	m.player.state.ProgressMS = int(pos.Milliseconds())
	return m.act("seek", "", func(ctx context.Context) error { return m.backend.Seek(ctx, int(pos.Milliseconds())) })
}

func (m *Model) changeVolume(delta int) tea.Cmd {
	v := m.player.volume()
	if v < 0 {
		m.setStatus("This device doesn't support volume control", true)
		return nil
	}
	v = min(max(v+delta, 0), 100)
	m.player.state.Device.VolumePercent = &v
	m.player.changedAt = time.Now()
	m.player.volumeSeq++
	seq := m.player.volumeSeq
	return schedule(volumeDebounce, func(time.Time) tea.Msg { return volumeMsg{seq: seq, percent: v} })
}

func (m *Model) toggleShuffle() tea.Cmd {
	st := m.player.state
	if st == nil {
		return m.nothingPlaying()
	}
	st.ShuffleState = !st.ShuffleState
	m.player.changedAt = time.Now()
	on := st.ShuffleState
	msg := "Shuffle off"
	if on {
		msg = "Shuffle on"
	}
	return m.act("shuffle", msg, func(ctx context.Context) error { return m.backend.SetShuffle(ctx, on) })
}

func (m *Model) cycleRepeat() tea.Cmd {
	st := m.player.state
	if st == nil {
		return m.nothingPlaying()
	}
	st.RepeatState = nextRepeat(st.RepeatState)
	m.player.changedAt = time.Now()
	mode := st.RepeatState
	return m.act("repeat", "Repeat: "+repeatLabel(mode), func(ctx context.Context) error {
		return m.backend.SetRepeat(ctx, mode)
	})
}

func (m *Model) nothingPlaying() tea.Cmd {
	m.setStatus(friendly(spotify.ErrNoActiveDevice), true)
	return nil
}

func repeatLabel(mode string) string {
	switch mode {
	case spotify.RepeatContext:
		return "all"
	case spotify.RepeatTrack:
		return "one"
	}
	return "off"
}
