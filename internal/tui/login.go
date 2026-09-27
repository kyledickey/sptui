package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/banner"
	"github.com/kyledickey/sptui/internal/config"
)

// The login screen runs before the app whenever Spotify needs the user's
// OK. Without a Client ID of their own it first walks them through making
// one, since the shared app is often out of requests; then it waits while
// they approve sptui in the browser.

// LoginStep is one approval page the user goes through in the browser.
type LoginStep struct {
	Purpose string                          // what it's for, e.g. "play music in sptui"
	Wait    func(ctx context.Context) error // returns once the user approves
}

// LoginAttempt is a login under way.
type LoginAttempt struct {
	URL    string // the first approval page; each hands on to the next
	Steps  []LoginStep
	Cancel func() // gives up, freeing what the attempt holds
}

// LoginOptions configures the login screen.
type LoginOptions struct {
	Config     config.Config
	SaveConfig func(config.Config) error // required
	// Welcome greets the user before anything else, for a first login.
	Welcome bool
	// WebLogin says the Web API needs logging in, which is the login the
	// user's own app is for. Without a Client ID, the screen offers to set
	// one up first.
	WebLogin bool
	// Start begins logging in with cfg: the config after any app changes.
	Start   func(cfg config.Config) (LoginAttempt, error) // required
	OpenURL func(url string) error                        // nil does nothing
	Log     *slog.Logger                                  // required
}

// RunLogin shows the login screen until the user is logged in or gives up,
// and reports which.
func RunLogin(ctx context.Context, opts LoginOptions) (bool, error) {
	m := newLogin(ctx, opts)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	m.stop()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return false, err
	}
	return m.done, nil
}

type loginStage int

const (
	stageWelcome loginStage = iota
	stageApp                // setting up the user's own app
	stageApprove            // waiting on the browser
	stageDone
)

const (
	loginWidth  = 70
	clientIDLen = 32
	flashFor    = 2 * time.Second
	doneLingers = 800 * time.Millisecond
	welcomeTick = 90 * time.Millisecond
)

type (
	stepDoneMsg struct {
		seq, step int
		err       error
	}
	flashOverMsg struct{ seq int }
	welcomeMsg   struct{}
	loginOverMsg struct{}
)

type loginModel struct {
	opts    LoginOptions
	ctx     context.Context
	cfg     config.Config
	st      styles
	dark    bool
	spinner spinner.Model
	input   textinput.Model

	width, height int
	stage         loginStage
	askedForApp   bool // the app stage is shown, so it's in the trail
	opened        bool // the dashboard was opened
	copied        bool // the redirect URI was copied
	appErr        string

	attempt *LoginAttempt
	actx    context.Context // the attempt's; cancel ends it
	cancel  context.CancelFunc
	seq     int // tells this attempt's messages from older ones
	step    int // the step being waited on
	err     error

	flash    string
	flashSeq int
	done     bool
}

func newLogin(ctx context.Context, opts LoginOptions) *loginModel {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = clientIDLen
	in.Placeholder = "paste it here"
	in.SetWidth(clientIDLen + 1)
	in.SetValue(opts.Config.ClientID)
	m := &loginModel{
		opts:    opts,
		ctx:     ctx,
		cfg:     opts.Config,
		spinner: spinner.New(spinner.WithSpinner(loadingSpinner)),
		input:   in,
		stage:   stageApprove,
	}
	if opts.WebLogin && opts.Config.ClientID == "" {
		m.stage, m.askedForApp = stageApp, true
	}
	if opts.Welcome {
		m.stage = stageWelcome
	}
	m.setTheme(true)
	return m
}

func (m *loginModel) setTheme(dark bool) {
	m.dark = dark
	accent := m.cfg.Theme.Accent
	if accent == "" || accent == AccentFromCover {
		accent = DefaultAccent
	}
	m.st = newStyles(accent, dark)
	m.spinner.Style = m.st.status
	s := textinput.DefaultStyles(dark)
	s.Focused.Text = m.st.title
	s.Focused.Placeholder = m.st.rowMuted
	s.Cursor.Color = m.st.accent
	s.Cursor.Blink = blinkCursor
	m.input.SetStyles(s)
}

