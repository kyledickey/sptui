package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/update"
)

// sptui looks for a newer release when it starts and every few hours
// after. When there is one, a slab at the top of the sidebar says so, and
// U installs it while the music keeps playing. U again restarts into it.

// Updater finds and installs new releases of sptui (see package update).
type Updater interface {
	// Available reports a release newer than the running one, or "".
	// Fresh skips the day-long memory of the last answer.
	Available(ctx context.Context, fresh bool) (string, error)
	Install(ctx context.Context, version string) error
}

const (
	updateRecheck = 6 * time.Hour
	updateTimeout = 5 * time.Minute // downloading can take a while
	updateNotice  = 10 * time.Second
)

type updateStage int

const (
	updateIdle updateStage = iota
	updateInstalling
	updateReady  // installed; restarting runs it
	updateFailed // installing failed; U tries again
)

type updateState struct {
	latest string // a release newer than this one, once known
	stage  updateStage
	err    string // why installing failed
}

type (
	updateCheckMsg struct {
		latest string
		err    error
		asked  bool // the user pressed U
	}
	updateRecheckMsg   struct{}
	updateInstalledMsg struct{ err error }
)

// NewRelease reports a newer sptui the UI learned of, and whether it was
// installed, for a word about it after quitting.
func (m *Model) NewRelease() (version string, installed bool) {
	return m.upd.latest, m.upd.stage == updateReady
}

// initUpdates starts looking for updates, if this build can have them.
func (m *Model) initUpdates() tea.Cmd {
	if m.opts.Updates == nil {
		return nil
	}
	return tea.Batch(pick(m.cfg.CheckUpdates, m.checkUpdates(false), nil), m.scheduleRecheck())
}

func (m *Model) scheduleRecheck() tea.Cmd {
	return schedule(updateRecheck, func(time.Time) tea.Msg { return updateRecheckMsg{} })
}

// checkUpdates asks whether there's a newer release. asked means the user
// wants to know now, not what was known this morning.
func (m *Model) checkUpdates(asked bool) tea.Cmd {
	u := m.opts.Updates
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		v, err := u.Available(ctx, asked)
		return updateCheckMsg{latest: v, err: err, asked: asked}
	}
}

func (m *Model) handleUpdate(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case updateRecheckMsg:
		return tea.Batch(m.scheduleRecheck(), pick(m.cfg.CheckUpdates, m.checkUpdates(false), nil))

	case updateCheckMsg:
		switch {
		case msg.err != nil:
			m.log.Warn("check for updates", "err", msg.err)
			if msg.asked {
				m.setStatus("Couldn't check for updates: "+friendly(msg.err), true)
			}
		case msg.latest == "":
			if msg.asked {
				m.setStatus("sptui "+m.opts.Version+" is the latest", false)
			}
		case m.upd.stage == updateIdle && msg.latest != m.upd.latest:
			m.upd.latest = msg.latest
			m.setStatus("sptui "+msg.latest+" is out · press U to update", false)
			m.status.until = time.Now().Add(updateNotice)
			if msg.asked {
				return m.installUpdate()
			}
		}

	case updateInstalledMsg:
		if msg.err != nil {
			m.log.Error("install update", "version", m.upd.latest, "err", msg.err)
			m.upd.stage = updateFailed
			m.upd.err = friendly(msg.err)
			if errors.Is(msg.err, update.ErrNotWritable) {
				m.upd.err = "no permission; run sudo sptui update"
			}
			m.setStatus("Couldn't update: "+m.upd.err, true)
			return nil
		}
		m.upd.stage = updateReady
		m.setStatus("sptui "+m.upd.latest+" installed · press U to restart into it", false)
		m.status.until = time.Now().Add(updateNotice)
	}
	return nil
}

// updateKey is U: check, install or restart, whichever comes next.
func (m *Model) updateKey() tea.Cmd {
	switch {
	case m.opts.Updates == nil:
		m.setStatus("This build of sptui doesn't update itself", true)
	case m.upd.stage == updateInstalling:
		m.setStatus("Already downloading sptui "+m.upd.latest+"…", false)
	case m.upd.stage == updateReady:
		m.log.Info("restarting into the update", "version", m.upd.latest)
		m.outcome = Relaunch
		return m.quit()
	case m.upd.latest == "":
		m.setStatus("Checking for updates…", false)
		return m.checkUpdates(true)
	default:
		return m.installUpdate()
	}
	return nil
}

