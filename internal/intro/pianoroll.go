package intro

import (
	"math"
	"strings"
)

// pianoroll: a piano roll, Synthesia-style. Note blocks fall down a faint
// grid onto a chunky keyboard, and each key lights up as its note crosses
// the playhead. The phrase slows into a final chord, and the roll comes to
// rest with the next thing on it, SPTUI in big blocks, which flares as the
// chord lands. A plain greeting types out and it all fades away.

const (
	pianoW = 46 // scene size, in cells
	pianoH = 16

	// Timeline, in milliseconds from the start.
	pianoKeys   = 26   // between keys rising in, left to right
	pianoRise   = 120  // how long each key takes to rise
	pianoFirst  = 350  // song time of the first note
	pianoEighth = 95   // song time of an eighth note
	pianoSlow   = 500  // how long the roll takes to slow to a stop
	pianoType   = 1800 // the greeting starts typing
	pianoFade   = 2150 // everything fades out
	pianoEnd    = 2500

	// Geometry. Keys are 4 cells wide with a 1-cell gap; black keys are 3
	// cells wide, over the gap. Rows are in pixels, two per cell.
	pianoKeyX   = 1  // the first key's left cell
	pianoStride = 5  // cells from one white key to the next
	pianoWhites = 9  // C4 to D5
	pianoTop    = 14 // D5, in semitones above C4
	pianoHead   = 20 // pixel row of the playhead, the felt above the keys
	pianoWhiteB = 27 // bottom pixel row of the white keys
	pianoBlackB = 24 // and of the black ones
	pianoHello  = 15 // row of the greeting

	pianoPx     = 6.0                   // pixels per eighth on the roll
	pianoSpeed  = pianoPx / pianoEighth // pixels the roll moves per ms of song
	pianoLand   = 3.0                   // pixels the chord slides in before the roll stops
	pianoBanner = 2                     // pixels between the chord and the banner

	pianoIvory = "#d9dbe1"
	pianoEbony = "#1a1b21"
)

// pianoNote is a note in the roll: its pitch in semitones above C4, and
// when it starts and how long it lasts, in eighths.
type pianoNote struct {
	pitch      int
	at, length float64
}

// pianoSong is the phrase: a rising arpeggio, a bluesy turn and a lift,
// then a C major chord held to the end.
var pianoSong = []pianoNote{
	{0, 0, 1}, {4, 1, 1}, {7, 2, 1}, {9, 3, 2},
	{7, 5, 1}, {4, 6, 1}, {10, 7, 1}, {9, 8, 1},
	{12, 9, 1}, {14, 10, 2},
	{0, pianoChord, pianoHold}, {4, pianoChord, pianoHold}, {7, pianoChord, pianoHold}, {12, pianoChord, pianoHold},
}

// The final chord: when it starts, and how long its blocks are, in
// eighths. It's held to the end however long the blocks are.
const (
	pianoChord = 12
	pianoHold  = 8.0 / pianoPx
)

// pianoWhite is where each semitone of the octave sits: its white key, or
// for a black key the white key to its left.
var (
	pianoWhite = [12]int{0, 0, 1, 1, 2, 3, 3, 4, 4, 5, 5, 6}
	pianoBlack = [12]bool{1: true, 3: true, 6: true, 8: true, 10: true}
)

// pianoKey is the cells a pitch's key spans, and whether it's black.
func pianoKey(pitch int) (x0, x1 int, black bool) {
	oct, n := pitch/12, pitch%12
	x := pianoKeyX + (oct*7+pianoWhite[n])*pianoStride
	if pianoBlack[n] {
		return x + 3, x + 5, true
	}
	return x, x + 3, false
}

// pianoSongTime is how far through the song the roll is at t, in ms. It
// runs in time with the clock, then slows evenly to a stop just after the
// chord lands.
func pianoSongTime(t float64) float64 {
	stop := pianoFirst + pianoChord*pianoEighth + pianoLand/pianoSpeed
	slow := stop - pianoSlow/2 // where it starts slowing, in song and clock time
	if t <= slow {
		return t
	}
	f := min((t-slow)/pianoSlow, 1)
	return slow + pianoSlow/2*(1-(1-f)*(1-f))
}

// pianoClock is the clock time at which the song reaches u; the inverse
// of pianoSongTime.
func pianoClock(u float64) float64 {
	stop := pianoFirst + pianoChord*pianoEighth + pianoLand/pianoSpeed
	slow := stop - pianoSlow/2
	if u <= slow {
		return u
	}
	g := 1 - (u-slow)/(pianoSlow/2)
	return slow + pianoSlow*(1-math.Sqrt(max(g, 0)))
}