func (m *loginModel) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	switch m.stage {
	case stageWelcome:
		cmds = append(cmds, welcomeAnim())
	case stageApp:
		cmds = append(cmds, m.input.Focus())
	default:
		cmds = append(cmds, m.begin())
	}
	return tea.Batch(cmds...)
}

// begin starts logging in, giving up on any login already under way.
func (m *loginModel) begin() tea.Cmd {
	m.stop()
	m.seq++
	m.stage, m.step, m.err = stageApprove, 0, nil
	m.input.Blur()
	a, err := m.opts.Start(m.cfg)
	if err != nil {
		m.opts.Log.Error("start login", "err", err)
		m.err = err
		return nil
	}
	if len(a.Steps) == 0 {
		return m.finish()
	}
	m.actx, m.cancel = context.WithCancel(m.ctx)
	m.attempt = &a
	m.open(a.URL)
	return tea.Batch(m.wait(), m.spinner.Tick)
}

// wait waits on the current step of the attempt.
func (m *loginModel) wait() tea.Cmd {
	ctx, seq, i, step := m.actx, m.seq, m.step, m.attempt.Steps[m.step]
	return func() tea.Msg { return stepDoneMsg{seq: seq, step: i, err: step.Wait(ctx)} }
}

// stop gives up on the login under way, if there is one.
func (m *loginModel) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.attempt != nil && m.attempt.Cancel != nil {
		m.attempt.Cancel()
	}
	m.attempt = nil
}

func (m *loginModel) finish() tea.Cmd {
	m.stop()
	m.stage, m.done = stageDone, true
	return schedule(doneLingers, func(time.Time) tea.Msg { return loginOverMsg{} })
}

func (m *loginModel) open(url string) {
	if m.opts.OpenURL == nil {
		return
	}
	if err := m.opts.OpenURL(url); err != nil {
		m.opts.Log.Warn("open browser", "err", err)
	}
}

func (m *loginModel) copy(text, what string) tea.Cmd {
	m.flashSeq++
	m.flash = what + " copied"
	seq := m.flashSeq
	return tea.Batch(tea.SetClipboard(text),
		schedule(flashFor, func(time.Time) tea.Msg { return flashOverMsg{seq} }))
}

// ownApp reports whether logins use the user's own app.
func (m *loginModel) ownApp() bool { return m.cfg.ClientID != "" }

func (m *loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
	case spinner.TickMsg:
		if m.stage != stageApprove || m.err != nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case stepDoneMsg:
		return m, m.stepDone(msg)
	case flashOverMsg:
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
	case loginOverMsg:
		return m, tea.Quit
	case welcomeMsg:
		if m.stage == stageWelcome {
			return m, welcomeAnim()
		}
	case tea.PasteMsg:
		if m.stage == stageApp {
			m.input.SetValue(clientIDOf(m.input.Value() + msg.Content))
			m.input.CursorEnd()
			m.appErr = ""
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *loginModel) stepDone(msg stepDoneMsg) tea.Cmd {
	if msg.seq != m.seq || m.attempt == nil {
		return nil // from a login given up on
	}
	if msg.err != nil {
		m.opts.Log.Error("login", "purpose", m.attempt.Steps[msg.step].Purpose, "err", msg.err)
		m.err = msg.err
		m.stop()
		return nil
	}
	m.step++
	if m.step == len(m.attempt.Steps) {
		return m.finish()
	}
	return m.wait()
}

func (m *loginModel) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "esc":
		return tea.Quit
	}
	switch m.stage {
	case stageWelcome:
		if msg.String() == "enter" {
			return m.welcomeDone()
		}
	case stageApp:
		return m.appKey(msg)
	case stageApprove:
		return m.approveKey(msg)
	}
	return nil
}

