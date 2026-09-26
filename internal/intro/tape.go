package intro

import (
	"math"

	"github.com/kyledickey/sptui/internal/banner"
)

// tape: the front panel of a reel-to-reel deck powers on. The meter lamp
// warms up and the counter's wheels roll back to 0000. Play goes down, the
// reels spin up and the counter starts ticking. The VU needle swings up,
// overshoots into the red and settles, bouncing with the beat. SPTUI lights
// up in the display, and it all fades into the app.

const (
	tapeW = 57 // scene size, in cells
	tapeH = 16

	// Timeline, in milliseconds from the start.
	tapeAppear = 200  // the panel fades up out of the background
	tapeLamp   = 120  // the meter lamp starts warming up
	tapeWarm   = 450  // time for the lamp to reach full glow
	tapeReset  = 80   // the counter's wheels start rolling back to zero
	tapeRoll   = 420  // time for a wheel to reach zero
	tapeStag   = 50   // between wheels
	tapePlay   = 420  // play goes down and the motors start
	tapeRamp   = 650  // time to reach full speed
	tapeSwing  = 520  // the needle starts to swing up
	tapeBeat   = 300  // one beat, once the needle has settled
	tapeShow   = 900  // the display's first letter lights
	tapeLetter = 70   // between letters
	tapeFlick  = 140  // how long each letter flickers
	tapeType   = 1350 // the greeting starts typing
	tapeFade   = 2150 // everything fades out
	tapeEnd    = 2500

	tapeRev   = 820.0 // ms per turn of the full supply reel at speed
	tapeCount = 7.0   // counter ticks per turn of the supply reel

	// Geometry, in pixels (two per cell, stacked).
	tapeReelL, tapeReelR = 10.0, 46.0 // reel centres
	tapeReelY            = 10.0
	tapeReelOut          = 8.6 // outer rim
	tapeReelIn           = 7.6 // inside of the rim
	tapeHub              = 2.9
	tapeSpindle          = 1.2
	tapePackS            = 6.8 // tape wound on the supply reel
	tapePackT            = 4.4 // and on the take-up reel

	tapeMeterX0, tapeMeterX1 = 20, 36 // meter bezel, inclusive
	tapeMeterY0, tapeMeterY1 = 1, 12
	tapePivotX, tapePivotY   = 28.0, 14.5 // needle pivot, hidden below the face
	tapeScale                = 9.0        // radius of the scale
	tapeSweep                = 0.78       // needle travel either side of centre, in radians

	tapeCountX, tapeCountY = 21, 15 // top left of the counter's first digit
	tapeTapeY              = 20     // where the tape runs between the guides
	tapeStrip              = 22     // top of the control strip
	tapeWinY               = 24     // top of the display's letters; even, so they line up with cells
	tapeHello              = 15     // row of the greeting

	// Neutral darks for the hardware, so the theme colours do the talking.
	tapePlate   = "#25282e" // deck plate behind the reels
	tapeChassis = "#2b2e35" // control strip
	tapeLip     = "#3a3e47"
	tapeWell    = "#1b1d22" // behind the reels' windows
	tapeReelCol = "#5d626d" // reel flanges
	tapeRimCol  = "#7b808b"
	tapeHubCol  = "#9ea3ad"
	tapeWound   = "#131418" // tape on the reels
	tapeDigit   = "#d4d7dd" // counter numerals
	tapeRed     = "#b5524f" // the meter's red zone
	tapeNeedle  = "#eceef1"
)

// tapeStale is what the counter shows before it's reset.
var tapeStale = [4]float64{0, 4, 8, 7}

