package intro

import (
	"math"
	"strings"
)

// boombox: a charcoal boombox drops onto the floor with a puff of dust,
// then the tape starts. It kicks on every beat, its woofers thump in and
// out, pressure rings roll off both sides, a level meter jumps, and SPTUI
// scrolls past in the cassette window. Then it all fades into the app.

const (
	boomboxW = 42 // scene size, in cells
	boomboxH = 16

	// Timeline, in milliseconds from the start.
	boomboxLand = 380  // feet touch the floor
	boomboxPlay = 520  // the tape starts and the beat kicks in
	boomboxBeat = 280  // one thump
	boomboxTape = 60   // per cell the tape scrolls
	boomboxType = 1250 // the greeting starts typing
	boomboxFade = 2150 // everything fades out
	boomboxEnd  = 2500

	// Layout, in pixels (two per cell, stacked).
	boomboxX     = 5  // left edge of the body, in cells
	boomboxBodyW = 32 // body size
	boomboxBodyH = 16
	boomboxRest  = 10 // top of the body when it sits on the floor; even, so the window lines up with cells
	boomboxWinX  = 10 // cassette window, from the body's left edge, in cells
	boomboxWinW  = 12
	boomboxWinY  = 4 // from the body's top, in pixels
	boomboxHello = 15

	// Neutral darks for the chassis, so the theme colors do the talking.
	boomboxChassis = "#2b2e35"
	boomboxLip     = "#3a3e47"
	boomboxBase    = "#202329"
	boomboxRim     = "#4b505b"
)

