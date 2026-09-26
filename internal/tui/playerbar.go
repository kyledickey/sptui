package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// The player bar at the bottom of every page. Its top border is the
// progress bar, carrying the song's title; inside are the cover, the
// artist and album, the buttons with their keys, and the volume; the
// device sits in the bottom border:
//
//	╭─ ▶ Life Itself · Glass Animals ━━━━━━━●────────── 1:12 / 4:41 ─╮
//	│ ▀▀▀▀  Glass Animals · How To Be A Human Being         ♥ liked  │
//	│ ▀▀▀▀  ⇄ s  ◀◀ p  ▶ space  ▶▶ n  ↻ r         vol ▮▮▮▮▮▮▮▯▯▯ 70% │
//	╰──────────────────────────────────────────────────── on sptui ─╯

func (m *Model) viewPlayer() string {
	w := m.width
	inner := w - 4
	edge := m.st.off
	t := m.player.track()
	if t == nil {
		top := edge.Render("╭─ ") + m.st.title.Render("Nothing playing") + " " +
			edge.Render(strings.Repeat("─", max(0, w-lipgloss.Width("╭─ Nothing playing ")-1))+"╮")
		body := []string{m.st.subtitle.Render(clampWidth(m.idleHint(), inner)), ""}
		return m.playerBox(top, body, "")
	}

	now := time.Now()
	st := m.player.state
	icon := m.st.on.Render("▶")
	if !st.IsPlaying {
		icon = m.st.subtitle.Render("‖")
	}

	// Top border: title, then progress up to the times.
	pos, dur := m.player.progress(now), t.Duration()
	times := m.st.subtitle.Render(clock(pos) + " / " + clock(dur))
	labelW := min(w*2/5, lipgloss.Width(t.Name)+3+lipgloss.Width(t.ArtistNames()))
	label := m.scroll(t.Name+" · "+t.ArtistNames(), labelW, m.player.since)
	title, rest, _ := strings.Cut(label, " · ")
	labelText := m.st.trackTitle.Render(title)
	if rest != "" {
		labelText += m.st.subtitle.Render(" · " + rest)
	}
	barW := max(w-3-2-lipgloss.Width(label)-2-lipgloss.Width(times)-3, 4)
	filled := 0
	if dur > 0 {
		filled = min(max(int(float64(barW)*float64(pos)/float64(dur)), 0), barW-1)
	}
	bar := m.st.progress.Render(strings.Repeat("━", filled)+"●") + edge.Render(strings.Repeat("─", barW-filled-1))
	top := edge.Render("╭─ ") + icon + " " + labelText + " " + bar + " " + times + edge.Render(" ─╮")

	// Inside: cover, then two lines.
	var thumb []string
	textW := inner
	if url := m.thumbURL(); url != "" {
		cols := m.coverCols(thumbRows)
		thumb = strings.Split(m.coverView(url, cols, thumbRows), "\n")
		textW -= cols + 2
	}
	heart := m.st.off.Render("♡ like")
	if m.player.liked {
		heart = m.st.on.Render("♥ liked")
	}
	about := m.st.trackArtist.Render(t.ArtistNames())
	if meta := trackMeta(t, pos); meta != "" {
		about += m.st.subtitle.Render(" · " + meta)
	}
	lines := []string{
		spread(clampWidth(about, textW-lipgloss.Width(heart)-2), heart, textW),
		spread(m.miniControls(st), m.volumeBar(), textW),
	}
	if lipgloss.Width(m.miniControls(st))+lipgloss.Width(m.volumeBar())+2 > textW {
		lines[1] = m.miniControls(st)
	}
	for i := range lines {
		lines[i] = clampWidth(lines[i], textW)
		if thumb != nil {
			lines[i] = thumb[i] + "  " + lines[i]
		}
	}
	return m.playerBox(top, lines, st.Device.Name)
}

// playerBox frames the player bar's lines under top, with the device in
// the bottom border.
func (m *Model) playerBox(top string, lines []string, device string) string {
	w, edge := m.width, m.st.off
	out := []string{clampWidth(top, w)}
	for _, l := range lines {
		out = append(out, edge.Render("│ ")+padRight(clampWidth(l, w-4), w-4)+edge.Render(" │"))
	}
	label := ""
	if device != "" {
		label = " on " + device + " "
	}
	label = clampWidth(label, w-6)
	bottom := edge.Render("╰"+strings.Repeat("─", max(0, w-3-lipgloss.Width(label)))) +
		m.st.subtitle.Render(label) + edge.Render("─╯")
	return strings.Join(append(out, bottom), "\n")
}

// miniControls are the buttons in small: each glyph with its key beside
// it, lit when on.
func (m *Model) miniControls(st *spotify.PlaybackState) string {
	k := m.keys
	button := func(glyph string, b string, lit bool) string {
		g := m.st.row.Render(glyph)
		if lit {
			g = m.st.on.Render(glyph)
		}
		return g + " " + m.st.off.Render(b)
	}
	play := "▶"
	if !st.IsPlaying {
		play = "‖"
	}
	repeat := "↻"
	if st.RepeatState == spotify.RepeatTrack {
		repeat = "↻¹"
	}
	return strings.Join([]string{
		button("⇄", k.Shuffle.Help().Key, st.ShuffleState),
		button("◀◀", k.Prev.Help().Key, false),
		button(play, k.PlayPause.Help().Key, st.IsPlaying),
		button("▶▶", k.Next.Help().Key, false),
		button(repeat, k.Repeat.Help().Key, st.RepeatState != spotify.RepeatOff),
	}, "   ")
}
