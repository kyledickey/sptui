package intro

import (
	"math"
)

// matrix: digital rain. Columns of flickering glyphs, music symbols among
// them, fall at their own pace. As heads pass over the cells that spell
// SPTUI those cells lock, frozen and bright, while the rain thins and
// drains away. The locked cells then harden into solid blocks in the
// accent-to-artist gradient, a glint crosses them, and it all fades.

const (
	matrixW = 57 // scene size, in cells
	matrixH = 14

	// Timeline, in milliseconds from the start.
	matrixStart  = 380  // columns start falling within this
	matrixLock   = 620  // heads start locking the banner's cells
	matrixWrite  = 600  // the last drop down each banner column starts
	matrixWrites = 260  // spread of those starts
	matrixThin   = 900  // columns stop starting drops from here
	matrixThins  = 300  // spread of when each column stops
	matrixDrain  = 1100 // the leftover rain dims away
	matrixDrains = 450  // time to dim away
	matrixSettle = 1250 // the locked cells start hardening into blocks
	matrixSweep  = 300  // left to right across the banner
	matrixGlint  = 1640 // a glint crosses the finished banner
	matrixGlints = 300  // time to cross
	matrixType   = 1500 // the greeting starts typing
	matrixFade   = 2050 // everything fades out
	matrixEnd    = 2400

	matrixTick = 70 // ms between tail glyph flickers

	// The banner: each font pixel is a block of cells, letters spaced by
	// a gap, with its top row here.
	matrixPxW   = 3
	matrixPxH   = 2
	matrixGap   = 2
	matrixTop   = 3
	matrixHello = 12 // row of the greeting
)

// matrixGlyphs is what the rain is made of.
var matrixGlyphs = []rune("♪♫♩♬♭♯♪♫0123456789ABCDEFHKLMNPRSTUVXZ▖▗▘▝▚▞▌▐▀▄░▒")

// matrixHash scatters a, b and c to a number in [0, 1).
func matrixHash(a, b, c int) float64 {
	n := uint32(a)*73856093 ^ uint32(b)*19349663 ^ uint32(c)*83492791
	n = n*2654435761 ^ 0x9e3779b9
	n ^= n >> 13
	n *= 0x5bd1e995
	n ^= n >> 15
	return float64(n%10007) / 10007
}

// matrixDrop is one falling streak: its head is at row (t-start)*speed.
type matrixDrop struct {
	start, speed float64
	length       int
}

// matrixDrops are the drops down column x, in the order they start. A
// column in the banner ends with a quick one that's sure to lock it.
func matrixDrops(x int, banner bool) []matrixDrop {
	var drops []matrixDrop
	stop := matrixThin + matrixHash(x, 1, 0)*matrixThins
	s := matrixHash(x, 2, 0) * matrixStart
	for k := 0; s < stop; k++ {
		d := matrixDrop{
			start:  s,
			speed:  0.022 + 0.026*matrixHash(x, k, 3),
			length: 4 + int(matrixHash(x, k, 4)*8),
		}
		if matrixHash(x, k, 5) > 0.35 { // now and then a column skips a beat
			drops = append(drops, d)
		}
		s += float64(d.length)/d.speed*0.8 + 160 + matrixHash(x, k, 6)*420
	}
	if banner {
		drops = append(drops, matrixDrop{
			start:  matrixWrite + matrixHash(x, 7, 0)*matrixWrites,
			speed:  0.034 + 0.016*matrixHash(x, 8, 0),
			length: 5 + int(matrixHash(x, 9, 0)*5),
		})
	}
	return drops
}

// matrixScene draws the matrix intro as it is t milliseconds in.
func (p *Play) matrixScene(t float64) *canvas {
	c := newCanvas(matrixW, matrixH)
	accent, purple, text, muted, faint, bg := p.colors()

	logo, logoW := sptuiPixels(matrixPxW, matrixPxH, matrixGap)
	logoX := (matrixW - logoW) / 2
	inLogo := func(x, y int) bool { return logo[[2]int{x - logoX, y - matrixTop}] }

	glyph := func(x, y int, t float64) string {
		frame := int((t + matrixHash(x, y, 11)*matrixTick) / matrixTick)
		return string(matrixGlyphs[int(matrixHash(x, y, frame)*float64(len(matrixGlyphs)))])
	}

	// Once the rain starts draining, everything but the locked cells dims
	// towards the background, easing out.
	drain := easeOut((t-matrixDrain)/matrixDrains, 2)

	for x := range matrixW {
		banner := false
		for y := matrixTop; y < matrixTop+4*matrixPxH; y++ {
			banner = banner || inLogo(x, y)
		}
		drops := matrixDrops(x, banner)

		// The rain: a bright head, a tail that cools through the accent
		// into faint, glyphs flickering as it falls.
		for _, d := range drops {
			if t < d.start {
				continue
			}
			head := int((t - d.start) * d.speed)
			for i := range d.length {
				y := head - i
				if y < 0 || y >= matrixH {
					continue
				}
				var col string
				switch i {
				case 0:
					col = text
				case 1:
					col = mixHex(text, accent, 0.55)
				default:
					col = mixHex(accent, faint, float64(i-1)/float64(d.length-1))
				}
				g := glyph(x, y, t)
				if i == 0 {
					g = glyph(x, y, t*2.5) // heads churn faster
				}
				c.text(x, y, g, mixHex(col, bg, drain))
			}
		}

		// Banner cells lock the moment a head passes them after the lock
		// starts, keeping the glyph they had.
		for y := matrixTop; y < matrixTop+4*matrixPxH; y++ {
			if !inLogo(x, y) {
				continue
			}
			lock := math.Inf(1)
			for _, d := range drops {
				if at := d.start + float64(y)/d.speed; at >= matrixLock {
					lock = min(lock, at)
				}
			}
			if t < lock {
				continue
			}
			f := float64(x-logoX) / float64(logoW-1)
			grad := mixHex(accent, purple, f)
			settle := matrixSettle + f*matrixSweep + matrixHash(x, y, 21)*60
			age := t - settle
			switch {
			case age < 0:
				// Frozen and bright, cooling a touch once the flash passes.
				col := mixHex(text, grad, min((t-lock)/400, 1)*0.35)
				c.text(x, y, glyph(x, y, lock), col)
			case age < 50:
				c.text(x, y, "▒", text)
			case age < 100:
				c.text(x, y, "▓", text)
			default:
				col := mixHex(text, grad, min((age-100)/200, 1))
				// The glint: a soft band of light crossing left to right.
				if g := t - matrixGlint; g >= 0 && g < matrixGlints+200 {
					d := math.Abs(float64(x-logoX) - (g/matrixGlints)*float64(logoW+8) + 4)
					col = mixHex(col, text, max(0, 1-d/4)*0.45)
				}
				c.text(x, y, "█", col)
			}
		}
	}

	// A plain greeting types itself out underneath, if we know a name,
	// on a row cleared so leftover rain doesn't mingle with it.
	g, n := p.greeting(t, matrixType)
	x := (matrixW - n) / 2
	for i := range n {
		if x+i >= 0 && x+i < matrixW {
			c.cells[matrixHello][x+i] = canvasCell{}
		}
	}
	c.text(x, matrixHello, g, muted)

	return c
}