func (m *loginModel) appKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		if m.input.Value() == "" {
			m.appErr = "paste it first, or tab to skip"
			return nil
		}
		return m.useApp(m.input.Value())
	case "tab":
		return m.useApp("")
	case "o":
		m.opened = true
		m.open(dashboardURL)
		return nil
	case "r":
		m.copied = true
		return m.copy(config.DefaultRedirectURI, "Redirect URI")
	}
	// Client IDs are hex, so other letters are free for the keys above.
	if t := msg.Text; t != "" && clientIDOf(t) != strings.ToLower(t) {
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := clientIDOf(m.input.Value()); v != m.input.Value() {
		m.input.SetValue(v)
	}
	m.appErr = ""
	return cmd
}

// useApp logs in with the user's own app, or the shared one for "".
func (m *loginModel) useApp(id string) tea.Cmd {
	if id != "" && !clientIDPattern.MatchString(id) {
		m.appErr = fmt.Sprintf("that's %d of %d characters", len(id), clientIDLen)
		return nil
	}
	if id != m.cfg.ClientID {
		cfg := m.cfg
		cfg.ClientID = id
		if id != "" && cfg.RedirectURI == "" {
			cfg.RedirectURI = config.DefaultRedirectURI
		}
		if err := m.opts.SaveConfig(cfg); err != nil {
			m.appErr = "couldn't save: " + err.Error()
			return nil
		}
		m.cfg = cfg
	}
	return m.begin()
}

func (m *loginModel) approveKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		if m.err != nil {
			return m.begin()
		}
	case "o":
		if m.attempt != nil {
			m.open(m.attempt.URL)
		}
	case "y":
		if m.attempt != nil {
			return m.copy(m.attempt.URL, "Link")
		}
	case "r":
		if m.ownApp() {
			return m.copy(config.DefaultRedirectURI, "Redirect URI")
		}
	case "a":
		if m.opts.WebLogin {
			m.stop()
			m.seq++
			m.stage, m.err, m.askedForApp = stageApp, nil, true
			m.input.SetValue(m.cfg.ClientID)
			m.input.CursorEnd()
			return m.input.Focus()
		}
	}
	return nil
}

// clientIDOf keeps what could be part of a Client ID: hex digits, in
// lower case.
func clientIDOf(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- view ---

func (m *loginModel) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = "sptui · log in"
	if m.width == 0 {
		return v
	}
	w := max(min(loginWidth, m.width-4), 20)
	align := lipgloss.Left
	if m.stage == stageWelcome {
		align = lipgloss.Center // around the equalizer
		w -= 1 - w%2            // which is odd, for a middle column
	}
	var body string
	switch m.stage {
	case stageWelcome:
		body = m.viewWelcome(w)
	case stageApp:
		body = m.viewApp(w)
	case stageApprove:
		body = m.viewApprove(w)
	case stageDone:
		body = m.st.status.Render("✓ ") + m.st.title.Render("You're in")
	}
	screen := lipgloss.JoinVertical(align, m.viewHeader(w), "", "", body, "", "", m.viewKeys(w))
	if lipgloss.Height(screen) > m.height {
		screen = lipgloss.JoinVertical(align, m.viewHeader(w), "", body, "", m.viewKeys(w))
	}
	v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Width(w).Align(align).Render(screen)))
	return v
}

// viewHeader is the name in the pixel font, and the trail of stages.
func (m *loginModel) viewHeader(w int) string {
	word, _ := banner.Spell("sptui")
	accent := lipgloss.NewStyle().Foreground(m.st.accent)
	if m.stage == stageWelcome {
		return accent.Render(word[0]) + "\n" + accent.Render(word[1]) // the welcome lists the stages
	}
	type stage struct {
		name string
		at   loginStage
	}
	stages := []stage{{"log in", stageApprove}, {"play", stageDone}}
	if m.askedForApp {
		stages = append([]stage{{"your app", stageApp}}, stages...)
	}
	var trail []string
	for _, s := range stages {
		switch {
		case s.at == m.stage:
			trail = append(trail, m.st.modalTitle.Render(s.name))
		case s.at < m.stage:
			trail = append(trail, m.st.off.Render("✓ "+s.name))
		default:
			trail = append(trail, m.st.crumb.Render(s.name))
		}
	}
	return accent.Render(word[0]) + "\n" +
		spread(accent.Render(word[1]), strings.Join(trail, m.st.off.Render("  ›  ")), w)
}

