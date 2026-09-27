// Package tui is sptui's terminal interface, built with Bubble Tea. It talks
// to Spotify only through the Backend interface.
package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/spotify"
)

const statusFor = 4 * time.Second

// Options configures the UI.
type Options struct {
	// Config is the user's settings. The settings screen edits a copy and
	// hands it to SaveConfig; appearance changes apply at once, the rest
	// after a restart (see Outcome).
	Config     config.Config
	SaveConfig func(config.Config) error // nil means settings can't be saved
	// LocalDevice is the name of sptui's built-in speaker. Music plays there
	// unless the user picks another device. Empty means remote control only.
	LocalDevice string
	// PollInterval is how often to refresh playback state. Default 5s; a
	// local player can afford much more often.
	PollInterval time.Duration
	// KeepAwake is told whether the computer should stay awake, following
	// the keep-awake setting. Nil does nothing (see package awake).
	KeepAwake func(on bool)
	// Intro plays the startup animation Config names, if any.
	Intro bool
	Log   *slog.Logger // required
}

// Outcome says why the UI exited.
type Outcome int

const (
	Quit    Outcome = iota // the user quit
	LogOut                 // log out, then quit
	LogIn                  // log out, then log in again (e.g. to switch account)
	Restart                // start again with the saved settings
)

type focusArea int

const (
	focusSidebar focusArea = iota
	focusMain
)

type inputMode int

const (
	inputNone inputMode = iota
	inputSearch
	inputFilter
	inputSetting // editing a text setting
)

type status struct {
	text  string
	err   bool
	until time.Time
}

// Model is the root Bubble Tea model.
type Model struct {
	backend Backend
	opts    Options
	log     *slog.Logger
	keys    keyMap
	st      styles

	width, height int
	me            spotify.User

	focus   focusArea
	sidebar sidebar
	stack   []*page // navigation history of the main pane; last is on screen
	nextID  int

	input     textinput.Model
	inputMode inputMode
	searchSeq int

	player       player
	remoteChosen bool // the user moved playback to another device on purpose
	menu         *menu
	showHelp     bool
	helpLayer    int    // the help keyboard's layer: plain, shift or ctrl
	helpPicked   string // the key last pressed on the help keyboard
	status       status

	spinner  spinner.Model
	spinning bool
	covers   covers
	swatches swatches // cover colors

	intro *introPlay // the startup animation, while it plays
	bg    string     // terminal background as hex, once known

	accentShown string    // the accent the styles were built with
	selected    string    // which row is selected, to notice when it changes
	selectedAt  time.Time // when it did, for scrolling its title
	lyrics      lyricsState
	// lyricsFound remembers answers (including "none") for this session.
	lyricsFound map[string]lyricsState

	playlistGen int // discards playlist pages from before a reload

	outcome      Outcome
	cfg          config.Config // current settings
	dark         bool          // terminal background
	needsRestart bool          // a setting changed that applies on restart
	awake        bool          // what KeepAwake was last told
}

// Outcome says why the UI exited; read it after the program ends.
func (m *Model) Outcome() Outcome { return m.outcome }

// ErrMsg reports a problem from outside the UI, such as the built-in
// speaker stopping. Send it with tea.Program.Send.
type ErrMsg struct{ Err error }

// Messages for library loading.
type (
	meMsg struct {
		user spotify.User
		err  error
	}
	playlistsMsg struct {
		gen    int
		offset int
		page   spotify.Page[spotify.Playlist]
		origin spotify.Origin
		err    error
	}
	pageMsg struct {
		id     int
		chunk  chunk
		origin spotify.Origin
		err    error
	}
	searchMsg struct {
		id     int
		query  string
		rows   []row
		origin spotify.Origin
		err    error
	}
	debounceMsg struct{ seq int }
	reloadMsg   struct{ id int } // reload a page, if it's still around
	savedMsg    struct {
		r     row
		saved bool
		err   error
	}
)

// Tests change these: the cursor's blinking never ends, and the spinner
// would hold up every step.
var (
	blinkCursor    = true
	loadingSpinner = spinner.MiniDot
)

