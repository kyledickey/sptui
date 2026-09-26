package intro

import "math"

// cat: a sleek cat lies curled on a speaker. The speaker starts to thump
// and its ears flick in time, until it lifts its head, stretches out long,
// sits up and swats a note clean across the screen. The note's streak
// strikes the letters of sptui into place as it passes.

const (
	catW = 44 // scene size, in cells
	catH = 14

	// Timeline, in milliseconds from the start.
	catThump   = 600  // the speaker's first thump
	catBeat    = 210  // between thumps
	catWake    = 1040 // head up, eyes open
	catStretch = 1200 // stretches out long
	catSit     = 1560 // sits up; the note floats over
	catWindup  = 1720 // paw goes up
	catBat     = 1840 // swipe: the note flies off
	catFly     = 320  // the note's flight across the screen
	catType    = 1980 // the greeting starts typing
	catFade    = 2300 // everything fades out
	catEnd     = 2600

	catBase  = 15 // pixel row the cat sits on: the speaker's top
	catSpkX  = 2  // speaker's left edge
	catWordX = 24 // left edge of the banner
	catWordY = 6  // top row of the banner
	catHi    = 9  // row of the greeting
)

// The speaker, 16×12 pixels. F frame, D cabinet, t tweeter, r cone
// ring, R cone, c dust cap.
var catSpeaker = []string{
	".FFFFFFFFFFFFFF.",
	"FDDDDDDDDDDDDDDF",
	"FDDDDDDttDDDDDDF",
	"FDDDDDDDDDDDDDDF",
	"FDDDDDrrrrDDDDDF",
	"FDDDrrRRRRrrDDDF",
	"FDDrRRRRRRRRrDDF",
	"FDDrRRRccRRRrDDF",
	"FDDrRRRccRRRrDDF",
	"FDDrRRRRRRRRrDDF",
	"FDDDrrRRRRrrDDDF",
	"FDDDDDrrrrDDDDDF",
}

// The cat's head, 7×7 pixels, a plain silhouette tapering to the chin.
// Its eyes, when open, are cut out of it.
var catHead = []string{
	"B.....B",
	"BB...BB",
	"BBBBBBB",
	"BBBBBBB",
	"BBBBBBB",
	".BBBBB.",
	"..BBB..",
}

// Bodies, drawn under the head. D is the shaded far side.
var (
	// Curled up, head on the right.
	catLoaf = []string{
		"....BBBBBBBB",
		"..BBBBBBBBBB",
		".DBBBBBBBBBB",
		"DDBBBBBBBBBB",
		"DDBBBBBBBBBB",
	}
	// Rear up, chest down, front legs reaching right.
	catLong = []string{
		"..BBBB..............",
		".BBBBBB.............",
		".BBBBBBBB...........",
		".BBBBBBBBBB.........",
		".BBBBBBBBBBB........",
		".BD..BBBBBBBB.......",
		".BD.....BBBBBBBBBBBB",
		".BD.......BBBBBBBBBB",
	}
	// Sitting up, facing out.
	catSitting = []string{
		"..BBBBBB.",
		".DBBBBBBB",
		"DDBBBBBBB",
		"DDBBBBBBB",
		"DDBBBBBBB",
		"DDBBBBBBB",
		"DDBBBBBBB",
		".BBBBBBB.",
	}
)

