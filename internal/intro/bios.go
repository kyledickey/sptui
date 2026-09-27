package intro

import (
	"fmt"
	"math"
	"strings"
)

// bios: a power-on self-test. Boot checks print down the screen, dots
// running out to a status that ticks in, while a progress bar fills along
// the bottom. It holds on "boot ok" for a moment, then fades into the app.

const (
	biosW = 40 // scene size, in cells
	biosH = 12

	// Timeline, in milliseconds from the start.
	biosChecks = 140  // the first check starts
	biosGap    = 40   // between one status and the next line
	biosDots   = 90   // dots run out to the status
	biosFade   = 1700 // everything fades out, a beat after "boot ok"
	biosEnd    = 2000

	biosFirst = 3  // row of the first check
	biosBar   = 10 // row of the progress bar
)

// biosCheck is one line of the self-test: its label, how long it takes,
// and the status it ends on.
type biosCheck struct {
	label  string
	dur    float64
	status string
}

var biosTests = []biosCheck{
	{"memory test", 330, "640K ok"},
	{"detecting audio device", 150, "ok"},
	{"spinning up speakers", 130, "ok"},
	{"negotiating link", 170, "ok"},
	{"loading library", 180, "ok"},
}

// biosScene draws the bios intro as it is t milliseconds in.
func (p *Play) biosScene(t float64) *canvas {
	c := newCanvas(biosW, biosH)
	accent, purple, text, muted, faint, _ := p.colors()

	// The user's name shows up as the last check.
	name, _, _ := strings.Cut(p.Env.Name, " ")
	name = strings.ToLower(name)
	if r := []rune(name); len(r) > 14 {
		name = string(r[:14])
	}

	c.text(1, 0, "SPTUI BIOS v2.0", text)
	c.text(1, 1, "(c) sptui systems  rev 2026.09", faint)

	checks := biosTests
	if name != "" {
		checks = append(checks[:len(checks):len(checks)], biosCheck{"user", 50, name})
	}

	// Each check prints its label, runs dots out to the status column,
	// then ticks in its status. progress counts checks done, fractionally.
	const right = biosW - 2 // last column of a status
	start := float64(biosChecks)
	progress := 0.0
	cursorX, cursorY := 1, biosFirst
	for i, ch := range checks {
		if t < start {
			break
		}
		y := biosFirst + i
		age := t - start
		c.text(1, y, ch.label, muted)

		status := []rune(ch.status)
		sx := right - len(status) + 1 // the status's first column
		dotsFrom := 1 + len(ch.label) + 1
		dotsTo := sx - 1 // one past the last dot
		dots := int(float64(dotsTo-dotsFrom) * min(age/biosDots, 1))
		c.text(dotsFrom, y, strings.Repeat(".", dots), faint)
		cursorX, cursorY = dotsFrom+dots, y

		switch {
		case age >= ch.dur:
			// Done: values in text, the ok in accent.
			if v, ok := strings.CutSuffix(ch.status, "ok"); ok {
				c.text(sx, y, v, text)
				c.text(sx+len([]rune(v)), y, "ok", accent)
			} else {
				c.text(sx, y, ch.status, text)
			}
			cursorX, cursorY = 1, y+1
			progress = float64(i + 1)
		case ch.label == "memory test" && age >= biosDots:
			// Memory counts up in 64K blocks before it passes.
			k := min(int((age-biosDots)/(ch.dur-biosDots)*10)+1, 10) * 64
			c.text(sx, y, fmt.Sprintf("%3dK", k), text)
			cursorX = sx + 4
			progress = float64(i) + smoothstep(age/ch.dur)
		default:
			progress = float64(i) + smoothstep(age/ch.dur)
		}
		start += ch.dur + biosGap
	}

	// A blinking block cursor where the next character goes.
	if int(t/90)%2 == 0 && cursorY < biosBar-1 {
		c.text(cursorX, cursorY, "█", muted)
	}

	// Progress bar: chunky blocks in the gradient over a faint track, and
	// the percentage, which reads "boot ok" once it's full.
	f := min(progress/float64(len(checks)), 1)
	const barX, barW = 1, 29
	filled := int(math.Round(f * barW))
	for i := range barW {
		if i < filled {
			c.text(barX+i, biosBar, "█", mixHex(accent, purple, float64(i)/(barW-1)))
		} else {
			c.text(barX+i, biosBar, "░", faint)
		}
	}
	// Right-aligned with the statuses above.
	if f >= 1 {
		c.text(biosW-8, biosBar, "boot", text)
		c.text(biosW-3, biosBar, "ok", accent)
	} else {
		c.text(biosW-5, biosBar, fmt.Sprintf("%3d%%", int(f*100)), muted)
	}
	return c
}