// New returns the root model.
func New(b Backend, opts Options) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	m := &Model{
		backend:     b,
		opts:        opts,
		log:         opts.Log.With("pkg", "tui"),
		keys:        newKeyMap(),
		sidebar:     newSidebar(),
		input:       in,
		focus:       focusMain,
		spinner:     spinner.New(spinner.WithSpinner(loadingSpinner)),
		covers:      newCovers(artMode(opts.Config)),
		swatches:    newSwatches(),
		cfg:         opts.Config,
		lyricsFound: map[string]lyricsState{},
		dark:        true,
	}
	m.player.every = cmp.Or(opts.PollInterval, 5*time.Second)
	m.setTheme(true)
	if opts.Intro {
		m.playIntro(opts.Config.Theme.Intro, false) // Init starts the ticks
	}
	return m
}

// artMode is how config says to draw covers; bad values mean off.
func artMode(cfg config.Config) art.Mode {
	mode, err := art.ParseMode(cfg.Theme.CoverArt)
	if err != nil {
		return art.Off
	}
	return mode
}

func (m *Model) setTheme(dark bool) {
	m.dark = dark
	m.accentShown = m.accentHex()
	accent := m.accentShown
	if m.cfg.Theme.Accent == AccentFromCover {
		accent = readable(accent, dark)
	}
	m.st = newStyles(accent, dark)
	m.spinner.Style = m.st.status
	s := textinput.DefaultStyles(dark)
	s.Focused.Text = m.st.row
	s.Focused.Placeholder = m.st.rowMuted
	s.Cursor.Color = m.st.accent
	s.Cursor.Blink = blinkCursor
	m.input.SetStyles(s)
}

// Init starts loading the user, their playlists and playback state.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadMe(),
		m.loadPlaylists(0),
		m.fetchPlayback(),
		tick(),
		tea.RequestBackgroundColor,
		requestCellSize(),
		pick(m.intro != nil, introTick(), nil),
	)
}

// Update handles a message and returns the next command. Afterwards it
// brings cover art in line with what's on screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.introUpdate(msg); ok {
		return m, cmd
	}
	cmd := m.update(msg)
	m.syncAwake()
	m.syncSelected()
	m.syncAccent()
	if m.intro != nil {
		return m, cmd // covers wait until the app is on screen
	}
	return m, tea.Batch(cmd, m.syncCovers(), m.syncSwatches(), m.syncLyrics())
}

// syncSelected notes when the selected row changes, so a long title under
// the cursor starts scrolling from its beginning.
func (m *Model) syncSelected() {
	sel := ""
	if p := m.current(); p != nil {
		if r, ok := p.selected(); ok {
			sel = fmt.Sprint(p.id, r.uri(), p.cursor)
		}
	}
	if sel != m.selected {
		m.selected, m.selectedAt = sel, time.Now()
	}
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, m.contentWidth()-12))
		return requestCellSize() // a font change resizes the window too
	case uv.CellSizeEvent:
		return m.setCellSize(msg.Width, msg.Height)
	case tea.BackgroundColorMsg:
		m.bg = hexOfColor(msg.Color)
		m.setTheme(msg.IsDark())
		return nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseWheelMsg, tea.MouseClickMsg:
		return m.handleMouse(msg.(tea.MouseMsg))
	case tea.PasteMsg:
		if m.inputMode != inputNone {
			return m.updateInput(msg)
		}
		return nil
	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	case coverMsg, coverSendMsg, coverReadyMsg:
		return m.handleCover(msg)
	case lyricsMsg:
		return m.setLyrics(msg)
	case lyricsRetryMsg:
		return m.retryLyrics(msg)
	case swatchMsg:
		m.handleSwatch(msg)
		return nil
	}
	return m.handleData(msg)
}

// syncAwake keeps the computer awake per the keep-awake setting.
func (m *Model) syncAwake() {
	if m.opts.KeepAwake == nil {
		return
	}
	want := false
	switch m.cfg.Player.KeepAwake {
	case "always":
		want = true
	case "playing":
		want = m.player.playing()
	}
	if want != m.awake {
		m.awake = want
		m.opts.KeepAwake(want)
	}
}

// quit frees terminal resources, then exits.
func (m *Model) quit() tea.Cmd {
	return tea.Sequence(m.freeCovers(), tea.Quit)
}

