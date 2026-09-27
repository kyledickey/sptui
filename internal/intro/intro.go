// Package intro is sptui's startup animations. Each lives in its own file
// and draws a small canvas of cells and half-block pixels as it is t
// milliseconds in; Play renders one frame of it with the colors of an Env.
package intro

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/kyledickey/sptui/internal/banner"
)

// Frame is how often the animations are meant to be redrawn.
const Frame = 33 * time.Millisecond

const (
	introTypeCh = 35 // per character typed, for greetings
	introDark   = "#16181d"
)

var introNotes = []string{"♪", "♫", "♬", "♪"}

// Anim is one of the animations.
type Anim struct {
	Name      string
	Fade, End float64 // when it starts fading out, and ends, in ms
	scene     func(p *Play, t float64) *canvas
}

// All are the animations to choose from; the first is the default.
var All = []Anim{
	{Name: "vinyl", Fade: vinylFade, End: vinylEnd, scene: (*Play).vinylScene},
	{Name: "cat", Fade: catFade, End: catEnd, scene: (*Play).catScene},
	{Name: "frog", Fade: frogFade, End: frogEnd, scene: (*Play).frogScene},
	{Name: "boombox", Fade: boomboxFade, End: boomboxEnd, scene: (*Play).boomboxScene},
	{Name: "crt", Fade: crtFade, End: crtEnd, scene: (*Play).crtScene},
	{Name: "bios", Fade: biosFade, End: biosEnd, scene: (*Play).biosScene},
	{Name: "tape", Fade: tapeFade, End: tapeEnd, scene: (*Play).tapeScene},
	{Name: "matrix", Fade: matrixFade, End: matrixEnd, scene: (*Play).matrixScene},
	{Name: "pianoroll", Fade: pianoFade, End: pianoEnd, scene: (*Play).pianoScene},
}

// Names lists the animations by name.
func Names() []string {
	names := make([]string, len(All))
	for i, a := range All {
		names[i] = a.Name
	}
	return names
}

// Find is the animation called name, or false for names it doesn't know.
func Find(name string) (Anim, bool) {
	for _, a := range All {
		if a.Name == name {
			return a, true
		}
	}
	return Anim{}, false
}

// Env is what the animations draw with: the theme's colors as hex, and
// who to greet.
type Env struct {
	Accent, Purple, Text, Muted, Faint string
	BG                                 string // the terminal's background
	Dark                               bool
	Name                               string // the user's name; "" greets no one
}

// Play is one showing of an animation.
type Play struct {
	Anim  Anim
	Env   Env    // may change between frames, e.g. when the theme does
	hello string // the greeting, chosen when typing starts, so it doesn't change midway
}

func NewPlay(a Anim, env Env) *Play { return &Play{Anim: a, Env: env} }

// Reset forgets the greeting, for playing from the start again.
func (p *Play) Reset() { p.hello = "" }

// Faded is how far the animation has faded out t ms in, from 0 to 1.
func (p *Play) Faded(t float64) float64 {
	if t <= p.Anim.Fade {
		return 0
	}
	return min((t-p.Anim.Fade)/(p.Anim.End-p.Anim.Fade), 1)
}

// Frame draws the animation t ms in.
func (p *Play) Frame(t float64) string {
	return p.Anim.scene(p, t).render(p.Faded(t), p.Env.BG)
}

func (p *Play) colors() (accent, purple, text, muted, faint, bg string) {
	e := p.Env
	return e.Accent, e.Purple, e.Text, e.Muted, e.Faint, e.BG
}

// greeting is "hi, <first name>" typed out so far, having started at
// start, and n its full length for centring it. Both are empty until then
// or if we don't know a name.
func (p *Play) greeting(t, start float64) (typed string, n int) {
	if t < start {
		return "", 0
	}
	if p.hello == "" {
		if name, _, _ := strings.Cut(p.Env.Name, " "); name != "" {
			p.hello = "hi, " + strings.ToLower(name)
		}
	}
	g := []rune(p.hello)
	return string(g[:min(len(g), int((t-start)/introTypeCh)+1)]), len(g)
}

// sptui is the name in the banner font, two rows of half blocks. Its
// letters are 3 cells wide with a cell between.
var sptui, _ = banner.Spell("SPTUI")

const sptuiW = 19 // cells

// sptuiLetter draws letter i of sptui, for the word starting at (x, y).
func (c *canvas) sptuiLetter(x, y, i int, hex string) {
	for row := range 2 {
		c.text(x+4*i, y+row, string([]rune(sptui[row])[4*i:4*i+3]), hex)
	}
}