// installUpdate downloads and installs the newer release in the
// background.
func (m *Model) installUpdate() tea.Cmd {
	m.upd.stage, m.upd.err = updateInstalling, ""
	m.setStatus("Downloading sptui "+m.upd.latest+"…", false)
	u, v := m.opts.Updates, m.upd.latest
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		defer cancel()
		return updateInstalledMsg{err: u.Install(ctx, v)}
	})
}

// updateBox is the slab at the top of the sidebar while there's an update
// to install, w wide. It's empty when there's nothing to say.
func (m *Model) updateBox(w int) []string {
	if m.upd.latest == "" {
		return nil
	}
	var title, action string
	slab, edge := m.st.logo, m.st.logoEdge
	switch m.upd.stage {
	case updateInstalling:
		title = m.spinner.View() + " DOWNLOADING " + m.upd.latest
		action = m.st.keyDesc.Render("music keeps playing")
	case updateReady:
		title = "✓ " + m.upd.latest + " INSTALLED"
		action = m.st.key.Render("U") + m.st.keyDesc.Render(" restart into it")
	case updateFailed:
		title = "✗ UPDATE FAILED"
		slab = slab.Background(m.st.danger)
		edge = edge.Foreground(m.st.danger)
		action = m.st.key.Render("U") + m.st.keyDesc.Render(" try again · "+m.upd.err)
	default:
		title = "↑ " + m.upd.latest + " IS OUT"
		action = m.st.key.Render("U") + m.st.keyDesc.Render(" update now")
	}
	top := edge.Render("▐") + slab.Render(fit(" "+title, w-2)) + edge.Render("▌")
	return []string{top, clampWidth("  "+action, w)}
}

// sidebarUpdateBox is the update box as the top of the sidebar shows it,
// with a line to set it apart from the library: not at all when it would
// crowd out the playlists.
func (m *Model) sidebarUpdateBox() []string {
	box := m.updateBox(m.sidebarWidth() - 2)
	if len(box) == 0 || m.bodyHeight()-2-len(box) < 5 {
		return nil
	}
	return append(box, "")
}

// updateMarker is a short note for the header, where there's no sidebar.
func (m *Model) updateMarker() string {
	switch {
	case m.upd.latest == "":
		return ""
	case m.upd.stage == updateReady:
		return m.st.status.Render("✓ "+m.upd.latest) + m.st.keyDesc.Render(" ") + m.st.key.Render("U") + m.st.keyDesc.Render(" restart  ")
	case m.upd.stage == updateInstalling:
		return m.st.status.Render(m.spinner.View()+" "+m.upd.latest) + "  "
	}
	return m.st.status.Render("↑ "+m.upd.latest) + m.st.keyDesc.Render(" ") + m.st.key.Render("U") + m.st.keyDesc.Render(" update  ")
}

// updateSettings are the settings screen's lines about updates.
func (m *Model) updateSettings() []setting {
	version := strings.TrimSpace("sptui " + m.opts.Version)
	check := setting{
		section: "Updates", label: "Check for updates",
		help:    "This is " + version + ". sptui looks for a new release once a day and says when there is one.",
		choices: []string{"on", "off"},
		get: func(c config.Config) string {
			return pick(c.CheckUpdates, "on", "off")
		},
		set: func(c *config.Config, v string) error { c.CheckUpdates = v == "on"; return nil },
	}
	if m.opts.Updates == nil {
		check.help = "This is " + version + ", a build that doesn't update itself."
		return []setting{check}
	}
	label := "Check now"
	switch {
	case m.upd.stage == updateReady:
		label = "Restart into " + m.upd.latest
	case m.upd.stage == updateInstalling:
		label = "Downloading " + m.upd.latest + "…"
	case m.upd.latest != "":
		label = "Update to " + m.upd.latest
	}
	return []setting{check, {
		section: "Updates", label: label,
		help:   "This is " + version + ". Same as pressing U anywhere, or running sptui update in a terminal.",
		action: m.updateKey,
	}}
}