// chip is a step's number on a slab: lit when it's the step to do next.
func (m *loginModel) chip(label string, lit bool) string {
	if lit {
		return m.st.logoEdge.Render("▐") + m.st.logo.Render(label) + m.st.logoEdge.Render("▌")
	}
	return m.st.tagEdge.Render("▐") + m.st.tag.Render(label) + m.st.tagEdge.Render("▌")
}

func welcomeAnim() tea.Cmd {
	return schedule(welcomeTick, func(time.Time) tea.Msg { return welcomeMsg{} })
}

// welcomeDone moves on from the welcome to setting up.
func (m *loginModel) welcomeDone() tea.Cmd {
	if m.askedForApp {
		m.stage = stageApp
		return m.input.Focus()
	}
	return m.begin()
}

func (m *loginModel) viewWelcome(w int) string {
	var steps []string
	if m.askedForApp {
		steps = append(steps, "Get your own Spotify app")
	}
	steps = append(steps, "Log in", "Play")
	// The steps line up with each other, and centre as one block.
	list := make([]string, len(steps))
	for i, s := range steps {
		list[i] = m.chip(fmt.Sprint(i+1), i == 0) + "  " + m.st.row.Render(s)
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		m.st.title.Render("Welcome to sptui"),
		m.st.subtitle.Render("Spotify, in your terminal."),
		"",
		m.equalizer(w, time.Now()),
		"",
		lipgloss.JoinVertical(lipgloss.Left, list...),
		"",
		m.st.off.Render("You'll need Spotify Premium."),
	)
}

// equalizer is a strip of bars two lines tall that bounce like music. It
// starts and ends with a bar, so it sits evenly under centred text.
func (m *loginModel) equalizer(w int, now time.Time) string {
	const cells = " ▁▂▃▄▅▆▇█"
	if w%2 == 0 {
		w--
	}
	t := float64(now.UnixMilli()) / 1000
	var top, bottom strings.Builder
	for x := range w {
		if x%2 == 1 {
			top.WriteByte(' ')
			bottom.WriteByte(' ')
			continue
		}
		f := float64(x) / float64(w)
		v := 0.55 + 0.25*math.Sin(t*3.1+f*9) + 0.2*math.Sin(t*5.3-f*23) - 0.25*f
		n := int(math.Round(max(1, min(16, v*16))))
		top.WriteRune([]rune(cells)[max(n-8, 0)])
		bottom.WriteRune([]rune(cells)[min(n, 8)])
	}
	bar := lipgloss.NewStyle().Foreground(m.st.accent)
	return bar.Render(top.String()) + "\n" + bar.Render(bottom.String())
}

