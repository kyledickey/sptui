package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Things that move: titles too long for their space scroll past like a
// marquee, and the playing song's row has a little equalizer.

var (
	animTick     = 150 * time.Millisecond // redraw rate while something moves
	marqueeStep  = 150 * time.Millisecond // time per cell scrolled
	marqueePause = 2 * time.Second        // still at the start of each pass
)

const marqueeGap = "   ·   "

// animating reports whether the screen needs the fast tick.
func (m *Model) animating() bool {
	return m.player.playing()
}

// scroll fits plain text s into w cells: as it is when it fits, cut with
// "…" when scrolling is off, else scrolled by how long ago since was.
func (m *Model) scroll(s string, w int, since time.Time) string {
	if ansi.StringWidth(s) <= w || !m.cfg.Theme.ScrollTitles {
		return clampWidth(s, w)
	}
	return marquee(s, w, time.Since(since))
}

// marquee is s scrolled left by elapsed, looping with a gap, in w cells.
// Each pass rests at the start for marqueePause.
func marquee(s string, w int, elapsed time.Duration) string {
	loop := []rune(s + marqueeGap)
	pass := marqueePause + time.Duration(len(loop))*marqueeStep
	t := elapsed % pass
	off := 0
	if t > marqueePause {
		off = int((t - marqueePause) / marqueeStep)
	}
	off %= len(loop)
	text := string(loop[off:]) + string(loop[:off]) + s
	return ansi.Truncate(text, w, "")
}

// eqFrames animate the playing song's row.
var eqFrames = []string{"▁▅▃", "▃▇▂", "▅▃▆", "▂▆▄", "▆▂▅", "▄▁▇", "▇▃▂", "▃▅▁"}

func equalizer(now time.Time) string {
	return eqFrames[int(now.UnixMilli()/int64(animTick/time.Millisecond))%len(eqFrames)]
}

// padRight pads rendered text to w cells.
func padRight(s string, w int) string {
	if pad := w - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
