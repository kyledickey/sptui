package art

import (
	"image"

	"golang.org/x/image/draw"
)

// Frame scales img to fit a w×h pixel box, keeping its shape, and centres it
// on a transparent canvas of exactly that size. Terminals differ in whether
// they scale an image to its cells or draw it at its own size; a framed
// image looks the same either way.
func Frame(img image.Image, w, h int) image.Image {
	src := img.Bounds()
	if w <= 0 || h <= 0 || src.Empty() {
		return img
	}
	// Fit: the largest size with the source's shape inside w×h.
	fw, fh := w, src.Dy()*w/src.Dx()
	if fh > h {
		fw, fh = src.Dx()*h/src.Dy(), h
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	x, y := (w-fw)/2, (h-fh)/2
	draw.CatmullRom.Scale(out, image.Rect(x, y, x+fw, y+fh), img, src, draw.Over, nil)
	return out
}