// tapeScene draws the tape deck intro as it is t milliseconds in.
func (p *Play) tapeScene(t float64) *canvas {
	c := newCanvas(tapeW, tapeH)
	accent, purple, _, muted, faint, bg := p.colors()

	// The whole panel fades up out of the background at the start.
	appear := smoothstep(t / tapeAppear)
	px := func(x, y int, hex string) { c.pixel(x, y, mixHex(hex, bg, 1-appear)) }
	playing := t >= tapePlay

	lamp := smoothstep((t - tapeLamp) / tapeWarm)

	// Panel: a deck plate for the reels and meter, and a control strip
	// below with a lit top lip.
	for y := 0; y < 30; y++ {
		for x := range tapeW {
			if (x == 0 || x == tapeW-1) && (y == 0 || y == 29) {
				continue
			}
			col := tapePlate
			switch {
			case y == 0:
				col = tapeLip
			case y == tapeStrip:
				col = mixHex(tapeLip, accent, 0.75*lamp) // lights with the meter
			case y > tapeStrip:
				col = tapeChassis
			}
			px(x, y, col)
		}
	}

	// Reels: each spins up from rest with a smooth motor ramp. The take-up
	// reel has less tape on it, so it turns faster for the same tape speed.
	turns := 0.0 // supply reel turns since play
	if playing {
		s := t - tapePlay
		if s < tapeRamp {
			u := s / tapeRamp
			turns = tapeRamp * (u*u*u - u*u*u*u/2) / tapeRev
		} else {
			turns = (tapeRamp/2 + s - tapeRamp) / tapeRev
		}
	}
	tapeReel(px, tapeReelL, tapePackS, turns*2*math.Pi)
	tapeReel(px, tapeReelR, tapePackT, turns*2*math.Pi*tapePackS/tapePackT)

	// Tape path: off the supply reel, round a guide, across under the
	// counter, round the other guide and onto the take-up reel.
	gl, gr := 19, 37
	line(px, tapeReelL+tapePackS*0.6, tapeReelY+tapePackS*0.8, float64(gl), tapeTapeY, tapeWound)
	line(px, float64(gr), tapeTapeY, tapeReelR-tapePackT*0.6, tapeReelY+tapePackT*0.8, tapeWound)
	for x := gl; x <= gr; x++ {
		px(x, tapeTapeY, tapeWound)
	}
	for _, g := range []int{gl, gr} {
		px(g, tapeTapeY-1, tapeHubCol)
		px(g, tapeTapeY, tapeRimCol)
	}
	// The heads, a small block the tape runs across, lit while playing.
	for x := 26; x <= 30; x++ {
		px(x, tapeTapeY+1, tapeRimCol)
	}
	px(28, tapeTapeY+1, pick(playing, accent, tapeHubCol))

	// Meter: a lamp-lit face with its scale, the red zone at the top end,
	// and a needle on a hidden pivot.
	face := mixHex(tapeWell, mixHex(accent, tapeWell, 0.72), lamp)
	for y := tapeMeterY0; y <= tapeMeterY1; y++ {
		for x := tapeMeterX0; x <= tapeMeterX1; x++ {
			edge := x == tapeMeterX0 || x == tapeMeterX1 || y == tapeMeterY0 || y == tapeMeterY1
			px(x, y, pick(edge, tapeWell, face))
		}
	}
	const red = 0.7 // where the red zone starts, as a needle level
	for a := -tapeSweep; a <= tapeSweep+0.01; a += 0.04 {
		level := (a + tapeSweep) / (2 * tapeSweep)
		col := mixHex(face, pick(level >= red, tapeRed, accent), 0.25+0.75*lamp)
		x, y := tapePolar(tapeScale, a)
		px(x, y, col)
	}
	for i := range 5 { // ticks, hanging under the scale
		level := float64(i) / 4
		a := -tapeSweep + level*2*tapeSweep
		col := mixHex(face, pick(level >= red, tapeRed, accent), 0.25+0.75*lamp)
		x, y := tapePolar(tapeScale-1, a)
		px(x, y, col)
	}
	level := tapeLevel(t)
	a := -tapeSweep + math.Min(level, 1.06)*2*tapeSweep
	for r := 3.0; r <= tapeScale+1.6; r += 0.3 {
		x, y := tapePolar(r, a)
		if x > tapeMeterX0 && x < tapeMeterX1 && y > tapeMeterY0 && y < tapeMeterY1 {
			px(x, y, tapeNeedle)
		}
	}
	for x := int(tapePivotX) - 1; x <= int(tapePivotX)+1; x++ {
		px(x, tapeMeterY1-1, tapeRimCol) // the pivot's cover
	}

	// Counter: four number wheels behind a window. They roll back from a
	// stale reading to 0000, then count up as the tape runs.
	count := turns * tapeCount
	for y := tapeCountY - 1; y <= tapeCountY+4; y++ {
		for x := tapeCountX - 1; x <= tapeCountX+15; x++ {
			px(x, y, tapeWell)
		}
	}
	for i := range 4 {
		place := math.Pow(10, float64(3-i))
		var pos float64
		if playing {
			// Like an odometer: a wheel turns only while the ones below it
			// roll over from 9.
			whole := math.Floor(count / place)
			lower := math.Mod(count, place)
			pos = whole + math.Max(0, lower-(place-1))
		} else if d := tapeStale[i]; d > 0 {
			u := easeOut((t-tapeReset-float64(i)*tapeStag)/tapeRoll, 3)
			pos = d + (10-d)*u
		}
		tapeWheel(px, tapeCountX+4*i, tapeCountY, pos, tapeDigit)
	}

	// Transport keys either side of the display. Play goes down and
	// lights; a power lamp on the right glows with the meter.
	for i, kx := range []int{3, 8, 13, 40, 45} {
		down := i == 1 && playing
		col := pick(down, accent, mixHex(tapeHubCol, tapeChassis, 0.45))
		top := pick(down, 25, 24)
		for y := top; y <= 27; y++ {
			for x := kx; x < kx+4; x++ {
				px(x, y, col)
			}
		}
		px(kx, 28, "#1f2126")
		px(kx+3, 28, "#1f2126")
	}
	for x := 51; x <= 52; x++ {
		px(x, 25, mixHex(tapeWell, accent, lamp))
		px(x, 26, mixHex(tapeWell, accent, lamp))
	}

	// Display: a window through the panel. The letters sit there unlit,
	// then flicker on one by one.
	wx := (tapeW - sptuiW) / 2
	for x := wx - 2; x <= wx+sptuiW+1; x++ {
		for y := tapeWinY - 1; y <= tapeWinY+4; y++ {
			c.pixel(x, y, "")
		}
		px(x, tapeWinY-1, tapeWell)
		px(x, tapeWinY+4, tapeWell)
	}
	for y := tapeWinY - 1; y <= tapeWinY+4; y++ {
		px(wx-2, y, tapeWell)
		px(wx+sptuiW+1, y, tapeWell)
	}
	ghost := mixHex(faint, bg, 0.55+0.45*(1-appear))
	for i := range 5 {
		col := ghost
		if age := t - (tapeShow + float64(i)*tapeLetter); age >= 0 {
			lit := mixHex(accent, purple, float64(i)/4)
			col = lit
			if age < tapeFlick && int(age/35)%2 == 1 {
				col = mixHex(lit, bg, 0.6) // flickering on
			}
		}
		c.sptuiLetter(wx, tapeWinY/2, i, col)
	}

	// A plain hello under the deck, if we know who's listening.
	g, n := p.greeting(t, tapeType)
	c.text((tapeW-n)/2, tapeHello, g, muted)

	return c
}