// pianoScene draws the piano roll intro as it is t milliseconds in.
func (p *Play) pianoScene(t float64) *canvas {
	c := newCanvas(pianoW, pianoH)
	accent, purple, text, muted, faint, bg := p.colors()
	right := pianoKeyX + pianoWhites*pianoStride - 2 // the last key's right cell
	grad := func(x int) string {
		return mixHex(accent, purple, float64(x-pianoKeyX)/float64(right-pianoKeyX))
	}

	u := pianoSongTime(t)
	chordAt := pianoClock(pianoFirst + pianoChord*pianoEighth)

	// The grid fades in: dotted lanes at each octave's E|F and B|C, and
	// beat lines that scroll with the song, stronger on the bar.
	in := min(t/250, 1)
	if t > chordAt {
		in = 1 - 0.5*min((t-chordAt)/300, 1) // it steps back for the banner
	}
	lane := mixHex(bg, mixHex(faint, bg, 0.35), in)
	beat := mixHex(bg, mixHex(faint, bg, 0.55), in)
	bar := mixHex(bg, faint, in)
	for q := -4; q < 12; q++ {
		// Quarter q crosses the playhead at song time pianoFirst + 2q eighths.
		y := pianoHead + int(math.Round((u-pianoFirst-float64(2*q)*pianoEighth)*pianoSpeed))
		row := y / 2
		if y < 0 || row >= pianoHead/2 {
			continue
		}
		s, col := "┈", beat
		if q%4 == 0 {
			s, col = "─", bar
		}
		c.text(pianoKeyX, row, strings.Repeat(s, right-pianoKeyX+1), col)
	}
	for k := 0; k < pianoWhites-1; k++ {
		if k%7 != 2 && k%7 != 6 {
			continue
		}
		x := pianoKeyX + k*pianoStride + 4
		for row := range pianoHead / 2 {
			c.text(x, row, "┊", lane)
		}
	}

	// Note blocks fall to the playhead and slide under it while they play.
	// The chord's blocks stay, resting where the roll stops.
	lit := map[int]float64{} // pitch → ms since it was struck
	for _, n := range pianoSong {
		start := pianoFirst + n.at*pianoEighth
		bottom := pianoHead + int(math.Round((u-start)*pianoSpeed))
		top := bottom - int(n.length*pianoPx) + 1 // a pixel's gap before the next
		x0, x1, _ := pianoKey(n.pitch)
		col := grad((x0 + x1) / 2)
		if u >= start && (n.at == pianoChord || u < start+n.length*pianoEighth) {
			lit[n.pitch] = t - pianoClock(start)
		}
		for y := max(top, 0); y < min(bottom, pianoHead); y++ {
			shade := col
			if y == top {
				shade = mixHex(col, text, 0.35) // a lit leading edge
			}
			for x := x0; x <= x1; x++ {
				c.pixel(x, y, shade)
			}
		}
	}

	// The banner rides the roll just above the chord, dim like a note yet
	// to play, and flares when the chord lands, left to right.
	chordBottom := pianoHead + int(math.Round((u-pianoFirst-pianoChord*pianoEighth)*pianoSpeed))
	bannerTop := chordBottom - int(pianoHold*pianoPx) + 1 - pianoBanner - 8
	font, w := sptuiPixels(2, 2, 2)
	bx := (pianoW - w) / 2
	for p := range font {
		x, y := bx+p[0], bannerTop+p[1]
		if y < 0 || y >= pianoHead {
			continue
		}
		col := grad(x)
		switch age := t - chordAt - float64(p[0])*4; {
		case age < 0:
			col = mixHex(col, bg, 0.6)
		case age < 320:
			col = mixHex(text, col, age/320)
		}
		c.pixel(x, y, col)
	}

	// The playhead: a strip of felt over the keys that lights where a
	// note is playing, with a flash as it's struck.
	felt := mixHex(mixHex(accent, bg, 0.72), faint, 0.2)
	for x := pianoKeyX; x <= right; x++ {
		c.pixel(x, pianoHead, mixHex(bg, felt, min(t/250, 1)))
	}

	// The keyboard rises in, key by key, white keys first so the black
	// ones sit on top.
	rise := func(k int) int { // pixels of key k showing
		return int(clamp01((t-float64(k)*pianoKeys)/pianoRise) * 7)
	}
	keyCol := func(pitch int, black bool, x int) (body, lip string) {
		body, lip = pianoIvory, mixHex(pianoIvory, "#000000", 0.28)
		if black {
			body, lip = pianoEbony, mixHex(pianoEbony, "#ffffff", 0.12)
		}
		if age, ok := lit[pitch]; ok {
			hot := grad(x)
			on := mixHex(text, hot, min(age/140, 1)) // struck: white hot, then its colour
			return on, mixHex(hot, "#000000", 0.35)
		}
		return body, lip
	}
	for pass := range 2 {
		for p := range pianoTop + 1 {
			x0, x1, black := pianoKey(p)
			if black != (pass == 1) || x1 > right {
				continue
			}
			k := (x0 - pianoKeyX) / pianoStride
			show := rise(k)
			body, lip := keyCol(p, black, (x0+x1)/2)
			bottom := pick(black, pianoBlackB, pianoWhiteB)
			for y := pianoHead + 1; y <= bottom; y++ {
				if y < pianoWhiteB+1-show {
					continue
				}
				for x := x0; x <= x1; x++ {
					c.pixel(x, y, pick(y == bottom, lip, body))
				}
			}
			if age, ok := lit[p]; ok {
				// The felt glows over a playing key, brightest as it's struck.
				glow := mixHex(text, grad(x0), min(age/140, 1))
				for x := x0; x <= x1; x++ {
					c.pixel(x, pianoHead, glow)
				}
			}
		}
	}

	// A plain greeting types itself out under the keys, if we know a name.
	g, n := p.greeting(t, pianoType)
	c.text((pianoW-n)/2, pianoHello, g, muted)

	return c
}