// handleData processes results of background work.
func (m *Model) handleData(msg tea.Msg) tea.Cmd {
	now := time.Now()
	switch msg := msg.(type) {
	case tickMsg:
		if !m.status.until.IsZero() && now.After(m.status.until) {
			m.status = status{}
		}
		// Tick faster while lyrics follow the song, and while things move.
		next := tickEvery
		if m.showingNowPlaying() {
			next = min(next, lyricsTick)
		}
		if m.animating() {
			next = min(next, animTick)
		}
		cmds := []tea.Cmd{tickAfter(next)}
		if m.player.due(now) {
			cmds = append(cmds, m.fetchPlayback())
		}
		return tea.Batch(cmds...)

	case refreshMsg:
		if m.player.fetching {
			m.player.lastFetch = time.Time{} // fetch again once this one lands
			return nil
		}
		return m.fetchPlayback()

	case playbackMsg:
		return m.setPlayback(msg, now)

	case likedMsg:
		if msg.uri == m.player.likedURI {
			m.player.liked = msg.liked
		}

	case volumeMsg:
		if msg.seq == m.player.volumeSeq {
			m.player.changedAt = now
			return m.act("volume", "", func(ctx context.Context) error { return m.backend.SetVolume(ctx, msg.percent) })
		}

	case actionMsg:
		switch {
		case msg.err != nil:
			m.setStatus(friendly(msg.err), true)
		case msg.ok != "":
			m.setStatus(msg.ok, false)
		}
		if msg.refresh {
			return refreshSoon()
		}

	case savedMsg:
		return m.handleSaved(msg)

	case meMsg:
		if msg.err != nil {
			m.log.Error("load profile", "err", msg.err)
			m.setStatus(friendly(msg.err), true)
			return retryLater(msg.err, m.loadMe())
		}
		m.me = msg.user
		m.log.Info("logged in as", "user", m.me.ID)
		if m.current() != nil {
			return nil // the user already went somewhere (e.g. search)
		}
		return m.openNav(m.sidebar.active)

	case playlistsMsg:
		if msg.gen != m.playlistGen {
			return nil
		}
		if msg.err != nil {
			m.log.Error("load playlists", "err", msg.err)
			m.setStatus(friendly(msg.err), true)
			return retryLater(msg.err, m.loadPlaylists(msg.offset))
		}
		m.sidebar.addPlaylists(msg.page.Items)
		m.sidebar.origin = mergeOrigin(m.sidebar.origin, msg.origin)
		if msg.page.HasMore() {
			// Limit, not len(Items): unavailable (null) playlists are dropped.
			return m.loadPlaylists(msg.page.Offset + max(msg.page.Limit, len(msg.page.Items)))
		}
		m.sidebar.loaded = true

	case pageMsg:
		p := m.findPage(msg.id)
		if p == nil {
			return nil
		}
		p.loading = false
		if msg.err != nil {
			m.log.Warn("load page", "page", p.title, "err", msg.err)
			p.err = msg.err
			if p.live { // the queue: try again once Spotify lets us
				return retryLater(msg.err, func() tea.Msg { return reloadMsg{id: p.id} })
			}
			return nil
		}
		p.append(msg.chunk)
		p.noteOrigin(msg.origin)
		return m.loadMore(p)

	case reloadMsg:
		if p := m.findPage(msg.id); p != nil {
			return m.reload(p)
		}

	case debounceMsg:
		if msg.seq == m.searchSeq {
			return m.runSearch(m.input.Value())
		}

	case searchMsg:
		p := m.findPage(msg.id)
		if p == nil || msg.query != p.query {
			return nil // superseded by a newer search
		}
		p.loading = false
		p.reset()
		p.subtitle = "Results for “" + msg.query + "”"
		// The user's own playlists come first, found locally, so they show
		// up even when Spotify's search fails.
		mine := m.myPlaylists(msg.query)
		if msg.err != nil {
			if len(mine) == 0 {
				p.err = msg.err
				return nil
			}
			m.setStatus(friendly(msg.err), true)
		}
		p.append(chunk{rows: append(mine, msg.rows...), next: -1})
		p.noteOrigin(msg.origin)

	case devicesMsg:
		m.setDevices(msg)

	case ErrMsg:
		m.log.Error("background error", "err", msg.Err)
		m.setStatus(friendly(msg.Err), true)
	}
	return nil
}