// tapeReel draws a metal reel centred at cx, with tape wound to radius
// pack, turned by angle radians. Three windows in the flange show the tape.
func tapeReel(px func(x, y int, hex string), cx, pack, angle float64) {
	for y := int(tapeReelY) - 9; y <= int(tapeReelY)+9; y++ {
		for x := int(cx - tapeReelOut); x <= int(cx+tapeReelOut)+1; x++ {
			dx, dy := float64(x)-cx, float64(y)-tapeReelY
			d := math.Hypot(dx, dy)
			if d > tapeReelOut {
				continue
			}
			spoke := math.Mod(math.Atan2(dy, dx)-angle+8*math.Pi, 2*math.Pi/3) / (2 * math.Pi / 3)
			col := tapeReelCol
			switch {
			case d < tapeSpindle:
				col = tapeWell
			case d < tapeHub:
				col = tapeHubCol
			case d >= tapeReelIn:
				col = tapeRimCol
			case d > tapeHub+0.8 && spoke < 0.62:
				col = pick(d < pack, tapeWound, tapeWell)
			}
			px(x, y, col)
		}
	}
}

// tapeWheel draws one counter wheel at (x, y), turned to pos: 3.5 is
// halfway from 3 to 4. Numerals are 4 pixels tall with a gap between.
func tapeWheel(px func(x, y int, hex string), x, y int, pos float64, hex string) {
	pos = math.Mod(pos, 10)
	d := int(pos)
	off := int(math.Round((pos - float64(d)) * 5))
	for k := range 2 {
		glyph := banner.Font[rune('0'+(d+k)%10)]
		for row := range 4 {
			gy := row + 5*k - off
			if gy < 0 || gy > 3 {
				continue
			}
			for col := range len(glyph[row]) {
				if glyph[row][col] == 'X' {
					px(x+col, y+gy, hex)
				}
			}
		}
	}
}

// tapeLevel is where the VU needle sits at t, from 0 (rest) to 1 (pinned).
// It swings up, overshoots into the red, rings down, and bounces to the beat.
func tapeLevel(t float64) float64 {
	s := t - tapeSwing
	if s <= 0 {
		return 0
	}
	const rest = 0.58
	level := rest * (1 - math.Exp(-s/430)*math.Cos(math.Pi*s/230))
	// Beats fade in as the swing dies down.
	in := smoothstep((s - 300) / 400)
	beat := math.Mod(s, tapeBeat) / tapeBeat
	n := uint32(s / tapeBeat)
	h := n*2654435761 ^ 0x5bd1e995
	h ^= h >> 15
	hit := 0.14 + 0.12*float64(h%5)/4
	pulse := math.Min(beat/0.12, 1) * math.Exp(-4*beat)
	return math.Max(0, level+in*(hit*pulse-0.06))
}

// tapePolar is the pixel r pixels from the needle's pivot, at angle a
// from straight up.
func tapePolar(r, a float64) (int, int) {
	return int(math.Round(tapePivotX + r*math.Sin(a))), int(math.Round(tapePivotY - r*math.Cos(a)))
}
