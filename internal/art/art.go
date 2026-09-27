// Package art draws cover images in the terminal: as real images with the
// kitty graphics protocol where the terminal supports it, otherwise as
// colored half-block "pixels" that work in any true-color terminal.
//
// Both render to plain cells, so they fit Bubble Tea's cell-based renderer:
// kitty images use Unicode placeholders, which the terminal replaces with the
// image, so they scroll, clip and redraw like text.
package art

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
)

// Mode is how covers are drawn.
type Mode int

const (
	Off    Mode = iota // no covers
	Blocks             // half-block pixels, works almost everywhere
	Kitty              // real images via the kitty graphics protocol
)

func (m Mode) String() string {
	switch m {
	case Blocks:
		return "blocks"
	case Kitty:
		return "kitty"
	}
	return "off"
}

// ParseMode reads a config value: "auto" (or ""), "kitty", "blocks" or "off".
// Auto detects the terminal.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "", "auto":
		return Detect(os.Getenv), nil
	case "kitty":
		return Kitty, nil
	case "blocks":
		return Blocks, nil
	case "off":
		return Off, nil
	}
	return Off, fmt.Errorf("unknown cover art mode %q (want auto, kitty, blocks or off)", s)
}

// Detect guesses the best mode from the environment. Kitty mode needs
// Unicode placeholder support, which kitty and Ghostty have. Inside tmux it
// also needs passthrough enabled, which can't be detected, so tmux gets
// blocks unless the user opts in.
func Detect(getenv func(string) string) Mode {
	if getenv("TMUX") != "" {
		return Blocks
	}
	switch {
	case getenv("KITTY_WINDOW_ID") != "",
		getenv("TERM") == "xterm-kitty",
		getenv("TERM") == "xterm-ghostty",
		getenv("TERM_PROGRAM") == "ghostty",
		getenv("GHOSTTY_RESOURCES_DIR") != "":
		return Kitty
	}
	return Blocks
}

// BlockArt renders img as cols×rows cells. Each cell is "▀" with the top
// pixel as foreground and the bottom one as background, so the image is
// cols×(2·rows) pixels, roughly square for a square cover when cols = 2·rows.
func BlockArt(img image.Image, cols, rows int) string {
	small := resize(img, cols, rows*2)
	var b strings.Builder
	for y := range rows {
		if y > 0 {
			b.WriteByte('\n')
		}
		for x := range cols {
			t, u := small[2*y][x], small[2*y+1][x]
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
		}
		b.WriteString("\x1b[m")
	}
	return b.String()
}

// resize scales img to w×h by averaging the source pixels under each target
// pixel, which keeps colors true when shrinking a lot.
func resize(img image.Image, w, h int) [][]color.RGBA {
	src := img.Bounds()
	out := make([][]color.RGBA, h)
	for y := range h {
		out[y] = make([]color.RGBA, w)
		y0 := src.Min.Y + y*src.Dy()/h
		y1 := max(y0+1, src.Min.Y+(y+1)*src.Dy()/h)
		for x := range w {
			x0 := src.Min.X + x*src.Dx()/w
			x1 := max(x0+1, src.Min.X+(x+1)*src.Dx()/w)
			var r, g, b, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, b, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), n+1
				}
			}
			out[y][x] = color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(b / n >> 8), 0xff}
		}
	}
	return out
}
