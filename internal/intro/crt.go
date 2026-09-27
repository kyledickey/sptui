package intro

import (
	"math"
)

// crt: an old monitor switching on. A bright dot appears, stretches into
// a line, and the line opens out to fill the screen with a white bloom.
// The bloom cools into scanlines, sptui flickers on with a phosphor glow,
// a band of interference rolls down the glass, and it all fades away.

const (
	crtW = 48 // scene size, in cells
	crtH = 14

	crtPxW   = crtW   // the screen, in pixels
	crtPxH   = 2 * 12 // twelve rows of cells
	crtHello = 13     // row of the greeting, under the screen
	crtWordX = 5      // the banner's top-left pixel on the screen
	crtWordY = 8

	// Timeline, in milliseconds from the start.
	crtDot     = 180  // the dot is lit; it starts stretching
	crtLine    = 460  // the line spans the screen; it starts opening
	crtOpen    = 720  // the screen is full of white
	crtCool    = 1060 // the bloom has cooled into scanlines
	crtWord    = 940  // the banner flickers on
	crtRoll    = 1180 // interference starts rolling down
	crtRollFor = 620  // top to bottom
	crtType    = 1450 // the greeting starts typing
	crtFade    = 2150 // everything fades out
	crtEnd     = 2550
)

// crtFlicker is how bright the banner is as it strikes, in steps of
// 30ms from crtWord: a stutter of on and off before it holds.
var crtFlicker = []float64{1, 0, 0.8, 1, 0.15, 0, 0.9, 0.6, 1}

// crtScene draws the crt intro as it is t milliseconds in.
func (p *Play) crtScene(t float64) *canvas {
	c := newCanvas(crtW, crtH)
	accent, purple, _, muted, _, bg := p.colors()
	// The glass is dark whatever the theme: a lifted background on dark
	// terminals, a neutral near-black on light ones. White is phosphor
	// white, a touch warm.
	glass := pick(p.Env.Dark, mixHex(bg, "#ffffff", 0.045), "#15171b")
	white := "#f4f3ee"
	scan := mixHex(glass, "#000000", 0.3) // the dark line between scanlines

	cx, cy := float64(crtPxW-1)/2, float64(crtPxH-1)/2

	// hot is how brightly each pixel is lit by the switch-on, 0–1, before
	// the bloom cools. glow is the accent halo around the lit shape.
	hot := func(x, y int) (lit, glow float64) {
		dx, dy := math.Abs(float64(x)-cx), math.Abs(float64(y)-cy)
		switch {
		case t < crtDot:
			// A dot swells in the middle.
			r := 0.5 + 1.2*easeOut(t/crtDot, 3)
			d := math.Hypot(dx, dy*1.4)
			return crtFall(d, r), crtFall(d, r+2.2) * 0.8
		case t < crtLine:
			// It stretches sideways, brightest in the middle.
			f := easeOut((t-crtDot)/(crtLine-crtDot), 3)
			hw := 1.5 + (cx+1-1.5)*f
			along := crtFall(dx, hw) * (1 - 0.3*dx/(cx+1))
			return along * crtFall(dy, 1), crtFall(dx, hw+1) * crtFall(dy, 1.8)
		default:
			// It opens out vertically to fill the screen.
			f := easeOut((t-crtLine)/(crtOpen-crtLine), 4)
			hh := 0.5 + (cy+1-0.5)*f
			return crtFall(dy, hh), crtFall(dy, hh+2) * (1 - f)
		}
	}

	// The rolling band: where its leading edge is, in pixel rows.
	roll := -100.0
	if p := (t - crtRoll) / crtRollFor; p >= 0 && p <= 1 {
		roll = -4 + (crtPxH+8)*p
	}
	inBand := func(y int) float64 {
		d := roll - float64(y)
		if d < 0 || d > 5 {
			return 0
		}
		return 1 - d/5 // bright at the leading edge, trailing off
	}

	// Scanlines show once the tube has warmed up.
	lines := easeOut((t-crtOpen)/(crtCool-crtOpen), 2)
	row := func(y int) string { return mixHex(glass, scan, lines*float64(y%2)) }

	// The screen, pixel by pixel.
	for y := range crtPxH {
		for x := range crtPxW {
			if crtCorner(x, y) {
				continue
			}
			base := row(y)
			band := inBand(y)
			if band > 0 {
				base = mixHex(base, accent, 0.10*band)
			}
			col := base
			switch {
			case t < crtOpen:
				lit, glow := hot(x, y)
				col = mixHex(mixHex(col, accent, glow*0.55), white, lit)
			case t < crtCool:
				// The bloom cools from the edges in, and the scanlines
				// show through first.
				d := math.Hypot((float64(x)-cx)/cx, (float64(y)-cy)/cy) / math.Sqrt2
				f := easeOut((t-crtOpen)/(crtCool-crtOpen)+0.35*d-0.1, 2)
				if y%2 == 1 {
					f = min(f*1.4, 1)
				}
				warm := mixHex(white, accent, min(f*1.6, 1)*0.6)
				col = mixHex(warm, base, f)
			}
			c.pixel(x, y, col)
		}
	}

	// The banner, doubled up so each font pixel is 2×2, and struck with
	// a flicker. The lower row of each doubled pixel is dimmer, like a
	// scanline, and the glow around it is phosphor bleeding into the glass.
	if t >= crtWord {
		step := int((t - crtWord) / 30)
		power := 1.0
		if step < len(crtFlicker) {
			power = crtFlicker[step]
		}
		power *= 1 - 0.06*math.Max(0, math.Sin(t/37)) // a faint hum
		age := t - crtWord
		font, w := sptuiPixels(2, 2, 2)
		lit := make(map[[2]int]bool, len(font))
		for p := range font {
			lit[[2]int{crtWordX + p[0], crtWordY + p[1]}] = true
		}
		shift := func(y int) int {
			if inBand(y) > 0.3 {
				return 2 // the band tears the rows it passes over
			}
			return 0
		}
		// Glow first, so the letters draw over it.
		for y := crtWordY - 2; y < crtWordY+8+2; y++ {
			for x := crtWordX - 2; x < crtWordX+w+2; x++ {
				if lit[[2]int{x, y}] {
					continue
				}
				near := 0.0
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						if lit[[2]int{x + dx, y + dy}] {
							near = max(near, pick(max(dx, -dx, dy, -dy) == 1, 0.2, 0.06))
						}
					}
				}
				if near > 0 {
					c.pixel(x+shift(y), y, mixHex(row(y), accent, near*power))
				}
			}
		}
		for p := range lit {
			x, y := p[0], p[1]
			f := float64(x-crtWordX) / float64(w-1)
			col := mixHex(accent, purple, f)
			col = mixHex(white, col, min(age/420, 1)) // strikes white-hot
			if y%2 == 1 {
				col = mixHex(col, glass, 0.22)
			}
			c.pixel(x+shift(y), y, mixHex(row(y), col, power))
		}
	}

	// A plain greeting types itself out under the screen, if we know a name.
	g, n := p.greeting(t, crtType)
	c.text((crtW-n)/2, crtHello, g, muted)

	return c
}

// crtFall is 1 within r of the middle, softening to 0 over the next pixel.
func crtFall(d, r float64) float64 {
	return min(max(r+0.5-d, 0), 1)
}

// crtCorner reports whether (x, y) is cut off by the screen's rounded
// corners.
func crtCorner(x, y int) bool {
	dx := min(x, crtPxW-1-x)
	dy := min(y, crtPxH-1-y)
	return dx+dy < 2
}