func (m *Model) setPlayback(msg playbackMsg, now time.Time) tea.Cmd {
	m.player.fetching = false
	if msg.err != nil {
		m.player.lastFetch = now
		m.log.Warn("fetch playback", "err", msg.err)
		m.setStatus(friendly(msg.err), true)
		return nil
	}
	prev := ""
	if t := m.player.track(); t != nil {
		prev = t.URI
	}
	if msg.started.Before(m.player.changedAt.Add(localGrace)) {
		// Fetched before a local change reached Spotify; it would undo it on
		// screen. Try again on the next tick.
		m.player.lastFetch = time.Time{}
		return nil
	}
	m.player.set(msg.state, now)

	t := m.player.track()
	if t == nil || t.URI == prev {
		return nil
	}
	m.log.Debug("now playing", "track", t.Name, "artist", t.ArtistNames())
	m.player.since = now
	var cmds []tea.Cmd
	if t.URI != m.player.likedURI {
		m.player.likedURI, m.player.liked = t.URI, false
		cmds = append(cmds, m.checkLiked(t.URI))
	}
	if p := m.current(); p != nil && p.live && prev != "" {
		cmds = append(cmds, m.reload(p))
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleSaved(msg savedMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus(friendly(msg.err), true)
		return nil
	}
	name := msg.r.name()
	var text string
	episode := msg.r.kind == kindTrack && msg.r.track.IsEpisode()
	follows := msg.r.kind == kindArtist || msg.r.kind == kindShow
	switch {
	case episode && msg.saved:
		text = "Saved “" + name + "” to Your Episodes"
	case episode:
		text = "Removed “" + name + "” from Your Episodes"
	case msg.r.kind == kindTrack && msg.saved:
		text = "Liked “" + name + "”"
	case msg.r.kind == kindTrack:
		text = "Removed “" + name + "” from Liked Songs"
	case follows && msg.saved:
		text = "Following " + name
	case follows:
		text = "Unfollowed " + name
	case msg.saved:
		text = "Saved “" + name + "” to your library"
	default:
		text = "Removed “" + name + "” from your library"
	}
	m.setStatus(text, false)
	if msg.r.kind == kindPlaylist {
		return m.reloadPlaylists() // followed playlists appear in the sidebar
	}
	if msg.r.kind == kindTrack && !msg.saved {
		saved := spotify.LikedSongsURI(m.me.ID)
		if episode {
			saved = spotify.YourEpisodesURI(m.me.ID)
		}
		for _, p := range m.stack {
			if p.context == saved {
				p.remove(msg.r.uri())
			}
		}
	}
	if msg.r.uri() == m.player.likedURI {
		m.player.liked = msg.saved
	}
	return nil
}

// retryLater runs cmd again once a rate limit is over, or after a short
// pause for other errors, since everything else depends on these loads.
func retryLater(err error, cmd tea.Cmd) tea.Cmd {
	wait := 5 * time.Second
	if apiErr, ok := errors.AsType[*spotify.Error](err); ok && apiErr.RetryAfter > 0 {
		wait = apiErr.RetryAfter
	}
	return schedule(wait, func(time.Time) tea.Msg { return cmd() })
}

func (m *Model) setStatus(text string, isErr bool) {
	m.status = status{text: text, err: isErr, until: time.Now().Add(statusFor)}
}

// busy reports whether anything on screen is waiting on the network.
func (m *Model) busy() bool {
	p := m.current()
	return (p != nil && p.loading) || (m.menu != nil && m.menu.loading) || m.lyrics.loading
}

func (m *Model) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spinner.Tick
}

// --- pages and navigation ---

