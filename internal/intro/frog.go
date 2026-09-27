package intro

import (
	"math"
)

// frog: a frog drops onto the first bar of an equalizer and hops from bar
// to bar, each one dipping under it as it lands and stamping a letter of
// SPTUI into place beneath. A note drifts in, the frog snatches it with its
// tongue, and a highlight sweeps across the banner.

const (
	frogW = 42 // scene size, in cells
	frogH = 15

	frogBars  = 5
	frogBarW  = 5  // bar width, in cells
	frogGap   = 2  // between bars
	frogFloor = 19 // bottom pixel row of the bars
	frogWord  = 11 // top row of the banner
	frogHello = 14 // row of the greeting

	// Timeline, in milliseconds from the start.
	frogDrop   = 280  // lands on the first bar
	frogHop    = 270  // from one landing to the next
	frogAir    = 170  // time in the air per hop
	frogCrouch = 50   // crouch before leaving
	frogLick   = 1420 // the tongue shoots out
	frogReach  = 90   // tongue out, and back again
	frogGulp   = frogLick + 2*frogReach
	frogType   = 1660 // the greeting starts typing
	frogSweep  = 380  // the highlight crossing the banner
	frogFade   = 2150 // everything fades out
	frogEnd    = 2550

	frogTongue = "#b5474b" // a muted red
)

// Bar heights at rest, in pixels.
var frogHeights = [frogBars]int{5, 8, 6, 9, 4}

// The frog, 8×6 pixels, a flat side-on silhouette facing right: G body,
// S its shaded underside and legs, D the eye. It sits, squashes when landing or
// crouching, and stretches mid-air.
var (
	frogSit = []string{
		".....GG.",
		"....GDGG",
		"..GGGGGG",
		".GGGGGGG",
		"GSSSSGS.",
		"SS..SS..",
	}
	frogSquash = []string{
		"........",
		".....GG.",
		"...GGDGG",
		".GGGGGGG",
		"GGSSSSGS",
		"SSS.SSS.",
	}
	frogLeap = []string{
		"......GG",
		".....GDG",
		"...GGGGG",
		"..GGGGG.",
		".SSSS...",
		"SS......",
		"S.......",
	}
	// An eighth note, 5×6 pixels.
	frogNote = []string{
		"..XX.",
		"..X.X",
		"..X.X",
		"..X..",
		"XXX..",
		"XXX..",
	}
)

// frogLanding is when the frog lands on bar i.
func frogLanding(i int) float64 { return frogDrop + float64(i)*frogHop }

// frogBarX is the left pixel column of bar i.
func frogBarX(i int) int { return 3 + i*(frogBarW+frogGap) }

// frogBarTop is the top pixel row of bar i at t: it sways a little, and
// dips and springs back after the frog lands on it.
func frogBarTop(i int, t float64) float64 {
	h := float64(frogHeights[i]) + 0.8*math.Sin(t/150+float64(i)*1.7)
	if a := t - frogLanding(i); a >= 0 {
		h -= 3.2 * math.Exp(-a/140) * math.Cos(a/60)
	}
	// The gulp sends one last kick through every bar, left to right.
	if a := t - frogGulp - float64(i)*40; a >= 0 {
		h += 2.5 * math.Exp(-a/120) * math.Sin(a/45)
	}
	return float64(frogFloor) + 1 - h
}