func (m *loginModel) viewApp(w int) string {
	id := m.input.Value()
	valid := clientIDPattern.MatchString(id)
	next := 3
	switch {
	case !m.opened:
		next = 1
	case !m.copied:
		next = 2
	}
	step := func(n int, done bool, title, hint string, more ...string) string {
		label := fmt.Sprint(n)
		if done {
			label = "✓"
		}
		head := m.chip(label, n == next) + "  " + m.st.title.Render(title)
		if hint != "" {
			head = spread(head, hint, w)
		}
		lines := []string{head}
		for _, l := range more {
			lines = append(lines, "     "+l)
		}
		return strings.Join(lines, "\n")
	}

	field := m.st.off.Render("▕ ") + padRight(m.input.View(), clientIDLen+1) + m.st.off.Render("▏")
	var check string
	switch {
	case m.appErr != "":
		check = m.st.errText.Render(m.appErr)
	case valid:
		check = m.st.status.Render("✓ looks right")
	case id != "":
		check = m.st.subtitle.Render(fmt.Sprintf("%d/%d", len(id), clientIDLen))
	}
	lines := []string{
		m.st.title.Render("Use your own Spotify app"),
		m.st.subtitle.Render("The shared one gets rate limited. Yours won't."),
		"",
		step(1, m.opened, "Create an app", m.hint("o", "open dashboard")),
		"",
		step(2, m.copied, "Add this redirect URI, tick Web API", m.hint("r", "copy"),
			m.st.status.Render(config.DefaultRedirectURI)),
		"",
		step(3, valid, "Paste its Client ID", "", field+"  "+check),
	}
	return strings.Join(lines, "\n")
}

func (m *loginModel) viewApprove(w int) string {
	para := m.st.subtitle.Width(w)
	if m.err != nil {
		lines := []string{
			m.st.errText.Render("✗ ") + m.st.title.Render("Login failed"),
			"",
			para.Render(loginProblem(m.err)),
		}
		if m.ownApp() {
			lines = append(lines, "", para.Render("Check your Client ID and redirect URI."))
		}
		return strings.Join(lines, "\n")
	}

	var steps []LoginStep
	if m.attempt != nil {
		steps = m.attempt.Steps
	}
	lines := []string{m.st.title.Render("Approve in your browser"), ""}
	for i, s := range steps {
		switch {
		case i < m.step:
			lines = append(lines, m.st.status.Render(" ✓ ")+m.st.subtitle.Render(s.Purpose))
		case i == m.step:
			lines = append(lines, " "+m.spinner.View()+" "+m.st.title.Render(s.Purpose))
		default:
			lines = append(lines, m.st.off.Render(" · ")+m.st.off.Render(s.Purpose))
		}
	}
	lines = append(lines, "", "",
		spread(m.st.subtitle.Render("Browser didn't open?"), m.hint("o", "open again")+"   "+m.hint("y", "copy link"), w))
	if m.attempt != nil {
		lines = append(lines, m.st.off.Render(clampWidth(m.attempt.URL, w)))
	}
	if m.ownApp() {
		lines = append(lines, "",
			spread(m.st.subtitle.Render("INVALID_CLIENT?"), m.hint("a", "fix your app"), w))
	}
	return strings.Join(lines, "\n")
}

// loginProblem says what went wrong logging in.
func loginProblem(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Spotify didn't hear back in time. Try again, and approve both pages in the browser."
	case strings.Contains(err.Error(), "access_denied"):
		return "The login was cancelled in the browser."
	case strings.Contains(err.Error(), "address already in use"):
		return "Something else is using " + config.DefaultRedirectURI + ". Is another sptui logging in?"
	}
	return err.Error()
}

func (m *loginModel) hint(k, what string) string {
	return m.st.key.Render(k) + " " + m.st.keyDesc.Render(what)
}

// viewKeys is the footer: what the keys do, and anything just copied.
func (m *loginModel) viewKeys(w int) string {
	var keys []string
	switch m.stage {
	case stageWelcome:
		keys = append(keys, m.hint("enter", "get started"))
	case stageApp:
		keys = append(keys, m.hint("enter", "log in"), m.hint("tab", "skip, use the shared app"))
	case stageApprove:
		if m.err != nil {
			keys = append(keys, m.hint("enter", "try again"))
		}
		if m.opts.WebLogin && !m.ownApp() {
			keys = append(keys, m.hint("a", "use your own app"))
		}
	case stageDone:
		return ""
	}
	keys = append(keys, m.hint("esc", "quit"))
	left := strings.Join(keys, m.st.off.Render("  ·  "))
	if m.flash != "" {
		return spread(left, m.st.status.Render("✓ "+m.flash), w)
	}
	return left
}
