package intro

import (
	"math"
)

// vinyl: a record drops onto a chunky turntable and bounces as it lands.
// The platter spins up, the tonearm swings over and the needle touches
// down with a brief spark. SPTUI lights up under the deck letter by
// letter, notes drift off it, a plain greeting types out, and it all fades
// into the app.

const (
	vinylW = 38 // scene size, in cells
	vinylH = 16

	// Timeline, in milliseconds from the start.
	vinylLand   = 420  // the record hits the platter
	vinylSpin   = 500  // the motor starts
	vinylRamp   = 450  // time to reach full speed
	vinylSwing  = 700  // the tonearm starts swinging over
	vinylOver   = 1080 // the arm is over the groove
	vinylDrop   = 1220 // the needle touches down
	vinylReveal = 1300 // the first letter under the deck lights up
	vinylLetter = 70   // between letters
	vinylType   = 1500 // the greeting starts typing
	vinylFade   = 2150 // everything fades out
	vinylEnd    = 2500

	vinylRev  = 700.0 // ms per turn at full speed
	vinylFall = 26.0  // pixels the record falls

	// Geometry, in pixels (two per cell, stacked).
	vinylCX     = 14.0
	vinylCY     = 12.5
	vinylRecX   = 10.2 // record
	vinylRecY   = 5.6
	vinylPlatX  = 11.6 // platter, around the record
	vinylPlatY  = 6.6
	vinylLabelX = 4.7
	vinylLabelY = 2.1

	vinylPivotX, vinylPivotY   = 31.0, 7.0 // tonearm pivot
	vinylArmLen                = 11.0
	vinylGrooveX, vinylGrooveY = 22.5, 14.0 // where the needle lands

	vinylWord  = 12 // top row of SPTUI, under the deck
	vinylHello = 15 // row of the greeting

	vinylBlack = "#1a1b21"
	vinylSpark = "#fff6c2"
)