// sptuiPixels spells SPTUI in the banner font, each font pixel sx×sy and
// the letters gap apart: the lit pixels, and how wide it is.
func sptuiPixels(sx, sy, gap int) (lit map[[2]int]bool, w int) {
	lit = map[[2]int]bool{}
	for i, r := range "SPTUI" {
		left := i * (3*sx + gap)
		for fy, row := range banner.Font[r] {
			for fx := range len(row) {
				if row[fx] != 'X' {
					continue
				}
				for dy := range sy {
					for dx := range sx {
						lit[[2]int{left + fx*sx + dx, fy*sy + dy}] = true
					}
				}
			}
		}
	}
	return lit, 15*sx + 4*gap
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func clamp01(f float64) float64 { return min(max(f, 0), 1) }

// smoothstep eases f in [0, 1] in and out.
func smoothstep(f float64) float64 {
	f = clamp01(f)
	return f * f * (3 - 2*f)
}

// easeOut eases f in [0, 1] out with a power curve: fast, then gentle.
func easeOut(f, p float64) float64 { return 1 - math.Pow(1-clamp01(f), p) }

// line draws a line of pixels from (x0, y0) to (x1, y1) with px.
func line(px func(x, y int, hex string), x0, y0, x1, y1 float64, hex string) {
	n := int(max(math.Abs(x1-x0), math.Abs(y1-y0))*2) + 1
	for i := 0; i <= n; i++ {
		f := float64(i) / float64(n)
		px(int(math.Round(x0+(x1-x0)*f)), int(math.Round(y0+(y1-y0)*f)), hex)
	}
}

// canvas is a small grid of colored cells, with a layer of half-block
// pixels (two per cell, stacked) drawn on top.
type canvas struct {
	w, h  int
	cells [][]canvasCell
	px    [][]string // color per pixel, "" for none
}

type canvasCell struct{ s, fg string }

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([][]canvasCell, h), px: make([][]string, h*2)}
	for i := range c.cells {
		c.cells[i] = make([]canvasCell, w)
	}
	for i := range c.px {
		c.px[i] = make([]string, w)
	}
	return c
}

// text writes s from (x, y), one rune per cell; spaces leave cells alone.
func (c *canvas) text(x, y int, s, fg string) {
	if y < 0 || y >= c.h {
		return
	}
	for i, r := range []rune(s) {
		if x+i >= 0 && x+i < c.w && r != ' ' {
			c.cells[y][x+i] = canvasCell{string(r), fg}
		}
	}
}

func (c *canvas) pixel(x, y int, hex string) {
	if x >= 0 && x < c.w && y >= 0 && y < c.h*2 {
		c.px[y][x] = hex
	}
}

// sprite draws rows of pixels from (x, y), each byte colored by colors;
// bytes it doesn't have are left alone.
func (c *canvas) sprite(x, y int, rows []string, colors map[byte]string) {
	for dy, row := range rows {
		for dx := range len(row) {
			if hex, ok := colors[row[dx]]; ok {
				c.pixel(x+dx, y+dy, hex)
			}
		}
	}
}

// render draws the canvas with every color faded towards bg by fade.
func (c *canvas) render(fade float64, bg string) string {
	base := lipgloss.NewStyle()
	col := func(hex string) color.Color { return lipgloss.Color(mixHex(hex, bg, fade)) }
	lines := make([]string, c.h)
	for y := range c.h {
		var b strings.Builder
		for x := range c.w {
			top, bottom := c.px[2*y][x], c.px[2*y+1][x]
			switch {
			case top != "" && bottom != "":
				b.WriteString(base.Foreground(col(top)).Background(col(bottom)).Render("▀"))
			case top != "":
				b.WriteString(base.Foreground(col(top)).Render("▀"))
			case bottom != "":
				b.WriteString(base.Foreground(col(bottom)).Render("▄"))
			case c.cells[y][x].s != "":
				b.WriteString(base.Foreground(col(c.cells[y][x].fg)).Render(c.cells[y][x].s))
			default:
				b.WriteByte(' ')
			}
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// Grid is one frame as data rather than terminal text, for drawing it
// somewhere else (the website). Colors are unfaded; Fade is how far to
// blend them all towards the background.
type Grid struct {
	W, H   int
	Fade   float64
	Pixels [][]string // 2H rows of W, color or ""
	Cells  [][]Cell   // H rows of W, under the pixels
}

// Cell is a character drawn in a cell, or "" for none.
type Cell struct{ Char, Color string }

// Grid draws the animation t ms in, as data.
func (p *Play) Grid(t float64) Grid {
	c := p.Anim.scene(p, t)
	g := Grid{W: c.w, H: c.h, Fade: p.Faded(t), Pixels: c.px, Cells: make([][]Cell, c.h)}
	for y, row := range c.cells {
		g.Cells[y] = make([]Cell, c.w)
		for x, cell := range row {
			g.Cells[y][x] = Cell{cell.s, cell.fg}
		}
	}
	return g
}

// mixHex blends color a towards b by f in [0, 1].
func mixHex(a, b string, f float64) string {
	ar, ag, ab, ok1 := parseHex(a)
	br, bg, bb, ok2 := parseHex(b)
	if !ok1 || !ok2 || f <= 0 {
		return a
	}
	return hexOf(ar+(br-ar)*f, ag+(bg-ag)*f, ab+(bb-ab)*f)
}

// parseHex reads a color like "#1ed760" as red, green and blue in 0–1.
func parseHex(hex string) (r, g, b float64, ok bool) {
	var ri, gi, bi int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &ri, &gi, &bi); err != nil {
		return 0, 0, 0, false
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255, true
}

// hexOf writes red, green and blue in 0–1 as a color like "#1ed760".
func hexOf(r, g, b float64) string {
	c := func(v float64) int { return int(math.Round(min(max(v, 0), 1) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", c(r), c(g), c(b))
}