func (m *Model) current() *page {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

func (m *Model) findPage(id int) *page {
	for _, p := range m.stack {
		if p.id == id {
			return p
		}
	}
	return nil
}

// push shows p on top of the current page.
func (m *Model) push(p *page) tea.Cmd {
	if p == nil {
		return nil
	}
	m.nextID++
	p.id = m.nextID
	m.stack = append(m.stack, p)
	m.focus = focusMain
	m.closeInput()
	m.log.Debug("open page", "title", p.title, "depth", len(m.stack))
	return m.loadMore(p)
}

// openNav replaces the navigation stack with the sidebar item at i.
func (m *Model) openNav(i int) tea.Cmd {
	it := m.sidebar.items[i]
	if it.play != nil && m.me.ID != "" {
		return it.play(m)
	}
	if it.open == nil || m.me.ID == "" {
		return nil
	}
	m.sidebar.active = i
	m.stack = nil
	return m.push(it.open(m))
}

// back pops the current page. It reports whether anything happened.
func (m *Model) back() bool {
	if len(m.stack) <= 1 {
		return false
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.closeInput()
	return true
}

// loadMore fetches the next chunk of p if it needs one.
func (m *Model) loadMore(p *page) tea.Cmd {
	if !p.needsMore() {
		return nil
	}
	p.loading = true
	id, offset, load, fresh := p.id, max(p.next, 0), p.load, p.fresh
	m.log.Debug("load page", "page", p.title, "offset", offset, "fresh", fresh)
	return tea.Batch(m.startSpinner(), m.call(func(ctx context.Context) tea.Msg {
		var origin spotify.Origin
		c, err := load(libraryContext(ctx, fresh, &origin), offset)
		return pageMsg{id: id, chunk: c, origin: origin, err: err}
	}))
}

// libraryContext prepares ctx for a library read: skipping caches when
// fresh is set, and noting in origin where the answer came from.
func libraryContext(ctx context.Context, fresh bool, origin *spotify.Origin) context.Context {
	if fresh {
		ctx = spotify.WithFresh(ctx)
	}
	return spotify.WithOrigin(ctx, origin)
}

// reload refetches p from Spotify, skipping caches.
func (m *Model) reload(p *page) tea.Cmd {
	p.fresh = true
	if p.isSearch {
		q := p.query
		p.query = ""
		return m.runSearch(q)
	}
	p.reset()
	p.loading = false
	m.nextID++
	p.id = m.nextID // drop results still in flight for the old rows
	return m.loadMore(p)
}

func (m *Model) loadMe() tea.Cmd {
	return m.call(func(ctx context.Context) tea.Msg {
		u, err := m.backend.Me(ctx)
		return meMsg{user: u, err: err}
	})
}

func (m *Model) loadPlaylists(offset int) tea.Cmd {
	gen, fresh := m.playlistGen, m.playlistGen > 0 // any reload skips caches
	return m.call(func(ctx context.Context) tea.Msg {
		var origin spotify.Origin
		pg, err := m.backend.Playlists(libraryContext(ctx, fresh, &origin), offset)
		return playlistsMsg{gen: gen, offset: offset, page: pg, origin: origin, err: err}
	})
}

func (m *Model) reloadPlaylists() tea.Cmd {
	m.playlistGen++
	items := m.sidebar.items[:0]
	for _, it := range m.sidebar.items {
		if it.playlist == nil {
			items = append(items, it)
		}
	}
	m.sidebar.items = items
	m.sidebar.loaded = false
	m.sidebar.origin = spotify.Origin{}
	m.sidebar.move(0)
	return m.loadPlaylists(0)
}

// --- search and filter input ---

func (m *Model) openSearch() tea.Cmd {
	if p := m.current(); p == nil || !p.isSearch {
		m.push(searchPage())
		m.input.SetValue("")
	} else {
		m.input.SetValue(p.query)
	}
	m.focus = focusMain
	m.inputMode = inputSearch
	m.input.Placeholder = "What do you want to listen to?"
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) openFilter() tea.Cmd {
	p := m.current()
	if p == nil || p.home {
		return nil
	}
	m.inputMode = inputFilter
	m.input.Placeholder = "Filter this list"
	m.input.SetValue(p.filter)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) closeInput() {
	m.inputMode = inputNone
	m.input.Blur()
}

// updateInput forwards msg to the text input and reacts to changes.
func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	after := m.input.Value()
	if after == before {
		return cmd
	}
	p := m.current()
	switch m.inputMode {
	case inputFilter:
		p.setFilter(after)
		return tea.Batch(cmd, m.loadMore(p))
	case inputSearch:
		m.searchSeq++
		seq := m.searchSeq
		return tea.Batch(cmd, schedule(searchDebounce, func(time.Time) tea.Msg { return debounceMsg{seq} }))
	}
	return cmd
}

// runSearch searches for q on the search page, unless it's already showing q.
func (m *Model) runSearch(q string) tea.Cmd {
	p := m.current()
	q = strings.TrimSpace(q)
	if p == nil || !p.isSearch || q == "" || q == p.query {
		return nil
	}
	p.query, p.loading = q, true
	id, fresh := p.id, p.fresh
	m.log.Debug("search", "query", q)
	return tea.Batch(m.startSpinner(), m.call(func(ctx context.Context) tea.Msg {
		var origin spotify.Origin
		res, err := m.backend.Search(libraryContext(ctx, fresh, &origin), q)
		return searchMsg{id: id, query: q, rows: searchRows(res), origin: origin, err: err}
	}))
}