// vinylScene draws the vinyl intro as it is t milliseconds in.
func (p *Play) vinylScene(t float64) *canvas {
	c := newCanvas(vinylW, vinylH)
	accent, purple, text, muted, faint, bg := p.colors()

	deck := mixHex(mixHex(accent, bg, 0.78), faint, 0.15)
	front := mixHex(deck, "#000000", 0.35)
	mat := mixHex(faint, bg, 0.35)

	// The deck: a chunky slab with a darker front edge and little feet.
	for y := 4; y <= 21; y++ {
		for x := 1; x <= 36; x++ {
			corner := (x == 1 || x == 36) && (y == 4 || y == 21)
			if !corner {
				c.pixel(x, y, pick(y >= 20, front, deck))
			}
		}
	}
	for _, x := range []int{3, 4, 5, 32, 33, 34} {
		c.pixel(x, 22, front)
	}

	// Speed buttons and a power light, which comes on with the motor.
	spinning := t >= vinylSpin
	c.pixel(3, 18, pick(spinning, accent, muted))
	c.pixel(4, 18, pick(spinning, accent, muted))
	c.pixel(6, 18, muted)
	c.pixel(7, 18, muted)
	c.pixel(34, 18, pick(spinning, accent, faint))

	// Pitch slider down the right edge.
	for y := 10; y <= 16; y++ {
		c.pixel(35, y, front)
	}
	c.pixel(35, 13, muted)

	theta := vinylAngle(t)

	// Platter, with strobe dots round its rim that march as it turns.
	for y := 5; y <= 20; y++ {
		for x := 1; x <= 27; x++ {
			u, v := (float64(x)-vinylCX)/vinylPlatX, (float64(y)-vinylCY)/vinylPlatY
			r := math.Hypot(u, v)
			if r > 1 {
				continue
			}
			col := mat
			if r > 0.86 {
				dot := int(math.Floor((math.Atan2(v, u)-theta)/(2*math.Pi)*24+100)) % 2
				col = pick(dot == 0, mixHex(muted, deck, 0.2), mixHex(muted, deck, 0.65))
			}
			c.pixel(x, y, col)
		}
	}
	c.pixel(int(vinylCX), 12, muted) // spindle
	c.pixel(int(vinylCX), 13, muted)

	// Record: falls with gravity, bounces once, then spins.
	off := 0.0
	rx, ry := vinylRecX, vinylRecY
	switch {
	case t < vinylLand:
		f := t / vinylLand
		off = -vinylFall * (1 - f*f)
	case t < vinylLand+60:
		rx, ry = rx+0.6, ry-0.5 // squashed on impact
	case t < vinylLand+200:
		off = -math.Round(1.4 * math.Sin(math.Pi*(t-vinylLand-60)/140))
	}
	cy := vinylCY + off
	landed := t >= vinylLand
	for y := int(cy - ry - 1); y <= int(cy+ry+1); y++ {
		for x := int(vinylCX - rx - 1); x <= int(vinylCX+rx+1); x++ {
			u, v := (float64(x)-vinylCX)/rx, (float64(y)-cy)/ry
			r := math.Hypot(u, v)
			if r > 1 {
				continue
			}
			phi := math.Atan2(v, u)
			lu, lv := (float64(x)-vinylCX)/vinylLabelX, (float64(y)-cy)/vinylLabelY
			if math.Hypot(lu, lv) <= 1 {
				// The label: a color wheel that turns with the record.
				c.pixel(x, y, mixHex(accent, purple, (1+math.Cos(math.Atan2(lv, lu)-theta))/2))
				continue
			}
			col := vinylBlack
			if math.Abs(r-0.74) < 0.06 {
				col = mixHex(vinylBlack, text, 0.08) // a groove
			}
			if landed && r > 0.55 && r < 0.93 {
				// Two glints turn with the record.
				s := math.Cos(2 * (phi - theta))
				switch {
				case s > 0.9:
					col = mixHex(vinylBlack, text, 0.35)
				case s > 0.6:
					col = mixHex(vinylBlack, text, 0.14)
				}
			}
			c.pixel(x, y, col)
		}
	}

	// Once the needle's down, SPTUI lights up under the deck, one letter
	// at a time: a flash, then the gradient.
	for i := range 5 {
		if age := t - vinylReveal - float64(i)*vinylLetter; age >= 0 {
			col := mixHex(text, mixHex(accent, purple, float64(i)/4), min(age/250, 1))
			c.sptuiLetter((vinylW-sptuiW)/2, vinylWord, i, col)
		}
	}

	// Tonearm: swings from its rest over the groove, then lowers.
	restA := math.Pi / 2
	playA := math.Atan2(vinylGrooveY-vinylPivotY, vinylGrooveX-vinylPivotX)
	f := smoothstep((t - vinylSwing) / (vinylOver - vinylSwing))
	a := restA + (playA-restA)*f
	dx, dy := math.Cos(a), math.Sin(a)
	tipX, tipY := vinylPivotX+dx*vinylArmLen, vinylPivotY+dy*vinylArmLen
	lift := pick(t < vinylDrop, 1.0, 0)
	c.pixel(31, 18, front) // the arm rest
	c.pixel(31, 17, front)
	for s := 0.0; s <= vinylArmLen; s += 0.5 {
		l := lift * s / vinylArmLen // it tilts up towards the head
		c.pixel(int(math.Round(vinylPivotX+dx*s)), int(math.Round(vinylPivotY+dy*s-l)), text)
	}
	for _, p := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		c.pixel(int(vinylPivotX)+p[0], int(vinylPivotY)+p[1], pick(p[0] != 0 && p[1] != 0, front, muted))
	}
	c.pixel(int(vinylPivotX), int(vinylPivotY), text)
	wx, wy := int(math.Round(vinylPivotX-dx*2.5)), int(math.Round(vinylPivotY-dy*2.5))
	c.pixel(wx, wy, muted) // counterweight
	c.pixel(wx+1, wy, muted)
	c.pixel(wx, wy-1, muted)
	c.pixel(wx+1, wy-1, muted)
	hx, hy := int(math.Round(tipX)), int(math.Round(tipY-lift))
	if lift > 0 && f > 0 {
		c.pixel(int(math.Round(tipX)), int(math.Round(tipY)), mixHex(vinylBlack, "#000000", 0.5)) // shadow
	}
	c.pixel(hx, hy, text) // headshell, with the cartridge in the accent
	c.pixel(hx-1, hy, text)
	c.pixel(hx, hy+1, accent)
	c.pixel(hx-1, hy+1, accent)

	// A brief spark where the needle meets the groove: a flash, then four
	// short rays that fade into the record.
	if age := t - vinylDrop; age >= 0 && age < 200 {
		if age < 70 {
			c.pixel(hx, hy+1, vinylSpark)
		}
		col := mixHex(vinylSpark, vinylBlack, age/200)
		d := 1.0 + age/100
		for _, p := range [][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			c.pixel(hx+int(math.Round(p[0]*d)), hy+1+int(math.Round(p[1]*d)), col)
		}
	}

	// Notes drift up off the deck while it plays.
	for k := 0; ; k++ {
		born := vinylDrop + 120 + float64(k)*170
		if born >= vinylFade {
			break
		}
		age := t - born
		if age < 0 || age >= 700 {
			continue
		}
		x := 17 + (k*7)%15 + int(age/350)
		row := 1 - int(age/350)
		col := []string{accent, purple, text}[k%3]
		c.text(x, row, introNotes[k%len(introNotes)], mixHex(col, faint, age/700))
	}

	// A plain greeting types itself out under the deck, if we know a name.
	g, n := p.greeting(t, vinylType)
	c.text((vinylW-n)/2, vinylHello, g, muted)

	return c
}

// vinylAngle is how far the record has turned by t: it speeds up evenly
// from the motor starting, then holds its speed.
func vinylAngle(t float64) float64 {
	s := t - vinylSpin
	if s <= 0 {
		return 0
	}
	w := 2 * math.Pi / vinylRev
	if s < vinylRamp {
		return w * s * s / (2 * vinylRamp)
	}
	return w * (vinylRamp/2 + s - vinylRamp)
}