// frogScene draws the frog intro as it is t milliseconds in.
func (p *Play) frogScene(t float64) *canvas {
	c := newCanvas(frogW, frogH)

	accent, purple, text, muted, faint, _ := p.colors()
	grad := func(f float64) string { return mixHex(accent, purple, 0.25+0.6*f) }

	// Where the frog is: dropping in, sitting on a bar, or mid-hop.
	sprite := frogSit
	var fx, fy float64 // top-left pixel
	perch := func(i int) (float64, float64) {
		return float64(frogBarX(i) + frogBarW/2 - 4), frogBarTop(i, t) - float64(len(frogSit))
	}
	switch {
	case t < frogDrop:
		fx, fy = perch(0)
		f := t / frogDrop
		fy = -8 + (fy+8)*f*f
	default:
		i := min(int((t-frogDrop)/frogHop), frogBars-1)
		since := t - frogLanding(i)
		leave := float64(frogHop - frogAir)
		fx, fy = perch(i)
		switch {
		case i == frogBars-1:
			if since < 80 || (t >= frogGulp && t < frogGulp+90) {
				sprite = frogSquash // landing, and later the gulp
			}
		case since < 70 || (since >= leave-frogCrouch && since < leave):
			sprite = frogSquash
		case since >= leave:
			// Leap in an arc to the next bar.
			f := (since - leave) / frogAir
			nx, ny := perch(i + 1)
			fx += (nx - fx) * f
			fy += (ny-fy)*f - 6*4*f*(1-f)
			if f < 0.55 {
				sprite = frogLeap // stretched on the way up, tucked coming down
			}
		}
	}
	x0, y0 := int(math.Round(fx)), int(math.Round(fy))
	// Keep the feet planted: sprites differ in height.
	y0 += len(frogSit) - len(sprite)

	// Bars, drawn first so the frog stands in front. The bar just landed
	// on flashes.
	for i := range frogBars {
		top := int(math.Round(frogBarTop(i, t)))
		flash := 0.0
		if a := t - frogLanding(i); a >= 0 {
			flash = math.Exp(-a / 180)
		}
		for y := top; y <= frogFloor; y++ {
			f := float64(y-top) / float64(max(frogFloor-top, 1))
			col := mixHex(grad(float64(i)/(frogBars-1)), faint, f*0.75)
			if y == top {
				col = mixHex(col, text, 0.35)
			}
			col = mixHex(col, text, flash*0.7)
			for x := range frogBarW {
				c.pixel(frogBarX(i)+x, y, col)
			}
		}
	}

	// The note drifts in from the right, bobbing, until the tongue gets it.
	nx, ny := float64(frogW-5), 1.0
	if t < frogLick {
		in := clamp01((t - 600) / (frogLick - 600))
		nx += 6 * (1 - in) * (1 - in)
		ny += 1.2 * math.Sin(t/120)
	}
	mouthX, mouthY := x0+7, y0+len(sprite)-4
	tipX, tipY := float64(mouthX), float64(mouthY)
	if t >= frogLick && t < frogGulp {
		// Out to the note, then back with it stuck on the end.
		reach := 1 - math.Abs(t-frogLick-frogReach)/frogReach
		reach = 1 - (1-reach)*(1-reach) // fast out, eases at full stretch
		tipX += (nx + 1 - tipX) * reach
		tipY += (ny + 4 - tipY) * reach
		if t >= frogLick+frogReach {
			nx, ny = tipX-1, tipY-4
		}
		line(c.pixel, float64(mouthX), float64(mouthY), tipX, tipY, frogTongue)
	}
	if t < frogGulp {
		col := mixHex(faint, text, clamp01((t-600)/300)*0.8)
		c.sprite(int(math.Round(nx)), int(math.Round(ny)), frogNote, map[byte]string{'X': col})
	}

	// The frog itself.
	c.sprite(x0, y0, sprite, map[byte]string{'G': accent, 'S': mixHex(accent, introDark, 0.35), 'D': introDark})

	// Each landing stamps a letter of the banner into place: it drops a
	// row, flashes white, and settles into color. The gulp sends a
	// highlight sweeping across it.
	left := (frogW - sptuiW) / 2
	for i := range frogBars {
		if age := t - frogLanding(i); age >= 0 {
			row := frogWord
			if age < 60 {
				row-- // still dropping in
			}
			col := mixHex(text, grad(float64(i)/(frogBars-1)), min(age/280, 1))
			if a := t - frogGulp; a >= 0 {
				// The highlight's centre crosses from left to right.
				centre := -4 + (sptuiW+8)*a/frogSweep
				d := math.Abs(float64(4*i) + 1.5 - centre)
				col = mixHex(col, text, 0.8*math.Max(0, 1-d/5))
			}
			c.sptuiLetter(left, row, i, col)
		}
	}

	// A plain greeting types itself out beneath, if we know a name.
	g, n := p.greeting(t, frogType)
	c.text((frogW-n)/2, frogHello, g, muted)

	return c
}