// boomboxScene draws the boombox intro as it is t milliseconds in.
func (p *Play) boomboxScene(t float64) *canvas {
	c := newCanvas(boomboxW, boomboxH)
	accent, purple, text, muted, faint, bg := p.colors()
	dark := introDark

	// Falls with gravity, then kicks up on every beat. pump is how hard the
	// woofers are pushing out right now, 0 to 1.
	by, pump, playing := boomboxRest, 0.0, t >= boomboxPlay
	switch {
	case t < boomboxLand:
		f := t / boomboxLand
		by = int(math.Round(-26+float64(boomboxRest+26)*f*f)) &^ 1
	case playing:
		beat := math.Mod(t-boomboxPlay, boomboxBeat) / boomboxBeat
		pump = math.Exp(-4 * beat)
		if beat < 0.25 {
			by -= 2 // one cell, so the window's letters move with it
		}
	}
	bx := boomboxX

	// Shadow on the floor: tighter while the box is in the air.
	if t >= boomboxLand*0.5 {
		inset := pick(by < boomboxRest, 4, 1)
		for x := bx + inset; x < bx+boomboxBodyW-inset; x++ {
			c.pixel(x, boomboxRest+boomboxBodyH+1, mixHex(faint, bg, 0.35))
		}
	}

	// Dust kicks out from the feet when it lands.
	if age := t - boomboxLand; age >= 0 && age < 300 {
		d := int(age / 75)
		dust := mixHex(muted, faint, age/300)
		row := (boomboxRest + boomboxBodyH) / 2
		c.text(bx-1-d, row, pick(d < 2, "∙", "·"), dust)
		c.text(bx+boomboxBodyW+d, row, pick(d < 2, "∙", "·"), dust)
		c.text(bx-2-d, row-1, "·", dust)
		c.text(bx+boomboxBodyW+1+d, row-1, "·", dust)
	}

	// Handle: a flat bar on two posts.
	handle := mixHex(text, faint, 0.45)
	for x := bx + 7; x < bx+boomboxBodyW-7; x++ {
		c.pixel(x, by-4, handle)
	}
	for _, x := range []int{bx + 7, bx + boomboxBodyW - 8} {
		for y := by - 3; y < by; y++ {
			c.pixel(x, y, handle)
		}
	}

	// Antenna: a straight rake to the right, with a plain metal tip.
	ax := bx + boomboxBodyW - 5
	for i := 1; i <= 6; i++ {
		c.pixel(ax+i/2, by-i, faint)
	}
	c.pixel(ax+3, by-7, handle)

	// Body: a charcoal slab with a lit top lip, an accent pinstripe and a
	// darker base.
	for y := range boomboxBodyH {
		for x := range boomboxBodyW {
			if (x == 0 || x == boomboxBodyW-1) && (y == 0 || y == boomboxBodyH-1) {
				continue
			}
			col := boomboxChassis
			switch {
			case y == 0:
				col = boomboxLip
			case y == boomboxBodyH-3:
				col = mixHex(accent, boomboxChassis, 0.25)
			case y >= boomboxBodyH-2:
				col = boomboxBase
			}
			c.pixel(bx+x, by+y, col)
		}
	}
	// Feet.
	for _, x := range []int{2, 3, boomboxBodyW - 4, boomboxBodyW - 3} {
		c.pixel(bx+x, by+boomboxBodyH, dark)
	}

	// Transport keys along the top: play goes down and lights up once the
	// tape runs.
	for i, x := range []int{12, 15, 18} {
		down := i == 1 && playing
		col := mixHex(text, boomboxChassis, 0.55)
		if down {
			col = accent
		}
		c.pixel(bx+x, by-1, col)
		c.pixel(bx+x+1, by-1, col)
		if !down {
			c.pixel(bx+x, by-2, col)
			c.pixel(bx+x+1, by-2, col)
		}
	}

	// Woofers: a dark well, a grey surround, and an accent cone whose
	// middle swells and brightens on the beat.
	for _, sx := range []int{1, boomboxBodyW - 9} {
		cx, cy := float64(bx+sx)+3.5, float64(by)+6.5
		cone := 2.3 + 0.7*pump
		coneCol := mixHex(mixHex(accent, dark, 0.35), accent, pump)
		capCol := mixHex(accent, text, 0.5*pump)
		for y := by + 2; y < by+11; y++ {
			for x := bx + sx; x < bx+sx+8; x++ {
				d := math.Hypot(float64(x)-cx, float64(y)-cy)
				switch {
				case d < 0.9+0.5*pump:
					c.pixel(x, y, capCol)
				case d < cone:
					c.pixel(x, y, coneCol)
				case d < 3.4:
					c.pixel(x, y, boomboxRim)
				case d < 4.4:
					c.pixel(x, y, dark)
				}
			}
		}
	}

	// Cassette window: a hole in the body with the tape's frame around it,
	// a pixel of room above and below, and SPTUI scrolling past inside.
	wx, wy := bx+boomboxWinX, by+boomboxWinY
	for x := wx - 1; x <= wx+boomboxWinW; x++ {
		for y := wy - 2; y <= wy+5; y++ {
			c.pixel(x, y, "")
		}
		c.pixel(x, wy-2, dark)
		c.pixel(x, wy+5, dark)
	}
	for y := wy - 2; y <= wy+5; y++ {
		c.pixel(wx-1, y, dark)
		c.pixel(wx+boomboxWinW, y, dark)
	}
	const gap = boomboxWinW // one word in the window at a time
	tape := [2][]rune{[]rune(sptui[0] + strings.Repeat(" ", gap)), []rune(sptui[1] + strings.Repeat(" ", gap))}
	n := len(tape[0])
	off := n - boomboxWinW + 1 // starts with the letters just coming in
	if playing {
		off += int((t - boomboxPlay) / boomboxTape)
	}
	for i := range boomboxWinW {
		k := (off + i) % n
		f := min(float64(k)/float64(n-gap-1), 1)
		col := mixHex(accent, purple, f)
		if !playing {
			col = mixHex(col, faint, 0.5)
		}
		for row := range 2 {
			c.text(wx+i, wy/2+row, string(tape[row][k]), col)
		}
	}

	// Level meter under the window: a row of LEDs that jumps with the beat
	// and falls back, lit from the left.
	for i := range boomboxWinW {
		col := mixHex(faint, boomboxChassis, 0.4)
		if playing && float64(i) < pump*float64(boomboxWinW)*0.95+1 {
			col = mixHex(accent, purple, float64(i)/float64(boomboxWinW-1))
		}
		c.pixel(wx+i, wy+7, col)
	}

	// Pressure rings roll off each side on every beat and fade out.
	if playing {
		arcs := [2][3]string{{"╭", "│", "╰"}, {"╮", "│", "╯"}}
		for k := 0; ; k++ {
			born := boomboxPlay + float64(k)*boomboxBeat
			if born > t || born >= boomboxFade {
				break
			}
			age := t - born
			if age >= 480 {
				continue
			}
			d := int(age / 80)
			row := (by + 6) / 2
			col := mixHex(accent, bg, 0.2+0.8*age/480)
			for r := range 3 {
				c.text(bx-1-d, row-1+r, arcs[0][r], col)
				c.text(bx+boomboxBodyW+d, row-1+r, arcs[1][r], col)
			}
		}
	}

	// A plain hello under the box, if we know who's listening.
	g, n := p.greeting(t, boomboxType)
	c.text((boomboxW-n)/2, boomboxHello, g, muted)

	return c
}
