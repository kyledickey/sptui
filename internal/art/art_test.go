package art

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func square(c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			img.Set(x, y, c)
		}
	}
	// Bottom half differs so we can see both halves of a block.
	for y := 20; y < 40; y++ {
		for x := range 40 {
			img.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	return img
}

func TestBlockArtSize(t *testing.T) {
	out := BlockArt(square(color.RGBA{255, 0, 0, 255}), 8, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 8 {
			t.Fatalf("line %d is %d wide", i, w)
		}
	}
	if !strings.Contains(lines[0], "38;2;255;0;0") || !strings.Contains(lines[3], "48;2;0;0;255") {
		t.Fatalf("colors missing: %q", lines[0])
	}
}

func TestKittyPlaceholder(t *testing.T) {
	out := KittyPlaceholder(42, 6, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 6 || !strings.HasPrefix(l, "\x1b[38;5;42m") {
			t.Fatalf("bad line %q", l)
		}
	}
}

func TestKittyTransmit(t *testing.T) {
	t.Setenv("TMUX", "")
	seq, err := KittyTransmit(42, square(color.White), 6, 3)
	if err != nil {
		t.Fatal(err)
	}
	first := seq[:strings.Index(seq, ";")]
	for _, want := range []string{"a=T", "i=42", "U=1", "c=6", "r=3", "f=100", "q=2"} {
		if !strings.Contains(first, want) {
			t.Errorf("first chunk %q lacks %s", first, want)
		}
	}
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Error("sequence not terminated")
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	seq, _ = KittyTransmit(42, square(color.White), 6, 3)
	if !strings.HasPrefix(seq, "\x1bPtmux;") {
		t.Errorf("inside tmux the sequence should use passthrough: %q", seq[:20])
	}
}

func TestDetect(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	cases := []struct {
		env  func(string) string
		want Mode
	}{
		{env("TERM", "xterm-kitty"), Kitty},
		{env("TERM_PROGRAM", "ghostty"), Kitty},
		{env("TERM", "xterm-256color"), Blocks},
		{env("TERM", "xterm-kitty", "TMUX", "x"), Blocks},
	}
	for i, c := range cases {
		if got := Detect(c.env); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestFrame(t *testing.T) {
	img := square(color.RGBA{255, 0, 0, 255}) // 40×40
	out := Frame(img, 300, 200)               // wider than tall
	if b := out.Bounds(); b.Dx() != 300 || b.Dy() != 200 {
		t.Fatalf("framed size %v", b)
	}
	// A 200×200 cover centred: 50px transparent margins left and right.
	if _, _, _, a := out.At(10, 100).RGBA(); a != 0 {
		t.Error("margin isn't transparent")
	}
	if r, _, _, a := out.At(150, 40).RGBA(); a == 0 || r < 0xf000 {
		t.Error("cover isn't in the middle")
	}
}