// catScene draws the cat intro as it is t milliseconds in.
func (p *Play) catScene(t float64) *canvas {
	c := newCanvas(catW, catH)
	accent, purple, text, muted, faint, bg := p.colors()

	// Beats: which thump we're on, and how far into it (0 just hit, 1 next).
	beat, into := -1, 1.0
	if t >= catThump {
		b, f := math.Modf((t - catThump) / catBeat)
		beat, into = int(b), f
	}
	hit := beat >= 0 && t < catFade && into < 0.3 // the cone is out

	// Speaker: the cone flashes the accent on every thump and fades back.
	flash := 0.0
	if beat >= 0 && t < catFade {
		flash = math.Exp(-4 * into)
	}
	spk := map[byte]string{
		'F': faint,
		'D': mixHex(faint, bg, 0.6),
		't': muted,
		'r': mixHex(mixHex(faint, bg, 0.2), accent, flash),
		'R': mixHex(mixHex(faint, bg, 0.4), accent, flash*0.45),
		'c': mixHex(muted, text, flash),
	}
	if hit {
		spk['r'] = spk['c'] // the cone pushes out: the cap looks bigger
	}
	c.sprite(catSpkX, catBase, catSpeaker, spk)

	// Sound rings off the right side of the speaker on every thump.
	if beat >= 0 && t < catFade && into < 0.6 {
		x := catSpkX + 17 + int(into*5)
		col := mixHex(accent, bg, into/0.6)
		for _, dy := range []int{-1, 0, 1} {
			c.text(x+pick(dy == 0, 1, 0), 10+dy, ")", col)
		}
	}

	fur := accent
	colors := map[byte]string{'B': fur, 'D': mixHex(fur, bg, 0.3)}
	draw := func(sprite []string, x, y int) { c.sprite(x, y, sprite, colors) }

	// head draws the head with its top-left at (x, y). look is -1 with its
	// eyes shut, else how far right its eyes are looking. A flicking ear
	// (twitch -1 or 1) folds outwards for a moment.
	head := func(x, y, look, twitch int) {
		draw(catHead, x, y)
		if look >= 0 {
			c.pixel(x+1+look, y+3, bg)
			c.pixel(x+5+look, y+3, bg)
		}
		switch twitch {
		case -1:
			c.pixel(x, y, "")
			c.pixel(x-1, y+1, fur)
		case 1:
			c.pixel(x+6, y, "")
			c.pixel(x+7, y+1, fur)
		}
	}

	// Tail: hangs off the back of the speaker and sways.
	tail := func(x, y, n int, swing float64) {
		for i := range n {
			f := float64(i) / float64(max(n-1, 1))
			dx := int(math.Round(swing * f * f))
			c.pixel(x+dx, y+i, fur)
		}
	}

	// Ears flick on each thump while it lies still, left and right in turn.
	twitch := 0
	if beat >= 0 && t < catWake && into < 0.45 {
		twitch = pick(beat%2 == 0, 1, -1)
	}

	switch {
	case t < catStretch:
		// Curled up, breathing slowly; then the head comes up.
		x := catSpkX + 1
		body := catLoaf
		if math.Sin(t/700*2*math.Pi) < 0 {
			body = append([]string{"............"}, catLoaf[1:]...)
		}
		tail(x, catBase-2, 5, 1.5*math.Sin(t/500)-1)
		draw(body, x, catBase-len(catLoaf))
		hy, look := catBase-7, -1
		if t >= catWake {
			hy, look = hy-1, 0
		}
		head(x+10, hy, look, twitch)
		c.pixel(x+11, catBase-1, fur) // front paws
		c.pixel(x+15, catBase-1, fur)
	case t < catSit:
		// Stretched out long, front legs reaching.
		x := catSpkX - 1
		tail(x+1, catBase-8, 1, 0)
		c.pixel(x, catBase-10, fur)
		c.pixel(x, catBase-11, fur)
		c.pixel(x+1, catBase-9, fur)
		draw(catLong, x, catBase-len(catLong))
		head(x+11, catBase-9, 0, 0)
	default:
		// Sitting up, watching the note, then batting it away.
		x := catSpkX + 6
		y := catBase - len(catSitting)
		// Tail curls up round the left side, its tip flicking.
		for _, p := range [][2]int{{-1, 7}, {-2, 6}, {-2, 5}, {-2, 4}} {
			c.pixel(x+p[0], y+p[1], fur)
		}
		c.pixel(x-2+int(math.Round(math.Sin(t/120))), y+3, fur)
		draw(catSitting, x, y)
		hy := y - 6
		if hit {
			hy++ // bobbing to the beat
		}
		head(x+2, hy, 1, 0) // eyes on the note
		switch {
		case t >= catBat && t < catBat+200:
			// Swipe: paw straight out.
			for i := range 5 {
				c.pixel(x+9+i, y+1, fur)
			}
			c.pixel(x+13, y, fur)
		case t >= catWindup && t < catBat:
			// Paw up, ready.
			c.pixel(x+9, y+2, fur)
			c.pixel(x+10, y+1, fur)
			c.pixel(x+10, y, fur)
			c.pixel(x+10, y-1, fur)
		}
	}

	// The note floats up out of the speaker, bobs in front of the cat,
	// and gets swatted off the right-hand side, leaving a streak.
	hx, hy := float64(catSpkX+20), 3.0
	if t >= catSit-120 {
		var nx, ny float64
		switch {
		case t < catSit+80:
			f := (t - (catSit - 120)) / 200
			nx, ny = hx-2+2*f, 10-(10-hy)*(1-(1-f)*(1-f))
		case t < catBat:
			nx, ny = hx, hy+0.6*math.Sin((t-catSit)/90)
		default:
			// Flat and fast, quickest off the paw.
			f := min((t-catBat)/catFly, 1)
			u := 1 - f
			nx, ny = hx+(1-0.6*u-0.4*u*u)*catReach(hx), hy
			// Streak: a line from the paw to the note, fading behind it
			// and then all at once once the note has gone.
			gone := max((t-catBat-catFly)/160, 0)
			for sx := int(hx) + 1; sx < min(int(math.Round(nx)), catW); sx++ {
				if age := (nx - float64(sx)) / 14; age < 1 && gone < 1 {
					c.text(sx, int(hy), "─", mixHex(muted, bg, max(age, gone)))
				}
			}
		}
		note := introNotes[0]
		if t >= catBat {
			note = introNotes[int((t-catBat)/60)%len(introNotes)]
		}
		c.text(int(math.Round(nx)), int(math.Round(ny)), note, mixHex(accent, purple, 0.3))
	}

	// Each letter drops in as the note passes over it, flashes white, and
	// settles into the accent-to-artist gradient.
	for i := range 5 {
		if age := t - catPassing(hx, float64(catWordX+4*i+1)); age >= 0 {
			row := catWordY
			if age < 70 {
				row-- // dropping in
			}
			col := mixHex(text, mixHex(accent, purple, float64(i)/4), min(age/220, 1))
			c.sptuiLetter(catWordX, row, i, col)
		}
	}

	// The greeting types itself out under the banner.
	g, _ := p.greeting(t, catType)
	c.text(catWordX, catHi, g, muted)

	return c
}

// catReach is how far the swatted note flies from hx: clean off the screen.
func catReach(hx float64) float64 { return catW + 4 - hx }

// catPassing is when the swatted note, flying from hx, passes column x.
func catPassing(hx, x float64) float64 {
	e := (x - hx) / catReach(hx) // fraction of the way along
	u := (-0.6 + math.Sqrt(0.36+1.6*(1-e))) / 0.8
	return catBat + (1-u)*catFly
}
