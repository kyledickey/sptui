package demo

import (
	"context"
	"hash/fnv"
	"image"
	"image/color"
	"math"

	"github.com/kyledickey/sptui/internal/spotify"
)

// coverImages returns the image list for a demo item. The URL encodes a
// seed; CoverArt draws the picture.
func coverImages(seed string) []spotify.Image {
	return []spotify.Image{{URL: "demo:cover:" + seed, Width: 300, Height: 300}}
}

// CoverArt draws a made-up cover: a diagonal gradient with a sun and rings,
// coloured from the URL so each album looks different but stays the same.
func (b *Backend) CoverArt(ctx context.Context, url string) (image.Image, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	b.mu.Unlock()

	h := fnv.New32a()
	h.Write([]byte(url))
	seed := h.Sum32()
	hue := float64(seed%360) / 360
	from, to := hsl(hue, 0.65, 0.55), hsl(math.Mod(hue+0.12+float64(seed>>9%30)/100, 1), 0.7, 0.25)
	sun := hsl(math.Mod(hue+0.5, 1), 0.8, 0.7)
	cx, cy := 90+float64(seed>>3%120), 90+float64(seed>>5%120)

	const size = 300
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			t := float64(x+y) / (2 * size)
			c := mix(from, to, t)
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			switch {
			case d < 45:
				c = sun
			case int(d)%24 < 3:
				c = mix(c, sun, 0.35)
			}
			img.Set(x, y, c)
		}
	}
	return img, nil
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

// hsl converts hue, saturation and lightness (all 0–1) to a colour.
func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch int(h * 6) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}
